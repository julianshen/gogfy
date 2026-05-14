package benchmark

import (
	"bytes"
	"strings"
	"testing"

	"github.com/julianshen/gogfy/internal/schema"
)

// fixtureGraph returns a small connected graph that the default sample
// questions will match against (label "auth" matches "how does
// authentication work", "main" matches "what is the main entry point").
func fixtureGraph() ([]schema.Node, []schema.Edge) {
	nodes := []schema.Node{
		{ID: "n1", Label: "authHandler", SourceFile: "/r/auth.go", SourceLocation: "10:1"},
		{ID: "n2", Label: "authMiddleware", SourceFile: "/r/auth.go", SourceLocation: "30:1"},
		{ID: "n3", Label: "main", SourceFile: "/r/main.go", SourceLocation: "1:1"},
		{ID: "n4", Label: "errorHandler", SourceFile: "/r/errors.go", SourceLocation: "5:1"},
		{ID: "n5", Label: "dataLayer", SourceFile: "/r/db.go", SourceLocation: "1:1"},
		{ID: "n6", Label: "apiHandler", SourceFile: "/r/api.go", SourceLocation: "1:1"},
		{ID: "n7", Label: "coreAbstractions", SourceFile: "/r/core.go", SourceLocation: "1:1"},
	}
	edges := []schema.Edge{
		{Source: "n1", Target: "n2", Relation: "calls"},
		{Source: "n2", Target: "n3", Relation: "calls"},
		{Source: "n3", Target: "n4", Relation: "calls"},
		{Source: "n3", Target: "n6", Relation: "calls"},
		{Source: "n6", Target: "n5", Relation: "calls"},
		{Source: "n7", Target: "n1", Relation: "references"},
	}
	return nodes, edges
}

