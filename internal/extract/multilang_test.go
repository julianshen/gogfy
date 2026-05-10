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
