package dedup

import (
	"github.com/julianshen/gogfy/internal/schema"
)

// pass1Exact groups nodes by normalized label and merges exact matches.
// It returns a union-find with all exact-duplicate nodes unioned.
func pass1Exact(nodes []schema.Node) *UnionFind {
	uf := NewUnionFind()
	groups := make(map[string][]schema.Node)
	for _, n := range nodes {
		label := n.Label
		if label == "" {
			label = n.ID
		}
		key := normalize(label)
		if key == "" {
			continue
		}
		groups[key] = append(groups[key], n)
	}
	for _, group := range groups {
		if len(group) < 2 {
			continue
		}
		winner := pickWinner(group)
		for _, n := range group {
			if n.ID != winner.ID {
				uf.Union(winner.ID, n.ID)
			}
		}
	}
	return uf
}
