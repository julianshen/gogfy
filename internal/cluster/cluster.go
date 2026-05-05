package cluster

import (
	"sort"
	"strconv"

	"github.com/julianshen/gogfy/internal/schema"
)

type Clusterer interface {
	Cluster(nodes []schema.Node, edges []schema.Edge) ([]schema.Node, error)
}

type ConnectedComponentsClusterer struct{}

func NewConnectedComponentsClusterer() *ConnectedComponentsClusterer {
	return &ConnectedComponentsClusterer{}
}

func (c *ConnectedComponentsClusterer) Cluster(nodes []schema.Node, edges []schema.Edge) ([]schema.Node, error) {
	// Build adjacency list
	adj := make(map[string][]string, len(nodes))
	for _, e := range edges {
		adj[e.Source] = append(adj[e.Source], e.Target)
		adj[e.Target] = append(adj[e.Target], e.Source)
	}

	// Build node index for O(1) lookup
	nodeIdx := make(map[string]int, len(nodes))
	result := make([]schema.Node, len(nodes))
	for i, n := range nodes {
		nodeIdx[n.ID] = i
		result[i] = n
	}

	visited := make(map[string]bool, len(nodes))
	var communityID int

	for _, n := range nodes {
		if visited[n.ID] {
			continue
		}

		queue := []string{n.ID}
		visited[n.ID] = true
		members := []string{n.ID}

		for len(queue) > 0 {
			curr := queue[0]
			queue = queue[1:]
			for _, neighbor := range adj[curr] {
				if !visited[neighbor] {
					visited[neighbor] = true
					queue = append(queue, neighbor)
					members = append(members, neighbor)
				}
			}
		}

		cid := strconv.Itoa(communityID)
		for _, id := range members {
			if idx, ok := nodeIdx[id]; ok {
				result[idx].Community = cid
			}
		}
		communityID++
	}

	// Sort by node ID for deterministic output
	sort.Slice(result, func(i, j int) bool {
		return result[i].ID < result[j].ID
	})

	return result, nil
}
