package graph

import (
	"testing"

	"github.com/julianshen/gogfy/internal/schema"
)

func TestGraphBuilderDedupesNodes(t *testing.T) {
	b := NewBuilder()
	b.AddNode(schema.Node{ID: "pkg:main", Label: "main"})
	b.AddNode(schema.Node{ID: "pkg:main", Label: "main"})
	g := b.Build()
	if len(g.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(g.Nodes))
	}
}

func TestGraphBuilderNodeOverwrite(t *testing.T) {
	b := NewBuilder()
	b.AddNode(schema.Node{ID: "pkg:main", Label: "main", SourceFile: "a.go"})
	b.AddNode(schema.Node{ID: "pkg:main", Label: "updated", SourceFile: "b.go"})
	g := b.Build()
	if len(g.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(g.Nodes))
	}
	if g.Nodes[0].Label != "updated" {
		t.Fatalf("expected label 'updated', got %s", g.Nodes[0].Label)
	}
	if g.Nodes[0].SourceFile != "b.go" {
		t.Fatalf("expected SourceFile 'b.go', got %s", g.Nodes[0].SourceFile)
	}
}

func TestGraphBuilderMergesEdges(t *testing.T) {
	b := NewBuilder()
	b.AddEdge(schema.Edge{Source: "a", Target: "b", Relation: "imports"})
	b.AddEdge(schema.Edge{Source: "a", Target: "b", Relation: "imports"})
	g := b.Build()
	if len(g.Edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(g.Edges))
	}
}

func TestGraphBuilderEdgeOverwrite(t *testing.T) {
	b := NewBuilder()
	b.AddEdge(schema.Edge{Source: "a", Target: "b", Relation: "imports", Confidence: schema.Inferred})
	b.AddEdge(schema.Edge{Source: "a", Target: "b", Relation: "imports", Confidence: schema.Extracted})
	g := b.Build()
	if len(g.Edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(g.Edges))
	}
	if g.Edges[0].Confidence != schema.Extracted {
		t.Fatalf("expected confidence EXTRACTED, got %s", g.Edges[0].Confidence)
	}
}

func TestGraphBuilderDeterministicOutput(t *testing.T) {
	b1 := NewBuilder()
	b1.AddNode(schema.Node{ID: "c", Label: "c"})
	b1.AddNode(schema.Node{ID: "a", Label: "a"})
	b1.AddNode(schema.Node{ID: "b", Label: "b"})
	b1.AddEdge(schema.Edge{Source: "c", Target: "d", Relation: "calls"})
	b1.AddEdge(schema.Edge{Source: "a", Target: "b", Relation: "imports"})
	g1 := b1.Build()

	b2 := NewBuilder()
	b2.AddNode(schema.Node{ID: "a", Label: "a"})
	b2.AddNode(schema.Node{ID: "b", Label: "b"})
	b2.AddNode(schema.Node{ID: "c", Label: "c"})
	b2.AddEdge(schema.Edge{Source: "a", Target: "b", Relation: "imports"})
	b2.AddEdge(schema.Edge{Source: "c", Target: "d", Relation: "calls"})
	g2 := b2.Build()

	if len(g1.Nodes) != len(g2.Nodes) {
		t.Fatalf("node count mismatch")
	}
	for i := range g1.Nodes {
		if g1.Nodes[i].ID != g2.Nodes[i].ID {
			t.Fatalf("node order not deterministic at index %d: %s vs %s", i, g1.Nodes[i].ID, g2.Nodes[i].ID)
		}
	}
	if len(g1.Edges) != len(g2.Edges) {
		t.Fatalf("edge count mismatch")
	}
	for i := range g1.Edges {
		if g1.Edges[i].Source != g2.Edges[i].Source {
			t.Fatalf("edge order not deterministic at index %d", i)
		}
	}
}

func TestGraphBuilderEmpty(t *testing.T) {
	g := NewBuilder().Build()
	if g.Nodes == nil {
		t.Fatal("expected non-nil Nodes slice")
	}
	if g.Edges == nil {
		t.Fatal("expected non-nil Edges slice")
	}
	if len(g.Nodes) != 0 {
		t.Fatalf("expected 0 nodes, got %d", len(g.Nodes))
	}
	if len(g.Edges) != 0 {
		t.Fatalf("expected 0 edges, got %d", len(g.Edges))
	}
}
