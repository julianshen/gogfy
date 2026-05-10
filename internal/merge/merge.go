// Package merge unions two or more graph exports into one. Used by
// `gogfy merge-graphs a.json b.json` to combine per-repo graphs into a
// cross-repo or team-aggregate view.
//
// Semantics:
//
//   - Nodes are deduped by ID. The first occurrence wins, so passing graphs
//     in commit-order yields a stable "canonical" graph where the earliest
//     observation of each node defines its label/community/source-file.
//   - Edges are deduped by (source, target, relation, confidence). EXTRACTED
//     and INFERRED variants of the same triple are kept as distinct facts
//     because they're genuinely different claims about how the relationship
//     was observed.
//   - Output is sorted by ID/source for determinism — re-running the merge
//     on the same inputs produces byte-identical output, which the eventual
//     git merge driver depends on.
package merge

import (
	"sort"

	"github.com/julianshen/gogfy/internal/export"
	"github.com/julianshen/gogfy/internal/schema"
)

// Merge unions two graphs. See MergeAll for the multi-input variant.
func Merge(a, b export.GraphExport) export.GraphExport {
	return MergeAll([]export.GraphExport{a, b})
}

// MergeAll unions all input graphs in order. Empty input yields an empty
// graph (not nil slices — keeps JSON output as `[]` rather than `null`,
// matching the rest of the export pipeline).
func MergeAll(graphs []export.GraphExport) export.GraphExport {
	out := export.GraphExport{
		Nodes: []schema.Node{},
		Edges: []schema.Edge{},
	}
	seenNode := map[string]bool{}
	for _, g := range graphs {
		for _, n := range g.Nodes {
			if seenNode[n.ID] {
				continue
			}
			seenNode[n.ID] = true
			out.Nodes = append(out.Nodes, n)
		}
	}
	type edgeKey struct {
		Source, Target, Relation string
		Confidence               schema.Confidence
	}
	seenEdge := map[edgeKey]bool{}
	for _, g := range graphs {
		for _, e := range g.Edges {
			k := edgeKey{e.Source, e.Target, e.Relation, e.Confidence}
			if seenEdge[k] {
				continue
			}
			seenEdge[k] = true
			out.Edges = append(out.Edges, e)
		}
	}
	sort.Slice(out.Nodes, func(i, j int) bool { return out.Nodes[i].ID < out.Nodes[j].ID })
	sort.Slice(out.Edges, func(i, j int) bool {
		if out.Edges[i].Source != out.Edges[j].Source {
			return out.Edges[i].Source < out.Edges[j].Source
		}
		if out.Edges[i].Target != out.Edges[j].Target {
			return out.Edges[i].Target < out.Edges[j].Target
		}
		if out.Edges[i].Relation != out.Edges[j].Relation {
			return out.Edges[i].Relation < out.Edges[j].Relation
		}
		return out.Edges[i].Confidence < out.Edges[j].Confidence
	})
	return out
}
