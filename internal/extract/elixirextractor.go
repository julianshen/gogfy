package extract

import (
	sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_elixir "github.com/tree-sitter/tree-sitter-elixir/bindings/go"
)

// ElixirExtractor extracts module/function nodes plus alias/import/require/
// use edges and call edges from Elixir sources.
//
// Elixir's grammar treats everything as a `call` — `defmodule Foo do ...`
// is a call whose head is the macro name and whose first argument is the
// alias. We walk all calls and recognize the special macro heads (defmodule,
// def, defp, alias, import, require, use) before falling back to the
// general "function call" interpretation.
type ElixirExtractor struct{}

func (ElixirExtractor) Extract(path string) (Result, error) {
	return runExtraction(path, tree_sitter_elixir.Language(), "elixir", walkElixir)
}

// elixirImportMacros are the macro heads that bring names into scope.
// First positional argument is the module being imported.
var elixirImportMacros = map[string]bool{
	"alias":   true,
	"import":  true,
	"require": true,
	"use":     true,
}

func walkElixir(cursor *sitter.TreeCursor, src []byte, state *extractState) {
	node := cursor.Node()
	if node.Kind() == "call" {
		head := elixirCallHead(node, src)
		switch head {
		case "defmodule":
			if alias := elixirFirstAlias(node, src); alias != "" {
				rewriteModuleLabel(state, alias)
			}
		case "def", "defp", "defmacro", "defmacrop":
			nameNode := elixirDefName(node)
			if nameNode != nil {
				kind := "function"
				if head == "defmacro" || head == "defmacrop" {
					kind = "macro"
				}
				state.emitDecl(kind, node, nameNode, src)
				state.walkFnScope(kind, nameNode, src, cursor, walkElixir)
				return
			}
		default:
			if elixirImportMacros[head] {
				if alias := elixirFirstAlias(node, src); alias != "" {
					state.addImport(alias)
				}
				break
			}
			// General call.
			if head != "" {
				state.addCall(head)
			}
		}
	}
	walkChildren(cursor, func() { walkElixir(cursor, src, state) })
}

// elixirCallHead returns the textual head of a call (the macro/function
// name) — typically the first identifier or alias child.
func elixirCallHead(call *sitter.Node, src []byte) string {
	if call.ChildCount() == 0 {
		return ""
	}
	first := call.Child(0)
	switch first.Kind() {
	case "identifier", "alias":
		return first.Utf8Text(src)
	case "dot":
		// `Module.fun(args)` — last identifier-bearing child is the name.
		if id := lastChildOfKind(first, "identifier", "alias"); id != nil {
			return id.Utf8Text(src)
		}
	}
	return ""
}

// elixirFirstAlias returns the first alias-typed argument under a call,
// for shapes like `defmodule Foo.Bar` and `alias Foo.Bar`.
func elixirFirstAlias(call *sitter.Node, src []byte) string {
	for i := uint(0); i < call.ChildCount(); i++ {
		c := call.Child(i)
		switch c.Kind() {
		case "alias":
			return c.Utf8Text(src)
		case "arguments":
			for j := uint(0); j < c.ChildCount(); j++ {
				ac := c.Child(j)
				if ac.Kind() == "alias" {
					return ac.Utf8Text(src)
				}
			}
		}
	}
	return ""
}

// elixirDefName extracts the defined name from a `def NAME(args) do ...`
// shape. The name is buried inside the `arguments` child as another `call`
// (the function header) whose head is the function name.
func elixirDefName(defCall *sitter.Node) *sitter.Node {
	args := firstChildOfKind(defCall, "arguments")
	if args == nil {
		return nil
	}
	for i := uint(0); i < args.ChildCount(); i++ {
		c := args.Child(i)
		switch c.Kind() {
		case "identifier":
			return c
		case "call":
			// `def foo(x, y)` — the inner call's head identifier is the name.
			if c.ChildCount() > 0 {
				inner := c.Child(0)
				if inner.Kind() == "identifier" {
					return inner
				}
			}
		case "binary_operator":
			// `def foo(x, y) when …` — left-hand side is the function header.
			if lhs := c.ChildByFieldName("left"); lhs != nil {
				if lhs.Kind() == "call" && lhs.ChildCount() > 0 {
					if lhs.Child(0).Kind() == "identifier" {
						return lhs.Child(0)
					}
				}
				if lhs.Kind() == "identifier" {
					return lhs
				}
			}
		}
	}
	return nil
}
