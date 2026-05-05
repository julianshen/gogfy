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
