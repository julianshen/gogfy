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

func TestGraphBuilderMergesEdges(t *testing.T) {
	b := NewBuilder()
	b.AddEdge(schema.Edge{Source: "a", Target: "b", Relation: "imports"})
	b.AddEdge(schema.Edge{Source: "a", Target: "b", Relation: "imports"})
	g := b.Build()
	if len(g.Edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(g.Edges))
	}
}
