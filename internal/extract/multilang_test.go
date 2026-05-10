package extract

import (
	"os"
	"path/filepath"
	"strings"
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
		KotlinExtractor{},
		ScalaExtractor{},
		PHPExtractor{},
		LuaExtractor{},
		ZigExtractor{},
		JuliaExtractor{},
		BashExtractor{},
		CSharpExtractor{},
		HaskellExtractor{},
		OCamlExtractor{},
		SvelteExtractor{},
		FortranExtractor{},
		ElixirExtractor{},
		DartExtractor{},
		SwiftExtractor{},
		MarkdownExtractor{},
		HTMLExtractor{},
		TextExtractor{},
		RSTExtractor{},
		DocxExtractor{},
		XlsxExtractor{},
		PDFExtractor{},
		PPTXExtractor{},
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

func TestRustExtractorUseListAliasWildcard(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lib.rs")
	source := `use std::collections::{HashMap, HashSet};
use foo as bar;
use crate::lib::*;
`
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	res, err := RustExtractor{}.Extract(path)
	if err != nil {
		t.Fatal(err)
	}
	targets := map[string]bool{}
	for _, e := range res.Edges {
		if e.Relation == "imports" {
			targets[e.Target] = true
		}
	}
	wants := []string{
		"rust:import:std::collections::HashMap",
		"rust:import:std::collections::HashSet",
		"rust:import:foo",
		"rust:import:crate::lib",
	}
	for _, w := range wants {
		if !targets[w] {
			t.Fatalf("missing edge target %q (got %v)", w, targets)
		}
	}
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

func TestKotlinExtractor(t *testing.T) {
	runExtractorCase(t, extractorCase{
		name:      "kotlin basic",
		filename:  "Hello.kt",
		extractor: KotlinExtractor{}.Extract,
		source: `package com.example
import kotlin.collections.List
class Foo { fun bar() {} }
object Singleton
fun greet() {}
`,
		wantNodes: []string{"Hello.kt", "Foo", "Singleton", "greet"},
		wantEdges: []string{"kotlin:import:kotlin.collections.List"},
	})
}

func TestScalaImportSelectorsAndRenames(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "S.scala")
	body := `import scala.collection.{Map, Set}
import a.b.{Foo => Bar}
`
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	res, err := ScalaExtractor{}.Extract(path)
	if err != nil {
		t.Fatal(err)
	}
	targets := map[string]bool{}
	for _, e := range res.Edges {
		if e.Relation == "imports" {
			targets[e.Target] = true
		}
	}
	for _, want := range []string{
		"scala:import:scala.collection.Map",
		"scala:import:scala.collection.Set",
		"scala:import:a.b.Foo",
	} {
		if !targets[want] {
			t.Fatalf("missing %q (got %v)", want, targets)
		}
	}
}

func TestPHPGroupUseAndMultiClause(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "g.php")
	body := `<?php
use App\Lib\{Foo, Bar};
use A\B, A\C;
`
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	res, err := PHPExtractor{}.Extract(path)
	if err != nil {
		t.Fatal(err)
	}
	targets := map[string]bool{}
	for _, e := range res.Edges {
		if e.Relation == "imports" {
			targets[e.Target] = true
		}
	}
	for _, want := range []string{
		`php:import:App\Lib\Foo`,
		`php:import:App\Lib\Bar`,
		`php:import:A\B`,
		`php:import:A\C`,
	} {
		if !targets[want] {
			t.Fatalf("missing %q (got %v)", want, targets)
		}
	}
}

func TestJuliaMultiUsing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "m.jl")
	if err := os.WriteFile(path, []byte("using LinearAlgebra, Statistics\n"), 0644); err != nil {
		t.Fatal(err)
	}
	res, err := JuliaExtractor{}.Extract(path)
	if err != nil {
		t.Fatal(err)
	}
	targets := map[string]bool{}
	for _, e := range res.Edges {
		if e.Relation == "imports" {
			targets[e.Target] = true
		}
	}
	for _, want := range []string{"julia:import:LinearAlgebra", "julia:import:Statistics"} {
		if !targets[want] {
			t.Fatalf("missing %q (got %v)", want, targets)
		}
	}
}

