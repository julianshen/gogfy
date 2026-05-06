package graph

import (
	"sort"

	"github.com/julianshen/gogfy/internal/schema"
)

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
	schema.SortNodesByID(g.Nodes)
	for _, e := range b.edges {
		g.Edges = append(g.Edges, e)
	}
	sort.Slice(g.Edges, func(i, j int) bool {
		if g.Edges[i].Source != g.Edges[j].Source {
			return g.Edges[i].Source < g.Edges[j].Source
		}
		if g.Edges[i].Target != g.Edges[j].Target {
			return g.Edges[i].Target < g.Edges[j].Target
		}
		return g.Edges[i].Relation < g.Edges[j].Relation
	})
	return g
}
