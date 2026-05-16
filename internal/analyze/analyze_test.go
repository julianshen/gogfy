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
