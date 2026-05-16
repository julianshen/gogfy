package rationale

import (
	"testing"

	"github.com/julianshen/gogfy/internal/schema"
)

func TestExtractGoStyleNoteComment(t *testing.T) {
	src := []byte(`package main

// NOTE: this is load-bearing — the retry loop assumes ordered delivery.
func foo() {}
`)
	nodes, edges := Extract("/proj/main.go", src)
	if len(nodes) != 1 || len(edges) != 1 {
		t.Fatalf("expected 1 rationale node + 1 edge, got %d nodes %d edges", len(nodes), len(edges))
	}
	if nodes[0].FileType != "rationale" {
		t.Errorf("rationale node should carry FileType=rationale, got %q", nodes[0].FileType)
	}
	if !contains(nodes[0].Label, "NOTE:") {
		t.Errorf("label should preserve marker: %q", nodes[0].Label)
	}
	if edges[0].Relation != "rationale_for" {
		t.Errorf("edge relation: got %q want rationale_for", edges[0].Relation)
	}
	if edges[0].Confidence != schema.Extracted {
		t.Errorf("rationale edges are directly extracted from source: got %v", edges[0].Confidence)
	}
}

func TestExtractPythonHashAndHackMarker(t *testing.T) {
	src := []byte(`# HACK: bypass auth in dev — production uses real middleware
def main():
    pass
`)
	nodes, _ := Extract("/proj/main.py", src)
	if len(nodes) != 1 {
		t.Fatalf("expected 1 rationale, got %d", len(nodes))
	}
	if !contains(nodes[0].Label, "HACK:") {
		t.Errorf("hack marker lost: %q", nodes[0].Label)
	}
}

func TestExtractMultipleMarkersAllCaptured(t *testing.T) {
	src := []byte(`// NOTE: line 1
// HACK: line 2
// TODO: line 3
// FIXME: line 4
// WARNING: line 5
// XXX: line 6
// IMPORTANT: line 7
// RATIONALE: line 8
// WHY: line 9
func f() {}
`)
	nodes, _ := Extract("/proj/main.go", src)
	if len(nodes) != 9 {
		t.Fatalf("expected 9 rationale comments, got %d: %+v", len(nodes), nodes)
	}
}

func TestExtractIgnoresNonMarkerComments(t *testing.T) {
	src := []byte(`// just a regular comment
// this is documentation
// see also: foo
func f() {}
`)
	nodes, _ := Extract("/proj/main.go", src)
	if len(nodes) != 0 {
		t.Fatalf("expected 0 rationale (no markers), got %d: %+v", len(nodes), nodes)
	}
}

func TestExtractDashDashSqlComments(t *testing.T) {
	// SQL uses -- as line comment marker. Rationale extraction should
	// not be Python/Go-specific — any common comment shape works.
	src := []byte(`-- NOTE: this index is critical for query latency
SELECT * FROM users;
`)
	nodes, _ := Extract("/db/schema.sql", src)
	if len(nodes) != 1 {
		t.Fatalf("expected SQL rationale, got %d", len(nodes))
	}
}

func TestExtractLineNumberInLocation(t *testing.T) {
	src := []byte(`package main
// line 2
// NOTE: this is on line 3
`)
	nodes, _ := Extract("/proj/main.go", src)
	if len(nodes) != 1 {
		t.Fatal("expected 1 rationale")
	}
	if nodes[0].SourceLocation != "L3" {
		t.Errorf("expected SourceLocation=L3, got %q", nodes[0].SourceLocation)
	}
}

func TestExtractSourceFileSetOnNode(t *testing.T) {
	nodes, _ := Extract("/proj/foo.go", []byte("// NOTE: x\n"))
	if nodes[0].SourceFile != "/proj/foo.go" {
		t.Errorf("SourceFile not set: %q", nodes[0].SourceFile)
	}
}

