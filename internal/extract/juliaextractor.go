package extract

import (
	sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_julia "github.com/tree-sitter/tree-sitter-julia/bindings/go"
)

// JuliaExtractor extracts module/function/struct nodes and using/import edges
// from Julia sources.
type JuliaExtractor struct{}

func (JuliaExtractor) Extract(path string) (Result, error) {
	return runExtraction(path, tree_sitter_julia.Language(), "julia", walkJulia)
}

func walkJulia(cursor *sitter.TreeCursor, src []byte, state *extractState) {
	node := cursor.Node()
	switch node.Kind() {
	case "using_statement", "import_statement":
		emitJuliaImports(state, node, src)
	case "module_definition":
		state.emitDecl("module", node, firstChildOfKind(node, "identifier"), src)
	case "function_definition":
		// Julia wraps the name inside a `signature` field's call_expression,
		// so descend rather than looking at direct children. The signature
		// also contains a call_expression that we must NOT treat as a real
		// call edge (it's the declaration shape, not an invocation).
		nameNode := firstDescendantIdentifier(node)
		state.emitDecl("function", node, nameNode, src)
		state.walkFnScope("function", nameNode, src, cursor, walkJulia)
		return
	case "struct_definition":
		state.emitDecl("struct", node, firstDescendantIdentifier(node), src)
	case "call_expression":
		// Skip the synthetic call_expression inside a function_definition's
		// signature — that's the function's own declaration, not a call.
		if p := node.Parent(); p != nil && p.Kind() == "signature" {
			break
		}
		state.addCall(callTargetName(firstCallee(node), src))
	}
	walkChildren(cursor, func() { walkJulia(cursor, src, state) })
}

// emitJuliaImports handles Julia's import shapes:
//
//	using LinearAlgebra            // single identifier child
//	using LinearAlgebra, Stats     // multiple identifier children (siblings)
//	import Base                    // single identifier child
//	import Foo: a, b               // selected_import wraps module + selected names
//
// All identifier children of the statement are treated as imported modules;
// selected_import children expand into one `Module.name` edge per selector
// while still emitting the module itself.
func emitJuliaImports(state *extractState, node *sitter.Node, src []byte) {
	n := node.ChildCount()
	for i := uint(0); i < n; i++ {
		c := node.Child(i)
		switch c.Kind() {
		case "identifier":
			state.addImport(c.Utf8Text(src))
		case "selected_import":
			emitJuliaSelectedImport(state, c, src)
		}
	}
}

func emitJuliaSelectedImport(state *extractState, sel *sitter.Node, src []byte) {
	var module string
	n := sel.ChildCount()
	for i := uint(0); i < n; i++ {
		c := sel.Child(i)
		if c.Kind() != "identifier" {
			continue
		}
		text := c.Utf8Text(src)
		if module == "" {
			module = text
			state.addImport(module)
		} else {
			state.addImport(module + "." + text)
		}
	}
}
