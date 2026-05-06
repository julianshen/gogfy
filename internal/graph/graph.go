package graph

import (
	"fmt"
	"sort"

	"github.com/julianshen/gogfy/internal/schema"
)

type Graph struct {
	nodes []schema.Node
	edges []schema.Edge
}

func (g Graph) Nodes() []schema.Node {
	result := make([]schema.Node, len(g.nodes))
	copy(result, g.nodes)
	return result
}

func (g Graph) Edges() []schema.Edge {
	result := make([]schema.Edge, len(g.edges))
	copy(result, g.edges)
	return result
}

type Builder struct {
	nodes map[string]schema.Node
	edges map[edgeKey]schema.Edge
}

type edgeKey struct {
	Source   string
	Target   string
	Relation string
}

func NewBuilder() *Builder {
	return &Builder{
		nodes: make(map[string]schema.Node),
		edges: make(map[edgeKey]schema.Edge),
	}
}

func (b *Builder) AddNode(n schema.Node) error {
	if err := n.Validate(); err != nil {
		return err
	}
	if _, exists := b.nodes[n.ID]; exists {
		return fmt.Errorf("node ID %q already exists", n.ID)
	}
	b.nodes[n.ID] = n
	return nil
}

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
