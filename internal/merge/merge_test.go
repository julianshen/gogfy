package merge

import (
	"testing"

	"github.com/julianshen/gogfy/internal/export"
	"github.com/julianshen/gogfy/internal/schema"
)

func TestMergeUnionDedupes(t *testing.T) {
	a := export.GraphExport{
		Nodes: []schema.Node{
			{ID: "n1", Label: "one"},
			{ID: "shared", Label: "shared-from-a"},
		},
		Edges: []schema.Edge{
			{Source: "n1", Target: "shared", Relation: "calls", Confidence: schema.Extracted},
		},
	}
	b := export.GraphExport{
		Nodes: []schema.Node{
			{ID: "shared", Label: "shared-from-b"},
			{ID: "n2", Label: "two"},
		},
		Edges: []schema.Edge{
			{Source: "n1", Target: "shared", Relation: "calls", Confidence: schema.Extracted}, // duplicate of a's edge
			{Source: "n2", Target: "shared", Relation: "calls", Confidence: schema.Extracted},
		},
	}
	got := Merge(a, b)
	if len(got.Nodes) != 3 {
		t.Fatalf("expected 3 unique nodes, got %d: %+v", len(got.Nodes), got.Nodes)
	}
	if len(got.Edges) != 2 {
		t.Fatalf("expected 2 unique edges, got %d: %+v", len(got.Edges), got.Edges)
	}
}

func TestMergeFirstNodeLabelWins(t *testing.T) {
	a := export.GraphExport{Nodes: []schema.Node{{ID: "x", Label: "from-a"}}}
	b := export.GraphExport{Nodes: []schema.Node{{ID: "x", Label: "from-b"}}}
	got := Merge(a, b)
	if len(got.Nodes) != 1 || got.Nodes[0].Label != "from-a" {
		t.Fatalf("expected first-label wins, got %+v", got.Nodes)
	}
}

func TestMergeIsDeterministic(t *testing.T) {
	a := export.GraphExport{
		Nodes: []schema.Node{{ID: "z"}, {ID: "a"}},
		Edges: []schema.Edge{{Source: "z", Target: "a", Relation: "r"}},
	}
	b := export.GraphExport{
		Nodes: []schema.Node{{ID: "m"}, {ID: "b"}},
		Edges: []schema.Edge{{Source: "m", Target: "b", Relation: "r"}},
	}
	first := Merge(a, b)
	second := Merge(a, b)
	if !graphsEqual(first, second) {
		t.Fatalf("merge not deterministic across calls")
	}
	// Sorted by ID — z must come last.
	if first.Nodes[0].ID != "a" || first.Nodes[len(first.Nodes)-1].ID != "z" {
		t.Fatalf("merge nodes not sorted by ID: %+v", first.Nodes)
	}
}

func TestMergeEmpty(t *testing.T) {
	got := Merge(export.GraphExport{}, export.GraphExport{})
	if len(got.Nodes) != 0 || len(got.Edges) != 0 {
		t.Fatalf("merging empties should yield empty, got %+v", got)
	}
}

func TestMergeMultipleInputs(t *testing.T) {
	a := export.GraphExport{Nodes: []schema.Node{{ID: "a"}}}
	b := export.GraphExport{Nodes: []schema.Node{{ID: "b"}}}
	c := export.GraphExport{Nodes: []schema.Node{{ID: "c"}}}
	got := MergeAll([]export.GraphExport{a, b, c})
	if len(got.Nodes) != 3 {
		t.Fatalf("expected 3 nodes from 3-way merge, got %d", len(got.Nodes))
	}
}

func TestMergeAllEmpty(t *testing.T) {
	got := MergeAll(nil)
	if len(got.Nodes) != 0 || len(got.Edges) != 0 {
		t.Fatalf("MergeAll of nothing should be empty")
	}
}

func TestMergeEdgesDifferByConfidence(t *testing.T) {
	// Same source/target/relation but different confidence levels are
	// distinct edges — graphify treats EXTRACTED, INFERRED, AMBIGUOUS as
	// genuinely different facts.
	a := export.GraphExport{Edges: []schema.Edge{
		{Source: "x", Target: "y", Relation: "calls", Confidence: schema.Extracted},
	}}
	b := export.GraphExport{Edges: []schema.Edge{
		{Source: "x", Target: "y", Relation: "calls", Confidence: schema.Inferred},
	}}
	got := Merge(a, b)
	if len(got.Edges) != 2 {
		t.Fatalf("EXTRACTED and INFERRED edges should be kept separately, got %d", len(got.Edges))
	}
}

func graphsEqual(a, b export.GraphExport) bool {
	if len(a.Nodes) != len(b.Nodes) || len(a.Edges) != len(b.Edges) {
		return false
	}
	for i := range a.Nodes {
		if a.Nodes[i] != b.Nodes[i] {
			return false
		}
	}
	for i := range a.Edges {
		if a.Edges[i] != b.Edges[i] {
			return false
		}
	}
	return true
}
