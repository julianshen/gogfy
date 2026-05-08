package report

import (
	"os"
	"testing"

	"github.com/julianshen/gogfy/internal/analyze"
	"github.com/julianshen/gogfy/internal/schema"
)

func TestRenderReport(t *testing.T) {
	r := analyze.Report{
		GodNodes:             []schema.Node{{ID: "hub", Label: "Hub"}},
		SurprisingLinks:      []schema.Edge{{Source: "a", Target: "b", Relation: "calls"}},
		ExplorationQuestions: []string{"What does hub do?"},
	}
	out, err := Render(r)
	if err != nil {
		t.Fatal(err)
	}
	golden, _ := os.ReadFile("testdata/golden/GRAPH_REPORT.md")
	if string(out) != string(golden) {
		t.Fatalf("output does not match golden file\n--- got ---\n%s\n--- want ---\n%s", string(out), string(golden))
	}
}

func TestRenderReportEmpty(t *testing.T) {
	r := analyze.Report{}
	out, err := Render(r)
	if err != nil {
		t.Fatal(err)
	}
	want := "# Graph Report\n\n## God Nodes\n_None found_\n\n## Surprising Links\n_None found_\n\n## Exploration Questions\n_None found_\n"
	if string(out) != want {
		t.Fatalf("empty report mismatch\n--- got ---\n%s\n--- want ---\n%s", string(out), want)
	}
}

func TestRenderReportMultipleItems(t *testing.T) {
	r := analyze.Report{
		GodNodes: []schema.Node{
			{ID: "hub", Label: "Hub"},
			{ID: "hub2", Label: "Hub2"},
		},
		SurprisingLinks: []schema.Edge{
			{Source: "a", Target: "b", Relation: "calls"},
			{Source: "c", Target: "d", Relation: "imports"},
		},
		ExplorationQuestions: []string{"Q1?", "Q2?"},
	}
	out, err := Render(r)
	if err != nil {
		t.Fatal(err)
	}
	// Verify all items are present
	output := string(out)
	if !contains(output, "- Hub") {
		t.Fatal("missing Hub")
	}
	if !contains(output, "- Hub2") {
		t.Fatal("missing Hub2")
	}
	if !contains(output, "- a -> b (calls)") {
		t.Fatal("missing a->b")
	}
	if !contains(output, "- c -> d (imports)") {
		t.Fatal("missing c->d")
	}
	if !contains(output, "- Q1?") {
		t.Fatal("missing Q1")
	}
	if !contains(output, "- Q2?") {
		t.Fatal("missing Q2")
	}
}

func TestRenderReportConfidenceSection(t *testing.T) {
	r := analyze.Report{
		ConfidenceSummary: map[schema.Confidence]int{
			schema.Extracted: 12,
			schema.Inferred:  3,
			schema.Ambiguous: 1,
		},
	}
	out, err := Render(r)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !contains(s, "## Confidence") {
		t.Fatalf("missing Confidence section:\n%s", s)
	}
	if !contains(s, "EXTRACTED") || !contains(s, "12") {
		t.Fatalf("missing EXTRACTED count:\n%s", s)
	}
	if !contains(s, "INFERRED") || !contains(s, "3") {
		t.Fatalf("missing INFERRED count:\n%s", s)
	}
	if !contains(s, "AMBIGUOUS") || !contains(s, "1") {
		t.Fatalf("missing AMBIGUOUS count:\n%s", s)
	}
}

func TestRenderReportMarkdownEscaping(t *testing.T) {
	r := analyze.Report{
		GodNodes: []schema.Node{
			{ID: "x", Label: "special_*_chars"},
		},
	}
	out, err := Render(r)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(out), "special\\_\\*\\_chars") {
		t.Fatal("markdown not escaped")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr))
}

func containsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
