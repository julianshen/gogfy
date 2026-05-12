package tree

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/julianshen/gogfy/internal/schema"
)

func TestBuildGroupsByDirectoryAndFile(t *testing.T) {
	nodes := []schema.Node{
		{ID: "n1", Label: "Foo", SourceFile: "/repo/pkg/a/foo.go"},
		{ID: "n2", Label: "Bar", SourceFile: "/repo/pkg/a/foo.go"},
		{ID: "n3", Label: "Baz", SourceFile: "/repo/pkg/b/baz.go"},
	}
	root := Build(nodes, Options{})
	if root == nil || len(root.Children) == 0 {
		t.Fatalf("empty tree: %+v", root)
	}
	names := childNames(root)
	if !contains(names, "a") || !contains(names, "b") {
		t.Fatalf("expected subdirs a + b in tree, got %v", names)
	}
}

func TestBuildSkipsNodesWithoutSourceFile(t *testing.T) {
	nodes := []schema.Node{
		{ID: "real", Label: "Real", SourceFile: "/repo/a.go"},
		{ID: "synthetic", Label: "InternalCall", SourceFile: ""},
	}
	root := Build(nodes, Options{})
	for _, c := range walk(root) {
		if c.Name == "InternalCall" {
			t.Fatalf("synthetic node with empty SourceFile should not appear: %+v", root)
		}
	}
}

func TestBuildEmptyReturnsEmptyMarker(t *testing.T) {
	root := Build(nil, Options{})
	if root.Name != "(empty graph)" {
		t.Fatalf("expected (empty graph) marker, got %q", root.Name)
	}
}

func TestBuildMaxChildrenTruncates(t *testing.T) {
	nodes := make([]schema.Node, 0, 250)
	for i := 0; i < 250; i++ {
		nodes = append(nodes, schema.Node{
			ID:         filepath.Join("n", fmtInt(i)),
			Label:      "sym_" + fmtInt(i),
			SourceFile: "/repo/wide.go",
		})
	}
	root := Build(nodes, Options{MaxChildren: 200})
	var fileNode *TreeNode
	for _, c := range walk(root) {
		if c.Name == "wide.go" {
			fileNode = c
		}
	}
	if fileNode == nil {
		t.Fatalf("wide.go file node missing in tree")
	}
	if len(fileNode.Children) != 201 {
		t.Fatalf("expected 200 real + 1 truncation leaf = 201 children, got %d", len(fileNode.Children))
	}
	// Truncation leaf has stable name and count regardless of sort
	// order — finalise re-sorts children, so we can't pin a position.
	var trunc *TreeNode
	for _, c := range fileNode.Children {
		if strings.HasPrefix(c.Name, "(+") && strings.HasSuffix(c.Name, " more)") {
			trunc = c
		}
	}
	if trunc == nil {
		t.Fatalf("truncation leaf missing from %d children", len(fileNode.Children))
	}
	if trunc.TotalCount != 50 {
		t.Fatalf("truncation leaf total_count should be 50, got %d", trunc.TotalCount)
	}
}

func TestBuildTotalCountRollup(t *testing.T) {
	nodes := []schema.Node{
		{ID: "n1", Label: "A", SourceFile: "/repo/x.go"},
		{ID: "n2", Label: "B", SourceFile: "/repo/x.go"},
		{ID: "n3", Label: "C", SourceFile: "/repo/y.go"},
	}
	root := Build(nodes, Options{})
	if root.TotalCount != 3 {
		t.Fatalf("root TotalCount should be 3 (sum of leaves), got %d", root.TotalCount)
	}
}

func TestBuildSkipsRedundantFileNameSymbol(t *testing.T) {
	// Some extractors emit a top-level module-decl whose label matches
	// the file's basename. The tree already represents the file via
	// its TreeNode; the redundant symbol shouldn't appear as a child.
	nodes := []schema.Node{
		{ID: "module", Label: "foo.go", SourceFile: "/repo/foo.go"},
		{ID: "real", Label: "DoStuff", SourceFile: "/repo/foo.go"},
	}
	root := Build(nodes, Options{})
	var fileNode *TreeNode
	for _, c := range walk(root) {
		if c.Name == "foo.go" {
			fileNode = c
		}
	}
	if fileNode == nil {
		t.Fatal("foo.go missing")
	}
	if len(fileNode.Children) != 1 || fileNode.Children[0].Name != "DoStuff" {
		t.Fatalf("expected single DoStuff child, got %+v", fileNode.Children)
	}
}

func TestHTMLEmbedsTreeJSON(t *testing.T) {
	root := Build([]schema.Node{
		{ID: "n1", Label: "Greet", SourceFile: "/repo/main.go"},
	}, Options{ProjectLabel: "repo"})
	out, err := HTML(root, HTMLOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"<!DOCTYPE html>",
		"d3.v7.min.js",
		`"name":`,
		"Greet",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("HTML missing %q", want)
		}
	}
}

func TestHTMLEscapesScriptTagInLabels(t *testing.T) {
	// A node label containing literal "</script>" must NOT terminate
	// the embedded JSON's enclosing <script> block.
	root := Build([]schema.Node{
		{ID: "n1", Label: `evil</script><script>alert(1)</script>`, SourceFile: "/repo/x.go"},
	}, Options{})
	out, err := HTML(root, HTMLOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, `</script><script>alert(1)`) {
		t.Fatalf("script-tag breakout sequence found unescaped in HTML output")
	}
}

func TestHTMLDeterministicForSameInput(t *testing.T) {
	nodes := []schema.Node{
		{ID: "n1", Label: "Foo", SourceFile: "/repo/a.go"},
		{ID: "n2", Label: "Bar", SourceFile: "/repo/a.go"},
		{ID: "n3", Label: "Baz", SourceFile: "/repo/b.go"},
	}
	root1 := Build(nodes, Options{})
	root2 := Build(nodes, Options{})
	a, _ := json.Marshal(root1)
	b, _ := json.Marshal(root2)
	if string(a) != string(b) {
		t.Fatalf("non-deterministic tree:\n%s\nvs\n%s", a, b)
	}
}

func childNames(n *TreeNode) []string {
	out := make([]string, 0, len(n.Children))
	for _, c := range n.Children {
		out = append(out, c.Name)
	}
	return out
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func walk(n *TreeNode) []*TreeNode {
	if n == nil {
		return nil
	}
	out := []*TreeNode{n}
	for _, c := range n.Children {
		out = append(out, walk(c)...)
	}
	return out
}

func fmtInt(i int) string {
	const digits = "0123456789"
	if i == 0 {
		return "0"
	}
	negative := i < 0
	if negative {
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = digits[i%10]
		i /= 10
	}
	if negative {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
