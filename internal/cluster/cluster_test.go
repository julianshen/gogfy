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
