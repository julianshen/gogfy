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

// fortranNameKinds enumerates the node kinds that can hold an identifier
// in tree-sitter-fortran. `name` is the canonical kind on declarations;
// `identifier` shows up on bare references; `module_name` wraps imports.
var fortranNameKinds = []string{"name", "identifier", "module_name"}

func walkFortran(cursor *sitter.TreeCursor, src []byte, state *extractState) {
	node := cursor.Node()
	switch node.Kind() {
	case "module_statement", "program_statement":
		if id := firstChildOfKind(node, fortranNameKinds...); id != nil {
			rewriteModuleLabel(state, id.Utf8Text(src))
		}
	case "use_statement":
		if id := firstChildOfKind(node, fortranNameKinds...); id != nil {
			state.addImport(id.Utf8Text(src))
		}
	case "subroutine":
		if stmt := firstChildOfKind(node, "subroutine_statement"); stmt != nil {
			if nameNode := firstChildOfKind(stmt, fortranNameKinds...); nameNode != nil {
				state.emitDecl("subroutine", node, nameNode, src)
				state.walkFnScope("subroutine", nameNode, src, cursor, walkFortran)
				return
			}
		}
		// Missing name (malformed source or grammar drift): use an
		// anonymous scope keyed on source position so multiple unnamed
		// subroutines don't collide on a single empty-key graph node.
		state.walkAnonFnScope("subroutine", node, src, cursor, walkFortran)
		return
	case "function":
		if stmt := firstChildOfKind(node, "function_statement"); stmt != nil {
			if nameNode := firstChildOfKind(stmt, fortranNameKinds...); nameNode != nil {
				state.emitDecl("function", node, nameNode, src)
				state.walkFnScope("function", nameNode, src, cursor, walkFortran)
				return
			}
		}
		state.walkAnonFnScope("function", node, src, cursor, walkFortran)
		return
	case "subroutine_call", "call_expression":
		if id := firstChildOfKind(node, fortranNameKinds...); id != nil {
			state.addCall(id.Utf8Text(src))
		}
	}
	walkChildren(cursor, func() { walkFortran(cursor, src, state) })
}
