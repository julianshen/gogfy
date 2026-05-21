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

func TestCallsImportScopeNarrowsAmbiguous(t *testing.T) {
	// Two files both define `login`. Calling file imports from auth.py
	// — the resolver must narrow the AMBIGUOUS fan-out to the imported
	// definition, upgrading to INFERRED.
	nodes := []schema.Node{
		// caller
		{ID: "py:module:/proj/main.py", Label: "main.py", SourceFile: "/proj/main.py"},
		{ID: "py:function:/proj/main.py:module:handler", Label: "handler", SourceFile: "/proj/main.py"},
		// imported target
		{ID: "py:import:auth.login", Label: "auth.login"},
		// candidate functions
		{ID: "py:function:/proj/auth.py:module:login", Label: "login", SourceFile: "/proj/auth.py"},
		{ID: "py:function:/proj/users.py:module:login", Label: "login", SourceFile: "/proj/users.py"},
		// the synthetic call target
		{ID: "py:call:login", Label: "login"},
	}
	edges := []schema.Edge{
		{Source: "py:module:/proj/main.py", Target: "py:import:auth.login", Relation: "imports", Confidence: schema.Extracted},
		{Source: "py:function:/proj/main.py:module:handler", Target: "py:call:login", Relation: "calls", Confidence: schema.Extracted},
	}
	_, gotE := Calls(nodes, edges)

	var callEdges []schema.Edge
	for _, e := range gotE {
		if e.Relation == "calls" {
			callEdges = append(callEdges, e)
		}
	}
	if len(callEdges) != 1 {
		t.Fatalf("import scope should narrow to 1 candidate, got %d: %+v", len(callEdges), callEdges)
	}
	if callEdges[0].Target != "py:function:/proj/auth.py:module:login" {
		t.Fatalf("wrong target: %s", callEdges[0].Target)
	}
	if callEdges[0].Confidence != schema.Inferred {
		t.Fatalf("import-scoped match should be INFERRED, got %v", callEdges[0].Confidence)
	}
}

func TestCallsImportScopeDifferentFileStaysAmbiguous(t *testing.T) {
	// /proj/main.py imports from auth; /proj/other.py imports from users.
	// A call from main.py should resolve to auth.login, but a call from
	// other.py (with the same name) should resolve to users.login.
	nodes := []schema.Node{
		{ID: "py:module:/proj/main.py", Label: "main.py", SourceFile: "/proj/main.py"},
		{ID: "py:module:/proj/other.py", Label: "other.py", SourceFile: "/proj/other.py"},
		{ID: "py:function:/proj/main.py:module:handler", Label: "handler", SourceFile: "/proj/main.py"},
		{ID: "py:function:/proj/other.py:module:handler", Label: "handler", SourceFile: "/proj/other.py"},
		{ID: "py:import:auth.login", Label: "auth.login"},
		{ID: "py:import:users.login", Label: "users.login"},
		{ID: "py:function:/proj/auth.py:module:login", Label: "login", SourceFile: "/proj/auth.py"},
		{ID: "py:function:/proj/users.py:module:login", Label: "login", SourceFile: "/proj/users.py"},
		{ID: "py:call:login", Label: "login"},
	}
	edges := []schema.Edge{
		{Source: "py:module:/proj/main.py", Target: "py:import:auth.login", Relation: "imports"},
		{Source: "py:module:/proj/other.py", Target: "py:import:users.login", Relation: "imports"},
		{Source: "py:function:/proj/main.py:module:handler", Target: "py:call:login", Relation: "calls"},
		{Source: "py:function:/proj/other.py:module:handler", Target: "py:call:login", Relation: "calls"},
	}
	_, gotE := Calls(nodes, edges)

	mainCall, otherCall := "", ""
	for _, e := range gotE {
		if e.Relation != "calls" {
			continue
		}
		switch e.Source {
		case "py:function:/proj/main.py:module:handler":
			mainCall = e.Target
		case "py:function:/proj/other.py:module:handler":
			otherCall = e.Target
		}
	}
	if mainCall != "py:function:/proj/auth.py:module:login" {
		t.Errorf("main.py should resolve to auth.login, got %s", mainCall)
	}
	if otherCall != "py:function:/proj/users.py:module:login" {
		t.Errorf("other.py should resolve to users.login, got %s", otherCall)
	}
}