func TestBashSourceQuoted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "q.sh")
	body := `source "lib.sh"
. 'other.sh'
`
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	res, err := BashExtractor{}.Extract(path)
	if err != nil {
		t.Fatal(err)
	}
	targets := map[string]bool{}
	for _, e := range res.Edges {
		if e.Relation == "imports" {
			targets[e.Target] = true
		}
	}
	for _, want := range []string{"bash:import:lib.sh", "bash:import:other.sh"} {
		if !targets[want] {
			t.Fatalf("missing %q (got %v)", want, targets)
		}
	}
}

func TestJuliaSelectedImport(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jl")
	body := "import Foo: a, b\n"
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	res, err := JuliaExtractor{}.Extract(path)
	if err != nil {
		t.Fatal(err)
	}
	targets := map[string]bool{}
	for _, e := range res.Edges {
		if e.Relation == "imports" {
			targets[e.Target] = true
		}
	}
	for _, want := range []string{"julia:import:Foo.a", "julia:import:Foo.b"} {
		if !targets[want] {
			t.Fatalf("missing %q (got %v)", want, targets)
		}
	}
}

func TestScalaExtractor(t *testing.T) {
	runExtractorCase(t, extractorCase{
		name:      "scala basic",
		filename:  "Hello.scala",
		extractor: ScalaExtractor{}.Extract,
		source: `package com.example
import scala.collection.mutable.Map
class Foo { def bar(): Unit = {} }
object Bar { def baz(): Int = 0 }
trait T
`,
		wantNodes: []string{"Hello.scala", "Foo", "Bar", "T", "bar", "baz"},
		wantEdges: []string{"scala:import:scala.collection.mutable.Map"},
	})
}

func TestPHPExtractor(t *testing.T) {
	runExtractorCase(t, extractorCase{
		name:      "php basic",
		filename:  "main.php",
		extractor: PHPExtractor{}.Extract,
		source: `<?php
namespace App;
use App\Lib\Foo;
class Bar { public function method() {} }
function greet() {}
interface I {}
trait T {}
enum Status { case Active; case Inactive; }
`,
		wantNodes: []string{"main.php", "Bar", "method", "greet", "I", "T", "Status"},
		wantEdges: []string{`php:import:App\Lib\Foo`},
	})
}

func TestLuaExtractor(t *testing.T) {
	runExtractorCase(t, extractorCase{
		name:      "lua basic",
		filename:  "main.lua",
		extractor: LuaExtractor{}.Extract,
		source: `local M = require("module")
local m2 = require "other"
function M.hello() end
local function priv() end
`,
		wantNodes: []string{"main.lua", "hello", "priv"},
		wantEdges: []string{"lua:import:module", "lua:import:other"},
	})
}

func TestZigExtractor(t *testing.T) {
	runExtractorCase(t, extractorCase{
		name:      "zig basic",
		filename:  "main.zig",
		extractor: ZigExtractor{}.Extract,
		source: `const std = @import("std");
pub fn add(a: i32, b: i32) i32 { return a + b; }
const Point = struct { x: i32, y: i32 };
const E = enum { A, B };
const U = union { i: i32, f: f32 };
`,
		wantNodes: []string{"main.zig", "add", "Point", "E", "U"},
		wantEdges: []string{"zig:import:std"},
	})
}

func TestJuliaExtractor(t *testing.T) {
	runExtractorCase(t, extractorCase{
		name:      "julia basic",
		filename:  "main.jl",
		extractor: JuliaExtractor{}.Extract,
		source: `using LinearAlgebra
import Base
module Foo
struct Point x; y end
function bar(a, b) return a+b end
end
`,
		wantNodes: []string{"main.jl", "Foo", "bar", "Point"},
		wantEdges: []string{"julia:import:LinearAlgebra", "julia:import:Base"},
	})
}

func TestBashExtractor(t *testing.T) {
	runExtractorCase(t, extractorCase{
		name:      "bash basic",
		filename:  "build.sh",
		extractor: BashExtractor{}.Extract,
		source: `#!/bin/bash
source ./lib.sh
. ./other.sh
greet() { echo hi; }
function bye { echo bye; }
`,
		wantNodes: []string{"build.sh", "greet", "bye"},
		wantEdges: []string{"bash:import:./lib.sh", "bash:import:./other.sh"},
	})
}

