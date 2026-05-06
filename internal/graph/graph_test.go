package graph

import (
	"testing"

	"github.com/julianshen/gogfy/internal/schema"
)

func TestGraphBuilderDedupesNodes(t *testing.T) {
	b := NewBuilder()
	if err := b.AddNode(schema.Node{ID: "pkg:main", Label: "main"}); err != nil {
		t.Fatal(err)
	}
	if err := b.AddNode(schema.Node{ID: "pkg:main", Label: "main"}); err != nil {
		t.Fatal(err)
	}
	g := b.Build()
	if len(g.Nodes()) != 1 {
		t.Fatalf("expected 1 node, got %d", len(g.Nodes()))
	}
}

func TestGraphBuilderNodeOverwriteRejected(t *testing.T) {
	b := NewBuilder()
	if err := b.AddNode(schema.Node{ID: "pkg:main", Label: "main", SourceFile: "a.go"}); err != nil {
		t.Fatal(err)
	}
	if err := b.AddNode(schema.Node{ID: "pkg:main", Label: "updated", SourceFile: "b.go"}); err != nil {
		t.Fatal(err)
	}
	g := b.Build()
	if len(g.Nodes()) != 1 {
		t.Fatalf("expected 1 node, got %d", len(g.Nodes()))
	}
	nodes := g.Nodes()
	if nodes[0].Label != "main" {
		t.Fatalf("expected label 'main', got %s", nodes[0].Label)
	}
}

func TestGraphBuilderMergesEdges(t *testing.T) {
	b := NewBuilder()
	if err := b.AddEdge(schema.Edge{Source: "a", Target: "b", Relation: "imports", Confidence: schema.Extracted}); err != nil {
		t.Fatal(err)
	}
	if err := b.AddEdge(schema.Edge{Source: "a", Target: "b", Relation: "imports", Confidence: schema.Extracted}); err == nil {
		t.Fatal("expected error for duplicate edge")
	}
	g := b.Build()
	if len(g.Edges()) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(g.Edges()))
	}
}

func TestGraphBuilderEdgeOverwriteRejected(t *testing.T) {
	b := NewBuilder()
	if err := b.AddEdge(schema.Edge{Source: "a", Target: "b", Relation: "imports", Confidence: schema.Inferred}); err != nil {
		t.Fatal(err)
	}
	if err := b.AddEdge(schema.Edge{Source: "a", Target: "b", Relation: "imports", Confidence: schema.Extracted}); err == nil {
		t.Fatal("expected error for duplicate edge")
	}
	g := b.Build()
	if len(g.Edges()) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(g.Edges()))
	}
	edges := g.Edges()
	if edges[0].Confidence != schema.Inferred {
		t.Fatalf("expected confidence INFERRED, got %s", edges[0].Confidence)
	}
}

func TestGraphBuilderDeterministicOutput(t *testing.T) {
	b1 := NewBuilder()
	b1.AddNode(schema.Node{ID: "c", Label: "c"})
	b1.AddNode(schema.Node{ID: "a", Label: "a"})
	b1.AddNode(schema.Node{ID: "b", Label: "b"})
	b1.AddEdge(schema.Edge{Source: "c", Target: "d", Relation: "calls", Confidence: schema.Extracted})
	b1.AddEdge(schema.Edge{Source: "a", Target: "b", Relation: "imports", Confidence: schema.Extracted})
	g1 := b1.Build()

	b2 := NewBuilder()
	b2.AddNode(schema.Node{ID: "a", Label: "a"})
	b2.AddNode(schema.Node{ID: "b", Label: "b"})
	b2.AddNode(schema.Node{ID: "c", Label: "c"})
	b2.AddEdge(schema.Edge{Source: "a", Target: "b", Relation: "imports", Confidence: schema.Extracted})
	b2.AddEdge(schema.Edge{Source: "c", Target: "d", Relation: "calls", Confidence: schema.Extracted})
	g2 := b2.Build()

	n1, n2 := g1.Nodes(), g2.Nodes()
	if len(n1) != len(n2) {
		t.Fatalf("node count mismatch")
	}
	for i := range n1 {
		if n1[i].ID != n2[i].ID {
			t.Fatalf("node order not deterministic at index %d: %s vs %s", i, n1[i].ID, n2[i].ID)
		}
	}
	e1, e2 := g1.Edges(), g2.Edges()
	if len(e1) != len(e2) {
		t.Fatalf("edge count mismatch")
	}
	for i := range e1 {
		if e1[i].Source != e2[i].Source {
			t.Fatalf("edge order not deterministic at index %d", i)
		}
	}
}

func TestGraphBuilderEmpty(t *testing.T) {
	g := NewBuilder().Build()
	nodes := g.Nodes()
	edges := g.Edges()
	if nodes == nil {
		t.Fatal("expected non-nil Nodes slice")
	}
	if edges == nil {
		t.Fatal("expected non-nil Edges slice")
	}
	if len(nodes) != 0 {
		t.Fatalf("expected 0 nodes, got %d", len(nodes))
	}
	if len(edges) != 0 {
		t.Fatalf("expected 0 edges, got %d", len(edges))
	}
}

func TestGraphBuilderInvalidNode(t *testing.T) {
	b := NewBuilder()
	if err := b.AddNode(schema.Node{ID: "", Label: "test"}); err == nil {
		t.Fatal("expected error for invalid node")
	}
}

func TestGraphBuilderInvalidEdge(t *testing.T) {
	b := NewBuilder()
	if err := b.AddEdge(schema.Edge{Source: "", Target: "b", Relation: "imports", Confidence: schema.Extracted}); err == nil {
		t.Fatal("expected error for invalid edge")
	}
}

func TestGraphMutationProtection(t *testing.T) {
	b := NewBuilder()
	b.AddNode(schema.Node{ID: "a", Label: "A"})
	g := b.Build()

	nodes := g.Nodes()
	nodes[0].Label = "mutated"

	nodes2 := g.Nodes()
	if nodes2[0].Label != "A" {
		t.Fatal("Graph.Nodes() should return a copy, not the internal slice")
	}
}
