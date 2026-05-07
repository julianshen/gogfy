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
		// First identifier child is the module name (e.g., LinearAlgebra).
		// `import Base: +` puts the module under selected_import → identifier.
		if id := firstChildOfKind(node, "identifier"); id != nil {
			state.addImport(id.Utf8Text(src))
		} else if sel := firstChildOfKind(node, "selected_import"); sel != nil {
			if id := firstChildOfKind(sel, "identifier"); id != nil {
				state.addImport(id.Utf8Text(src))
			}
		}
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
