package extract

import (
	sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_fortran "github.com/stadelmanma/tree-sitter-fortran/bindings/go"
)

// FortranExtractor extracts module/program/subroutine/function nodes plus
// `use` import edges and subroutine-call edges from Fortran sources.
type FortranExtractor struct{}

func (FortranExtractor) Extract(path string) (Result, error) {
	return runExtraction(path, tree_sitter_fortran.Language(), "fortran", walkFortran)
}

func walkFortran(cursor *sitter.TreeCursor, src []byte, state *extractState) {
	node := cursor.Node()
	switch node.Kind() {
	case "module_statement", "program_statement":
		// `module foo` / `program bar` — set the module-node label so
		// `fortran:module:<file>` carries the declared name.
		nameNode := firstChildOfKind(node, "name", "identifier")
		if nameNode != nil {
			rewriteModuleLabel(state, nameNode.Utf8Text(src))
		}
	case "use_statement":
		// `use foo, only: bar` / `use, intrinsic :: iso_fortran_env`
		if id := firstChildOfKind(node, "module_name", "identifier", "name"); id != nil {
			state.addImport(id.Utf8Text(src))
		}
	case "subroutine":
		// Top-level subroutine. The opening `subroutine_statement` child
		// holds the name field.
		if stmt := firstChildOfKind(node, "subroutine_statement"); stmt != nil {
			nameNode := firstChildOfKind(stmt, "name", "identifier")
			state.emitDecl("subroutine", node, nameNode, src)
			state.walkFnScope("subroutine", nameNode, src, cursor, walkFortran)
			return
		}
	case "function":
		if stmt := firstChildOfKind(node, "function_statement"); stmt != nil {
			nameNode := firstChildOfKind(stmt, "name", "identifier")
			state.emitDecl("function", node, nameNode, src)
			state.walkFnScope("function", nameNode, src, cursor, walkFortran)
			return
		}
	case "subroutine_call":
		// `call foo(args)` — first identifier-bearing child is the callee.
		if id := firstChildOfKind(node, "identifier", "name"); id != nil {
			state.addCall(id.Utf8Text(src))
		}
	case "call_expression":
		// Function calls in expression position (returning a value).
		if id := firstChildOfKind(node, "identifier", "name"); id != nil {
			state.addCall(id.Utf8Text(src))
		}
	}
	walkChildren(cursor, func() { walkFortran(cursor, src, state) })
}
