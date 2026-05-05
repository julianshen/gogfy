package extract

import (
	"os"
	"path/filepath"
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
		if result.Edges[i].Relation == "imports" && result.Edges[i].Target == "pkg:import:fmt" {
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
}

func TestGoExtractorGroupedImports(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "grouped.go")
	os.WriteFile(path, []byte(`package grouped

import (
	"fmt"
	"os"
)

func run() {}
`), 0644)

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
	if !importTargets["pkg:import:fmt"] {
		t.Fatal("expected import edge for 'fmt'")
	}
	if !importTargets["pkg:import:os"] {
		t.Fatal("expected import edge for 'os'")
	}
}
