package cluster

import (
	"testing"

	"github.com/julianshen/gogfy/internal/schema"
)

func TestExpandHyperedgesCliqueShape(t *testing.T) {
	// A 3-member hyperedge → 3 pairwise edges (the complete graph K3).
	hes := []schema.Hyperedge{
		{Members: []string{"a", "b", "c"}, Relation: "co_authored"},
	}
	got := ExpandHyperedges(hes, DefaultHyperedgeExpansionCap)
	if len(got) != 3 {
		t.Fatalf("3-member hyperedge should expand to 3 edges, got %d: %+v", len(got), got)
	}
	// Every synthetic edge is tagged co_member / Inferred so it's
	// distinguishable from real extracted edges.
	for _, e := range got {
		if e.Relation != "co_member" {
			t.Errorf("expected co_member relation, got %q", e.Relation)
		}
		if e.Confidence != schema.Inferred {
			t.Errorf("expected Inferred confidence, got %v", e.Confidence)
		}
	}
	// Verify the actual pairs: a-b, a-c, b-c.
	want := map[string]bool{"a|b": true, "a|c": true, "b|c": true}
	for _, e := range got {
		key := e.Source + "|" + e.Target
		if !want[key] {
			t.Errorf("unexpected pair %s", key)
		}
		delete(want, key)
	}
	if len(want) != 0 {
		t.Errorf("missing pairs: %v", want)
	}
}

func TestExpandHyperedgesDedupesAcrossHyperedges(t *testing.T) {
	// Two hyperedges sharing the pair (a,b) should produce that edge
	// only once — clustering shouldn't double-count the same pair's
	// co-membership.
	hes := []schema.Hyperedge{
		{Members: []string{"a", "b", "c"}},
		{Members: []string{"a", "b", "d"}},
	}
	got := ExpandHyperedges(hes, DefaultHyperedgeExpansionCap)
	count := map[string]int{}
	for _, e := range got {
		count[e.Source+"|"+e.Target]++
	}
	if count["a|b"] != 1 {
		t.Errorf("pair a-b should appear once after dedup, got %d", count["a|b"])
	}
}

func TestExpandHyperedgesSkipsOversized(t *testing.T) {
	// A hyperedge larger than the cap is skipped entirely — it would
	// inject too many synthetic edges and dominate clustering.
	big := make([]string, 20)
	for i := range big {
		big[i] = string(rune('a' + i))
	}
	hes := []schema.Hyperedge{{Members: big}}
	got := ExpandHyperedges(hes, 8)
	if len(got) != 0 {
		t.Errorf("oversized hyperedge should be skipped, got %d edges", len(got))
	}
	// With cap disabled (0), the same hyperedge expands fully:
	// 20*19/2 = 190 edges.
	got = ExpandHyperedges(hes, 0)
	if len(got) != 190 {
		t.Errorf("cap=0 should expand fully (190 edges), got %d", len(got))
	}
}

func TestExpandHyperedgesIgnoresDegenerate(t *testing.T) {
	hes := []schema.Hyperedge{
		{Members: []string{"only-one"}},     // <2 members
		{Members: []string{"", ""}},          // all empty
		{Members: []string{"x", "", "x"}},    // dedups to 1 real member
	}
	if got := ExpandHyperedges(hes, DefaultHyperedgeExpansionCap); len(got) != 0 {
		t.Errorf("degenerate hyperedges should produce no edges, got %+v", got)
	}
}

func TestExpandHyperedgesDeterministic(t *testing.T) {
	// Same input → byte-identical output (clustering determinism
	// depends on this). Members deliberately out of order to exercise
	// the internal sort.
	hes := []schema.Hyperedge{
		{Members: []string{"c", "a", "b"}},
		{Members: []string{"z", "y"}},
	}
	first := ExpandHyperedges(hes, DefaultHyperedgeExpansionCap)
	second := ExpandHyperedges(hes, DefaultHyperedgeExpansionCap)
	if len(first) != len(second) {
		t.Fatalf("length differs across runs: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i] != second[i] {
			t.Errorf("edge %d differs: %+v vs %+v", i, first[i], second[i])
		}
	}
}

func TestExpandHyperedgesPullsMembersIntoSameCommunity(t *testing.T) {
	// End-to-end: four nodes with NO binary edges → raw clustering
	// puts each in its own (singleton) community. A hyperedge
	// spanning all four clique-expands to K4 (the complete graph),
	// which clusters as a single dense community. This is the
	// deterministic case — a fully-connected clique always
	// maximizes modularity as one community.
	nodes := []schema.Node{
		{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"},
	}
	// Raw clustering with no edges: 4 isolated singletons.
	rawClustered, err := NewLouvainClusterer().Cluster(nodes, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := distinctCommunities(rawClustered); got != 4 {
		t.Fatalf("4 edgeless nodes should be 4 singleton communities, got %d", got)
	}

	// Hyperedge {a,b,c,d} → K4 (6 edges) → one community.
	hes := []schema.Hyperedge{{Members: []string{"a", "b", "c", "d"}}}
	expanded := ExpandHyperedges(hes, DefaultHyperedgeExpansionCap)
	if len(expanded) != 6 {
		t.Fatalf("K4 expansion should be 6 edges, got %d", len(expanded))
	}
	bridgedClustered, err := NewLouvainClusterer().Cluster(nodes, expanded)
	if err != nil {
		t.Fatal(err)
	}
	if got := distinctCommunities(bridgedClustered); got != 1 {
		t.Errorf("K4 clique should cluster as 1 community, got %d", got)
	}
}

func distinctCommunities(nodes []schema.Node) int {
	set := map[string]struct{}{}
	for _, n := range nodes {
		set[n.Community] = struct{}{}
	}
	return len(set)
}
