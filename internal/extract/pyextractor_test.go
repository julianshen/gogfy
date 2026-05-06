package extract

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/julianshen/gogfy/internal/schema"
)

func TestPythonExtractor(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "main.py")
	if err := os.WriteFile(path, []byte(`import os
import sys as system
from collections import OrderedDict

def hello():
    print("hi")

class MyClass:
    def method(self):
        pass
`), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &PythonExtractor{}
	result, err := ex.Extract(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Nodes) == 0 {
		t.Fatal("expected nodes")
	}

	// Verify module node exists
	var moduleNode *schema.Node
	for i := range result.Nodes {
		if result.Nodes[i].Label == "main.py" {
			moduleNode = &result.Nodes[i]
			break
		}
	}
	if moduleNode == nil {
		t.Fatal("expected module node with label 'main.py'")
	}
	if moduleNode.SourceFile == "" {
		t.Fatal("expected module node to have SourceFile")
	}

	// Verify function node exists
	funcLabels := make(map[string]bool)
	for _, n := range result.Nodes {
		if n.Label == "hello" || n.Label == "method" {
			funcLabels[n.Label] = true
		}
	}
	if !funcLabels["hello"] {
		t.Fatal("expected function node 'hello'")
	}
	if !funcLabels["method"] {
		t.Fatal("expected function node 'method'")
	}

	// Verify class node exists
	classLabels := make(map[string]bool)
	for _, n := range result.Nodes {
		if n.Label == "MyClass" {
			classLabels[n.Label] = true
		}
	}
	if !classLabels["MyClass"] {
		t.Fatal("expected class node 'MyClass'")
	}

	// Verify import edges exist
	importTargets := make(map[string]bool)
	for _, e := range result.Edges {
		if e.Relation == "imports" {
			importTargets[e.Target] = true
		}
	}
	if !importTargets["py:import:os"] {
		t.Fatal("expected import edge for 'os'")
	}
	if !importTargets["py:import:sys"] {
		t.Fatal("expected import edge for 'sys'")
	}
	if !importTargets["py:import:collections.OrderedDict"] {
		t.Fatal("expected import edge for 'collections.OrderedDict'")
	}
}

func TestPythonExtractorNoImports(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "naked.py")
	if err := os.WriteFile(path, []byte(`def alone():
    pass
`), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &PythonExtractor{}
	result, err := ex.Extract(path)
	if err != nil {
		t.Fatal(err)
	}
	// module + function = 2 nodes
	if len(result.Nodes) != 2 {
		t.Fatalf("expected 2 nodes (module + func), got %d", len(result.Nodes))
	}
	if len(result.Edges) != 0 {
		t.Fatalf("expected 0 edges, got %d", len(result.Edges))
	}
}

func TestPythonExtractorEmptyFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "empty.py")
	if err := os.WriteFile(path, []byte(``), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &PythonExtractor{}
	result, err := ex.Extract(path)
	if err != nil {
		t.Fatal(err)
	}
	// Empty file still has a module node
	if len(result.Nodes) != 1 {
		t.Fatalf("expected 1 node (module), got %d", len(result.Nodes))
	}
	if len(result.Edges) != 0 {
		t.Fatalf("expected 0 edges, got %d", len(result.Edges))
	}
}

func TestPythonExtractorNonExistentFile(t *testing.T) {
	ex := &PythonExtractor{}
	_, err := ex.Extract("/nonexistent/path/file.py")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestPythonExtractorMultipleImports(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "multi.py")
	if err := os.WriteFile(path, []byte(`import os
import sys
import json
`), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &PythonExtractor{}
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
	if !importTargets["py:import:os"] {
		t.Fatal("expected import edge for 'os'")
	}
	if !importTargets["py:import:sys"] {
		t.Fatal("expected import edge for 'sys'")
	}
	if !importTargets["py:import:json"] {
		t.Fatal("expected import edge for 'json'")
	}
}

func TestPythonExtractorFromImportMultiple(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "from_multi.py")
	if err := os.WriteFile(path, []byte(`from typing import List, Dict, Optional
`), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &PythonExtractor{}
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
	if !importTargets["py:import:typing.List"] {
		t.Fatal("expected import edge for 'typing.List'")
	}
	if !importTargets["py:import:typing.Dict"] {
		t.Fatal("expected import edge for 'typing.Dict'")
	}
	if !importTargets["py:import:typing.Optional"] {
		t.Fatal("expected import edge for 'typing.Optional'")
	}
}
