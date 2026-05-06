// Package graph provides an immutable graph data structure and a builder for constructing it.
package graph

import (
	"fmt"
	"sort"

	"github.com/julianshen/gogfy/internal/schema"
)

// Graph is an immutable collection of nodes and edges.
type Graph struct {
	nodes []schema.Node
	edges []schema.Edge
}

// Nodes returns a copy of the graph's nodes.
func (g Graph) Nodes() []schema.Node {
	result := make([]schema.Node, len(g.nodes))
	copy(result, g.nodes)
	return result
}

// Edges returns a copy of the graph's edges.
func (g Graph) Edges() []schema.Edge {
	result := make([]schema.Edge, len(g.edges))
	copy(result, g.edges)
	return result
}

// Builder constructs a Graph incrementally, deduplicating nodes and edges.
type Builder struct {
	nodes map[string]schema.Node
	edges map[edgeKey]schema.Edge
}

type edgeKey struct {
	Source   string
	Target   string
	Relation string
}

// NewBuilder creates a new Builder ready to accept nodes and edges.
func NewBuilder() *Builder {
	return &Builder{
		nodes: make(map[string]schema.Node),
		edges: make(map[edgeKey]schema.Edge),
	}
}

// AddNode adds a node to the builder, returning an error if validation fails.
// Duplicate IDs are silently ignored (idempotent).
func (b *Builder) AddNode(n schema.Node) error {
	if err := n.Validate(); err != nil {
		return err
	}
	if _, exists := b.nodes[n.ID]; exists {
		return nil
	}
	b.nodes[n.ID] = n
	return nil
}

// AddEdge adds an edge to the builder, returning an error if validation fails or the edge already exists.
func (b *Builder) AddEdge(e schema.Edge) error {
	if err := e.Validate(); err != nil {
		return err
	}
	if _, exists := b.edges[edgeKey{e.Source, e.Target, e.Relation}]; exists {
		return fmt.Errorf("edge %s-%s-%s already exists", e.Source, e.Target, e.Relation)
	}
	b.edges[edgeKey{e.Source, e.Target, e.Relation}] = e
	return nil
}

// Build constructs an immutable Graph from the accumulated nodes and edges.
func (b *Builder) Build() Graph {
	g := Graph{
		nodes: make([]schema.Node, 0, len(b.nodes)),
		edges: make([]schema.Edge, 0, len(b.edges)),
	}
	for _, n := range b.nodes {
		g.nodes = append(g.nodes, n)
	}
	schema.SortNodesByID(g.nodes)
	for _, e := range b.edges {
		g.edges = append(g.edges, e)
	}
	sort.Slice(g.edges, func(i, j int) bool {
		if g.edges[i].Source != g.edges[j].Source {
			return g.edges[i].Source < g.edges[j].Source
		}
		if g.edges[i].Target != g.edges[j].Target {
			return g.edges[i].Target < g.edges[j].Target
		}
		return g.edges[i].Relation < g.edges[j].Relation
	})
	return g
}
