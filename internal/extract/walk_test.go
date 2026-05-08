package extract

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_go "github.com/tree-sitter/tree-sitter-go/bindings/go"
)

func parseGo(t *testing.T, src string) (*sitter.Tree, *sitter.TreeCursor, func()) {
	t.Helper()
	parser := sitter.NewParser()
	if err := parser.SetLanguage(sitter.NewLanguage(tree_sitter_go.Language())); err != nil {
		t.Fatal(err)
	}
	tree := parser.Parse([]byte(src), nil)
	cursor := tree.Walk()
	return tree, cursor, func() {
		cursor.Close()
		tree.Close()
		parser.Close()
	}
}

func TestParseErrorWarningOnMalformedSource(t *testing.T) {
	// Tree-sitter recovers from syntax errors by inserting ERROR nodes.
	// Previously the extractor surfaced no signal; users got partial graphs
	// with no hint that input was malformed. ParseErrorLogger now warns.
	dir := t.TempDir()
	path := filepath.Join(dir, "broken.go")
	if err := os.WriteFile(path, []byte("package main\nfunc broken( { foo()\n"), 0644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	old := ParseErrorLogger
	ParseErrorLogger = &buf
	defer func() { ParseErrorLogger = old }()

	if _, err := (GoExtractor{}).Extract(path); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "broken.go") {
		t.Fatalf("expected parse warning to mention broken.go; got %q", buf.String())
	}
	if !strings.Contains(buf.String(), "syntax errors") {
		t.Fatalf("expected warning to mention syntax errors; got %q", buf.String())
	}
}

func TestNodeLocationOneBased(t *testing.T) {
	_, cursor, cleanup := parseGo(t, "package main\n\nfunc f() {}\n")
	defer cleanup()
	// Drill to the function_declaration on row 2 (0-based), col 0.
	cursor.GotoFirstChild() // package_clause
	for cursor.Node().Kind() != "function_declaration" {
		if !cursor.GotoNextSibling() {
			t.Fatalf("function_declaration not found")
		}
	}
	got := nodeLocation(cursor.Node())
	if got != "3:1" {
		t.Fatalf("nodeLocation = %q, want %q", got, "3:1")
	}
}

func TestWalkChildrenVisitsEachChildOnce(t *testing.T) {
	_, cursor, cleanup := parseGo(t, "package main\n\nfunc a(){}\nfunc b(){}\nfunc c(){}\n")
	defer cleanup()
	var kinds []string
	walkChildren(cursor, func() {
		kinds = append(kinds, cursor.Node().Kind())
	})
	want := []string{"package_clause", "function_declaration", "function_declaration", "function_declaration"}
	if len(kinds) != len(want) {
		t.Fatalf("visited %v, want %v", kinds, want)
	}
	for i, k := range kinds {
		if k != want[i] {
			t.Fatalf("visit %d: got %q want %q (full=%v)", i, k, want[i], kinds)
		}
	}
}

func TestWalkChildrenRestoresCursor(t *testing.T) {
	_, cursor, cleanup := parseGo(t, "package main\nfunc f(){}\n")
	defer cleanup()
	root := cursor.Node()
	walkChildren(cursor, func() {})
	if cursor.Node().Kind() != root.Kind() {
		t.Fatalf("cursor not restored: at %q, want %q", cursor.Node().Kind(), root.Kind())
	}
}

func TestWalkChildrenNoOpOnLeaf(t *testing.T) {
	_, cursor, cleanup := parseGo(t, "package main\n")
	defer cleanup()
	// Drill to a token leaf with no children.
	cursor.GotoFirstChild() // package_clause
	cursor.GotoFirstChild() // "package" keyword
	leaf := cursor.Node()
	called := false
	walkChildren(cursor, func() { called = true })
	if called {
		t.Fatal("visit should not be called for a leaf node")
	}
	if cursor.Node().Kind() != leaf.Kind() {
		t.Fatalf("cursor moved: at %q, want %q", cursor.Node().Kind(), leaf.Kind())
	}
}