func TestRunReturnsCorpusAndQueryReductions(t *testing.T) {
	nodes, edges := fixtureGraph()
	res, err := Run(nodes, edges, Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Nodes != 7 || res.Edges != 6 {
		t.Fatalf("node/edge count off: nodes=%d edges=%d", res.Nodes, res.Edges)
	}
	if res.CorpusWords <= 0 || res.CorpusTokens <= 0 {
		t.Fatalf("corpus should be estimated when CorpusWords unset, got words=%d tokens=%d", res.CorpusWords, res.CorpusTokens)
	}
	if len(res.PerQuestion) == 0 {
		t.Fatalf("expected at least one matching sample question against fixture")
	}
	for _, p := range res.PerQuestion {
		if p.QueryTokens <= 0 {
			t.Fatalf("query tokens must be >0 for matched question %q", p.Question)
		}
		if p.Reduction <= 0 {
			t.Fatalf("reduction must be >0 for %q (corpus=%d, query=%d)", p.Question, res.CorpusTokens, p.QueryTokens)
		}
	}
	if res.AvgQueryTokens <= 0 || res.ReductionRatio <= 0 {
		t.Fatalf("aggregate avg/ratio must be >0: avg=%d ratio=%v", res.AvgQueryTokens, res.ReductionRatio)
	}
}

func TestRunHonorsExplicitCorpusWords(t *testing.T) {
	// Explicit CorpusWords must bypass the node-count estimation so
	// callers can plug in a real wc-style count from detect().
	nodes, edges := fixtureGraph()
	res, err := Run(nodes, edges, Options{CorpusWords: 9000})
	if err != nil {
		t.Fatal(err)
	}
	if res.CorpusWords != 9000 {
		t.Fatalf("CorpusWords override ignored: got %d", res.CorpusWords)
	}
	// Pin the upstream words→tokens conversion (words*100/75).
	want := 9000 * 100 / 75
	if res.CorpusTokens != want {
		t.Fatalf("CorpusTokens: got %d want %d (must match upstream words*100/75)", res.CorpusTokens, want)
	}
}

func TestRunNoMatchingQuestionsReturnsError(t *testing.T) {
	// Custom question with no overlap against any label should produce
	// an explicit error (not a divide-by-zero or silent zero ratio).
	nodes, edges := fixtureGraph()
	_, err := Run(nodes, edges, Options{Questions: []string{"zzz-no-such-token here"}})
	if err == nil {
		t.Fatal("expected error when no question matches any node label")
	}
}

func TestRunCustomQuestionsUsedInsteadOfDefaults(t *testing.T) {
	// Custom Questions list must replace defaults entirely.
	nodes, edges := fixtureGraph()
	res, err := Run(nodes, edges, Options{Questions: []string{"main entry point"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.PerQuestion) != 1 {
		t.Fatalf("expected exactly 1 per-question entry, got %d", len(res.PerQuestion))
	}
	if !strings.Contains(res.PerQuestion[0].Question, "main") {
		t.Fatalf("returned question doesn't match input: %+v", res.PerQuestion[0])
	}
}

func TestRunBFSDepthBoundsContext(t *testing.T) {
	// A larger Depth visits more nodes than a smaller one (BFS expands).
	// This pins the contract that Depth actually affects subgraph size.
	nodes, edges := fixtureGraph()
	shallow, err := Run(nodes, edges, Options{Questions: []string{"main"}, Depth: 1})
	if err != nil {
		t.Fatal(err)
	}
	deep, err := Run(nodes, edges, Options{Questions: []string{"main"}, Depth: 3})
	if err != nil {
		t.Fatal(err)
	}
	if deep.PerQuestion[0].QueryTokens < shallow.PerQuestion[0].QueryTokens {
		t.Fatalf("Depth=3 should visit >= as many tokens as Depth=1, got %d vs %d",
			deep.PerQuestion[0].QueryTokens, shallow.PerQuestion[0].QueryTokens)
	}
}

func TestRunDeterministic(t *testing.T) {
	// Same input → identical Result. BFS frontiers use maps; without
	// sorted iteration over neighbors the per-question token count
	// drifts across runs as edges_seen accumulates in different orders.
	nodes, edges := fixtureGraph()
	r1, err := Run(nodes, edges, Options{})
	if err != nil {
		t.Fatal(err)
	}
	r2, err := Run(nodes, edges, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if r1.AvgQueryTokens != r2.AvgQueryTokens || r1.ReductionRatio != r2.ReductionRatio {
		t.Fatalf("non-deterministic Result:\n%+v\nvs\n%+v", r1, r2)
	}
	if len(r1.PerQuestion) != len(r2.PerQuestion) {
		t.Fatalf("per-question count drift: %d vs %d", len(r1.PerQuestion), len(r2.PerQuestion))
	}
	for i := range r1.PerQuestion {
		if r1.PerQuestion[i] != r2.PerQuestion[i] {
			t.Fatalf("per-question drift at %d: %+v vs %+v", i, r1.PerQuestion[i], r2.PerQuestion[i])
		}
	}
}

func TestRenderHumanReadable(t *testing.T) {
	nodes, edges := fixtureGraph()
	res, err := Run(nodes, edges, Options{})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := Render(res, &buf); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		"token reduction benchmark",
		"Corpus:",
		"Avg query cost:",
		"Reduction:",
		"Per question:",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("rendered output missing %q:\n%s", want, out)
		}
	}
}

func TestRenderEmptyResultReportsError(t *testing.T) {
	// If a user passes a zero-Result (e.g. forgot to call Run), Render
	// shouldn't divide-by-zero or print a misleading 0x reduction
	// banner. Surface it as a clear message.
	var buf bytes.Buffer
	if err := Render(Result{}, &buf); err == nil {
		t.Fatal("Render(Result{}) should return an error for an empty/un-run result")
	}
}

func TestEstimateTokens(t *testing.T) {
	// Pinning the 4-chars-per-token approximation and the floor of 1.
	if got := estimateTokens("", 4); got != 1 {
		t.Fatalf("empty string should still cost 1 token, got %d", got)
	}
	if got := estimateTokens("abcd", 4); got != 1 {
		t.Fatalf("4 chars / 4 = 1, got %d", got)
	}
	if got := estimateTokens("abcdefghij", 4); got != 2 {
		t.Fatalf("10 chars / 4 = 2 (integer division), got %d", got)
	}
}
