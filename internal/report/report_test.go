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

func TestRenderReportSummarySection(t *testing.T) {
	r := analyze.Report{
		GodNodes: []schema.Node{{ID: "h", Label: "Hub"}},
	}
	opts := Options{
		Nodes: []schema.Node{{ID: "a", Community: "1"}, {ID: "b", Community: "1"}, {ID: "c", Community: "2"}},
		Edges: []schema.Edge{{Source: "a", Target: "b"}, {Source: "b", Target: "c"}},
	}
	out, err := RenderWithOptions(r, opts)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !contains(s, "## Summary") {
		t.Fatal("missing Summary section")
	}
	if !contains(s, "3 node") || !contains(s, "2 edge") || !contains(s, "2 communit") {
		t.Fatalf("summary should report counts: %s", s)
	}
}

func TestRenderReportGraphFreshnessConditional(t *testing.T) {
	r := analyze.Report{}
	withCommit, _ := RenderWithOptions(r, Options{BuiltAtCommit: "abc1234"})
	if !contains(string(withCommit), "## Graph Freshness") || !contains(string(withCommit), "abc1234") {
		t.Fatalf("Graph Freshness section should appear with commit hash: %s", withCommit)
	}
	without, _ := RenderWithOptions(r, Options{})
	if contains(string(without), "## Graph Freshness") {
		t.Fatalf("Graph Freshness should be omitted when no commit: %s", without)
	}
}

func TestRenderReportCommunitiesSection(t *testing.T) {
	opts := Options{
		Nodes: []schema.Node{
			{ID: "a", Label: "Auth", Community: "1"},
			{ID: "b", Label: "Bouncer", Community: "1"},
			{ID: "c", Label: "Cache", Community: "2"},
			{ID: "d", Label: "Disk", Community: "2"},
		},
	}
	out, _ := RenderWithOptions(analyze.Report{}, opts)
	s := string(out)
	if !contains(s, "## Communities") {
		t.Fatal("missing Communities section")
	}
	if !contains(s, "Community 1") || !contains(s, "Community 2") {
		t.Fatalf("missing community headings: %s", s)
	}
}

func TestRenderReportThinCommunityFiltering(t *testing.T) {
	// Single-node "communities" should be hidden by default — they're
	// extraction noise, not real groupings.
	opts := Options{
		Nodes: []schema.Node{
			{ID: "a", Label: "Auth", Community: "big"},
			{ID: "b", Label: "Bouncer", Community: "big"},
			{ID: "c", Label: "Lonely", Community: "thin"},
		},
	}
	out, _ := RenderWithOptions(analyze.Report{}, opts)
	s := string(out)
	if !contains(s, "Community big") {
		t.Fatalf("non-thin community missing: %s", s)
	}
	if contains(s, "Community thin") {
		t.Fatalf("thin community should be filtered: %s", s)
	}
}

func TestRenderReportAmbiguousEdgesSection(t *testing.T) {
	opts := Options{
		Nodes: []schema.Node{{ID: "a", Label: "Auth"}, {ID: "b", Label: "Billing"}},
		Edges: []schema.Edge{
			{Source: "a", Target: "b", Relation: "calls", Confidence: schema.Ambiguous},
			{Source: "a", Target: "b", Relation: "imports", Confidence: schema.Extracted},
		},
	}
	out, _ := RenderWithOptions(analyze.Report{}, opts)
	s := string(out)
	if !contains(s, "## Ambiguous Edges") {
		t.Fatal("missing Ambiguous Edges section")
	}
	if !contains(s, "Auth") || !contains(s, "Billing") || !contains(s, "calls") {
		t.Fatalf("ambiguous edge details missing: %s", s)
	}
	// The Extracted edge must not be listed in the Ambiguous section.
	// A simple-but-imperfect check: section appears only once and the
	// "imports" relation doesn't appear under it.
	if contains(s, "## Ambiguous Edges\n- Auth -> Billing (imports)") {
		t.Fatal("Extracted edge leaked into Ambiguous Edges section")
	}
}

func TestRenderReportKnowledgeGapsSection(t *testing.T) {
	opts := Options{
		Nodes: []schema.Node{
			{ID: "a", Label: "Connected", Community: "1"},
			{ID: "b", Label: "AlsoConnected", Community: "1"},
			{ID: "iso", Label: "Orphan", Community: "2"},
		},
		Edges: []schema.Edge{
			{Source: "a", Target: "b", Confidence: schema.Ambiguous},
		},
	}
	out, _ := RenderWithOptions(analyze.Report{}, opts)
	s := string(out)
	if !contains(s, "## Knowledge Gaps") {
		t.Fatal("missing Knowledge Gaps section")
	}
	if !contains(s, "1 isolated") && !contains(s, "isolated node") {
		t.Fatalf("isolated node count missing: %s", s)
	}
	if !contains(s, "1 ambiguous") && !contains(s, "ambiguous edge") {
		t.Fatalf("ambiguous count missing: %s", s)
	}
}

func TestRenderReportCommunityHubsSection(t *testing.T) {
	opts := Options{
		Nodes: []schema.Node{
			{ID: "a", Label: "A", Community: "1"},
			{ID: "b", Label: "BigB", Community: "1"},
			{ID: "c", Label: "C", Community: "1"},
		},
		Edges: []schema.Edge{
			{Source: "b", Target: "a"},
			{Source: "b", Target: "c"},
		},
	}
	out, _ := RenderWithOptions(analyze.Report{}, opts)
	s := string(out)
	if !contains(s, "## Community Hubs") {
		t.Fatal("missing Community Hubs section")
	}
	if !contains(s, "BigB") {
		t.Fatalf("highest-degree member missing: %s", s)
	}
}

func TestRenderReportCorpusCheckSection(t *testing.T) {
	opts := Options{
		Nodes: []schema.Node{
			{ID: "a", SourceFile: "src/auth.go"},
			{ID: "b", SourceFile: "src/billing.go"},
			{ID: "c", SourceFile: "lib/cache.py"},
		},
	}
	out, _ := RenderWithOptions(analyze.Report{}, opts)
	s := string(out)
	if !contains(s, "## Corpus") {
		t.Fatal("missing Corpus section")
	}
	if !contains(s, "3 file") {
		t.Fatalf("file count missing: %s", s)
	}
}

func TestRenderWithOptionsBackwardCompat(t *testing.T) {
	// The old Render(r) signature must keep working — it just produces a
	// trimmed report without the data-dependent sections.
	r := analyze.Report{GodNodes: []schema.Node{{ID: "h", Label: "Hub"}}}
	out, err := Render(r)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(out), "## God Nodes") {
		t.Fatal("backward-compat Render lost God Nodes section")
	}
	// Sections that require nodes/edges must NOT appear.
	for _, s := range []string{"## Communities", "## Knowledge Gaps", "## Corpus"} {
		if contains(string(out), s) {
			t.Fatalf("section %q should be omitted when Render called without options: %s", s, out)
		}
	}
}