func TestCSharpExtractor(t *testing.T) {
	runExtractorCase(t, extractorCase{
		name:     "csharp basic",
		filename: "Program.cs",
		source: `using System;
using System.Collections.Generic;

namespace MyApp.Core
{
    public class Greeter
    {
        public void Greet(string name)
        {
            Console.WriteLine("Hello " + name);
        }
    }
}
`,
		extractor: CSharpExtractor{}.Extract,
		wantNodes: []string{"MyApp.Core", "Greeter", "Greet"},
		wantEdges: []string{
			"csharp:import:System",
			"csharp:import:System.Collections.Generic",
		},
	})
}

func TestCSharpExtractorEmitsCalls(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Calls.cs")
	source := `class C {
    void M() {
        Helper();
        obj.Other();
    }
}
`
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	res, err := CSharpExtractor{}.Extract(path)
	if err != nil {
		t.Fatal(err)
	}
	targets := map[string]bool{}
	for _, e := range res.Edges {
		if e.Relation == "calls" {
			targets[e.Target] = true
		}
	}
	for _, want := range []string{"csharp:call:Helper", "csharp:call:Other"} {
		if !targets[want] {
			t.Fatalf("missing call target %q in %v", want, targets)
		}
	}
}

func TestHaskellExtractor(t *testing.T) {
	runExtractorCase(t, extractorCase{
		name:     "haskell basic",
		filename: "Main.hs",
		source: `module Main where
import Data.List
import qualified Data.Map as Map

greet :: String -> String
greet name = "Hello " ++ name

main :: IO ()
main = putStrLn (greet "world")
`,
		extractor: HaskellExtractor{}.Extract,
		wantNodes: []string{"Main", "greet", "main"},
		wantEdges: []string{
			"haskell:import:Data.List",
			"haskell:import:Data.Map",
		},
	})
}

func TestCSharpExtractorRecordDeclaration(t *testing.T) {
	runExtractorCase(t, extractorCase{
		name:      "csharp record",
		filename:  "Models.cs",
		source:    "public record Point(int X, int Y);\n",
		extractor: CSharpExtractor{}.Extract,
		wantNodes: []string{"Point"},
	})
}

func TestHaskellExtractorEmitsCalls(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Calls.hs")
	source := `module M where
greet name = putStrLn name
`
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	res, err := HaskellExtractor{}.Extract(path)
	if err != nil {
		t.Fatal(err)
	}
	targets := map[string]bool{}
	for _, e := range res.Edges {
		if e.Relation == "calls" {
			targets[e.Target] = true
		}
	}
	if !targets["haskell:call:putStrLn"] {
		t.Fatalf("expected haskell:call:putStrLn in %v", targets)
	}
}

func TestOCamlExtractorInterfaceFile(t *testing.T) {
	runExtractorCase(t, extractorCase{
		name:      "ocaml interface",
		filename:  "lib.mli",
		source:    "val greet : string -> string\n",
		extractor: OCamlExtractor{}.Extract,
		wantNodes: []string{"greet"},
	})
}

func TestOCamlExtractor(t *testing.T) {
	runExtractorCase(t, extractorCase{
		name:     "ocaml basic",
		filename: "main.ml",
		source: `open Printf
open List

let greet name = "Hello " ^ name

let main () = print_endline (greet "world")
`,
		extractor: OCamlExtractor{}.Extract,
		wantNodes: []string{"greet", "main"},
		wantEdges: []string{
			"ocaml:import:Printf",
			"ocaml:import:List",
		},
	})
}

func TestSvelteExtractorReexports(t *testing.T) {
	runExtractorCase(t, extractorCase{
		name:     "svelte re-export",
		filename: "index.svelte",
		source: `<script>
  export { default as Btn } from './Button.svelte';
  export * from './utils';
</script>
`,
		extractor: SvelteExtractor{}.Extract,
		wantNodes: []string{"index.svelte"},
		wantEdges: []string{
			"svelte:import:./Button.svelte",
			"svelte:import:./utils",
		},
	})
}

func TestSvelteExtractorImports(t *testing.T) {
	runExtractorCase(t, extractorCase{
		name:     "svelte basic",
		filename: "App.svelte",
		source: `<script lang="ts">
  import { onMount } from 'svelte';
  import Button from './Button.svelte';
  import "side-effect.css";
  let count = 0;
</script>

<button on:click={() => count++}>{count}</button>
`,
		extractor: SvelteExtractor{}.Extract,
		wantNodes: []string{"App.svelte"},
		wantEdges: []string{
			"svelte:import:svelte",
			"svelte:import:./Button.svelte",
			"svelte:import:side-effect.css",
		},
	})
}

