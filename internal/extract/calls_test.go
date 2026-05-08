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
