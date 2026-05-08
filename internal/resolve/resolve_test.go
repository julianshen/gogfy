package resolve

import (
	"testing"

	"github.com/julianshen/gogfy/internal/schema"
)

func TestCallsResolvesUniqueCandidateAsInferred(t *testing.T) {
	nodes := []schema.Node{
		{ID: "go:function:/m.go:main:bar", Label: "bar"},
		{ID: "go:function:/m.go:main:foo", Label: "foo"},
		{ID: "go:call:foo", Label: "foo"},
	}
	edges := []schema.Edge{
		{Source: "go:function:/m.go:main:bar", Target: "go:call:foo", Relation: "calls", Confidence: schema.Extracted},
	}
	gotN, gotE := Calls(nodes, edges)
	if len(gotE) != 1 {
		t.Fatalf("expected 1 edge, got %d: %v", len(gotE), gotE)
	}
	if gotE[0].Target != "go:function:/m.go:main:foo" {
		t.Fatalf("expected upgraded target to function node, got %q", gotE[0].Target)
	}
	if gotE[0].Confidence != schema.Inferred {
		t.Fatalf("expected INFERRED confidence, got %v", gotE[0].Confidence)
	}
	for _, n := range gotN {
		if n.ID == "go:call:foo" {
			t.Fatalf("synthetic call-target node should be pruned when fully resolved")
		}
	}
}

func TestCallsAmbiguousPreservesSyntheticAnchor(t *testing.T) {
	// The package doc commits to preserving the synthetic call-target node
	// when fanning out AMBIGUOUS edges so the original-callee identity is
	// still discoverable in the graph. Regression test against doc/code
	// mismatch.
	nodes := []schema.Node{
		{ID: "go:function:/a.go:a:foo", Label: "foo"},
		{ID: "go:function:/b.go:b:foo", Label: "foo"},
		{ID: "go:function:/m.go:main:bar", Label: "bar"},
		{ID: "go:call:foo", Label: "foo"},
	}
	edges := []schema.Edge{
		{Source: "go:function:/m.go:main:bar", Target: "go:call:foo", Relation: "calls", Confidence: schema.Extracted},
	}
	gotN, _ := Calls(nodes, edges)
	for _, n := range gotN {
		if n.ID == "go:call:foo" {
			return
		}
	}
	t.Fatalf("AMBIGUOUS fan-out pruned the synthetic anchor; nodes=%v", gotN)
}

func TestCallsAmbiguousFanOutsToAllCandidates(t *testing.T) {
	nodes := []schema.Node{
		{ID: "go:function:/a.go:a:foo", Label: "foo"},
		{ID: "go:function:/b.go:b:foo", Label: "foo"},
		{ID: "go:function:/m.go:main:bar", Label: "bar"},
		{ID: "go:call:foo", Label: "foo"},
	}
	edges := []schema.Edge{
		{Source: "go:function:/m.go:main:bar", Target: "go:call:foo", Relation: "calls", Confidence: schema.Extracted},
	}
	_, gotE := Calls(nodes, edges)
	if len(gotE) != 2 {
		t.Fatalf("expected 2 ambiguous edges, got %d: %v", len(gotE), gotE)
	}
	targets := map[string]schema.Confidence{}
	for _, e := range gotE {
		targets[e.Target] = e.Confidence
	}
	for _, want := range []string{"go:function:/a.go:a:foo", "go:function:/b.go:b:foo"} {
		if c, ok := targets[want]; !ok {
			t.Fatalf("missing AMBIGUOUS edge to %q (got %v)", want, targets)
		} else if c != schema.Ambiguous {
			t.Fatalf("edge to %q should be AMBIGUOUS, got %v", want, c)
		}
	}
}

