package analyze

import (
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
}

func TestSurprisingLinksCappedAtMax(t *testing.T) {
	nodes := []schema.Node{}
	edges := []schema.Edge{}
	// 25 cross-community edges; cap should limit to 10.
	for i := 0; i < 25; i++ {
		idA := "a" + string(rune('A'+i%26))
		idB := "b" + string(rune('A'+i%26))
		nodes = append(nodes,
			schema.Node{ID: idA + "_src", Community: "A"},
			schema.Node{ID: idB + "_dst", Community: "B"},
		)
		edges = append(edges, schema.Edge{Source: idA + "_src", Target: idB + "_dst"})
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
	if len(report.ExplorationQuestions) != 0 {
		t.Fatalf("expected 0 questions, got %d", len(report.ExplorationQuestions))
	}
}
