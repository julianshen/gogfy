// Package graphdiff compares two graph snapshots and surfaces the
// added/removed/changed nodes and edges between them. Lets users see
// what changed between two runs (refactors, dependency cuts, etc.)
// without diffing the raw graph.json (which would also surface
// purely cosmetic field-order changes).
package graphdiff

import (
	"sort"

	"github.com/julianshen/gogfy/internal/schema"
)

// Diff captures the structural changes between an "old" and a "new"
// graph. Slices are sorted (by ID for nodes; by source+target+relation
// for edges) so renders are byte-stable across runs.
type Diff struct {
	NodesAdded   []schema.Node
	NodesRemoved []schema.Node
	NodesChanged []NodeChange
	EdgesAdded   []schema.Edge
	EdgesRemoved []schema.Edge
}

// NodeChange describes a node that exists in both snapshots but whose
// non-ID fields differ. Label/Community/FileType/etc. drift is the
// usual case after a re-extraction.
type NodeChange struct {
	Old schema.Node
	New schema.Node
}

// Compute returns the diff between two snapshots. Symmetric in the
// sense that swapping arguments swaps Added↔Removed and reverses the
// order of Old/New inside NodeChange.
func Compute(oldNodes, newNodes []schema.Node, oldEdges, newEdges []schema.Edge) Diff {
	oldNodeByID := indexNodes(oldNodes)
	newNodeByID := indexNodes(newNodes)

	var d Diff
	for _, n := range newNodes {
		prev, ok := oldNodeByID[n.ID]
		if !ok {
			d.NodesAdded = append(d.NodesAdded, n)
			continue
		}
		if prev != n {
			d.NodesChanged = append(d.NodesChanged, NodeChange{Old: prev, New: n})
		}
	}
	for _, n := range oldNodes {
		if _, ok := newNodeByID[n.ID]; !ok {
			d.NodesRemoved = append(d.NodesRemoved, n)
		}
	}

	oldEdgeSet := indexEdges(oldEdges)
	newEdgeSet := indexEdges(newEdges)
	for k, e := range newEdgeSet {
		if _, ok := oldEdgeSet[k]; !ok {
			d.EdgesAdded = append(d.EdgesAdded, e)
		}
	}
	for k, e := range oldEdgeSet {
		if _, ok := newEdgeSet[k]; !ok {
			d.EdgesRemoved = append(d.EdgesRemoved, e)
		}
	}

	sort.Slice(d.NodesAdded, func(i, j int) bool { return d.NodesAdded[i].ID < d.NodesAdded[j].ID })
	sort.Slice(d.NodesRemoved, func(i, j int) bool { return d.NodesRemoved[i].ID < d.NodesRemoved[j].ID })
	sort.Slice(d.NodesChanged, func(i, j int) bool { return d.NodesChanged[i].New.ID < d.NodesChanged[j].New.ID })
	sortEdges(d.EdgesAdded)
	sortEdges(d.EdgesRemoved)
	return d
}

func indexNodes(ns []schema.Node) map[string]schema.Node {
	m := make(map[string]schema.Node, len(ns))
	for _, n := range ns {
		m[n.ID] = n
	}
	return m
}

// edgeKey identifies an edge by (source, target, relation). Two edges
// with the same triple but different confidences DO collide here — a
// confidence flip on the same edge shows up as no-change, which is the
// right semantic for "is this connection still present?" but loses the
// confidence-drift signal. Acceptable for v1; a future Diff.EdgesChanged
// could surface confidence drift specifically.
type edgeKey struct{ source, target, relation string }

func indexEdges(es []schema.Edge) map[edgeKey]schema.Edge {
	m := make(map[edgeKey]schema.Edge, len(es))
	for _, e := range es {
		m[edgeKey{e.Source, e.Target, e.Relation}] = e
	}
	return m
}

func sortEdges(es []schema.Edge) {
	sort.Slice(es, func(i, j int) bool {
		if es[i].Source != es[j].Source {
			return es[i].Source < es[j].Source
		}
		if es[i].Target != es[j].Target {
			return es[i].Target < es[j].Target
		}
		return es[i].Relation < es[j].Relation
	})
}