func TestExtractEdgeTargetsModuleNode(t *testing.T) {
	// The rationale edge points TO the file's module node — the same
	// node ID extractors emit so the rationale attaches to the existing
	// graph rather than orphaning a sibling tree.
	src := []byte("// NOTE: x\n")
	_, edges := Extract("/proj/main.go", src)
	if edges[0].Target != "go:module:/proj/main.go" {
		t.Errorf("edge should target the module node ID: got %q", edges[0].Target)
	}
}

func TestExtractLabelTruncatesLongComment(t *testing.T) {
	// Long rationale comments could blow up labels in downstream
	// renderers (wiki, mermaid). Cap to keep them readable.
	long := "// NOTE: " + repeat("x", 500)
	nodes, _ := Extract("/proj/main.go", []byte(long+"\n"))
	if len([]rune(nodes[0].Label)) > 256 {
		t.Errorf("label not capped: %d runes", len([]rune(nodes[0].Label)))
	}
}

func TestExtractDeterministicNodeIDsAcrossRuns(t *testing.T) {
	// Two runs over the same file must produce the same node IDs so
	// downstream merge/dedup stays idempotent.
	src := []byte("// NOTE: a\n// HACK: b\n")
	n1, _ := Extract("/proj/main.go", src)
	n2, _ := Extract("/proj/main.go", src)
	for i := range n1 {
		if n1[i].ID != n2[i].ID {
			t.Fatalf("ID drift at %d: %q vs %q", i, n1[i].ID, n2[i].ID)
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func repeat(s string, n int) string {
	out := ""
	for i := 0; i < n; i++ {
		out += s
	}
	return out
}

func TestExtractEdgeTargetsMatchExtractorLangTags(t *testing.T) {
	// CRITICAL: rationale_for edges must target the SAME module-node ID
	// the per-language extractor would emit. Drift here makes edges
	// dangle. Pinning the language tags rationale uses for the cases
	// where they're non-obvious (extension != tag).
	cases := map[string]string{
		"/a.rs":  "rust",
		"/a.rb":  "ruby",
		"/a.hs":  "haskell",
		"/a.jl":  "julia",
		"/a.f90": "fortran",
		"/a.kt":  "kotlin",
		"/a.cs":  "csharp",
	}
	for path, wantLang := range cases {
		_, edges := Extract(path, []byte("// NOTE: x\n# NOTE: y\n"))
		if len(edges) == 0 {
			t.Errorf("%s: no edges emitted", path)
			continue
		}
		wantTarget := wantLang + ":module:" + path
		if edges[0].Target != wantTarget {
			t.Errorf("%s target: got %q, want %q", path, edges[0].Target, wantTarget)
		}
	}
}

func TestExtractColonOptional(t *testing.T) {
	// Idiomatic Go/Python: `// TODO add tests` without a colon. Skipping
	// these was the most common false-negative.
	src := []byte(`// TODO add tests
# FIXME broken on windows
// XXX rethink this
`)
	nodes, _ := Extract("/proj/main.go", src)
	if len(nodes) != 3 {
		t.Fatalf("colon-less rationale markers should match: got %d, want 3", len(nodes))
	}
}

func TestExtractBlockCommentStripsCloser(t *testing.T) {
	// Single-line block comment: trailing `*/` must be stripped from the
	// label so downstream renderers don't show it.
	src := []byte("/* NOTE: this matters */\n")
	nodes, _ := Extract("/proj/main.go", src)
	if len(nodes) != 1 {
		t.Fatalf("expected 1 rationale, got %d", len(nodes))
	}
	if contains(nodes[0].Label, "*/") {
		t.Fatalf("block-comment closer must be stripped: %q", nodes[0].Label)
	}
}

func TestExtractTruncationMarker(t *testing.T) {
	// A clipped label must signal truncation so a downstream renderer
	// doesn't show a comment that looks like it ends mid-word.
	long := "// NOTE: " + repeat("y", 500)
	nodes, _ := Extract("/proj/main.go", []byte(long+"\n"))
	if len(nodes) != 1 {
		t.Fatal("expected 1 rationale")
	}
	rs := []rune(nodes[0].Label)
	if rs[len(rs)-1] != '…' {
		t.Fatalf("truncated label must end with …: %q", nodes[0].Label)
	}
}
