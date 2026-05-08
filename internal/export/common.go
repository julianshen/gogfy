package export

import (
	"sort"

	"github.com/julianshen/gogfy/internal/schema"
)

// sortEdges sorts edges in-place by (Source, Target, Relation) for
// deterministic output. Both GraphML and Cypher exporters need the same
// canonical order so re-emitting the same graph yields a stable artifact
// for diffs and snapshot tests.
func sortEdges(edges []schema.Edge) {
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].Source != edges[j].Source {
			return edges[i].Source < edges[j].Source
		}
		if edges[i].Target != edges[j].Target {
			return edges[i].Target < edges[j].Target
		}
		return edges[i].Relation < edges[j].Relation
	})
}
