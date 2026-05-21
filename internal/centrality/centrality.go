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
