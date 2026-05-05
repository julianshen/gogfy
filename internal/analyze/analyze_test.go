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
	for _, q := range report.ExplorationQuestions {
		if q == "What is the role of Hub?" {
			foundHub = true
		}
	}
	if !foundHub {
		t.Fatalf("expected question about Hub, got %v", report.ExplorationQuestions)
	}
}
