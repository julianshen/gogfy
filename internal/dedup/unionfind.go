package dedup

// UnionFind implements the disjoint-set (union-find) data structure.
type UnionFind struct {
	parent map[string]string
	rank   map[string]int
}

// NewUnionFind creates a new UnionFind.
func NewUnionFind() *UnionFind {
	return &UnionFind{
		parent: make(map[string]string),
		rank:   make(map[string]int),
	}
}

// Find returns the representative of the set containing id.
func (uf *UnionFind) Find(id string) string {
	if uf.parent[id] == "" {
		uf.parent[id] = id
		uf.rank[id] = 0
	}
	if uf.parent[id] != id {
		uf.parent[id] = uf.Find(uf.parent[id])
	}
	return uf.parent[id]
}

// Union merges the sets containing a and b.
func (uf *UnionFind) Union(a, b string) {
	ra := uf.Find(a)
	rb := uf.Find(b)
	if ra == rb {
		return
	}
	if uf.rank[ra] < uf.rank[rb] {
		ra, rb = rb, ra
	}
	uf.parent[rb] = ra
	if uf.rank[ra] == uf.rank[rb] {
		uf.rank[ra]++
	}
}

// Components returns all disjoint sets as slices of member IDs.
func (uf *UnionFind) Components() map[string][]string {
	groups := make(map[string][]string)
	for id := range uf.parent {
		root := uf.Find(id)
		groups[root] = append(groups[root], id)
	}
	return groups
}
