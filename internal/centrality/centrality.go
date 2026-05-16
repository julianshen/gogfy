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
// Complexity: O(V·E). For graphs >1000 nodes consider sampling
// (graphify uses networkx's k= parameter for that); not yet exposed
// here since gogfy's current targets are smaller.
func Betweenness(nodes []schema.Node, edges []schema.Edge) map[string]float64 {
	if len(nodes) == 0 {
		return map[string]float64{}
	}
	adj := buildAdj(nodes, edges)

	// Output bucket. Pre-seeded so isolated nodes appear with 0 (caller
	// shouldn't need to defensively check for absent keys).
	cb := make(map[string]float64, len(nodes))
	for _, n := range nodes {
		cb[n.ID] = 0
	}

	// Brandes' single-source-shortest-paths pass.
	for _, src := range nodes {
		s := src.ID
		stack := make([]string, 0, len(nodes))
		pred := make(map[string][]string, len(nodes))
		sigma := make(map[string]float64, len(nodes))
		dist := make(map[string]int, len(nodes))
		for _, n := range nodes {
			sigma[n.ID] = 0
			dist[n.ID] = -1
		}
		sigma[s] = 1
		dist[s] = 0

		queue := []string{s}
		for len(queue) > 0 {
			v := queue[0]
			queue = queue[1:]
			stack = append(stack, v)
			for _, w := range adj[v] {
				if dist[w] < 0 {
					dist[w] = dist[v] + 1
					queue = append(queue, w)
				}
				if dist[w] == dist[v]+1 {
					sigma[w] += sigma[v]
					pred[w] = append(pred[w], v)
				}
			}
		}

		delta := make(map[string]float64, len(nodes))
		// Reverse-BFS dependency accumulation.
		for i := len(stack) - 1; i >= 0; i-- {
			w := stack[i]
			for _, v := range pred[w] {
				delta[v] += (sigma[v] / sigma[w]) * (1 + delta[w])
			}
			if w != s {
				cb[w] += delta[w]
			}
		}
	}

	// Undirected graphs double-count each pair (once from each source),
	// so halve the result to match the standard definition.
	for k := range cb {
		cb[k] /= 2
	}
	return cb
}

// buildAdj returns an undirected adjacency list with duplicates and
// self-loops removed. Keys are limited to the node-set so dangling
// edge endpoints don't leak into results.
func buildAdj(nodes []schema.Node, edges []schema.Edge) map[string][]string {
	idSet := make(map[string]struct{}, len(nodes))
	for _, n := range nodes {
		idSet[n.ID] = struct{}{}
	}
	type pair struct{ a, b string }
	seen := make(map[pair]struct{}, len(edges))
	adj := make(map[string][]string, len(nodes))
	for _, e := range edges {
		if e.Source == e.Target {
			continue
		}
		if _, ok := idSet[e.Source]; !ok {
			continue
		}
		if _, ok := idSet[e.Target]; !ok {
			continue
		}
		a, b := e.Source, e.Target
		if a > b {
			a, b = b, a
		}
		k := pair{a, b}
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		adj[e.Source] = append(adj[e.Source], e.Target)
		adj[e.Target] = append(adj[e.Target], e.Source)
	}
	return adj
}
