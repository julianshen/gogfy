package report

import (
	"fmt"
	"os"
	"testing"

	"github.com/julianshen/gogfy/internal/analyze"
	"github.com/julianshen/gogfy/internal/schema"
)

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

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

func TestRenderReportAmbiguousEdgesCapped(t *testing.T) {
	// 27 ambiguous edges should produce at most 25 listed + 1 overflow line.
	nodes := []schema.Node{}
	edges := []schema.Edge{}
	for i := 0; i < 27; i++ {
		nodes = append(nodes,
			schema.Node{ID: fmt.Sprintf("s%02d", i), Label: fmt.Sprintf("S%02d", i)},
			schema.Node{ID: fmt.Sprintf("t%02d", i), Label: fmt.Sprintf("T%02d", i)},
		)
		edges = append(edges, schema.Edge{
			Source: fmt.Sprintf("s%02d", i), Target: fmt.Sprintf("t%02d", i),
			Relation: "calls", Confidence: schema.Ambiguous,
		})
	}
	out, _ := RenderWithOptions(analyze.Report{}, Options{Nodes: nodes, Edges: edges})
	s := string(out)
	if !contains(s, "_… 2 more_") {
		t.Fatalf("overflow line should report 2 hidden edges: %s", s)
	}
	// Count "- S" lines under the Ambiguous Edges section as a proxy.
	count := 0
	idx := indexOf(s, "## Ambiguous Edges")
	if idx < 0 {
		t.Fatal("section missing")
	}
	for i := idx; i+3 < len(s); i++ {
		if s[i] == '\n' && s[i+1] == '-' && s[i+2] == ' ' && s[i+3] == 'S' {
			count++
		}
	}
	if count != 25 {
		t.Fatalf("expected 25 listed ambiguous edges (cap), got %d", count)
	}
}

func TestRenderReportCommunityLabelsOverride(t *testing.T) {
	// Populated CommunityLabels must replace "Community <id>" in both
	// Communities and Community Hubs section headings.
	opts := Options{
		Nodes: []schema.Node{
			{ID: "a", Label: "A", Community: "1"},
			{ID: "b", Label: "B", Community: "1"},
			{ID: "c", Label: "C", Community: "2"},
			{ID: "d", Label: "D", Community: "2"},
		},
		Edges:           []schema.Edge{{Source: "a", Target: "b"}, {Source: "c", Target: "d"}},
		CommunityLabels: map[string]string{"1": "Auth Layer", "2": "Storage Layer"},
	}
	out, _ := RenderWithOptions(analyze.Report{}, opts)
	s := string(out)
	if !contains(s, "Auth Layer") || !contains(s, "Storage Layer") {
		t.Fatalf("custom labels not applied: %s", s)
	}
	if contains(s, "Community 1") || contains(s, "Community 2") {
		t.Fatalf("default community names leaked despite override: %s", s)
	}
}

func TestRenderReportKnowledgeGapsOmittedWhenClean(t *testing.T) {
	// All-zero state: section should not appear.
	opts := Options{
		Nodes: []schema.Node{
			{ID: "a", Label: "A", Community: "1"},
			{ID: "b", Label: "B", Community: "1"},
		},
		Edges: []schema.Edge{{Source: "a", Target: "b", Confidence: schema.Extracted}},
	}
	out, _ := RenderWithOptions(analyze.Report{
		ConfidenceSummary: map[schema.Confidence]int{schema.Extracted: 1},
	}, opts)
	if contains(string(out), "## Knowledge Gaps") {
		t.Fatalf("section should be omitted when isolated=0 thin=0 ambiguous=0: %s", out)
	}
}

func TestRenderReportThinCommunityMinCustom(t *testing.T) {
	// ThinCommunityMin=3 should filter out a 2-member community.
	opts := Options{
		Nodes: []schema.Node{
			{ID: "a", Label: "A", Community: "small"},
			{ID: "b", Label: "B", Community: "small"},
			{ID: "x", Label: "X", Community: "big"},
			{ID: "y", Label: "Y", Community: "big"},
			{ID: "z", Label: "Z", Community: "big"},
		},
		ThinCommunityMin: 3,
	}
	out, _ := RenderWithOptions(analyze.Report{}, opts)
	s := string(out)
	if !contains(s, "Community big") {
		t.Fatalf("3-member community missing: %s", s)
	}
	if contains(s, "Community small") {
		t.Fatalf("2-member community should be filtered when ThinCommunityMin=3: %s", s)
	}
}