func TestCallsImportScopeFallsBackToAmbiguousWhenUnimported(t *testing.T) {
	// Caller's file doesn't import `login` from anywhere. With no scope
	// hint, the resolver must keep the original AMBIGUOUS fan-out — we
	// must not invent INFERRED edges from thin air.
	nodes := []schema.Node{
		{ID: "py:module:/proj/main.py", Label: "main.py", SourceFile: "/proj/main.py"},
		{ID: "py:function:/proj/main.py:module:handler", Label: "handler", SourceFile: "/proj/main.py"},
		{ID: "py:function:/proj/auth.py:module:login", Label: "login", SourceFile: "/proj/auth.py"},
		{ID: "py:function:/proj/users.py:module:login", Label: "login", SourceFile: "/proj/users.py"},
		{ID: "py:call:login", Label: "login"},
	}
	edges := []schema.Edge{
		{Source: "py:function:/proj/main.py:module:handler", Target: "py:call:login", Relation: "calls"},
	}
	_, gotE := Calls(nodes, edges)
	count := 0
	for _, e := range gotE {
		if e.Relation == "calls" {
			count++
			if e.Confidence != schema.Ambiguous {
				t.Errorf("expected AMBIGUOUS, got %v for %s", e.Confidence, e.Target)
			}
		}
	}
	if count != 2 {
		t.Fatalf("expected 2 AMBIGUOUS fan-out edges, got %d", count)
	}
}

func TestCallsImportScopeStaysAmbiguousOnSameBasenameAcrossDirs(t *testing.T) {
	// Two files at /a/auth.py and /b/auth.py both stem "auth". A call
	// site importing "auth.login" narrows to BOTH (still 2 matches) —
	// must remain AMBIGUOUS rather than wrongly picking one.
	nodes := []schema.Node{
		{ID: "py:module:/proj/main.py", Label: "main.py", SourceFile: "/proj/main.py"},
		{ID: "py:function:/proj/main.py:module:handler", Label: "handler", SourceFile: "/proj/main.py"},
		{ID: "py:import:auth.login", Label: "auth.login"},
		{ID: "py:function:/proj/a/auth.py:module:login", Label: "login", SourceFile: "/proj/a/auth.py"},
		{ID: "py:function:/proj/b/auth.py:module:login", Label: "login", SourceFile: "/proj/b/auth.py"},
		{ID: "py:call:login", Label: "login"},
	}
	edges := []schema.Edge{
		{Source: "py:module:/proj/main.py", Target: "py:import:auth.login", Relation: "imports"},
		{Source: "py:function:/proj/main.py:module:handler", Target: "py:call:login", Relation: "calls"},
	}
	_, gotE := Calls(nodes, edges)
	callCount := 0
	for _, e := range gotE {
		if e.Relation == "calls" {
			callCount++
			if e.Confidence != schema.Ambiguous {
				t.Errorf("same-basename collision should stay AMBIGUOUS, got %v", e.Confidence)
			}
		}
	}
	if callCount != 2 {
		t.Fatalf("expected 2 AMBIGUOUS fan-out edges, got %d", callCount)
	}
}

func TestCallsImportScopeMethodCaller(t *testing.T) {
	// callerFile accepts both kind=function and kind=method. Pin that
	// a method caller (e.g., `def Handler.run(self): login()`) still
	// gets its import scope honored.
	nodes := []schema.Node{
		{ID: "py:module:/proj/main.py", Label: "main.py", SourceFile: "/proj/main.py"},
		{ID: "py:method:/proj/main.py:Handler:run", Label: "run", SourceFile: "/proj/main.py"},
		{ID: "py:import:auth.login", Label: "auth.login"},
		{ID: "py:function:/proj/auth.py:module:login", Label: "login", SourceFile: "/proj/auth.py"},
		{ID: "py:function:/proj/users.py:module:login", Label: "login", SourceFile: "/proj/users.py"},
		{ID: "py:call:login", Label: "login"},
	}
	edges := []schema.Edge{
		{Source: "py:module:/proj/main.py", Target: "py:import:auth.login", Relation: "imports"},
		{Source: "py:method:/proj/main.py:Handler:run", Target: "py:call:login", Relation: "calls"},
	}
	_, gotE := Calls(nodes, edges)
	for _, e := range gotE {
		if e.Relation != "calls" {
			continue
		}
		if e.Target != "py:function:/proj/auth.py:module:login" {
			t.Errorf("method caller's import scope ignored: %s", e.Target)
		}
		if e.Confidence != schema.Inferred {
			t.Errorf("expected INFERRED, got %v", e.Confidence)
		}
	}
}

