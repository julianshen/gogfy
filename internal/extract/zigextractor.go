package extract

import (
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_zig "github.com/tree-sitter-grammars/tree-sitter-zig/bindings/go"
)

// ZigExtractor extracts module/function/struct/enum nodes and @import edges
// from Zig sources.
type ZigExtractor struct{}

func (ZigExtractor) Extract(path string) (Result, error) {
	return runExtraction(path, tree_sitter_zig.Language(), "zig", walkZig)
}

func walkZig(cursor *sitter.TreeCursor, src []byte, state *extractState) {
	node := cursor.Node()
	switch node.Kind() {
	case "function_declaration":
		state.emitDecl("function", node, firstChildOfKind(node, "identifier"), src)
	case "variable_declaration":
		// Zig idiom: `const X = struct { … }` / `enum { … }` / `@import("y")`.
		// Use the first identifier child as the declaration name.
		nameNode := firstChildOfKind(node, "identifier")
		if rhs := firstChildOfKind(node, "struct_declaration"); rhs != nil {
			state.emitDecl("struct", node, nameNode, src)
		} else if rhs := firstChildOfKind(node, "enum_declaration"); rhs != nil {
			_ = rhs
			state.emitDecl("enum", node, nameNode, src)
		} else if rhs := firstChildOfKind(node, "union_declaration"); rhs != nil {
			_ = rhs
			state.emitDecl("union", node, nameNode, src)
		}
		if target := zigImportTarget(node, src); target != "" {
			state.addImport(target)
		}
	}
	walkChildren(cursor, func() { walkZig(cursor, src, state) })
}

// zigImportTarget extracts the string argument of `@import("…")` from a
// variable_declaration's RHS. Returns "" if the RHS isn't a builtin call.
func zigImportTarget(varDecl *sitter.Node, src []byte) string {
	bi := firstChildOfKind(varDecl, "builtin_function")
	if bi == nil {
		return ""
	}
	id := firstChildOfKind(bi, "builtin_identifier")
	if id == nil || id.Utf8Text(src) != "@import" {
		return ""
	}
	args := firstChildOfKind(bi, "arguments")
	if args == nil {
		return ""
	}
	if str := firstChildOfKind(args, "string"); str != nil {
		return strings.Trim(str.Utf8Text(src), `"`)
	}
	return ""
}
