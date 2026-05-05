package cluster

import "github.com/julianshen/gogfy/internal/schema"

type Clusterer interface {
	Cluster(nodes []schema.Node, edges []schema.Edge) ([]schema.Node, error)
}

type LeidenClusterer struct{}

func NewLeidenClusterer() *LeidenClusterer {
	return &LeidenClusterer{}
}

func (l *LeidenClusterer) Cluster(nodes []schema.Node, edges []schema.Edge) ([]schema.Node, error) {
	adj := make(map[string][]string, len(nodes))
	for _, e := range edges {
		adj[e.Source] = append(adj[e.Source], e.Target)
		adj[e.Target] = append(adj[e.Target], e.Source)
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

		cid := itoa(communityID)
		for _, id := range members {
			for i := range nodes {
				if nodes[i].ID == id {
					nodes[i].Community = cid
					break
				}
			}
		}
		communityID++
	}

	return nodes, nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
