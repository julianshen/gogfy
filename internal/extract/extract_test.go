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

func TestGoExtractorTypesAndMethodOwnership(t *testing.T) {
	// Go type declarations become nodes; methods link to their
	// receiver type via `contains` (the type→method ownership axis
	// graphify has). A method whose type is declared in the same
	// file gets type→method; type itself gets module→type.
	root := t.TempDir()
	path := filepath.Join(root, "repo.go")
	src := `package store

type Repo struct{ db string }

func (r *Repo) Save(x int) error { return nil }
func (r Repo) Load() int { return 0 }

func freeFunc() {}
`
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	result, err := (&GoExtractor{}).Extract(path)
	if err != nil {
		t.Fatal(err)
	}
	var typeID, repoSaveID, moduleID string
	for _, n := range result.Nodes {
		switch {
		case strings.Contains(n.ID, ":type:") && n.Label == "Repo":
			typeID = n.ID
		case strings.Contains(n.ID, ":method:") && n.Label == "Save":
			repoSaveID = n.ID
		case strings.Contains(n.ID, ":module:"):
			moduleID = n.ID
		}
	}
	if typeID == "" {
		t.Fatal("Repo type node not extracted")
	}
	if repoSaveID == "" {
		t.Fatal("Save method node not extracted")
	}
	// module → type contains
	var moduleContainsType, typeContainsMethod bool
	for _, e := range result.Edges {
		if e.Relation == "contains" && e.Source == moduleID && e.Target == typeID {
			moduleContainsType = true
		}
		if e.Relation == "contains" && e.Source == typeID && e.Target == repoSaveID {
			typeContainsMethod = true
		}
	}
	if !moduleContainsType {
		t.Errorf("expected module→Repo contains edge")
	}
	if !typeContainsMethod {
		t.Errorf("expected Repo→Save contains (method ownership) edge")
	}
}

func TestGoMethodOwnershipDeferredAcrossDeclOrder(t *testing.T) {
	// The method appears BEFORE its receiver type in the file. The
	// deferred finalize() must still link them — ownership can't
	// depend on lexical order.
	root := t.TempDir()
	path := filepath.Join(root, "order.go")
	src := "package p\n\nfunc (s *Svc) Do() {}\n\ntype Svc struct{}\n"
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	result, _ := (&GoExtractor{}).Extract(path)
	var svcID, doID string
	for _, n := range result.Nodes {
		if strings.Contains(n.ID, ":type:") && n.Label == "Svc" {
			svcID = n.ID
		}
		if strings.Contains(n.ID, ":method:") && n.Label == "Do" {
			doID = n.ID
		}
	}
	var linked bool
	for _, e := range result.Edges {
		if e.Relation == "contains" && e.Source == svcID && e.Target == doID {
			linked = true
		}
	}
	if !linked {
		t.Errorf("method-before-type ownership not linked by finalize()")
	}
}

func TestGoExtractorStructFieldsAndInterfaceMethods(t *testing.T) {
	// Struct fields and interface method specs become nodes contained
	// by their type — the finer-grained entities graphify extracts.
	root := t.TempDir()
	path := filepath.Join(root, "types.go")
	src := `package m

type Repo struct {
	db   string
	conn *Conn
}

type Reader interface {
	Read() error
	Close()
}
`
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	result, _ := (&GoExtractor{}).Extract(path)
	labels := map[string]string{} // label → kind(from ID)
	for _, n := range result.Nodes {
		parts := strings.Split(n.ID, ":")
		if len(parts) >= 2 {
			labels[n.Label] = parts[1]
		}
	}
	// Fields present as field nodes.
	for _, f := range []string{"db", "conn"} {
		if labels[f] != "field" {
			t.Errorf("expected struct field %q as field node, got kind %q", f, labels[f])
		}
	}
	// Interface methods present as method nodes.
	for _, m := range []string{"Read", "Close"} {
		if labels[m] != "method" {
			t.Errorf("expected interface method %q as method node, got kind %q", m, labels[m])
		}
	}
	// Containment: Repo contains db; Reader contains Read.
	var repoID, dbID string
	for _, n := range result.Nodes {
		if n.Label == "Repo" && strings.Contains(n.ID, ":type:") {
			repoID = n.ID
		}
		if n.Label == "db" {
			dbID = n.ID
		}
	}
	var contained bool
	for _, e := range result.Edges {
		if e.Relation == "contains" && e.Source == repoID && e.Target == dbID {
			contained = true
		}
	}
	if !contained {
		t.Errorf("expected Repo→db contains edge")
	}
}

func TestGoExtractorConstAndVar(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "decls.go")
	src := "package m\n\nconst MaxN = 10\n\nconst ( A = iota; B )\n\nvar Global = 1\n\nvar _ = ignored()\n\nfunc ignored() int { return 0 }\n"
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	result, _ := (&GoExtractor{}).Extract(path)
	kinds := map[string]string{}
	for _, n := range result.Nodes {
		parts := strings.Split(n.ID, ":")
		if len(parts) >= 2 {
			kinds[n.Label] = parts[1]
		}
	}
	for _, c := range []string{"MaxN", "A", "B"} {
		if kinds[c] != "const" {
			t.Errorf("expected const %q, got kind %q", c, kinds[c])
		}
	}
	if kinds["Global"] != "var" {
		t.Errorf("expected var Global, got kind %q", kinds["Global"])
	}
	// Blank identifier must NOT be a node.
	if _, ok := kinds["_"]; ok {
		t.Errorf("blank identifier should not be extracted")
	}
}
