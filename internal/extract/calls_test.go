package extract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/julianshen/gogfy/internal/schema"
)

// hasEdge reports whether res contains a `calls` edge whose source ID ends
// with `:srcSuffix` and whose target ID equals or ends with `targetSuffix`.
// Suffix-matching keeps tests independent of absolute file paths.
func hasEdge(res Result, relation, srcSuffix, targetSuffix string) bool {
	for _, e := range res.Edges {
		if e.Relation != relation {
			continue
		}
		if !strings.HasSuffix(e.Source, srcSuffix) {
			continue
		}
		if e.Target == targetSuffix || strings.HasSuffix(e.Target, targetSuffix) {
			return true
		}
	}
	return false
}

type callCase struct {
	name      string
	filename  string
	source    string
	extractor func(string) (Result, error)
	wantCalls [][3]string // {srcSuffix, targetID, _}
}

func TestExtractorsEmitCallEdges(t *testing.T) {
	cases := []callCase{
		{
			name:      "go",
			filename:  "main.go",
			extractor: GoExtractor{}.Extract,
			source: `package main
import "fmt"
func bar() { fmt.Println("hi"); foo(1) }
func foo(x int) int { return x }
`,
			wantCalls: [][3]string{
				{"main.bar", "go:call:Println", ""},
				{"main.bar", "go:call:foo", ""},
			},
		},
		{
			name:      "python",
			filename:  "m.py",
			extractor: PythonExtractor{}.Extract,
			source: `def bar():
    print("hi")
    foo(1)
def foo(x):
    return x
`,
			wantCalls: [][3]string{
				{":bar", "py:call:print", ""},
				{":bar", "py:call:foo", ""},
			},
		},
		{
			name:      "javascript",
			filename:  "main.js",
			extractor: JavaScriptExtractor{}.Extract,
			source: `function bar() { console.log("hi"); foo(1); }
function foo(x) { return x; }
`,
			wantCalls: [][3]string{
				{":bar", "js:call:log", ""},
				{":bar", "js:call:foo", ""},
			},
		},
		{
			name:      "typescript",
			filename:  "main.ts",
			extractor: TypeScriptExtractor{}.Extract,
			source: `function bar(): void { console.log("hi"); foo(1); }
function foo(x: number): number { return x; }
`,
			wantCalls: [][3]string{
				{":bar", "ts:call:log", ""},
				{":bar", "ts:call:foo", ""},
			},
		},
		{
			name:      "java",
			filename:  "C.java",
			extractor: JavaExtractor{}.Extract,
			source: `class C {
    void bar() { System.out.println("hi"); foo(1); }
    int foo(int x) { return x; }
}
`,
			wantCalls: [][3]string{
				{":bar", "java:call:println", ""},
				{":bar", "java:call:foo", ""},
			},
		},
		{
			name:      "rust",
			filename:  "lib.rs",
			extractor: RustExtractor{}.Extract,
			source: `fn bar() { println!("hi"); foo(1); }
fn foo(x: i32) -> i32 { x }
`,
			wantCalls: [][3]string{
				{":bar", "rust:call:println!", ""},
				{":bar", "rust:call:foo", ""},
			},
		},
		{
			name:      "ruby",
			filename:  "main.rb",
			extractor: RubyExtractor{}.Extract,
			source: `def bar
  puts "hi"
  foo(1)
end
def foo(x); x end
`,
			wantCalls: [][3]string{
				{":bar", "ruby:call:puts", ""},
				{":bar", "ruby:call:foo", ""},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, tc.filename)
			if err := os.WriteFile(path, []byte(tc.source), 0644); err != nil {
				t.Fatal(err)
			}
			res, err := tc.extractor(path)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range tc.wantCalls {
				srcSuffix, target := want[0], want[1]
				if !hasEdge(res, "calls", srcSuffix, target) {
					t.Fatalf("expected calls edge ending-in %q -> %q; edges=%s",
						srcSuffix, target, formatEdges(res.Edges, "calls"))
				}
			}
		})
	}
}

func TestJSMethodDefinitionEmitsDeclNodeForCallSource(t *testing.T) {
	// Ship-blocker regression: walkJS pushed the method's funcID onto fnStack
	// but never emitted the corresponding `js:method:...` node, so call edges
	// from inside class methods referenced a dangling source.
	dir := t.TempDir()
	path := filepath.Join(dir, "c.js")
	if err := os.WriteFile(path, []byte(`class C { greet() { foo(); } }
function foo() {}
`), 0644); err != nil {
		t.Fatal(err)
	}
	res, err := JavaScriptExtractor{}.Extract(path)
	if err != nil {
		t.Fatal(err)
	}
	// The method node must exist so the call edge has a real source.
	var methodID string
	for _, e := range res.Edges {
		if e.Relation == "calls" && e.Target == "js:call:foo" {
			methodID = e.Source
			break
		}
	}
	if methodID == "" {
		t.Fatalf("no calls edge to foo; edges=%s", formatEdges(res.Edges, "calls"))
	}
	for _, n := range res.Nodes {
		if n.ID == methodID {
			return
		}
	}
	t.Fatalf("call source %q has no corresponding node — dangling edge", methodID)
}