func TestDartExtractorMethodNotDoubleEmitted(t *testing.T) {
	// method_signature wraps function_signature. Walking the method node
	// must NOT also emit a function-kinded decl from the inner
	// function_signature — that would mean every Dart method shows up
	// twice in the graph (once as method, once as function).
	dir := t.TempDir()
	path := filepath.Join(dir, "G.dart")
	source := `class G {
  String greet(String name) => 'Hi $name';
}
`
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	res, err := DartExtractor{}.Extract(path)
	if err != nil {
		t.Fatal(err)
	}
	greetCount := 0
	for _, n := range res.Nodes {
		if n.Label == "greet" {
			greetCount++
		}
	}
	if greetCount != 1 {
		t.Fatalf("expected exactly one `greet` node, got %d: %+v", greetCount, res.Nodes)
	}
}

func TestDartExtractorDeferredImport(t *testing.T) {
	// `import 'foo.dart' deferred as Bar;` — the URI sits as a direct
	// `uri` child of import_specification, not wrapped in
	// configurable_uri. PR #36's simplification accidentally dropped
	// this shape; restored.
	runExtractorCase(t, extractorCase{
		name:     "dart deferred import",
		filename: "lazy.dart",
		source: `import 'package:foo/lazy.dart' deferred as lazy;
`,
		extractor: DartExtractor{}.Extract,
		wantEdges: []string{"dart:import:package:foo/lazy.dart"},
	})
}

func TestDartExtractorMixinExtensionEnum(t *testing.T) {
	runExtractorCase(t, extractorCase{
		name:     "dart mixin/extension/enum",
		filename: "shapes.dart",
		source: `mixin Drawable {
  void draw();
}

extension StringExt on String {
  bool get isShout => this == toUpperCase();
}

enum Suit { clubs, diamonds, hearts, spades }
`,
		extractor: DartExtractor{}.Extract,
		wantNodes: []string{"Drawable", "StringExt", "Suit"},
	})
}

func TestElixirExtractorNoSelfCallFromDefHeader(t *testing.T) {
	// `def foo(x, y) do ... end` is itself a `call` node whose arguments
	// contain another `call` (the function header). Walking the def's
	// children naively would emit `elixir:call:foo` from foo's body —
	// every defined function would self-edge to itself. Regression test:
	// the def must not produce a call edge whose target equals its own
	// declared name.
	dir := t.TempDir()
	path := filepath.Join(dir, "self.ex")
	source := `defmodule M do
  def foo(x), do: x
  def bar(x) do
    foo(x)
  end
end
`
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	res, err := ElixirExtractor{}.Extract(path)
	if err != nil {
		t.Fatal(err)
	}
	calls := map[string]bool{}
	for _, e := range res.Edges {
		if e.Relation == "calls" {
			calls[e.Target] = true
		}
	}
	if calls["elixir:call:foo"] && calls["elixir:call:bar"] {
		// We expect bar to call foo (one edge), not both functions to
		// self-emit. Distinguish by checking only the function-headers
		// don't appear as call targets when there's no body call.
	}
	// Specifically: there must be NO call edge whose target equals the
	// function being defined and whose source is the def itself.
	for _, e := range res.Edges {
		if e.Relation == "calls" && e.Source == "elixir:function:"+path+":foo" && e.Target == "elixir:call:foo" {
			t.Fatalf("self-call edge from foo to itself: %+v", e)
		}
		if e.Relation == "calls" && e.Source == "elixir:function:"+path+":bar" && e.Target == "elixir:call:bar" {
			t.Fatalf("self-call edge from bar to itself: %+v", e)
		}
	}
	// Sanity: bar's body call to foo should still be captured.
	foundBarCallsFoo := false
	for _, e := range res.Edges {
		if e.Relation == "calls" && e.Source == "elixir:function:"+path+":bar" && e.Target == "elixir:call:foo" {
			foundBarCallsFoo = true
		}
	}
	if !foundBarCallsFoo {
		t.Fatalf("expected bar→foo call edge, got %+v", res.Edges)
	}
}

