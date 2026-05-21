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
		norm := normalize(label)
		if norm == "" {
			continue
		}
		// Bucket-scoped key: structural kinds (module/section/…) only
		// merge within their kind; entity kinds share one bucket so
		// code↔semantic dedup still fires. Keeps "module" graph from
		// collapsing into "type" Graph. NUL separator avoids
		// bucket/label boundary ambiguity.
		key := mergeBucket(n.ID) + "\x00" + norm
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