func TestJSArrowFunctionAttributesCallsToScope(t *testing.T) {
	// Arrow / function-expression / IIFE bodies should source their calls
	// to a non-module scope. The exact scope can be anonymous; we just
	// require it ISN'T the module node.
	dir := t.TempDir()
	path := filepath.Join(dir, "a.js")
	if err := os.WriteFile(path, []byte(`const handler = () => { trigger(); };
[1,2,3].map(x => use(x));
`), 0644); err != nil {
		t.Fatal(err)
	}
	res, err := JavaScriptExtractor{}.Extract(path)
	if err != nil {
		t.Fatal(err)
	}
	moduleID := "js:module:" + path
	for _, e := range res.Edges {
		if e.Relation != "calls" {
			continue
		}
		if e.Target == "js:call:trigger" || e.Target == "js:call:use" {
			if e.Source == moduleID {
				t.Fatalf("call %s should source from arrow scope, not module; edge=%s->%s",
					e.Target, e.Source, e.Target)
			}
		}
	}
}

func TestCallTargetNameDropsNonIdentifierFallback(t *testing.T) {
	// Calls with non-identifier callees like `(a + b)()` or `arr[0]()` should
	// not pollute the graph with junk targets like `js:call:(a + b)`.
	dir := t.TempDir()
	path := filepath.Join(dir, "weird.js")
	if err := os.WriteFile(path, []byte(`(a + b)();
arr[0]();
foo();
`), 0644); err != nil {
		t.Fatal(err)
	}
	res, err := JavaScriptExtractor{}.Extract(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range res.Edges {
		if e.Relation != "calls" {
			continue
		}
		if strings.ContainsAny(e.Target, " +[]()") {
			t.Fatalf("non-identifier callee leaked into target: %q", e.Target)
		}
	}
	if !hasEdge(res, "calls", path, "js:call:foo") {
		t.Fatal("identifier callee `foo` should still be emitted")
	}
}

func TestCallEdgesDedupCallTargetNodes(t *testing.T) {
	// Repeated calls to the same callee should produce one target node, not
	// N copies. The graph builder dedups in Build(); the extractor should
	// also dedup so state.nodes doesn't bloat.
	dir := t.TempDir()
	path := filepath.Join(dir, "rep.go")
	if err := os.WriteFile(path, []byte(`package main
func bar() { foo(); foo(); foo(); foo() }
func foo() {}
`), 0644); err != nil {
		t.Fatal(err)
	}
	res, err := GoExtractor{}.Extract(path)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, n := range res.Nodes {
		if n.ID == "go:call:foo" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 call-target node for `foo`; got %d", count)
	}
}

func TestGoReceiverMethodCallsScopedToMethod(t *testing.T) {
	// PR-review regression: walkGo now matches method_declaration too; verify
	// calls inside a receiver method source from the method, not the package.
	dir := t.TempDir()
	path := filepath.Join(dir, "m.go")
	if err := os.WriteFile(path, []byte(`package main
type T struct{}
func (t T) Run() { helper() }
func helper() {}
`), 0644); err != nil {
		t.Fatal(err)
	}
	res, err := GoExtractor{}.Extract(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range res.Edges {
		if e.Relation == "calls" && e.Target == "go:call:helper" {
			if !strings.HasSuffix(e.Source, ".Run") {
				t.Fatalf("call should source from method Run, got %q", e.Source)
			}
			return
		}
	}
	t.Fatalf("expected calls edge to helper from Run; edges=%s",
		formatEdges(res.Edges, "calls"))
}

func TestCallEdgesUseModuleSourceForTopLevelCalls(t *testing.T) {
	// A call outside any function should source from the file's module
	// node, not silently disappear.
	dir := t.TempDir()
	path := filepath.Join(dir, "top.py")
	if err := os.WriteFile(path, []byte(`print("hi")`), 0644); err != nil {
		t.Fatal(err)
	}
	res, err := PythonExtractor{}.Extract(path)
	if err != nil {
		t.Fatal(err)
	}
	if !hasEdge(res, "calls", path, "py:call:print") {
		t.Fatalf("top-level call should source from module; edges=%s",
			formatEdges(res.Edges, "calls"))
	}
}

func formatEdges(edges []schema.Edge, relation string) string {
	var b strings.Builder
	for _, e := range edges {
		if e.Relation == relation {
			b.WriteString("\n  ")
			b.WriteString(e.Source)
			b.WriteString(" -> ")
			b.WriteString(e.Target)
		}
	}
	return b.String()
}
