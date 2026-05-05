package report

import (
	"github.com/julianshen/gogfy/internal/analyze"
	"github.com/julianshen/gogfy/internal/schema"
	"os"
	"testing"
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
		t.Fatal("output does not match golden file")
	}
}
