package cluster

import (
	"testing"

	"github.com/julianshen/gogfy/internal/schema"
)

func TestLeidenClustererAssignsCommunities(t *testing.T) {
	nodes := []schema.Node{
		{ID: "a"}, {ID: "b"}, {ID: "c"},
	}
	edges := []schema.Edge{
		{Source: "a", Target: "b"},
		{Source: "b", Target: "c"},
	}
	c := NewLeidenClusterer()
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

func TestLeidenClustererConnectedNodesShareCommunity(t *testing.T) {
	nodes := []schema.Node{
		{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"},
	}
	edges := []schema.Edge{
		{Source: "a", Target: "b"},
		{Source: "b", Target: "c"},
	}
	c := NewLeidenClusterer()
	result, err := c.Cluster(nodes, edges)
	if err != nil {
		t.Fatal(err)
	}

	// a, b, c should share the same community (they're connected)
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
	// d is isolated, should be in its own community
	if communities["d"] == communities["a"] {
		t.Fatal("d should be in a different community from a")
	}
}

func TestLeidenClustererIsolatedNodes(t *testing.T) {
	nodes := []schema.Node{
		{ID: "a"}, {ID: "b"}, {ID: "c"},
	}
	edges := []schema.Edge{}
	c := NewLeidenClusterer()
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

func TestLeidenClustererDoesNotMutateInput(t *testing.T) {
	nodes := []schema.Node{
		{ID: "a"}, {ID: "b"},
	}
	edges := []schema.Edge{
		{Source: "a", Target: "b"},
	}
	c := NewLeidenClusterer()
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

func TestLeidenClustererDeterministicOutput(t *testing.T) {
	nodes := []schema.Node{
		{ID: "c"}, {ID: "a"}, {ID: "b"},
	}
	edges := []schema.Edge{
		{Source: "a", Target: "b"},
	}
	c := NewLeidenClusterer()
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

func TestLeidenClustererTwoCliques(t *testing.T) {
	// Two distinct cliques that should be detected as separate communities
	nodes := []schema.Node{
		{ID: "a"}, {ID: "b"}, {ID: "c"},
		{ID: "d"}, {ID: "e"}, {ID: "f"},
	}
	edges := []schema.Edge{
		// Clique 1: a-b-c (triangle)
		{Source: "a", Target: "b"},
		{Source: "b", Target: "c"},
		{Source: "c", Target: "a"},
		// Clique 2: d-e-f (triangle)
		{Source: "d", Target: "e"},
		{Source: "e", Target: "f"},
		{Source: "f", Target: "d"},
	}
	c := NewLeidenClusterer()
	result, err := c.Cluster(nodes, edges)
	if err != nil {
		t.Fatal(err)
	}

	communities := make(map[string]string)
	for _, n := range result {
		communities[n.ID] = n.Community
	}

	// Nodes within each clique should share communities
	if communities["a"] != communities["b"] || communities["b"] != communities["c"] {
		t.Logf("communities: %v", communities)
		t.Fatal("clique 1 nodes should share a community")
	}
	if communities["d"] != communities["e"] || communities["e"] != communities["f"] {
		t.Logf("communities: %v", communities)
		t.Fatal("clique 2 nodes should share a community")
	}
	// The two cliques should be in different communities
	if communities["a"] == communities["d"] {
		t.Logf("communities: %v", communities)
		t.Fatal("two cliques should be in different communities")
	}
}

func TestLeidenClustererEmptyGraph(t *testing.T) {
	c := NewLeidenClusterer()
	result, err := c.Cluster([]schema.Node{}, []schema.Edge{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 0 {
		t.Fatalf("expected empty result, got %d nodes", len(result))
	}
}

func TestLeidenClustererAutoCreatesMissingNodes(t *testing.T) {
	// Leiden clusterer auto-creates nodes referenced by edges but missing from the node list
	nodes := []schema.Node{{ID: "a"}, {ID: "b"}}
	edges := []schema.Edge{{Source: "x", Target: "a"}}
	c := NewLeidenClusterer()
	result, err := c.Cluster(nodes, edges)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("expected 3 nodes (including auto-created x), got %d", len(result))
	}
}

func TestLeidenClustererAutoCreatesMissingTarget(t *testing.T) {
	nodes := []schema.Node{{ID: "a"}, {ID: "b"}}
	edges := []schema.Edge{{Source: "a", Target: "x"}}
	c := NewLeidenClusterer()
	result, err := c.Cluster(nodes, edges)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("expected 3 nodes (including auto-created x), got %d", len(result))
	}
}

func TestLeidenClustererOptions(t *testing.T) {
	c := NewLeidenClusterer(
		WithResolution(0.5),
		WithMaxIterations(50),
		WithMinModularityGain(0.001),
		WithRandomSeed(123),
	)
	// Just verify it doesn't panic and produces output
	nodes := []schema.Node{{ID: "a"}, {ID: "b"}}
	edges := []schema.Edge{{Source: "a", Target: "b"}}
	result, err := c.Cluster(nodes, edges)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(result))
	}
}

func TestLeidenClustererSingleNode(t *testing.T) {
	nodes := []schema.Node{{ID: "a"}}
	edges := []schema.Edge{}
	c := NewLeidenClusterer()
	result, err := c.Cluster(nodes, edges)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 node, got %d", len(result))
	}
	if result[0].Community == "" {
		t.Fatal("single node should have a community")
	}
}

func TestLeidenClustererWeightedEdges(t *testing.T) {
	// Multiple edges between same nodes should be treated as higher weight
	nodes := []schema.Node{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	edges := []schema.Edge{
		{Source: "a", Target: "b"},
		{Source: "a", Target: "b"},
		{Source: "b", Target: "c"},
	}
	c := NewLeidenClusterer()
	result, err := c.Cluster(nodes, edges)
	if err != nil {
		t.Fatal(err)
	}
	// All connected nodes should share a community
	communities := make(map[string]string)
	for _, n := range result {
		communities[n.ID] = n.Community
	}
	if communities["a"] != communities["b"] || communities["b"] != communities["c"] {
		t.Logf("communities: %v", communities)
		t.Fatal("all connected nodes should share a community")
	}
}
