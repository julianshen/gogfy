// Package centrality implements graph-centrality measures used by
// analyze to surface bridge nodes (high-betweenness nodes that
// connect otherwise-distant parts of the graph).
//
// Algorithm: Brandes' (Linear-Time Computation of Betweenness
// Centrality, 2001). Treats the graph as undirected and unweighted —
// matches graphify's networkx default and the user-facing
// "bridge node" intuition.
package centrality

import (
	"runtime"
	"sync"

	"github.com/julianshen/gogfy/internal/schema"
)

// Betweenness returns the un-normalized betweenness centrality for
// each node. Higher score → more shortest paths pass through this
// node. Isolated nodes and graph endpoints return 0.
//
// Self-loops and duplicate edges between the same pair are
// deduplicated so a multigraph encoding doesn't inflate scores.
// Dangling edges (target not in nodes) are silently skipped.
//
// Complexity: O(V·E). Brandes' per-source passes are independent, so
// the source loop is fanned out across GOMAXPROCS workers (each with
// its own scratch + accumulator, reduced at the end). All state is
// indexed by dense int node IDs rather than string-keyed maps, which
// removes the per-source map allocation + string-hash cost that
// dominated the original implementation on graphs of a few thousand
// nodes. The result is identical to a serial run (floating-point
// reduction order across workers can differ in the lowest bits, which
// does not affect the top-k bridge ranking the result feeds).
func Betweenness(nodes []schema.Node, edges []schema.Edge) map[string]float64 {
	if len(nodes) == 0 {
		return map[string]float64{}
	}

	// Dense int index over UNIQUE node IDs (matches the original
	// ID-keyed semantics: repeated IDs collapse to one vertex).
	idx := make(map[string]int, len(nodes))
	ids := make([]string, 0, len(nodes))
	for _, n := range nodes {
		if _, ok := idx[n.ID]; !ok {
			idx[n.ID] = len(ids)
			ids = append(ids, n.ID)
		}
	}
	m := len(ids)
	adj := buildAdjInt(edges, idx)

	cb := brandesParallel(adj)

	// Undirected graphs double-count each pair (once from each source),
	// so halve to match the standard definition.
	out := make(map[string]float64, m)
	for i, id := range ids {
		out[id] = cb[i] / 2
	}
	return out
}

// brandesParallel runs Brandes' algorithm with the source loop split
// across workers. Each worker accumulates into a private []float64 to
// avoid contention; the partials are summed at the end.
func brandesParallel(adj [][]int32) []float64 {
	m := len(adj)
	workers := runtime.GOMAXPROCS(0)
	if workers > m {
		workers = m
	}
	if workers < 1 {
		workers = 1
	}

	partials := make([][]float64, workers)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			local := make([]float64, m)
			sc := newScratch(m)
			// Strided source assignment balances load across workers.
			for s := w; s < m; s += workers {
				brandesSource(int32(s), adj, local, sc)
			}
			partials[w] = local
		}(w)
	}
	wg.Wait()

	cb := make([]float64, m)
	for _, p := range partials {
		for i, v := range p {
			cb[i] += v
		}
	}
	return cb
}

// scratch holds the per-source working buffers, reused across the
// sources a single worker handles so the inner loop allocates nothing.
type scratch struct {
	dist  []int32
	sigma []float64
	delta []float64
	pred  [][]int32
	stack []int32
	queue []int32
}

func newScratch(m int) *scratch {
	return &scratch{
		dist:  make([]int32, m),
		sigma: make([]float64, m),
		delta: make([]float64, m),
		pred:  make([][]int32, m),
		stack: make([]int32, 0, m),
		queue: make([]int32, 0, m),
	}
}

// brandesSource runs one single-source shortest-path pass from s and
// adds its dependency contributions into cb.
func brandesSource(s int32, adj [][]int32, cb []float64, sc *scratch) {
	m := len(adj)
	for i := 0; i < m; i++ {
		sc.dist[i] = -1
		sc.sigma[i] = 0
		sc.delta[i] = 0
	}
	sc.stack = sc.stack[:0]
	sc.queue = sc.queue[:0]

	sc.sigma[s] = 1
	sc.dist[s] = 0
	sc.queue = append(sc.queue, s)

	// BFS over the unweighted graph; queue is consumed via a head index
	// (no front-reslice) and visit order is recorded on the stack.
	for h := 0; h < len(sc.queue); h++ {
		v := sc.queue[h]
		sc.stack = append(sc.stack, v)
		dv := sc.dist[v]
		sv := sc.sigma[v]
		for _, w := range adj[v] {
			if sc.dist[w] < 0 {
				sc.dist[w] = dv + 1
				sc.queue = append(sc.queue, w)
			}
			if sc.dist[w] == dv+1 {
				sc.sigma[w] += sv
				sc.pred[w] = append(sc.pred[w], v)
			}
		}
	}

	// Reverse-BFS dependency accumulation. Each predecessor v of w
	// inherits its share sigma[v]/sigma[w] of the through-paths reaching
	// w, plus the +1 for the (s, w) pair itself.
	for i := len(sc.stack) - 1; i >= 0; i-- {
		w := sc.stack[i]
		coeff := (1 + sc.delta[w]) / sc.sigma[w]
		for _, v := range sc.pred[w] {
			sc.delta[v] += sc.sigma[v] * coeff
		}
		if w != s {
			cb[w] += sc.delta[w]
		}
		// Reset this vertex's predecessor list for the next source,
		// reusing the backing array.
		sc.pred[w] = sc.pred[w][:0]
	}
}