func TestRenderReportCommunityHubsDeterministicOrder(t *testing.T) {
	// Multiple communities — must appear in sorted-ID order so report
	// diffs stay clean across runs.
	opts := Options{
		Nodes: []schema.Node{
			{ID: "a1", Label: "A1", Community: "zeta"},
			{ID: "a2", Label: "A2", Community: "zeta"},
			{ID: "b1", Label: "B1", Community: "alpha"},
			{ID: "b2", Label: "B2", Community: "alpha"},
			{ID: "c1", Label: "C1", Community: "mid"},
			{ID: "c2", Label: "C2", Community: "mid"},
		},
		Edges: []schema.Edge{
			{Source: "a1", Target: "a2"},
			{Source: "b1", Target: "b2"},
			{Source: "c1", Target: "c2"},
		},
	}
	out, _ := RenderWithOptions(analyze.Report{}, opts)
	s := string(out)
	hubStart := indexOf(s, "## Community Hubs")
	if hubStart < 0 {
		t.Fatal("Community Hubs section missing")
	}
	hubBody := s[hubStart:]
	ialpha := indexOf(hubBody, "Community alpha")
	imid := indexOf(hubBody, "Community mid")
	izeta := indexOf(hubBody, "Community zeta")
	if !(ialpha < imid && imid < izeta) {
		t.Fatalf("community hubs not in sorted-ID order: alpha=%d mid=%d zeta=%d in %s", ialpha, imid, izeta, hubBody)
	}
}

func TestRenderReportCorpusBreaksDownByFileType(t *testing.T) {
	opts := Options{
		Nodes: []schema.Node{
			{ID: "a", SourceFile: "src/auth.go", FileType: schema.FileTypeCode},
			{ID: "b", SourceFile: "src/billing.go", FileType: schema.FileTypeCode},
			// Two nodes on same source file — counted as one for file tally.
			{ID: "c", SourceFile: "src/billing.go", FileType: schema.FileTypeCode},
			{ID: "d", SourceFile: "README.md", FileType: schema.FileTypeDocument},
		},
	}
	out, _ := RenderWithOptions(analyze.Report{}, opts)
	s := string(out)
	if !contains(s, "File types:") {
		t.Fatalf("missing file-type breakdown: %s", s)
	}
	if !contains(s, "2 code") {
		t.Fatalf("code file count wrong (should dedupe by SourceFile): %s", s)
	}
	if !contains(s, "1 document") {
		t.Fatalf("document file count wrong: %s", s)
	}
}

func TestRenderReportCorpusReclassifiesSyntheticNodes(t *testing.T) {
	// Regression: a node with empty FileType (e.g., from resolve.Calls
	// fan-out) used to silently drop the file from the type tally even
	// if a later node for the same file had a populated FileType.
	// Corpus now re-classifies from SourceFile so synthetic nodes don't
	// poison the count.
	opts := Options{
		Nodes: []schema.Node{
			{ID: "syn", SourceFile: "src/auth.go"},                                // FileType empty
			{ID: "real", SourceFile: "src/auth.go", FileType: schema.FileTypeCode}, // gets classified
		},
	}
	out, _ := RenderWithOptions(analyze.Report{}, opts)
	s := string(out)
	if !contains(s, "1 code") {
		t.Fatalf("Corpus must classify even when first-seen node has empty FileType: %s", s)
	}
}

func TestRenderReportCorpusOmitsFileTypesLineWhenAllUnknown(t *testing.T) {
	// Files we don't classify (lock files, go.sum, Dockerfile) shouldn't
	// produce an empty "File types: " bullet. Section header still
	// appears because there are tracked files.
	opts := Options{
		Nodes: []schema.Node{
			{ID: "a", SourceFile: "go.sum"},
			{ID: "b", SourceFile: "Dockerfile"},
		},
	}
	out, _ := RenderWithOptions(analyze.Report{}, opts)
	s := string(out)
	if !contains(s, "## Corpus") {
		t.Fatalf("Corpus header should appear when SourceFiles exist: %s", s)
	}
	if contains(s, "File types:") {
		t.Fatalf("File types bullet should be omitted when no node classifies: %s", s)
	}
}

func TestRenderReportCorpusFileTypesDeterministicOrder(t *testing.T) {
	// Sorting is alphabetic on the FileType string value, so
	// "code" < "document" < "image". Pin the exact substring so
	// reports diff cleanly across runs.
	opts := Options{
		Nodes: []schema.Node{
			{ID: "i", SourceFile: "logo.png"},
			{ID: "d", SourceFile: "README.md"},
			{ID: "c", SourceFile: "main.go"},
		},
	}
	out, _ := RenderWithOptions(analyze.Report{}, opts)
	if !contains(string(out), "File types: 1 code, 1 document, 1 image") {
		t.Fatalf("file types not in expected sorted order: %s", out)
	}
}

func TestRenderReportCorpusTinyWarning(t *testing.T) {
	opts := Options{
		Nodes: []schema.Node{
			{ID: "a", SourceFile: "a.go"},
			{ID: "b", SourceFile: "b.go"},
		},
	}
	out, _ := RenderWithOptions(analyze.Report{}, opts)
	if !contains(string(out), "Tiny corpus") {
		t.Fatalf("expected tiny-corpus warning for 2 files: %s", out)
	}
}

func TestRenderReportCorpusNoWarningForReasonableCorpus(t *testing.T) {
	nodes := []schema.Node{}
	for i := 0; i < 10; i++ {
		nodes = append(nodes, schema.Node{ID: fmt.Sprintf("n%d", i), SourceFile: fmt.Sprintf("f%d.go", i)})
	}
	out, _ := RenderWithOptions(analyze.Report{}, Options{Nodes: nodes})
	if contains(string(out), "Tiny corpus") {
		t.Fatalf("tiny-corpus warning should not fire at 10 files: %s", out)
	}
}