func TestCallsLeavesUnresolvableEdgesAsExtracted(t *testing.T) {
	// A call to an external library has no candidate function in the graph.
	// The edge stays EXTRACTED and points at the synthetic call node, which
	// must NOT be pruned (it's still referenced).
	nodes := []schema.Node{
		{ID: "go:function:/m.go:main:bar", Label: "bar"},
		{ID: "go:call:Println", Label: "Println"},
	}
	edges := []schema.Edge{
		{Source: "go:function:/m.go:main:bar", Target: "go:call:Println", Relation: "calls", Confidence: schema.Extracted},
	}
	gotN, gotE := Calls(nodes, edges)
	if len(gotE) != 1 {
		t.Fatalf("expected 1 unchanged edge, got %d", len(gotE))
	}
	if gotE[0].Confidence != schema.Extracted {
		t.Fatalf("unresolvable call should stay EXTRACTED, got %v", gotE[0].Confidence)
	}
	if gotE[0].Target != "go:call:Println" {
		t.Fatalf("target should be unchanged, got %q", gotE[0].Target)
	}
	hasCallNode := false
	for _, n := range gotN {
		if n.ID == "go:call:Println" {
			hasCallNode = true
		}
	}
	if !hasCallNode {
		t.Fatal("referenced synthetic call node must NOT be pruned")
	}
}

func TestCallsRespectsLanguageNamespacing(t *testing.T) {
	// A `go:call:foo` should not match a Python function named `foo`.
	nodes := []schema.Node{
		{ID: "py:function:/m.py:foo", Label: "foo"},
		{ID: "go:function:/m.go:main:bar", Label: "bar"},
		{ID: "go:call:foo", Label: "foo"},
	}
	edges := []schema.Edge{
		{Source: "go:function:/m.go:main:bar", Target: "go:call:foo", Relation: "calls", Confidence: schema.Extracted},
	}
	_, gotE := Calls(nodes, edges)
	if gotE[0].Confidence != schema.Extracted {
		t.Fatalf("cross-language false-match upgraded the edge: %+v", gotE[0])
	}
	if gotE[0].Target != "go:call:foo" {
		t.Fatalf("cross-language false-match changed target: %q", gotE[0].Target)
	}
}

func TestCallsHandlesSharedSchemeFunctionNodes(t *testing.T) {
	// Languages using the shared "<lang>:function:..." / ":method:..." scheme
	// (JS/TS/Java/Rust/Ruby/etc) must resolve the same way as Go/Python.
	nodes := []schema.Node{
		{ID: "js:function:/m.js:foo", Label: "foo"},
		{ID: "js:function:/m.js:bar", Label: "bar"},
		{ID: "js:call:foo", Label: "foo"},
	}
	edges := []schema.Edge{
		{Source: "js:function:/m.js:bar", Target: "js:call:foo", Relation: "calls", Confidence: schema.Extracted},
	}
	_, gotE := Calls(nodes, edges)
	if gotE[0].Target != "js:function:/m.js:foo" {
		t.Fatalf("shared-scheme resolution failed: %+v", gotE[0])
	}
	if gotE[0].Confidence != schema.Inferred {
		t.Fatalf("expected INFERRED, got %v", gotE[0].Confidence)
	}
}

func TestCallsLeavesNonCallEdgesAlone(t *testing.T) {
	nodes := []schema.Node{
		{ID: "go:function:/m.go:main:bar", Label: "bar"},
		{ID: "go:function:/m.go:main:foo", Label: "foo"},
	}
	edges := []schema.Edge{
		{Source: "go:function:/m.go:main:bar", Target: "go:function:/m.go:main:foo", Relation: "imports", Confidence: schema.Extracted},
	}
	gotN, gotE := Calls(nodes, edges)
	if len(gotE) != 1 || gotE[0].Relation != "imports" || gotE[0].Confidence != schema.Extracted {
		t.Fatalf("non-calls edge changed: %+v", gotE)
	}
	if len(gotN) != 2 {
		t.Fatalf("non-synthetic nodes pruned: %d", len(gotN))
	}
}