func TestElixirExtractorMacrosAndUseAndGuard(t *testing.T) {
	runExtractorCase(t, extractorCase{
		name:     "elixir defmacro + use + when guard",
		filename: "advanced.ex",
		source: `defmodule MyApp.Macros do
  use GenServer
  use Phoenix.Controller

  defmacro guard_clause(x) do
    quote do
      is_atom(unquote(x))
    end
  end

  defmacrop internal_helper(y), do: quote(do: unquote(y))

  def safe_call(arg) when is_binary(arg) do
    String.upcase(arg)
  end
end
`,
		extractor: ElixirExtractor{}.Extract,
		wantNodes: []string{
			"MyApp.Macros",
			"guard_clause",
			"internal_helper",
			"safe_call", // covers `when guard` shape — exercises callHeadIdent
		},
		wantEdges: []string{
			"elixir:import:GenServer",
			"elixir:import:Phoenix.Controller",
		},
	})
}

func TestFortranExtractorCallExpression(t *testing.T) {
	// Coverage for the `call_expression` half of the merged case (a
	// function call in expression position, distinct from a `subroutine_call`).
	runExtractorCase(t, extractorCase{
		name:     "fortran function call expression",
		filename: "expr.f90",
		source: `program main
  integer :: x
  x = compute(5)
end program main
`,
		extractor: FortranExtractor{}.Extract,
		wantEdges: []string{"fortran:call:compute"},
	})
}

func TestSwiftExtractorLambda(t *testing.T) {
	// Closures push an anonymous scope so calls inside attribute to the
	// enclosing function rather than the program-level module.
	runExtractorCase(t, extractorCase{
		name:     "swift lambda",
		filename: "Lambda.swift",
		source: `func process() {
    let nums = [1, 2, 3]
    let doubled = nums.map { x in
        compute(x)
    }
    print(doubled)
}
`,
		extractor: SwiftExtractor{}.Extract,
		wantNodes: []string{"process"},
		wantEdges: []string{
			"swift:call:compute",
			"swift:call:print",
		},
	})
}

func TestMarkdownExtractorHeadingsAndLinks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "README.md")
	source := `# Project Title

Intro paragraph.

## Installation

See [the docs](https://example.com/docs) and [setup](./setup.md).

## Usage

Refer to <https://example.com/api> for details.

### Detail

Extra detail (H3, captured as section).

#### Sub-detail

Too deep — folded into Detail's scope, not its own node.
`
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	res, err := MarkdownExtractor{}.Extract(path)
	if err != nil {
		t.Fatal(err)
	}
	labels := map[string]bool{}
	for _, n := range res.Nodes {
		labels[n.Label] = true
	}
	// Module label should be the H1, not the filename.
	if !labels["Project Title"] {
		t.Fatalf("expected module label 'Project Title', got labels=%v", labels)
	}
	for _, want := range []string{"Installation", "Usage", "Detail"} {
		if !labels[want] {
			t.Fatalf("missing section %q in %v", want, labels)
		}
	}
	// H4 must NOT produce a section node.
	if labels["Sub-detail"] {
		t.Fatalf("H4 'Sub-detail' should not be a section node: %v", labels)
	}
	targets := map[string]bool{}
	for _, e := range res.Edges {
		if e.Relation == "references" {
			targets[e.Target] = true
		}
	}
	for _, want := range []string{
		"markdown:link:https://example.com/docs",
		"markdown:link:./setup.md",
		"markdown:link:https://example.com/api",
	} {
		if !targets[want] {
			t.Fatalf("missing reference target %q in %v", want, targets)
		}
	}
}

func TestMarkdownExtractorFallsBackToBasenameWhenNoH1(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "no_h1.md")
	source := `Just some prose.

## Section without title above

Some content.
`
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	res, err := MarkdownExtractor{}.Extract(path)
	if err != nil {
		t.Fatal(err)
	}
	foundFilename := false
	for _, n := range res.Nodes {
		if n.Label == "no_h1.md" {
			foundFilename = true
		}
	}
	if !foundFilename {
		t.Fatalf("expected basename fallback for module label, got %+v", res.Nodes)
	}
}

