package extract

import (
	sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_elixir "github.com/julianshen/tree-sitter-elixir/bindings/go"
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
			kind := "function"
			if head == "defmacro" || head == "defmacrop" {
				kind = "macro"
			}
			nameNode := elixirDefName(node)
			if nameNode != nil {
				state.emitDecl(kind, node, nameNode, src)
				state.pushFn(declID(state.lang, kind, state.filePath, nameNode, src))
				walkElixirDefBody(cursor, src, state)
				state.popFn()
				return
			}
			// def with an unrecognized header shape: anonymous scope
			// keeps inner calls attributed correctly and avoids
			// collapsing multiple unrecognized defs onto one node.
			state.walkAnonFnScope(kind, node, src, cursor, walkElixir)
			return
		default:
			if elixirImportMacros[head] {
				if alias := elixirFirstAlias(node, src); alias != "" {
					state.addImport(alias)
				}
				break
			}
			if head != "" {
				state.addCall(head)
			}
		}
	}
	walkChildren(cursor, func() { walkElixir(cursor, src, state) })
}

// walkElixirDefBody walks a `def`/`defp`/`defmacro` node's children but
// skips the `arguments` subtree entirely. The function header lives in
// arguments as a nested `call` whose head is the function name; walking
// it would emit a spurious self-call edge from every defined function.
func walkElixirDefBody(cursor *sitter.TreeCursor, src []byte, state *extractState) {
	walkChildren(cursor, func() {
		// At this point cursor is on a child of the def node. Skip the
		// function-header `arguments` subtree; recurse on the rest
		// (`do_block`, guard expressions, etc.) so body calls and
		// nested defs are still captured.
		if cursor.Node().Kind() == "arguments" {
			return
		}
		walkElixir(cursor, src, state)
	})
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
// (the function header) whose head is the function name. `def NAME(...)
// when GUARD` wraps that header in a `binary_operator` whose `left` is
// the header.
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
			if id := callHeadIdent(c); id != nil {
				return id
			}
		case "binary_operator":
			if lhs := c.ChildByFieldName("left"); lhs != nil {
				if id := callHeadIdent(lhs); id != nil {
					return id
				}
				if lhs.Kind() == "identifier" {
					return lhs
				}
			}
		}
	}
	return nil
}

// callHeadIdent returns the head identifier of a `call` node — the
// function/macro name in `name(args)` shapes. nil for any other node.
func callHeadIdent(n *sitter.Node) *sitter.Node {
	if n == nil || n.Kind() != "call" || n.ChildCount() == 0 {
		return nil
	}
	if head := n.Child(0); head.Kind() == "identifier" {
		return head
	}
	return nil
}
