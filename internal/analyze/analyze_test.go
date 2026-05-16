package analyze

import (
	"fmt"
	"testing"

	"github.com/julianshen/gogfy/internal/schema"
)

func TestGodNodes(t *testing.T) {
	nodes := []schema.Node{
		{ID: "hub"}, {ID: "a"}, {ID: "b"}, {ID: "c"},
	}
	edges := []schema.Edge{
		{Source: "hub", Target: "a"},
		{Source: "hub", Target: "b"},
		{Source: "hub", Target: "c"},
	}
	a := NewAnalyzer()
	report := a.Analyze(nodes, edges)
	if len(report.GodNodes) == 0 {
		t.Fatal("expected god nodes")
	}
	if report.GodNodes[0].ID != "hub" {
		t.Fatalf("expected hub, got %s", report.GodNodes[0].ID)
	}
	// hub has degree 3, a/b/c have degree 1
	// top 20% of 4 connected = 0 (rounded down), but at least 1
	// So only hub should be a god node
	if len(report.GodNodes) != 1 {
		t.Fatalf("expected 1 god node, got %d", len(report.GodNodes))
	}
}

func TestGodNodesMultiple(t *testing.T) {
	nodes := []schema.Node{
		{ID: "hub"}, {ID: "hub2"}, {ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"},
	}
	edges := []schema.Edge{
		{Source: "hub", Target: "a"},
		{Source: "hub", Target: "b"},
		{Source: "hub", Target: "c"},
		{Source: "hub2", Target: "a"},
		{Source: "hub2", Target: "b"},
		{Source: "hub2", Target: "d"},
	}
	a := NewAnalyzer()
	report := a.Analyze(nodes, edges)
	// 6 connected nodes, top 20% = 1, so only hub (degree 3) should be god node
	if len(report.GodNodes) != 1 {
		t.Fatalf("expected 1 god node, got %d", len(report.GodNodes))
	}
	if report.GodNodes[0].ID != "hub" {
		t.Fatalf("expected hub first, got %s", report.GodNodes[0].ID)
	}
}

func TestGodNodesIsolatedExcluded(t *testing.T) {
	nodes := []schema.Node{
		{ID: "hub"}, {ID: "a"}, {ID: "isolated"},
	}
	edges := []schema.Edge{
		{Source: "hub", Target: "a"},
	}
	a := NewAnalyzer()
	report := a.Analyze(nodes, edges)
	for _, n := range report.GodNodes {
		if n.ID == "isolated" {
			t.Fatal("isolated node should not be a god node")
		}
	}
}

func TestSurprisingLinks(t *testing.T) {
	nodes := []schema.Node{
		{ID: "a", Community: "A"},
		{ID: "b", Community: "B"},
		{ID: "c", Community: "A"},
	}
	edges := []schema.Edge{
		{Source: "a", Target: "c"},
		{Source: "a", Target: "b"},
	}
	a := NewAnalyzer()
	report := a.Analyze(nodes, edges)
	if len(report.SurprisingLinks) != 1 {
		t.Fatalf("expected 1 surprising link, got %d", len(report.SurprisingLinks))
	}
	if report.SurprisingLinks[0].Source != "a" || report.SurprisingLinks[0].Target != "b" {
		t.Fatalf("expected a->b surprising link, got %s->%s", report.SurprisingLinks[0].Source, report.SurprisingLinks[0].Target)
	}
}

func TestSurprisingLinksEmptyCommunity(t *testing.T) {
	nodes := []schema.Node{
		{ID: "a", Community: "A"},
		{ID: "b", Community: ""},
		{ID: "c", Community: "A"},
	}
	edges := []schema.Edge{
		{Source: "a", Target: "b"},
		{Source: "a", Target: "c"},
	}
	a := NewAnalyzer()
	report := a.Analyze(nodes, edges)
	if len(report.SurprisingLinks) != 0 {
		t.Fatalf("expected 0 surprising links with empty community, got %d", len(report.SurprisingLinks))
	}
}

func TestSurprisingLinksSameCommunity(t *testing.T) {
	nodes := []schema.Node{
		{ID: "a", Community: "A"},
		{ID: "b", Community: "A"},
	}
	edges := []schema.Edge{
		{Source: "a", Target: "b"},
	}
	a := NewAnalyzer()
	report := a.Analyze(nodes, edges)
	if len(report.SurprisingLinks) != 0 {
		t.Fatalf("expected 0 surprising links within same community, got %d", len(report.SurprisingLinks))
	}
}

func TestExplorationQuestions(t *testing.T) {
	nodes := []schema.Node{
		{ID: "hub", Label: "Hub", Community: "A"},
		{ID: "a", Label: "A", Community: "A"},
		{ID: "b", Label: "B", Community: "B"},
	}
	edges := []schema.Edge{
		{Source: "hub", Target: "a"},
		{Source: "hub", Target: "b"},
	}
	a := NewAnalyzer()
	report := a.Analyze(nodes, edges)
	if len(report.ExplorationQuestions) == 0 {
		t.Fatal("expected exploration questions")
	}
	foundHub := false
	foundCommunity := false
	for _, q := range report.ExplorationQuestions {
		if q == "What is the role of Hub?" {
			foundHub = true
		}
		if q == "Why does A connect to B?" {
			foundCommunity = true
		}
	}
	if !foundHub {
		t.Fatalf("expected question about Hub, got %v", report.ExplorationQuestions)
	}
	if !foundCommunity {
		t.Fatalf("expected community pair question, got %v", report.ExplorationQuestions)
	}
}

func TestExplorationQuestionsNoSurprisingLinks(t *testing.T) {
	nodes := []schema.Node{
		{ID: "hub", Label: "Hub", Community: "A"},
		{ID: "a", Label: "A", Community: "A"},
	}
	edges := []schema.Edge{
		{Source: "hub", Target: "a"},
	}
	a := NewAnalyzer()
	report := a.Analyze(nodes, edges)
	for _, q := range report.ExplorationQuestions {
		if q == "Why does A connect to A?" {
			t.Fatal("should not generate community pair question for same community")
		}
	}
}

func TestConfidenceSummaryCountsEdgesByLevel(t *testing.T) {
	nodes := []schema.Node{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	edges := []schema.Edge{
		{Source: "a", Target: "b", Confidence: schema.Extracted},
		{Source: "a", Target: "c", Confidence: schema.Extracted},
		{Source: "b", Target: "c", Confidence: schema.Inferred},
		{Source: "c", Target: "a", Confidence: schema.Ambiguous},
	}
	report := NewAnalyzer().Analyze(nodes, edges)
	if got := report.ConfidenceSummary[schema.Extracted]; got != 2 {
		t.Fatalf("EXTRACTED count: got %d, want 2", got)
	}
	if got := report.ConfidenceSummary[schema.Inferred]; got != 1 {
		t.Fatalf("INFERRED count: got %d, want 1", got)
	}
	if got := report.ConfidenceSummary[schema.Ambiguous]; got != 1 {
		t.Fatalf("AMBIGUOUS count: got %d, want 1", got)
	}
}

func TestSurprisingLinksRankedByInverseExpectedness(t *testing.T) {
	// hub is connected to many; leaves are not. An edge between two leaves
	// crossing communities is more surprising than an edge involving the hub.
	nodes := []schema.Node{
		{ID: "hub", Community: "A"},
		{ID: "fillerA1", Community: "A"}, {ID: "fillerA2", Community: "A"}, {ID: "fillerA3", Community: "A"},
		{ID: "leafA", Community: "A"},
		{ID: "hubX", Community: "B"},
		{ID: "fillerB1", Community: "B"}, {ID: "fillerB2", Community: "B"}, {ID: "fillerB3", Community: "B"},
		{ID: "leafB", Community: "B"},
	}
	edges := []schema.Edge{
		// inflate hub/hubX degrees with many intra-community edges
		{Source: "hub", Target: "fillerA1"},
		{Source: "hub", Target: "fillerA2"},
		{Source: "hub", Target: "fillerA3"},
		{Source: "hub", Target: "leafA"},
		{Source: "hubX", Target: "fillerB1"},
		{Source: "hubX", Target: "fillerB2"},
		{Source: "hubX", Target: "fillerB3"},
		{Source: "hubX", Target: "leafB"},
		// cross-community candidates (both surprising)
		{Source: "hub", Target: "hubX"},    // hub-to-hub: low surprise (high degrees)
		{Source: "leafA", Target: "leafB"}, // leaf-to-leaf: high surprise (low degrees)
	}
	report := NewAnalyzer().Analyze(nodes, edges)
	if len(report.SurprisingLinks) < 2 {
		t.Fatalf("expected ≥2 surprising links, got %d", len(report.SurprisingLinks))
	}
	first := report.SurprisingLinks[0]
	if first.Source != "leafA" || first.Target != "leafB" {
		t.Fatalf("expected leafA->leafB ranked first, got %s->%s", first.Source, first.Target)
	}
	last := report.SurprisingLinks[len(report.SurprisingLinks)-1]
	if last.Source != "hub" || last.Target != "hubX" {
		t.Fatalf("expected hub->hubX ranked last (lowest surprise), got %s->%s", last.Source, last.Target)
	}
}

func TestSurprisingLinksCappedAtMax(t *testing.T) {
	nodes := []schema.Node{}
	edges := []schema.Edge{}
	// 25 cross-community edges; cap should limit to 10.
	for i := 0; i < 25; i++ {
		src := fmt.Sprintf("src%d", i)
		dst := fmt.Sprintf("dst%d", i)
		nodes = append(nodes,
			schema.Node{ID: src, Community: "A"},
			schema.Node{ID: dst, Community: "B"},
		)
		edges = append(edges, schema.Edge{Source: src, Target: dst})
	}
	report := NewAnalyzer().Analyze(nodes, edges)
	if got := len(report.SurprisingLinks); got > 10 {
		t.Fatalf("surprising links not capped: got %d, want ≤10", got)
	}
}

func TestAnalyzeEmpty(t *testing.T) {
	a := NewAnalyzer()
	report := a.Analyze([]schema.Node{}, []schema.Edge{})
	if len(report.GodNodes) != 0 {
		t.Fatalf("expected 0 god nodes, got %d", len(report.GodNodes))
	}
	if len(report.SurprisingLinks) != 0 {
		t.Fatalf("expected 0 surprising links, got %d", len(report.SurprisingLinks))
	}
	// Empty graph now surfaces a single no-signal prompt (covered by
	// TestQuestionsNoSignalForEmptyGraph); pin that count here so the
	// "no other categories fire" contract stays explicit.
	if got := len(report.ExplorationQuestions); got != 1 {
		t.Fatalf("expected 1 (no-signal) question, got %d: %v", got, report.ExplorationQuestions)
	}
}

func TestQuestionsAmbiguousEdge(t *testing.T) {
	nodes := []schema.Node{
		{ID: "a", Label: "Auth", Community: "1"},
		{ID: "b", Label: "Billing", Community: "2"},
	}
	edges := []schema.Edge{
		{Source: "a", Target: "b", Relation: "calls", Confidence: schema.Ambiguous},
	}
	r := NewAnalyzer().Analyze(nodes, edges)
	if !containsSubstr(r.ExplorationQuestions, "Auth") || !containsSubstr(r.ExplorationQuestions, "Billing") || !containsSubstr(r.ExplorationQuestions, "calls") {
		t.Fatalf("expected ambiguous-edge question naming both endpoints + relation, got %v", r.ExplorationQuestions)
	}
	if !containsSubstr(r.ExplorationQuestions, "ambiguous") && !containsSubstr(r.ExplorationQuestions, "accurate") && !containsSubstr(r.ExplorationQuestions, "correct") {
		t.Fatalf("ambiguous-edge question should flag uncertainty: %v", r.ExplorationQuestions)
	}
}

func TestQuestionsVerifyInferred(t *testing.T) {
	nodes := []schema.Node{
		{ID: "a", Label: "Cache", Community: "1"},
		{ID: "b", Label: "Worker", Community: "2"},
	}
	edges := []schema.Edge{
		{Source: "a", Target: "b", Relation: "depends_on", Confidence: schema.Inferred},
	}
	r := NewAnalyzer().Analyze(nodes, edges)
	if !containsSubstr(r.ExplorationQuestions, "Verify") && !containsSubstr(r.ExplorationQuestions, "verify") {
		t.Fatalf("verify-inferred question should ask user to verify: %v", r.ExplorationQuestions)
	}
	if !containsSubstr(r.ExplorationQuestions, "Cache") || !containsSubstr(r.ExplorationQuestions, "Worker") {
		t.Fatalf("verify-inferred question should name endpoints: %v", r.ExplorationQuestions)
	}
}

func TestQuestionsIsolatedNodes(t *testing.T) {
	nodes := []schema.Node{
		{ID: "a", Label: "Connected", Community: "1"},
		{ID: "b", Label: "Other", Community: "1"},
		{ID: "iso", Label: "OrphanThing", Community: "2"},
	}
	edges := []schema.Edge{{Source: "a", Target: "b"}}
	r := NewAnalyzer().Analyze(nodes, edges)
	if !containsSubstr(r.ExplorationQuestions, "OrphanThing") {
		t.Fatalf("expected isolated-node question naming OrphanThing, got %v", r.ExplorationQuestions)
	}
	if !containsSubstr(r.ExplorationQuestions, "isolated") && !containsSubstr(r.ExplorationQuestions, "no connections") && !containsSubstr(r.ExplorationQuestions, "unconnected") {
		t.Fatalf("isolated-node question should flag isolation: %v", r.ExplorationQuestions)
	}
}

func TestQuestionsLowCohesion(t *testing.T) {
	// Community "1": 10 members, 1 intra-edge → density 1/45 ≈ 0.022, below
	// the 0.05 threshold. Community "2": 3 members, fully connected.
	nodes := []schema.Node{}
	for i := 0; i < 10; i++ {
		nodes = append(nodes, schema.Node{
			ID: fmt.Sprintf("a%d", i), Label: fmt.Sprintf("A%d", i), Community: "1",
		})
	}
	nodes = append(nodes,
		schema.Node{ID: "x", Label: "X", Community: "2"},
		schema.Node{ID: "y", Label: "Y", Community: "2"},
		schema.Node{ID: "z", Label: "Z", Community: "2"},
	)
	edges := []schema.Edge{
		{Source: "a0", Target: "a1"},
		{Source: "x", Target: "y"},
		{Source: "y", Target: "z"},
		{Source: "x", Target: "z"},
	}
	r := NewAnalyzer().Analyze(nodes, edges)
	if !containsSubstr(r.ExplorationQuestions, "cohesion") && !containsSubstr(r.ExplorationQuestions, "loosely") && !containsSubstr(r.ExplorationQuestions, "split") {
		t.Fatalf("expected low-cohesion question for community 1, got %v", r.ExplorationQuestions)
	}
}

func TestQuestionsNoSignalForEmptyGraph(t *testing.T) {
	r := NewAnalyzer().Analyze(nil, nil)
	if len(r.ExplorationQuestions) == 0 {
		t.Fatal("empty graph should still surface a no-signal question")
	}
	if !containsSubstr(r.ExplorationQuestions, "no relationships") && !containsSubstr(r.ExplorationQuestions, "No relationships") && !containsSubstr(r.ExplorationQuestions, "corpus") {
		t.Fatalf("no-signal question should hint at empty corpus: %v", r.ExplorationQuestions)
	}
}

func TestQuestionsNoSignalForEdgelessGraph(t *testing.T) {
	// Nodes but no edges — also a no-signal case (likely a parser failure).
	nodes := []schema.Node{
		{ID: "a", Label: "A", Community: "1"},
		{ID: "b", Label: "B", Community: "1"},
	}
	r := NewAnalyzer().Analyze(nodes, nil)
	if !containsSubstr(r.ExplorationQuestions, "no relationships") && !containsSubstr(r.ExplorationQuestions, "No relationships") && !containsSubstr(r.ExplorationQuestions, "extraction") {
		t.Fatalf("edgeless graph should surface a no-signal question, got %v", r.ExplorationQuestions)
	}
}

func TestQuestionsCappedAtMax(t *testing.T) {
	// Pin the cap: many ambiguous edges shouldn't blow past the limit.
	nodes := make([]schema.Node, 0, 40)
	edges := make([]schema.Edge, 0, 20)
	for i := 0; i < 20; i++ {
		nodes = append(nodes,
			schema.Node{ID: fmt.Sprintf("s%d", i), Label: fmt.Sprintf("S%d", i), Community: "1"},
			schema.Node{ID: fmt.Sprintf("t%d", i), Label: fmt.Sprintf("T%d", i), Community: "2"},
		)
		edges = append(edges, schema.Edge{
			Source: fmt.Sprintf("s%d", i), Target: fmt.Sprintf("t%d", i),
			Relation: "calls", Confidence: schema.Ambiguous,
		})
	}
	r := NewAnalyzer().Analyze(nodes, edges)
	if len(r.ExplorationQuestions) > MaxExplorationQuestions {
		t.Fatalf("over cap %d: got %d", MaxExplorationQuestions, len(r.ExplorationQuestions))
	}
}

func containsSubstr(qs []string, sub string) bool {
	for _, q := range qs {
		if len(q) >= len(sub) && (q == sub || indexOf(q, sub) >= 0) {
			return true
		}
	}
	return false
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestQuestionsAmbiguousPerCategoryBudget(t *testing.T) {
	// 4 ambiguous edges → exactly 2 questions (per-category budget = 2).
	// Pins the budget against future refactors that might remove the cap.
	nodes := []schema.Node{}
	edges := []schema.Edge{}
	for i := 0; i < 4; i++ {
		nodes = append(nodes,
			schema.Node{ID: fmt.Sprintf("s%d", i), Label: fmt.Sprintf("S%d", i), Community: "1"},
			schema.Node{ID: fmt.Sprintf("t%d", i), Label: fmt.Sprintf("T%d", i), Community: "2"},
		)
		edges = append(edges, schema.Edge{
			Source: fmt.Sprintf("s%d", i), Target: fmt.Sprintf("t%d", i),
			Relation: "calls", Confidence: schema.Ambiguous,
		})
	}
	r := NewAnalyzer().Analyze(nodes, edges)
	count := 0
	for _, q := range r.ExplorationQuestions {
		if indexOf(q, "ambiguous edge") >= 0 {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("per-category budget broken: got %d ambiguous questions, want 2; full: %v", count, r.ExplorationQuestions)
	}
}

func TestQuestionsBlankRelationFallback(t *testing.T) {
	// Extractor left Relation blank — output must still read naturally
	// (no "Foo  Bar" double-space) by substituting "relates_to".
	nodes := []schema.Node{
		{ID: "a", Label: "Foo", Community: "1"},
		{ID: "b", Label: "Bar", Community: "2"},
	}
	edges := []schema.Edge{{Source: "a", Target: "b", Confidence: schema.Inferred}}
	r := NewAnalyzer().Analyze(nodes, edges)
	if !containsSubstr(r.ExplorationQuestions, "relates_to") {
		t.Fatalf("blank Relation should fall back to relates_to: %v", r.ExplorationQuestions)
	}
	for _, q := range r.ExplorationQuestions {
		if indexOf(q, "Foo  Bar") >= 0 || indexOf(q, "does Foo  ") >= 0 {
			t.Fatalf("double-space artifact from blank Relation: %q", q)
		}
	}
}

func TestQuestionsDeterministicAmbiguousOrder(t *testing.T) {
	// Same input run twice must produce identical question lists, even
	// when the candidate count exceeds the budget so truncation kicks in.
	nodes := []schema.Node{}
	edges := []schema.Edge{}
	for i := 0; i < 5; i++ {
		nodes = append(nodes,
			schema.Node{ID: fmt.Sprintf("s%d", i), Label: fmt.Sprintf("S%d", i), Community: "1"},
			schema.Node{ID: fmt.Sprintf("t%d", i), Label: fmt.Sprintf("T%d", i), Community: "2"},
		)
		edges = append(edges, schema.Edge{
			Source: fmt.Sprintf("s%d", i), Target: fmt.Sprintf("t%d", i),
			Relation: "calls", Confidence: schema.Ambiguous,
		})
	}
	a := NewAnalyzer()
	first := a.Analyze(nodes, edges).ExplorationQuestions
	for i := 0; i < 5; i++ {
		got := a.Analyze(nodes, edges).ExplorationQuestions
		if len(got) != len(first) {
			t.Fatalf("run %d length drift: %d vs %d", i, len(got), len(first))
		}
		for j := range got {
			if got[j] != first[j] {
				t.Fatalf("run %d differs at %d: %q vs %q", i, j, got[j], first[j])
			}
		}
	}
}

func TestLowCohesionUndirectedDedup(t *testing.T) {
	// Reciprocal extraction (A→B + B→A) must not double-count toward
	// intra-density, which would mask a genuinely sparse community.
	nodes := []schema.Node{}
	for i := 0; i < 10; i++ {
		nodes = append(nodes, schema.Node{
			ID: fmt.Sprintf("a%d", i), Label: fmt.Sprintf("A%d", i), Community: "1",
		})
	}
	// 1 unique intra-edge represented twice (forward + reverse).
	// Without dedup, intra=2/45=0.044 still below 0.05 → false negative.
	// With reciprocal pair counted once, intra=1/45=0.022 → low cohesion.
	// Sharpen the case: 2 unique pairs each represented twice → without
	// dedup intra=4/45=0.088 (above threshold, would miss); with dedup
	// intra=2/45=0.044 (below).
	edges := []schema.Edge{
		{Source: "a0", Target: "a1"},
		{Source: "a1", Target: "a0"},
		{Source: "a2", Target: "a3"},
		{Source: "a3", Target: "a2"},
	}
	r := NewAnalyzer().Analyze(nodes, edges)
	if !containsSubstr(r.ExplorationQuestions, "low cohesion") {
		t.Fatalf("reciprocal-edge dedup broken: low-cohesion question missing for community with density 2/45: %v", r.ExplorationQuestions)
	}
}

func TestLowCohesionMinMembersGate(t *testing.T) {
	// 4-member community is below minLowCohesionMembers (5) — must
	// NOT fire even with zero intra-edges, because the density math is
	// dominated by integer rounding at that size.
	nodes := []schema.Node{
		{ID: "a", Label: "A", Community: "1"},
		{ID: "b", Label: "B", Community: "1"},
		{ID: "c", Label: "C", Community: "1"},
		{ID: "d", Label: "D", Community: "1"},
	}
	r := NewAnalyzer().Analyze(nodes, []schema.Edge{{Source: "a", Target: "b"}})
	for _, q := range r.ExplorationQuestions {
		if indexOf(q, "low cohesion") >= 0 {
			t.Fatalf("community below minLowCohesionMembers should be skipped: %v", r.ExplorationQuestions)
		}
	}
}

func TestSurprisingLinksCrossFileTypeRanksHigher(t *testing.T) {
	// Two cross-community edges with same degree shape, but one bridges
	// code↔document (notable) and the other stays within code↔code.
	// Composite scoring must put the cross-file-type edge first.
	nodes := []schema.Node{
		{ID: "code1", Community: "A", FileType: schema.FileTypeCode, SourceFile: "a/main.go"},
		{ID: "code2", Community: "B", FileType: schema.FileTypeCode, SourceFile: "b/util.go"},
		{ID: "code3", Community: "A", FileType: schema.FileTypeCode, SourceFile: "a/lib.go"},
		{ID: "doc1", Community: "B", FileType: schema.FileTypeDocument, SourceFile: "b/README.md"},
	}
	edges := []schema.Edge{
		{Source: "code1", Target: "code2", Relation: "calls", Confidence: schema.Extracted}, // code↔code
		{Source: "code3", Target: "doc1", Relation: "mentions", Confidence: schema.Extracted}, // code↔doc
	}
	r := NewAnalyzer().Analyze(nodes, edges)
	if len(r.SurprisingLinks) < 2 {
		t.Fatalf("expected 2 surprising links, got %d", len(r.SurprisingLinks))
	}
	first := r.SurprisingLinks[0]
	if !(first.Source == "code3" && first.Target == "doc1") {
		t.Fatalf("cross file-type edge should rank first, got %s->%s", first.Source, first.Target)
	}
}

func TestSurprisingLinksCrossRepoRanksHigher(t *testing.T) {
	// Top-level dir is the repo proxy. Edge bridging repos a/ ↔ b/ is
	// more surprising than one inside a/.
	nodes := []schema.Node{
		{ID: "a1", Community: "X", SourceFile: "a/foo.go", FileType: schema.FileTypeCode},
		{ID: "a2", Community: "Y", SourceFile: "a/bar.go", FileType: schema.FileTypeCode},
		{ID: "a3", Community: "X", SourceFile: "a/baz.go", FileType: schema.FileTypeCode},
		{ID: "b1", Community: "Y", SourceFile: "b/qux.go", FileType: schema.FileTypeCode},
	}
	edges := []schema.Edge{
		{Source: "a1", Target: "a2", Relation: "calls", Confidence: schema.Extracted}, // intra-repo
		{Source: "a3", Target: "b1", Relation: "calls", Confidence: schema.Extracted}, // cross-repo
	}
	r := NewAnalyzer().Analyze(nodes, edges)
	if r.SurprisingLinks[0].Source != "a3" {
		t.Fatalf("cross-repo edge should rank first, got %+v", r.SurprisingLinks)
	}
}

func TestSurprisingLinksAmbiguousRanksHigherThanExtracted(t *testing.T) {
	// Same topology, only confidence differs.
	nodes := []schema.Node{
		{ID: "a", Community: "X", SourceFile: "x.go", FileType: schema.FileTypeCode},
		{ID: "b", Community: "Y", SourceFile: "y.go", FileType: schema.FileTypeCode},
		{ID: "c", Community: "X", SourceFile: "z.go", FileType: schema.FileTypeCode},
		{ID: "d", Community: "Y", SourceFile: "w.go", FileType: schema.FileTypeCode},
	}
	edges := []schema.Edge{
		{Source: "a", Target: "b", Relation: "calls", Confidence: schema.Extracted},
		{Source: "c", Target: "d", Relation: "calls", Confidence: schema.Ambiguous},
	}
	r := NewAnalyzer().Analyze(nodes, edges)
	first := r.SurprisingLinks[0]
	if !(first.Source == "c" && first.Confidence == schema.Ambiguous) {
		t.Fatalf("AMBIGUOUS should rank first, got %+v", first)
	}
}

func TestSurprisingLinksCrossLanguageInferredCallsDowngraded(t *testing.T) {
	// Cross-language INFERRED `calls` edges are usually resolver
	// pollution. The confidence bonus is zeroed so they don't dominate.
	// LangIDs on both endpoints so the lang prefix is parsable.
	nodes := []schema.Node{
		{ID: "py:function:/main.py:m:f1", Community: "X", SourceFile: "main.py", FileType: schema.FileTypeCode},
		{ID: "go:function:/main.go:m:f2", Community: "Y", SourceFile: "main.go", FileType: schema.FileTypeCode},
		{ID: "go:function:/a.go:m:f3", Community: "X", SourceFile: "a.go", FileType: schema.FileTypeCode},
		{ID: "go:function:/b.go:m:f4", Community: "Y", SourceFile: "b.go", FileType: schema.FileTypeCode},
	}
	edges := []schema.Edge{
		{Source: "py:function:/main.py:m:f1", Target: "go:function:/main.go:m:f2", Relation: "calls", Confidence: schema.Inferred},
		{Source: "go:function:/a.go:m:f3", Target: "go:function:/b.go:m:f4", Relation: "uses", Confidence: schema.Extracted},
	}
	r := NewAnalyzer().Analyze(nodes, edges)
	if r.SurprisingLinks[0].Confidence == schema.Inferred && r.SurprisingLinks[0].Relation == "calls" {
		t.Fatalf("cross-language INFERRED calls should be downgraded, not surfaced first: %+v", r.SurprisingLinks)
	}
}

func TestSurprisingLinksPeripheralToHubBonus(t *testing.T) {
	// Hub has degree 5 (4 within community + 1 cross). Leaf has degree 1
	// (just the cross-edge). The peripheral→hub bonus fires.
	nodes := []schema.Node{
		{ID: "hub", Community: "X", SourceFile: "hub.go", FileType: schema.FileTypeCode},
		{ID: "leaf", Community: "Y", SourceFile: "leaf.go", FileType: schema.FileTypeCode},
		// fillers to inflate hub's degree
		{ID: "f1", Community: "X", SourceFile: "f1.go", FileType: schema.FileTypeCode},
		{ID: "f2", Community: "X", SourceFile: "f2.go", FileType: schema.FileTypeCode},
		{ID: "f3", Community: "X", SourceFile: "f3.go", FileType: schema.FileTypeCode},
		{ID: "f4", Community: "X", SourceFile: "f4.go", FileType: schema.FileTypeCode},
		// neutral cross-community pair (high-degree on both sides, no peripheral)
		{ID: "x", Community: "X", SourceFile: "x.go", FileType: schema.FileTypeCode},
		{ID: "y", Community: "Y", SourceFile: "y.go", FileType: schema.FileTypeCode},
		{ID: "x2", Community: "X", SourceFile: "x2.go", FileType: schema.FileTypeCode},
		{ID: "y2", Community: "Y", SourceFile: "y2.go", FileType: schema.FileTypeCode},
		{ID: "x3", Community: "X", SourceFile: "x3.go", FileType: schema.FileTypeCode},
		{ID: "y3", Community: "Y", SourceFile: "y3.go", FileType: schema.FileTypeCode},
	}
	edges := []schema.Edge{
		// hub's intra-community edges (degree 4 on hub)
		{Source: "hub", Target: "f1"}, {Source: "hub", Target: "f2"},
		{Source: "hub", Target: "f3"}, {Source: "hub", Target: "f4"},
		// cross-community: peripheral→hub
		{Source: "leaf", Target: "hub", Relation: "calls", Confidence: schema.Extracted},
		// x has intra-community fillers to bring degree up
		{Source: "x", Target: "x2"}, {Source: "x", Target: "x3"},
		{Source: "y", Target: "y2"}, {Source: "y", Target: "y3"},
		{Source: "x", Target: "y", Relation: "calls", Confidence: schema.Extracted},
	}
	r := NewAnalyzer().Analyze(nodes, edges)
	if len(r.SurprisingLinks) < 1 {
		t.Fatal("expected surprising links")
	}
	first := r.SurprisingLinks[0]
	// The peripheral→hub edge should rank above the symmetric x↔y edge.
	if !((first.Source == "leaf" && first.Target == "hub") || (first.Source == "hub" && first.Target == "leaf")) {
		t.Fatalf("peripheral→hub edge should rank first, got %s->%s", first.Source, first.Target)
	}
}

func TestSurpriseScoreSemanticallySimilarMultiplier(t *testing.T) {
	// Test the ×1.5 multiplier directly on surpriseScore so we pin the
	// numeric output, not just ranking. Score before multiplier: 1 (EXTRACTED)
	// + 2 (cross-file-type) + 2 (cross-repo) = 5 → ×1.5 truncated = 7.
	src := schema.Node{ID: "py:function:/a/x.py:m:a", FileType: schema.FileTypeCode, SourceFile: "a/x.py", Community: "X"}
	dst := schema.Node{ID: "py:function:/b/y.py:m:b", FileType: schema.FileTypeDocument, SourceFile: "b/y.md", Community: "Y"}
	semantic := schema.Edge{Source: src.ID, Target: dst.ID, Relation: "semantically_similar_to", Confidence: schema.Extracted}
	other := schema.Edge{Source: src.ID, Target: dst.ID, Relation: "calls", Confidence: schema.Extracted}
	semScore := surpriseScore(semantic, src, dst, 2, 2)
	otherScore := surpriseScore(other, src, dst, 2, 2)
	if semScore <= otherScore {
		t.Fatalf("semantically_similar_to should boost score; semantic=%d, other=%d", semScore, otherScore)
	}
	// Pin the exact value so a regression on the truncation formula fails.
	if semScore != 7 {
		t.Fatalf("semantically_similar_to score: got %d, want 7 (1+2+2=5; 5*3/2=7)", semScore)
	}
}

func TestSurpriseScoreCombinedBonusesAdditivity(t *testing.T) {
	// One edge hitting confidence + cross-file-type + cross-repo +
	// peripheral→hub. Pin the exact sum so a future refactor that
	// double-counts or drops one component fails the test.
	src := schema.Node{ID: "py:function:/a/x.py:m:leaf", FileType: schema.FileTypeCode, SourceFile: "a/x.py", Community: "X"}
	dst := schema.Node{ID: "py:function:/b/y.py:m:hub", FileType: schema.FileTypeDocument, SourceFile: "b/y.md", Community: "Y"}
	e := schema.Edge{Source: src.ID, Target: dst.ID, Relation: "calls", Confidence: schema.Ambiguous}
	// du=1 (leaf), dv=10 (hub) → peripheral→hub fires
	got := surpriseScore(e, src, dst, 1, 10)
	// 3 (AMBIGUOUS) + 2 (cross-file-type) + 2 (cross-repo) + 1 (peripheral→hub) = 8
	if got != 8 {
		t.Fatalf("combined bonuses: got %d, want 8 (3+2+2+1)", got)
	}
}

func TestSurpriseScoreNoCrossRepoBonusWhenOneSourceFileEmpty(t *testing.T) {
	// Regression: previously topLevelDir("") == "" and topLevelDir("a/x")
	// == "a", so the comparison `"a" != ""` wrongly fired +2 for an edge
	// whose endpoints had asymmetric SourceFile classification.
	src := schema.Node{ID: "x", SourceFile: "a/x.go", FileType: schema.FileTypeCode, Community: "X"}
	dst := schema.Node{ID: "y", SourceFile: "", FileType: schema.FileTypeCode, Community: "Y"}
	e := schema.Edge{Source: "x", Target: "y", Relation: "calls", Confidence: schema.Extracted}
	got := surpriseScore(e, src, dst, 2, 2)
	// 1 (EXTRACTED) + 0 (FileTypes equal so no cross-file) + 0 (no cross-repo gate) = 1
	if got != 1 {
		t.Fatalf("missing SourceFile must not trigger cross-repo: got %d, want 1", got)
	}
}

func TestSurpriseScoreNoCrossFileTypeBonusWhenOneFileTypeEmpty(t *testing.T) {
	src := schema.Node{ID: "x", FileType: schema.FileTypeCode, SourceFile: "a/x.go", Community: "X"}
	dst := schema.Node{ID: "y", FileType: "", SourceFile: "b/y.go", Community: "Y"}
	e := schema.Edge{Source: "x", Target: "y", Relation: "calls", Confidence: schema.Extracted}
	// 1 (EXTRACTED) + 0 (FileType missing on dst) + 2 (cross-repo) = 3
	if got := surpriseScore(e, src, dst, 2, 2); got != 3 {
		t.Fatalf("missing FileType must skip cross-file-type bonus: got %d, want 3", got)
	}
}

func TestSurprisingLinksTieBreakIsInputOrder(t *testing.T) {
	// Two edges with identical scoring inputs: composite must be equal,
	// tieScore must be equal, and the final tiebreaker (input idx)
	// must preserve the original ordering for byte-stable reports.
	nodes := []schema.Node{
		{ID: "a", Community: "X", SourceFile: "x/1.go", FileType: schema.FileTypeCode},
		{ID: "b", Community: "Y", SourceFile: "x/2.go", FileType: schema.FileTypeCode},
		{ID: "c", Community: "X", SourceFile: "x/3.go", FileType: schema.FileTypeCode},
		{ID: "d", Community: "Y", SourceFile: "x/4.go", FileType: schema.FileTypeCode},
	}
	edges := []schema.Edge{
		{Source: "a", Target: "b", Relation: "calls", Confidence: schema.Extracted},
		{Source: "c", Target: "d", Relation: "calls", Confidence: schema.Extracted},
	}
	r := NewAnalyzer().Analyze(nodes, edges)
	if len(r.SurprisingLinks) != 2 {
		t.Fatalf("expected 2 links, got %d", len(r.SurprisingLinks))
	}
	if r.SurprisingLinks[0].Source != "a" || r.SurprisingLinks[1].Source != "c" {
		t.Fatalf("tie-break order broken: got %+v", r.SurprisingLinks)
	}
}

func TestAnalyzeBridgeNodesSurfaceCrossClusterConnectors(t *testing.T) {
	// Barbell graph: two triangles connected by a single bridge node.
	// The bridge MUST appear in BridgeNodes; the triangle interiors
	// MUST NOT (they sit on no cross-cluster shortest path).
	nodes := []schema.Node{
		{ID: "a1", Label: "A1", Community: "A"},
		{ID: "a2", Label: "A2", Community: "A"},
		{ID: "a3", Label: "A3", Community: "A"},
		{ID: "bridge", Label: "Bridge", Community: "A"},
		{ID: "b1", Label: "B1", Community: "B"},
		{ID: "b2", Label: "B2", Community: "B"},
		{ID: "b3", Label: "B3", Community: "B"},
	}
	edges := []schema.Edge{
		{Source: "a1", Target: "a2"}, {Source: "a2", Target: "a3"}, {Source: "a3", Target: "a1"},
		{Source: "a1", Target: "bridge"},
		{Source: "bridge", Target: "b1"},
		{Source: "b1", Target: "b2"}, {Source: "b2", Target: "b3"}, {Source: "b3", Target: "b1"},
	}
	r := NewAnalyzer().Analyze(nodes, edges)
	if len(r.BridgeNodes) == 0 {
		t.Fatal("expected at least one bridge node")
	}
	if r.BridgeNodes[0].ID != "bridge" {
		t.Fatalf("expected 'bridge' as top BridgeNode, got %s", r.BridgeNodes[0].ID)
	}
}

func TestAnalyzeBridgeNodesEmptyWhenNoStructuralBridges(t *testing.T) {
	// A triangle has zero betweenness everywhere — no bridge nodes.
	nodes := []schema.Node{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	edges := []schema.Edge{
		{Source: "a", Target: "b"},
		{Source: "b", Target: "c"},
		{Source: "c", Target: "a"},
	}
	r := NewAnalyzer().Analyze(nodes, edges)
	if len(r.BridgeNodes) != 0 {
		t.Fatalf("triangle has no bridges; got %+v", r.BridgeNodes)
	}
}

func TestAnalyzeBridgeNodesCappedAtMax(t *testing.T) {
	// Star with 10 leaves around one hub — hub gets all the betweenness;
	// the leaves contribute 0; only 1 bridge surfaces, well under cap.
	// Build a longer chain where every interior node bridges: 12-node
	// line — should be capped to MaxBridgeNodes (3).
	nodes := []schema.Node{}
	edges := []schema.Edge{}
	for i := 0; i < 12; i++ {
		nodes = append(nodes, schema.Node{ID: fmt.Sprintf("n%02d", i), Label: fmt.Sprintf("N%02d", i)})
		if i > 0 {
			edges = append(edges, schema.Edge{
				Source: fmt.Sprintf("n%02d", i-1), Target: fmt.Sprintf("n%02d", i),
			})
		}
	}
	r := NewAnalyzer().Analyze(nodes, edges)
	if len(r.BridgeNodes) > MaxBridgeNodes {
		t.Fatalf("BridgeNodes must be capped to %d, got %d", MaxBridgeNodes, len(r.BridgeNodes))
	}
	if len(r.BridgeNodes) == 0 {
		t.Fatal("line graph should produce some bridge nodes")
	}
}