func TestMarkdownExtractorLinksAttributedToEnclosingSection(t *testing.T) {
	// A link inside a section should be sourced from that section's node,
	// not from the document. This is what makes the surprising-connections
	// analysis useful for docs — it shows which section references what.
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.md")
	source := `# Top

## Sec A

[a-link](https://a.example)

## Sec B

[b-link](https://b.example)
`
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	res, err := MarkdownExtractor{}.Extract(path)
	if err != nil {
		t.Fatal(err)
	}
	// Find the section node IDs.
	var secAID, secBID string
	for _, n := range res.Nodes {
		if n.Label == "Sec A" {
			secAID = n.ID
		}
		if n.Label == "Sec B" {
			secBID = n.ID
		}
	}
	if secAID == "" || secBID == "" {
		t.Fatalf("missing section node IDs: %+v", res.Nodes)
	}
	srcByTarget := map[string]string{}
	for _, e := range res.Edges {
		srcByTarget[e.Target] = e.Source
	}
	if srcByTarget["markdown:link:https://a.example"] != secAID {
		t.Fatalf("a-link should source from Sec A, got %s", srcByTarget["markdown:link:https://a.example"])
	}
	if srcByTarget["markdown:link:https://b.example"] != secBID {
		t.Fatalf("b-link should source from Sec B, got %s", srcByTarget["markdown:link:https://b.example"])
	}
}

func TestHTMLExtractorTitleHeadingsAndLinks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.html")
	source := `<!DOCTYPE html>
<html>
<head><title>My Doc</title></head>
<body>
  <h1>Page Heading</h1>
  <p>Intro</p>
  <h2>First Section</h2>
  <p>See <a href="https://example.com/api">the API docs</a> and
     <a href="./related.html">related</a>.</p>
  <h2>Second Section</h2>
  <p>Jump to <a href="#fragment">section above</a> — should be skipped.</p>
  <p>Visit <a href="./other.html#anchor">other doc</a> instead.</p>
  <h4>Too deep</h4>
  <p>Should NOT become a section node.</p>
</body>
</html>`
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	res, err := HTMLExtractor{}.Extract(path)
	if err != nil {
		t.Fatal(err)
	}
	labels := map[string]bool{}
	for _, n := range res.Nodes {
		labels[n.Label] = true
	}
	if !labels["My Doc"] {
		t.Fatalf("module label should be <title>, got labels=%v", labels)
	}
	for _, want := range []string{"Page Heading", "First Section", "Second Section"} {
		if !labels[want] {
			t.Fatalf("missing section %q in %v", want, labels)
		}
	}
	if labels["Too deep"] {
		t.Fatalf("h4 should not produce a section node: %v", labels)
	}
	targets := map[string]bool{}
	for _, e := range res.Edges {
		if e.Relation == "references" {
			targets[e.Target] = true
		}
	}
	for _, want := range []string{
		"html:link:https://example.com/api",
		"html:link:./related.html",
		"html:link:./other.html#anchor",
	} {
		if !targets[want] {
			t.Fatalf("missing reference target %q in %v", want, targets)
		}
	}
	if targets["html:link:#fragment"] {
		t.Fatalf("self-fragment link should be skipped: %v", targets)
	}
}

func TestHTMLExtractorFallsBackThroughTitleH1Basename(t *testing.T) {
	dir := t.TempDir()
	// No <title>, no <h1> — basename should win.
	path := filepath.Join(dir, "no_title.html")
	if err := os.WriteFile(path, []byte(`<html><body><h2>Only h2</h2></body></html>`), 0644); err != nil {
		t.Fatal(err)
	}
	res, _ := HTMLExtractor{}.Extract(path)
	got := ""
	for _, n := range res.Nodes {
		if strings.Contains(n.ID, "html:module:") {
			got = n.Label
		}
	}
	if got != "no_title.html" {
		t.Fatalf("expected basename fallback, got %q", got)
	}

	// <title> wins over <h1>.
	path2 := filepath.Join(dir, "with_both.html")
	if err := os.WriteFile(path2, []byte(`<html><head><title>T</title></head><body><h1>H</h1></body></html>`), 0644); err != nil {
		t.Fatal(err)
	}
	res2, _ := HTMLExtractor{}.Extract(path2)
	got2 := ""
	for _, n := range res2.Nodes {
		if strings.Contains(n.ID, "html:module:") {
			got2 = n.Label
		}
	}
	if got2 != "T" {
		t.Fatalf("expected <title> to win, got %q", got2)
	}

	// No <title>, but <h1> present.
	path3 := filepath.Join(dir, "h1_only.html")
	if err := os.WriteFile(path3, []byte(`<html><body><h1>H Only</h1></body></html>`), 0644); err != nil {
		t.Fatal(err)
	}
	res3, _ := HTMLExtractor{}.Extract(path3)
	got3 := ""
	for _, n := range res3.Nodes {
		if strings.Contains(n.ID, "html:module:") {
			got3 = n.Label
		}
	}
	if got3 != "H Only" {
		t.Fatalf("expected <h1> fallback, got %q", got3)
	}
}

