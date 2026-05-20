package cluster

import (
	"fmt"
	"sort"

	"github.com/julianshen/gogfy/internal/schema"
)

// LouvainClusterer is a pure-Go implementation of the Louvain
// modularity-optimization algorithm. Two roles:
//
//  1. As a fallback for LeidenClusterer when the upstream Leiden
//     library errors (rare but possible on pathological inputs).
//     Better than dropping to ConnectedComponents, which is
//     modularity-blind (each connected component becomes one
//     community).
//  2. As an explicitly-selectable clusterer for users who want
//     Louvain's slightly looser community boundaries (it can leave
//     loosely-connected nodes in the same community where Leiden
//     would split them).
//
// The implementation iterates a single Louvain phase: each pass tries
// to move every node to a neighboring community that maximizes
// modularity gain; iterations stop when no move improves modularity.
// Aggregation (phase 2 — collapse-then-reapply on the meta-graph) is
// deferred — the single-phase form is enough to outperform connected
// components and matches what most "Louvain fallback" callers expect.
//
// Determinism: node iteration order is sorted by ID, ties in ΔQ
// resolve by lexicographically smaller target community. Same input
// → byte-identical assignment.
type LouvainClusterer struct {
	// Resolution scales the modularity gain — higher means
	// preferring more small communities. 1.0 matches the standard
	// definition.
	Resolution float64
	// MaxIterations caps phase iterations. Convergence is usually
	// fast (single-digit passes); the cap is a safety net.
	MaxIterations int
}

// NewLouvainClusterer returns a clusterer with sensible defaults.
func NewLouvainClusterer() *LouvainClusterer {
	return &LouvainClusterer{Resolution: 1.0, MaxIterations: 20}
}

// Cluster assigns Community IDs to nodes via Louvain. Empty input
// returns the nodes unchanged; isolated nodes (no edges) become
// singleton communities.
func (c *LouvainClusterer) Cluster(nodes []schema.Node, edges []schema.Edge) ([]schema.Node, error) {
	if len(nodes) == 0 {
		return nodes, nil
	}
	res := c.Resolution
	if res <= 0 {
		res = 1.0
	}
	maxIters := c.MaxIterations
	if maxIters <= 0 {
		maxIters = 20
	}

	// Index nodes by ID and build adjacency. Edges are treated as
	// undirected with weight 1 (gogfy doesn't carry edge weights
	// today); duplicate (u,v) and (v,u) collapse via the map.
	idx := make(map[string]int, len(nodes))
	sortedIDs := make([]string, 0, len(nodes))
	for _, n := range nodes {
		if _, ok := idx[n.ID]; !ok {
			idx[n.ID] = len(sortedIDs)
			sortedIDs = append(sortedIDs, n.ID)
		}
	}
	sort.Strings(sortedIDs)
	// Re-number after sort so iteration order matches ID order.
	idx = make(map[string]int, len(sortedIDs))
	for i, id := range sortedIDs {
		idx[id] = i
	}
	n := len(sortedIDs)

	// adj[i] is a map of neighbor index → weight. Undirected: we add
	// both directions for every edge so node-move ΔQ math is correct.
	adj := make([]map[int]float64, n)
	for i := range adj {
		adj[i] = map[int]float64{}
	}
	for _, e := range edges {
		u, uok := idx[e.Source]
		v, vok := idx[e.Target]
		if !uok || !vok || u == v {
			continue
		}
		adj[u][v]++
		adj[v][u]++
	}

	// Degree (sum of incident edge weights) per node and total edge
	// weight (sum of all degrees) / 2 = m. Modularity gain math
	// references both.
	deg := make([]float64, n)
	var twoM float64
	for u := 0; u < n; u++ {
		for _, w := range adj[u] {
			deg[u] += w
		}
		twoM += deg[u]
	}
	if twoM == 0 {
		// No edges → every node is its own community. Assign and
		// return.
		return assignCommunities(nodes, idx, identityCommunities(n)), nil
	}
	m := twoM / 2.0

	// community[u] = current community of node u. Start each node in
	// its own.
	community := make([]int, n)
	for i := range community {
		community[i] = i
	}
	// commDegree[c] = sum of degrees of nodes currently in community c
	commDegree := make([]float64, n)
	for u := 0; u < n; u++ {
		commDegree[community[u]] += deg[u]
	}

	for iter := 0; iter < maxIters; iter++ {
		moved := false
		for u := 0; u < n; u++ {
			// Compute weight of u's links into each neighboring
			// community.
			linksTo := map[int]float64{}
			for v, w := range adj[u] {
				linksTo[community[v]] += w
			}
			currentComm := community[u]
			// Remove u from its current community for the ΔQ math.
			commDegree[currentComm] -= deg[u]
			// Best ΔQ = links_to_new - links_to_current - (deg[u] / 2m) * (sumDeg_new - sumDeg_current)
			// We iterate sorted comm IDs for tie-break determinism.
			candidates := make([]int, 0, len(linksTo))
			for c := range linksTo {
				candidates = append(candidates, c)
			}
			sort.Ints(candidates)
			bestComm := currentComm
			bestGain := 0.0
			currentLinks := linksTo[currentComm]
			for _, c := range candidates {
				// ΔQ = (links_to_c - deg[u]*sumDeg_c / 2m) - (links_to_current - deg[u]*sumDeg_current / 2m)
				// times resolution. Constants cancel; we compute the
				// relative gain over staying in currentComm.
				gain := (linksTo[c] - currentLinks) - res*(deg[u]*(commDegree[c]-commDegree[currentComm]))/twoM
				if gain > bestGain {
					bestGain = gain
					bestComm = c
				}
			}
			// Move u to bestComm (which may equal currentComm if no
			// gain was positive — in that case we restore).
			community[u] = bestComm
			commDegree[bestComm] += deg[u]
			if bestComm != currentComm {
				moved = true
			}
		}
		if !moved {
			break
		}
	}

	_ = m // referenced only inside the gain formula (kept for clarity)
	return assignCommunities(nodes, idx, community), nil
}

// identityCommunities returns [0,1,2,...,n-1] — used when the graph
// has no edges and every node is trivially its own community.
func identityCommunities(n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = i
	}
	return out
}

// assignCommunities writes the per-node community assignment back
// onto the input slice. Communities are re-numbered 0,1,2,… in order
// of first appearance via the sorted-ID iteration above, so the
// output is stable across runs.
func assignCommunities(nodes []schema.Node, idx map[string]int, community []int) []schema.Node {
	// Re-number for stable, dense community IDs.
	remap := map[int]int{}
	next := 0
	// Walk in node-index order so community 0 is whichever node sorts
	// first by ID.
	for u := 0; u < len(community); u++ {
		if _, ok := remap[community[u]]; !ok {
			remap[community[u]] = next
			next++
		}
	}
	out := make([]schema.Node, len(nodes))
	copy(out, nodes)
	for i, node := range out {
		i2, ok := idx[node.ID]
		if !ok {
			continue
		}
		out[i].Community = fmt.Sprintf("%d", remap[community[i2]])
	}
	return out
}