// buildAdjInt returns an undirected adjacency list (int-indexed) with
// duplicate pairs and self-loops removed. Edges whose endpoints aren't
// in idx are skipped.
func buildAdjInt(edges []schema.Edge, idx map[string]int) [][]int32 {
	adj := make([][]int32, len(idx))
	type pair struct{ a, b int32 }
	seen := make(map[pair]struct{}, len(edges))
	for _, e := range edges {
		si, ok := idx[e.Source]
		if !ok {
			continue
		}
		ti, ok := idx[e.Target]
		if !ok || si == ti {
			continue
		}
		a, b := int32(si), int32(ti)
		if a > b {
			a, b = b, a
		}
		k := pair{a, b}
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		adj[si] = append(adj[si], int32(ti))
		adj[ti] = append(adj[ti], int32(si))
	}
	return adj
}

// PageRank computes the (optionally personalized) PageRank of each node
// over the DIRECTED graph: an edge source → target flows rank to the
// target, so heavily-referenced definitions (callees, imported packages,
// implemented interfaces) accumulate the most rank. This is the ranking
// signal behind a "repo map" — the project's most important symbols.
//
// personalization maps node IDs to non-negative teleport weights. A
// nil/empty map (or one whose weights sum to zero over known nodes)
// yields uniform teleport = standard global PageRank. A non-empty map
// biases the random surfer toward those seed nodes, so the ranking
// reflects "what matters relative to where the agent is looking" — this
// is the Personalized-PageRank trick from Aider's repo map.
//
// Damping is 0.85. Dangling nodes (no out-edges) redistribute their rank
// through the teleport vector each iteration so total rank is conserved.
// Iterates until the L1 delta falls below 1e-8 or 100 iterations.
func PageRank(nodes []schema.Node, edges []schema.Edge, personalization map[string]float64) map[string]float64 {
	const (
		damping = 0.85
		maxIter = 100
		epsilon = 1e-8
	)
	if len(nodes) == 0 {
		return map[string]float64{}
	}

	// Dense int index over unique node IDs (matches Betweenness semantics).
	idx := make(map[string]int, len(nodes))
	ids := make([]string, 0, len(nodes))
	for _, n := range nodes {
		if _, ok := idx[n.ID]; !ok {
			idx[n.ID] = len(ids)
			ids = append(ids, n.ID)
		}
	}
	m := len(ids)

	// Directed adjacency + out-degree. Self-loops and dangling endpoints
	// are skipped so they don't distort the flow.
	outAdj := make([][]int32, m)
	outDeg := make([]int, m)
	for _, e := range edges {
		si, ok := idx[e.Source]
		if !ok {
			continue
		}
		ti, ok := idx[e.Target]
		if !ok || si == ti {
			continue
		}
		outAdj[si] = append(outAdj[si], int32(ti))
		outDeg[si]++
	}

	// Teleport vector p. Normalize the personalization weights over known
	// nodes; fall back to uniform when nothing usable was supplied.
	p := make([]float64, m)
	var sum float64
	for id, w := range personalization {
		if w <= 0 {
			continue
		}
		if i, ok := idx[id]; ok {
			p[i] += w
			sum += w
		}
	}
	if sum == 0 {
		uniform := 1.0 / float64(m)
		for i := range p {
			p[i] = uniform
		}
	} else {
		for i := range p {
			p[i] /= sum
		}
	}

	// Power iteration. Initialize at the teleport distribution.
	r := make([]float64, m)
	copy(r, p)
	next := make([]float64, m)
	for iter := 0; iter < maxIter; iter++ {
		// Dangling mass (rank stuck on out-degree-0 nodes) teleports out.
		var dangling float64
		for i := 0; i < m; i++ {
			if outDeg[i] == 0 {
				dangling += r[i]
			}
		}
		base := (1 - damping) // teleport-to-p contribution multiplier
		for i := 0; i < m; i++ {
			next[i] = base*p[i] + damping*dangling*p[i]
		}
		for i := 0; i < m; i++ {
			if outDeg[i] == 0 {
				continue
			}
			share := damping * r[i] / float64(outDeg[i])
			for _, t := range outAdj[i] {
				next[t] += share
			}
		}
		var delta float64
		for i := 0; i < m; i++ {
			d := next[i] - r[i]
			if d < 0 {
				d = -d
			}
			delta += d
		}
		r, next = next, r
		if delta < epsilon {
			break
		}
	}

	out := make(map[string]float64, m)
	for i, id := range ids {
		out[id] = r[i]
	}
	return out
}