func TestTextExtractorURLs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "NOTES.txt")
	source := `Project notes.

See https://example.com/api for details, or check the issue tracker
at https://github.com/foo/bar/issues. Also (https://example.com/sla);
trailing punctuation should be stripped.

Not a URL: just-text.no-scheme/here
`
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	res, err := TextExtractor{}.Extract(path)
	if err != nil {
		t.Fatal(err)
	}
	targets := map[string]bool{}
	for _, e := range res.Edges {
		if e.Relation == "references" {
			targets[e.Target] = true
		}
	}
	for _, want := range []string{
		"text:link:https://example.com/api",
		"text:link:https://github.com/foo/bar/issues",
		"text:link:https://example.com/sla",
	} {
		if !targets[want] {
			t.Fatalf("missing reference target %q in %v", want, targets)
		}
	}
	// Trailing `;` and `)` must be stripped.
	for bad := range targets {
		if strings.HasSuffix(bad, ";") || strings.HasSuffix(bad, ")") {
			t.Fatalf("trailing punctuation not stripped: %q", bad)
		}
	}
}

func TestRSTExtractorHeadingsAndLinks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.rst")
	source := `My Project
==========

Intro.

Installation
------------

Run ` + "``pip install foo``" + `, then visit ` + "`the docs <https://example.com/docs>`_" + ` for next steps.

Usage
-----

Bare URL: https://example.com/api.

Subsection
~~~~~~~~~~

Level 3 — should still be a section node.

Deep
^^^^

Level 4 — should NOT.
`
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	res, err := RSTExtractor{}.Extract(path)
	if err != nil {
		t.Fatal(err)
	}
	labels := map[string]bool{}
	for _, n := range res.Nodes {
		labels[n.Label] = true
	}
	if !labels["My Project"] {
		t.Fatalf("module label should be the H1 title, got labels=%v", labels)
	}
	for _, want := range []string{"Installation", "Usage", "Subsection"} {
		if !labels[want] {
			t.Fatalf("missing section %q in %v", want, labels)
		}
	}
	if labels["Deep"] {
		t.Fatalf("level-4 'Deep' should not produce a section node: %v", labels)
	}
	targets := map[string]bool{}
	for _, e := range res.Edges {
		if e.Relation == "references" {
			targets[e.Target] = true
		}
	}
	for _, want := range []string{
		"rst:link:https://example.com/docs",
		"rst:link:https://example.com/api",
	} {
		if !targets[want] {
			t.Fatalf("missing reference target %q in %v", want, targets)
		}
	}
}

func TestRSTExtractorAdornmentLevelInference(t *testing.T) {
	// First adornment char defines level 1, second defines level 2, etc.
	// This document uses `~` first, then `=`, then `-`. The level
	// assignment should follow first-appearance order, not the visual
	// height of the adornment.
	dir := t.TempDir()
	path := filepath.Join(dir, "weird.rst")
	source := `First
~~~~~

Second
======

Third
-----
`
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	res, err := RSTExtractor{}.Extract(path)
	if err != nil {
		t.Fatal(err)
	}
	labels := map[string]bool{}
	for _, n := range res.Nodes {
		labels[n.Label] = true
	}
	for _, want := range []string{"First", "Second", "Third"} {
		if !labels[want] {
			t.Fatalf("missing %q (first-appearance level inference broken): %v", want, labels)
		}
	}
}

// distinctIDs returns IDs of nodes with the given label.
func sectionIDsForLabel(res Result, label string) map[string]bool {
	out := map[string]bool{}
	for _, n := range res.Nodes {
		if n.Label == label {
			out[n.ID] = true
		}
	}
	return out
}

func TestMarkdownExtractorDuplicateHeadingsProduceDistinctIDs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.md")
	source := "# Top\n\n## Examples\n\nFirst.\n\n## Examples\n\nSecond.\n"
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	res, err := MarkdownExtractor{}.Extract(path)
	if err != nil {
		t.Fatal(err)
	}
	ids := sectionIDsForLabel(res, "Examples")
	if len(ids) != 2 {
		t.Fatalf("two H2 'Examples' must produce two distinct IDs, got %d (%v)", len(ids), ids)
	}
}

