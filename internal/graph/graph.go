package graph

import "github.com/julianshen/gogfy/internal/schema"

type Graph struct {
	Nodes []schema.Node
	Edges []schema.Edge
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

func (b *Builder) AddNode(n schema.Node) {
	b.nodes[n.ID] = n
}

func (b *Builder) AddEdge(e schema.Edge) {
	b.edges[edgeKey{e.Source, e.Target, e.Relation}] = e
}

func (b *Builder) Build() Graph {
	g := Graph{
		Nodes: make([]schema.Node, 0, len(b.nodes)),
		Edges: make([]schema.Edge, 0, len(b.edges)),
	}
	for _, n := range b.nodes {
		g.Nodes = append(g.Nodes, n)
	}
	for _, e := range b.edges {
		g.Edges = append(g.Edges, e)
	}
	return g
}