func TestCallsImportScopeNonFunctionCallerFallsBack(t *testing.T) {
	// If a call edge's Source isn't a function/method node, callerFile
	// returns "" and narrowing is skipped — must NOT panic or mis-key
	// the scope as scope[""]. Original AMBIGUOUS fan-out preserved.
	nodes := []schema.Node{
		{ID: "py:module:/proj/main.py", Label: "main.py", SourceFile: "/proj/main.py"},
		{ID: "py:import:auth.login", Label: "auth.login"},
		{ID: "py:function:/proj/auth.py:module:login", Label: "login", SourceFile: "/proj/auth.py"},
		{ID: "py:function:/proj/users.py:module:login", Label: "login", SourceFile: "/proj/users.py"},
		{ID: "py:call:login", Label: "login"},
	}
	edges := []schema.Edge{
		{Source: "py:module:/proj/main.py", Target: "py:import:auth.login", Relation: "imports"},
		// Synthetic call from the module node itself (top-level call).
		{Source: "py:module:/proj/main.py", Target: "py:call:login", Relation: "calls"},
	}
	_, gotE := Calls(nodes, edges)
	callCount := 0
	for _, e := range gotE {
		if e.Relation == "calls" {
			callCount++
			if e.Confidence != schema.Ambiguous {
				t.Errorf("non-function caller should fall back to AMBIGUOUS, got %v", e.Confidence)
			}
		}
	}
	if callCount != 2 {
		t.Fatalf("expected 2 fan-out edges (no narrowing), got %d", callCount)
	}
}

