package cluster

import (
	"sort"

	"github.com/julianshen/gogfy/internal/schema"
)

// DefaultHyperedgeExpansionCap bounds the size of a hyperedge that
// gets clique-expanded. A k-member hyperedge expands to k*(k-1)/2
// pairwise edges; an unbounded cap would let a single 50-member
// hyperedge inject 1225 synthetic edges and dominate the clustering.
// Semantic extraction rarely emits hyperedges above ~6 members, so 8
// is a comfortable ceiling that skips only pathological cases.
const DefaultHyperedgeExpansionCap = 8

// ExpandHyperedges performs clique expansion: each hyperedge becomes
// a set of pairwise edges connecting every pair of its members. This
// is the standard hypergraph→graph reduction for feeding N-ary
// relations into a pairwise community-detection algorithm (Leiden,
// Louvain) that has no native hyperedge support.
//
// The synthetic edges are tagged Relation="co_member" and
// Confidence=Inferred so they're distinguishable from real extracted
// edges if a caller ever inspects them — though the intended use is
// cluster-input-only (NOT persisted to graph.json). Clustering then
// sees "these nodes co-occur in an N-ary relation" as structural
// pull toward the same community.
//
// Hyperedges with fewer than 2 resolvable members produce nothing
// (no pair to connect). Hyperedges larger than cap are skipped
// entirely — see DefaultHyperedgeExpansionCap. A non-positive cap
// disables the size limit.
//
// Output is deterministic: pairs are emitted in sorted (source,
// target) order with source < target lexicographically, and
// duplicate pairs across hyperedges are de-duplicated. Re-running on
// the same input yields byte-identical output, which the clustering
// determinism guarantee depends on.
func ExpandHyperedges(hyperedges []schema.Hyperedge, cap int) []schema.Edge {
	if len(hyperedges) == 0 {
		return nil
	}
	type pair struct{ a, b string }
	seen := map[pair]struct{}{}
	var out []schema.Edge
	for _, h := range hyperedges {
		// Dedup + sort members so the pair enumeration is stable and
		// a member repeated within one hyperedge doesn't self-loop.
		members := uniqueSorted(h.Members)
		if len(members) < 2 {
			continue
		}
		if cap > 0 && len(members) > cap {
			continue
		}
		for i := 0; i < len(members); i++ {
			for j := i + 1; j < len(members); j++ {
				a, b := members[i], members[j]
				key := pair{a, b}
				if _, dup := seen[key]; dup {
					continue
				}
				seen[key] = struct{}{}
				out = append(out, schema.Edge{
					Source:     a,
					Target:     b,
					Relation:   "co_member",
					Confidence: schema.Inferred,
				})
			}
		}
	}
	// Sort the final list so output is byte-stable regardless of
	// hyperedge input order.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Source != out[j].Source {
			return out[i].Source < out[j].Source
		}
		return out[i].Target < out[j].Target
	})
	return out
}

// uniqueSorted returns the distinct elements of in, sorted. Empty
// strings are dropped (a hyperedge member that didn't resolve to a
// node ID).
func uniqueSorted(in []string) []string {
	set := make(map[string]struct{}, len(in))
	for _, s := range in {
		if s != "" {
			set[s] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
