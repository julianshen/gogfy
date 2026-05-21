package extract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/julianshen/gogfy/internal/schema"
)

func TestGoExtractor(t *testing.T) {
	ex := &GoExtractor{}
	result, err := ex.Extract("testdata/fixtures/go/simple/main.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Nodes) == 0 {
		t.Fatal("expected nodes")
	}
	if len(result.Edges) == 0 {
		t.Fatal("expected edges")
	}

	// Verify package node exists
	var pkgNode *schema.Node
	for i := range result.Nodes {
		if result.Nodes[i].Label == "main" {
			pkgNode = &result.Nodes[i]
			break
		}
	}
	if pkgNode == nil {
		t.Fatal("expected package node with label 'main'")
	}
	if pkgNode.SourceFile == "" {
		t.Fatal("expected package node to have SourceFile")
	}
	if err := pkgNode.Validate(); err != nil {
		t.Fatalf("package node invalid: %v", err)
	}

	// Verify function nodes exist
	funcLabels := make(map[string]bool)
	for _, n := range result.Nodes {
		if n.Label == "main" || n.Label == "helper" {
			funcLabels[n.Label] = true
		}
	}
	if !funcLabels["main"] {
		t.Fatal("expected function node 'main'")
	}
	if !funcLabels["helper"] {
		t.Fatal("expected function node 'helper'")
	}

	// Verify import edge exists
	var importEdge *schema.Edge
	for i := range result.Edges {
		if result.Edges[i].Relation == "imports" && result.Edges[i].Target == "go:import:fmt" {
			importEdge = &result.Edges[i]
			break
		}
	}
	if importEdge == nil {
		t.Fatal("expected import edge for 'fmt'")
	}
	if importEdge.Confidence != schema.Extracted {
		t.Fatalf("expected confidence EXTRACTED, got %s", importEdge.Confidence)
	}
	if importEdge.Source == "" {
		t.Fatal("expected import edge to have Source")
	}
	if err := importEdge.Validate(); err != nil {
		t.Fatalf("import edge invalid: %v", err)
	}
}

func TestGoExtractorGroupedImports(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "grouped.go")
	if err := os.WriteFile(path, []byte(`package grouped

import (
	"fmt"
	"os"
)

func run() {}
`), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &GoExtractor{}
	result, err := ex.Extract(path)
	if err != nil {
		t.Fatal(err)
	}

	importTargets := make(map[string]bool)
	for _, e := range result.Edges {
		if e.Relation == "imports" {
			importTargets[e.Target] = true
		}
	}
	if !importTargets["go:import:fmt"] {
		t.Fatal("expected import edge for 'fmt'")
	}
	if !importTargets["go:import:os"] {
		t.Fatal("expected import edge for 'os'")
	}
}

func TestGoExtractorNoImports(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "naked.go")
	if err := os.WriteFile(path, []byte(`package naked

func alone() {}
`), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &GoExtractor{}
	result, err := ex.Extract(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Nodes) != 2 {
		t.Fatalf("expected 2 nodes (pkg + func), got %d", len(result.Nodes))
	}
	// No imports → no `imports` edges. There IS a `contains` edge
	// (module → func) from the containment backbone; assert on the
	// import relation specifically, not the total.
	for _, e := range result.Edges {
		if e.Relation == "imports" {
			t.Fatalf("expected 0 import edges, got one: %+v", e)
		}
	}
}

func TestGoExtractorEmptyFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "empty.go")
	if err := os.WriteFile(path, []byte(``), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &GoExtractor{}
	result, err := ex.Extract(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Nodes) != 1 {
		t.Fatalf("expected 1 module node, got %d", len(result.Nodes))
	}
	if len(result.Edges) != 0 {
		t.Fatalf("expected 0 edges, got %d", len(result.Edges))
	}
}

func TestGoExtractorPackageOnly(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "pkgonly.go")
	if err := os.WriteFile(path, []byte(`package pkgonly
`), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &GoExtractor{}
	result, err := ex.Extract(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(result.Nodes))
	}
	if result.Nodes[0].Label != "pkgonly" {
		t.Fatalf("expected package node 'pkgonly', got %s", result.Nodes[0].Label)
	}
	if len(result.Edges) != 0 {
		t.Fatalf("expected 0 edges, got %d", len(result.Edges))
	}
}

func TestGoExtractorNonExistentFile(t *testing.T) {
	ex := &GoExtractor{}
	_, err := ex.Extract("/nonexistent/path/file.go")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestGoExtractorAnonymousFunction(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "anon.go")
	if err := os.WriteFile(path, []byte(`package anon

var _ = func() {}
`), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &GoExtractor{}
	result, err := ex.Extract(path)
	if err != nil {
		t.Fatal(err)
	}
	// Should have package node + 0 function_declaration nodes (lambda is different AST node)
	if len(result.Nodes) != 1 {
		t.Fatalf("expected 1 node (pkg only), got %d", len(result.Nodes))
	}
}

func TestGoExtractorEmitsContainsEdges(t *testing.T) {
	// The containment backbone: every top-level declaration gets a
	// `contains` edge from its file's module node. Without this,
	// uncalled functions float as zero-edge singletons that the
	// clusterer isolates into their own communities (the gap the
	// mmgo comparison surfaced: 281 singletons vs graphify's 14).
	root := t.TempDir()
	path := filepath.Join(root, "lib.go")
	src := "package lib\n\nfunc Exported() {}\n\nfunc helper() {}\n"
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	result, err := (&GoExtractor{}).Extract(path)
	if err != nil {
		t.Fatal(err)
	}
	var moduleID string
	for _, n := range result.Nodes {
		if strings.Contains(n.ID, ":module:") {
			moduleID = n.ID
		}
	}
	if moduleID == "" {
		t.Fatal("no module node emitted")
	}
	contained := map[string]bool{}
	for _, e := range result.Edges {
		if e.Relation == "contains" && e.Source == moduleID {
			contained[e.Target] = true
		}
	}
	// Both functions should be contained by the module.
	var funcs int
	for _, n := range result.Nodes {
		if strings.Contains(n.ID, ":function:") {
			funcs++
			if !contained[n.ID] {
				t.Errorf("function %s has no contains edge from module", n.ID)
			}
		}
	}
	if funcs != 2 {
		t.Fatalf("expected 2 function nodes, got %d", funcs)
	}
}
