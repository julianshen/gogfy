package cluster

import (
	"testing"

	"github.com/julianshen/gogfy/internal/schema"
)

func TestClustererAssignsCommunities(t *testing.T) {
	nodes := []schema.Node{
		{ID: "a"}, {ID: "b"}, {ID: "c"},
	}
	edges := []schema.Edge{
		{Source: "a", Target: "b"},
		{Source: "b", Target: "c"},
	}
	c := NewConnectedComponentsClusterer()
	result, err := c.Cluster(nodes, edges)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range result {
		if n.Community == "" {
			t.Fatalf("node %s missing community", n.ID)
		}
	}
}

func TestClustererConnectedNodesShareCommunity(t *testing.T) {
	nodes := []schema.Node{
		{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"},
	}
	edges := []schema.Edge{
		{Source: "a", Target: "b"},
		{Source: "b", Target: "c"},
	}
	c := NewConnectedComponentsClusterer()
	result, err := c.Cluster(nodes, edges)
	if err != nil {
		t.Fatal(err)
	}

	// a, b, c should share the same community
	communities := make(map[string]string)
	for _, n := range result {
		communities[n.ID] = n.Community
	}
	if communities["a"] != communities["b"] {
		t.Fatal("a and b should share community")
	}
	if communities["b"] != communities["c"] {
		t.Fatal("b and c should share community")
	}
	// d should be in a different community
	if communities["d"] == communities["a"] {
		t.Fatal("d should be in a different community from a")
	}
}

func TestClustererIsolatedNodes(t *testing.T) {
	nodes := []schema.Node{
		{ID: "a"}, {ID: "b"}, {ID: "c"},
	}
	edges := []schema.Edge{}
	c := NewConnectedComponentsClusterer()
	result, err := c.Cluster(nodes, edges)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(result))
	}
	// Each isolated node should get its own community
	communities := make(map[string]bool)
	for _, n := range result {
		communities[n.Community] = true
	}
	if len(communities) != 3 {
		t.Fatalf("expected 3 communities, got %d", len(communities))
	}
}

func TestClustererDoesNotMutateInput(t *testing.T) {
	nodes := []schema.Node{
		{ID: "a"}, {ID: "b"},
	}
	edges := []schema.Edge{
		{Source: "a", Target: "b"},
	}
	c := NewConnectedComponentsClusterer()
	_, err := c.Cluster(nodes, edges)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range nodes {
		if n.Community != "" {
			t.Fatalf("input node %s was mutated", n.ID)
		}
	}
}

func TestClustererDeterministicOutput(t *testing.T) {
	nodes := []schema.Node{
		{ID: "c"}, {ID: "a"}, {ID: "b"},
	}
	edges := []schema.Edge{
		{Source: "a", Target: "b"},
	}
	c := NewConnectedComponentsClusterer()
	result1, err := c.Cluster(nodes, edges)
	if err != nil {
		t.Fatal(err)
	}
	result2, err := c.Cluster(nodes, edges)
	if err != nil {
		t.Fatal(err)
	}
	if len(result1) != len(result2) {
		t.Fatal("result lengths differ")
	}
	for i := range result1 {
		if result1[i].ID != result2[i].ID {
			t.Fatalf("node order differs at index %d", i)
		}
		if result1[i].Community != result2[i].Community {
			t.Fatalf("community differs for node %s", result1[i].ID)
		}
	}
}