func TestLastImportSegmentHandlesGoPath(t *testing.T) {
	// Go's path-style imports — the user-facing package name is the
	// last slash-separated segment, NOT a dot-split. The naive
	// LastIndexByte('.') would yield "com" for github.com/foo/bar.
	cases := map[string]string{
		"github.com/foo/bar":     "bar",
		"github.com/foo/bar/baz": "baz",
		"net/http":               "http",
		"auth.login":             "login",
		"com.example.Foo":        "Foo",
		"gopkg.in/yaml.v2":       "yaml",
		"gopkg.in/redis.v8":      "redis",
		"single":                 "single",
		"":                       "",
	}
	for in, want := range cases {
		if got := lastImportSegment(in); got != want {
			t.Errorf("lastImportSegment(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsVersionTagOnlyMatchesGoMajor(t *testing.T) {
	// `v2`, `v3`, `v10` are Go major-version path suffixes — strip
	// them so the package name (`yaml`, not `v2`) is what we keep.
	// Anything else (including `var`, `version`, plain digits) should
	// not match.
	matches := []string{"v0", "v1", "v2", "v42"}
	for _, m := range matches {
		if !isVersionTag(m) {
			t.Errorf("expected %q to be a version tag", m)
		}
	}
	nonMatches := []string{"", "v", "vfoo", "var", "version", "2v", "1"}
	for _, m := range nonMatches {
		if isVersionTag(m) {
			t.Errorf("did not expect %q to match version tag", m)
		}
	}
}

func TestMethodOwnershipSameDirectory(t *testing.T) {
	// Method in fileA, receiver type Svc declared in fileB of the SAME
	// directory (package). MethodOwnership must link them — this is the
	// cross-file case per-file extraction can't do alone.
	nodes := []schema.Node{
		{ID: "go:method:/pkg/a.go:Do", Label: "Do", SourceFile: "/pkg/a.go"},
		{ID: "go:type:/pkg/b.go:Svc", Label: "Svc", SourceFile: "/pkg/b.go"},
		{ID: "go:typeref:Svc", Label: "Svc"},
	}
	edges := []schema.Edge{
		{Source: "go:method:/pkg/a.go:Do", Target: "go:typeref:Svc", Relation: "method_of", Confidence: schema.Extracted},
	}
	outNodes, outEdges := MethodOwnership(nodes, edges)
	var linked bool
	for _, e := range outEdges {
		if e.Relation == "contains" && e.Source == "go:type:/pkg/b.go:Svc" && e.Target == "go:method:/pkg/a.go:Do" {
			linked = true
		}
		if e.Relation == "method_of" {
			t.Errorf("method_of edge should be rewritten, not kept: %+v", e)
		}
	}
	if !linked {
		t.Errorf("expected cross-file Svc→Do contains edge, got %+v", outEdges)
	}
	// Synthetic typeref pruned once unreferenced.
	for _, n := range outNodes {
		if n.ID == "go:typeref:Svc" {
			t.Errorf("synthetic typeref should be pruned after resolution")
		}
	}
}

func TestMethodOwnershipDropsUnresolvedReceiver(t *testing.T) {
	// Receiver type from another package (no type node in this dir) →
	// the method_of edge is dropped (method keeps its module link
	// elsewhere), and the typeref is pruned. No dangling edges.
	nodes := []schema.Node{
		{ID: "go:method:/pkg/a.go:String", Label: "String", SourceFile: "/pkg/a.go"},
		{ID: "go:typeref:time.Duration", Label: "time.Duration"},
	}
	edges := []schema.Edge{
		{Source: "go:method:/pkg/a.go:String", Target: "go:typeref:time.Duration", Relation: "method_of", Confidence: schema.Extracted},
	}
	outNodes, outEdges := MethodOwnership(nodes, edges)
	for _, e := range outEdges {
		if e.Relation == "method_of" {
			t.Errorf("unresolved method_of should be dropped, got %+v", e)
		}
	}
	for _, n := range outNodes {
		if n.ID == "go:typeref:time.Duration" {
			t.Errorf("unresolved typeref should be pruned")
		}
	}
}

func TestMethodOwnershipDoesNotCrossPackages(t *testing.T) {
	// Two packages each declaring a type named Svc. A method in pkg1
	// must link to pkg1's Svc, NOT pkg2's — directory-scoped matching.
	nodes := []schema.Node{
		{ID: "go:method:/pkg1/a.go:Do", Label: "Do", SourceFile: "/pkg1/a.go"},
		{ID: "go:type:/pkg1/t.go:Svc", Label: "Svc", SourceFile: "/pkg1/t.go"},
		{ID: "go:type:/pkg2/t.go:Svc", Label: "Svc", SourceFile: "/pkg2/t.go"},
		{ID: "go:typeref:Svc", Label: "Svc"},
	}
	edges := []schema.Edge{
		{Source: "go:method:/pkg1/a.go:Do", Target: "go:typeref:Svc", Relation: "method_of", Confidence: schema.Extracted},
	}
	_, outEdges := MethodOwnership(nodes, edges)
	for _, e := range outEdges {
		if e.Relation == "contains" && e.Target == "go:method:/pkg1/a.go:Do" {
			if e.Source != "go:type:/pkg1/t.go:Svc" {
				t.Errorf("method linked to wrong package's type: %s", e.Source)
			}
		}
	}
}

func TestPruneOrphanCallTargets(t *testing.T) {
	// A synthetic call target with no referencing edge is pruned; one
	// still referenced is kept; real nodes are never touched even at
	// zero degree.
	nodes := []schema.Node{
		{ID: "go:call:Orphan", Label: "Orphan"},      // no edge → prune
		{ID: "go:call:Used", Label: "Used"},          // referenced → keep
		{ID: "go:function:/a.go:Lonely", Label: "Lonely"}, // real, 0-deg → keep
	}
	edges := []schema.Edge{
		{Source: "go:function:/a.go:Caller", Target: "go:call:Used", Relation: "calls"},
	}
	out := PruneOrphanCallTargets(nodes, edges)
	ids := map[string]bool{}
	for _, n := range out {
		ids[n.ID] = true
	}
	if ids["go:call:Orphan"] {
		t.Errorf("orphan call target should be pruned")
	}
	if !ids["go:call:Used"] {
		t.Errorf("referenced call target should be kept")
	}
	if !ids["go:function:/a.go:Lonely"] {
		t.Errorf("real zero-degree node must NOT be pruned")
	}
}

func TestInheritanceResolvesUniqueBaseAsInferred(t *testing.T) {
	nodes := []schema.Node{
		{ID: "py:class:/a.py:Sub", Label: "Sub", SourceFile: "a.py"},
		{ID: "py:class:/b.py:Base", Label: "Base", SourceFile: "b.py"},
		{ID: "py:typeref:Base", Label: "Base"},
	}
	edges := []schema.Edge{
		{Source: "py:class:/a.py:Sub", Target: "py:typeref:Base", Relation: "inherits", Confidence: schema.Extracted},
	}
	_, got := Inheritance(nodes, edges)
	if len(got) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(got))
	}
	if got[0].Target != "py:class:/b.py:Base" {
		t.Errorf("target should resolve to real base class, got %q", got[0].Target)
	}
	if got[0].Confidence != schema.Inferred {
		t.Errorf("resolved inheritance should be INFERRED, got %v", got[0].Confidence)
	}
	if got[0].Relation != "inherits" {
		t.Errorf("relation must stay 'inherits', got %q", got[0].Relation)
	}
}

func TestInheritanceKeepsUnresolvedExternalBase(t *testing.T) {
	// `class Foo(Exception)` — Exception isn't in the corpus.
	nodes := []schema.Node{
		{ID: "py:class:/a.py:Foo", Label: "Foo", SourceFile: "a.py"},
		{ID: "py:typeref:Exception", Label: "Exception"},
	}
	edges := []schema.Edge{
		{Source: "py:class:/a.py:Foo", Target: "py:typeref:Exception", Relation: "inherits", Confidence: schema.Extracted},
	}
	gotN, got := Inheritance(nodes, edges)
	if len(got) != 1 || got[0].Target != "py:typeref:Exception" {
		t.Fatalf("external base edge should be kept pointing at typeref, got %+v", got)
	}
	// typeref node must survive (it's still referenced).
	found := false
	for _, n := range gotN {
		if n.ID == "py:typeref:Exception" {
			found = true
		}
	}
	if !found {
		t.Errorf("external-base typeref node should be kept as an observed fact")
	}
}

func TestInheritanceAmbiguousFansOut(t *testing.T) {
	nodes := []schema.Node{
		{ID: "java:class:/a.java:Sub", Label: "Sub", SourceFile: "a.java"},
		{ID: "java:class:/b.java:Base", Label: "Base", SourceFile: "b.java"},
		{ID: "java:class:/c.java:Base", Label: "Base", SourceFile: "c.java"},
		{ID: "java:typeref:Base", Label: "Base"},
	}
	edges := []schema.Edge{
		{Source: "java:class:/a.java:Sub", Target: "java:typeref:Base", Relation: "inherits", Confidence: schema.Extracted},
	}
	_, got := Inheritance(nodes, edges)
	n := 0
	for _, e := range got {
		if e.Relation == "inherits" {
			n++
			if e.Confidence != schema.Ambiguous {
				t.Errorf("multi-candidate inheritance should be AMBIGUOUS, got %v", e.Confidence)
			}
		}
	}
	if n != 2 {
		t.Errorf("expected 2 fanned-out AMBIGUOUS edges, got %d", n)
	}
}

func TestInheritanceImplementsAndEmbeds(t *testing.T) {
	nodes := []schema.Node{
		{ID: "go:type:/a.go:Server", Label: "Server", SourceFile: "a.go"},
		{ID: "go:type:/b.go:Handler", Label: "Handler", SourceFile: "b.go"},
		{ID: "go:typeref:Handler", Label: "Handler"},
	}
	edges := []schema.Edge{
		{Source: "go:type:/a.go:Server", Target: "go:typeref:Handler", Relation: "embeds", Confidence: schema.Extracted},
	}
	_, got := Inheritance(nodes, edges)
	if len(got) != 1 || got[0].Target != "go:type:/b.go:Handler" || got[0].Relation != "embeds" {
		t.Fatalf("embeds edge should resolve to real type keeping relation, got %+v", got)
	}
}
