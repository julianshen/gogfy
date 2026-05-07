package extract

import (
	"os"
	"path/filepath"
	"testing"
)

type extractorCase struct {
	name      string
	filename  string
	source    string
	extractor func(string) (Result, error)
	wantNodes []string // labels expected to appear among result.Nodes
	wantEdges []string // edge targets expected (for "imports"/"contains")
}

func runExtractorCase(t *testing.T, tc extractorCase) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, tc.filename)
	if err := os.WriteFile(path, []byte(tc.source), 0644); err != nil {
		t.Fatal(err)
	}
	res, err := tc.extractor(path)
	if err != nil {
		t.Fatalf("%s: extract: %v", tc.name, err)
	}
	labels := map[string]bool{}
	for _, n := range res.Nodes {
		labels[n.Label] = true
	}
	for _, want := range tc.wantNodes {
		if !labels[want] {
			t.Fatalf("%s: missing node label %q (got labels=%v)", tc.name, want, labels)
		}
	}
	targets := map[string]bool{}
	for _, e := range res.Edges {
		targets[e.Target] = true
	}
	for _, want := range tc.wantEdges {
		if !targets[want] {
			t.Fatalf("%s: missing edge target %q (got targets=%v)", tc.name, want, targets)
		}
	}
}

func TestExtractorsMissingFile(t *testing.T) {
	extractors := []Extractor{
		GoExtractor{},
		PythonExtractor{},
		JavaScriptExtractor{},
		TypeScriptExtractor{},
		TypeScriptExtractor{TSX: true},
		JavaExtractor{},
		CExtractor{},
		CppExtractor{},
		RustExtractor{},
		RubyExtractor{},
		YAMLExtractor{},
		TOMLExtractor{},
	}
	for _, ex := range extractors {
		if _, err := ex.Extract("/nonexistent/path/does-not-exist.txt"); err == nil {
			t.Errorf("%T: expected error for missing file", ex)
		}
	}
}

func TestJavaScriptExtractor(t *testing.T) {
	runExtractorCase(t, extractorCase{
		name:     "js basic",
		filename: "main.js",
		source: `import fs from 'fs';
import {a, b as c} from './lib';
function greet() { return 'hi'; }
class Hello { method() {} }
`,
		extractor: JavaScriptExtractor{}.Extract,
		wantNodes: []string{"main.js", "greet", "Hello"},
		wantEdges: []string{"js:import:fs", "js:import:./lib"},
	})
}

func TestTypeScriptExtractor(t *testing.T) {
	runExtractorCase(t, extractorCase{
		name:     "ts basic",
		filename: "main.ts",
		source: `import {Foo} from './x';
type T = number;
interface I { x: number }
class C { m(): void {} }
function bar(): void {}
`,
		extractor: TypeScriptExtractor{}.Extract,
		wantNodes: []string{"main.ts", "T", "I", "C", "bar"},
		wantEdges: []string{"ts:import:./x"},
	})
}

func TestJavaExtractor(t *testing.T) {
	runExtractorCase(t, extractorCase{
		name:     "java basic",
		filename: "Hello.java",
		source: `package com.example;
import java.util.List;
import java.util.Map;
public class Hello {
    public void greet() {}
}
`,
		extractor: JavaExtractor{}.Extract,
		wantNodes: []string{"Hello.java", "Hello", "greet"},
		wantEdges: []string{"java:import:java.util.List", "java:import:java.util.Map"},
	})
}

func TestCExtractor(t *testing.T) {
	runExtractorCase(t, extractorCase{
		name:     "c basic",
		filename: "main.c",
		source: `#include <stdio.h>
#include "local.h"
int add(int a, int b) { return a+b; }
struct Point { int x; int y; };
`,
		extractor: CExtractor{}.Extract,
		wantNodes: []string{"main.c", "add", "Point"},
		wantEdges: []string{"c:import:stdio.h", "c:import:local.h"},
	})
}

func TestCppExtractor(t *testing.T) {
	runExtractorCase(t, extractorCase{
		name:     "cpp basic",
		filename: "main.cpp",
		source: `#include <iostream>
namespace ns { class Foo { public: void m(); }; }
int main() { return 0; }
`,
		extractor: CppExtractor{}.Extract,
		wantNodes: []string{"main.cpp", "ns", "Foo", "main"},
		wantEdges: []string{"cpp:import:iostream"},
	})
}

func TestRustExtractor(t *testing.T) {
	runExtractorCase(t, extractorCase{
		name:     "rust basic",
		filename: "lib.rs",
		source: `use std::collections::HashMap;
mod inner;
pub struct Point { x: i32, y: i32 }
pub fn add(a: i32, b: i32) -> i32 { a+b }
trait Shape { fn area(&self) -> f64; }
`,
		extractor: RustExtractor{}.Extract,
		wantNodes: []string{"lib.rs", "Point", "add", "Shape", "inner"},
		wantEdges: []string{"rust:import:std::collections::HashMap"},
	})
}

func TestRubyExtractor(t *testing.T) {
	runExtractorCase(t, extractorCase{
		name:     "ruby basic",
		filename: "main.rb",
		source: `require 'json'
require_relative 'lib'
module Greetings
  class Hello
    def say; end
  end
end
def top_level; end
`,
		extractor: RubyExtractor{}.Extract,
		wantNodes: []string{"main.rb", "Greetings", "Hello", "say", "top_level"},
		wantEdges: []string{"ruby:import:json", "ruby:import:lib"},
	})
}

func TestYAMLExtractor(t *testing.T) {
	runExtractorCase(t, extractorCase{
		name:     "yaml basic",
		filename: "config.yaml",
		source: `name: gogfy
version: "1.0"
deps:
  - go
  - python
config:
  enabled: true
`,
		extractor: YAMLExtractor{}.Extract,
		wantNodes: []string{"config.yaml", "name", "version", "deps", "config"},
		wantEdges: []string{},
	})
}

func TestTOMLExtractor(t *testing.T) {
	runExtractorCase(t, extractorCase{
		name:     "toml basic",
		filename: "config.toml",
		source: `name = "gogfy"
version = "1.0"

[deps]
go = "1.24"

[[packages]]
name = "extract"
`,
		extractor: TOMLExtractor{}.Extract,
		wantNodes: []string{"config.toml", "name", "version", "deps", "packages"},
		wantEdges: []string{},
	})
}