func TestHTMLExtractorDuplicateHeadingsProduceDistinctIDs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.html")
	source := `<html><body><h2>Examples</h2><p>1</p><h2>Examples</h2><p>2</p></body></html>`
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	res, err := HTMLExtractor{}.Extract(path)
	if err != nil {
		t.Fatal(err)
	}
	ids := sectionIDsForLabel(res, "Examples")
	if len(ids) != 2 {
		t.Fatalf("two <h2>Examples must produce two distinct IDs, got %d (%v)", len(ids), ids)
	}
}

func TestRSTExtractorDuplicateHeadingsProduceDistinctIDs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.rst")
	source := "Top\n===\n\nExamples\n--------\n\nFirst.\n\nExamples\n--------\n\nSecond.\n"
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	res, err := RSTExtractor{}.Extract(path)
	if err != nil {
		t.Fatal(err)
	}
	ids := sectionIDsForLabel(res, "Examples")
	if len(ids) != 2 {
		t.Fatalf("two RST 'Examples' sections must produce two distinct IDs, got %d (%v)", len(ids), ids)
	}
}

func TestSwiftExtractorBasic(t *testing.T) {
	runExtractorCase(t, extractorCase{
		name:     "swift basic",
		filename: "Greeter.swift",
		source: `import Foundation
import os.log

protocol Greetable {
    func greet(_ name: String) -> String
}

class Greeter: Greetable {
    func greet(_ name: String) -> String {
        return "Hello \(name)"
    }
}

func main() {
    let g = Greeter()
    print(g.greet("world"))
}
`,
		extractor: SwiftExtractor{}.Extract,
		wantNodes: []string{"Greetable", "Greeter", "greet", "main"},
		wantEdges: []string{
			"swift:import:Foundation",
			"swift:import:os.log",
			"swift:call:print",
			"swift:call:greet",
		},
	})
}

func TestDartExtractorBasic(t *testing.T) {
	runExtractorCase(t, extractorCase{
		name:     "dart basic",
		filename: "main.dart",
		source: `library my_app;
import 'package:flutter/material.dart';
import 'dart:async';
export 'src/utils.dart';

class Greeter {
  String greet(String name) => 'Hello $name';
}

void main() {
  print(Greeter().greet('world'));
}
`,
		extractor: DartExtractor{}.Extract,
		wantNodes: []string{"Greeter", "greet", "main"},
		wantEdges: []string{
			"dart:import:package:flutter/material.dart",
			"dart:import:dart:async",
			"dart:import:src/utils.dart",
		},
	})
}

func TestElixirExtractorBasic(t *testing.T) {
	runExtractorCase(t, extractorCase{
		name:     "elixir basic",
		filename: "greeter.ex",
		source: `defmodule MyApp.Greeter do
  alias MyApp.Util
  import Logger
  require Ecto.Query

  def greet(name) do
    Util.upcase(name)
  end

  defp internal_helper(x), do: x + 1
end
`,
		extractor: ElixirExtractor{}.Extract,
		wantNodes: []string{"MyApp.Greeter", "greet", "internal_helper"},
		wantEdges: []string{
			"elixir:import:MyApp.Util",
			"elixir:import:Logger",
			"elixir:import:Ecto.Query",
		},
	})
}

func TestFortranExtractorBasic(t *testing.T) {
	runExtractorCase(t, extractorCase{
		name:     "fortran basic",
		filename: "main.f90",
		source: `module greet_mod
  use iso_fortran_env
contains
  subroutine say_hi(name)
    character(*), intent(in) :: name
    print *, "Hello ", name
  end subroutine say_hi

  function add_one(x) result(y)
    integer, intent(in) :: x
    integer :: y
    y = x + 1
  end function add_one
end module greet_mod

program main
  use greet_mod
  call say_hi("world")
end program main
`,
		extractor: FortranExtractor{}.Extract,
		wantNodes: []string{"say_hi", "add_one"},
		wantEdges: []string{
			"fortran:import:iso_fortran_env",
			"fortran:import:greet_mod",
			"fortran:call:say_hi",
		},
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
