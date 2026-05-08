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
		// so descend rather than looking at direct children.
		state.emitDecl("function", node, firstDescendantIdentifier(node), src)
	case "struct_definition":
		state.emitDecl("struct", node, firstDescendantIdentifier(node), src)
	}
	walkChildren(cursor, func() { walkJulia(cursor, src, state) })
}

// emitJuliaImports handles Julia's three import shapes:
//
//	using LinearAlgebra            // identifier child of using_statement
//	import Base                    // identifier child of import_statement
//	import Foo: a, b               // selected_import wraps module + selected names
//
// For selected_import, the first identifier is the module and any
// subsequent identifiers are emitted as `Module.name` edges.
func emitJuliaImports(state *extractState, node *sitter.Node, src []byte) {
	if id := firstChildOfKind(node, "identifier"); id != nil {
		state.addImport(id.Utf8Text(src))
		return
	}
	sel := firstChildOfKind(node, "selected_import")
	if sel == nil {
		return
	}
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
