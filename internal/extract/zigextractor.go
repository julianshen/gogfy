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
		nameNode := firstChildOfKind(node, "identifier")
		state.emitDecl("function", node, nameNode, src)
		state.walkFnScope("function", nameNode, src, cursor, walkZig)
		return
	case "call_expression":
		state.addCall(callTargetName(firstCallee(node), src))
	case "variable_declaration":
		// Zig idiom: `const X = struct/enum/union { … }` / `@import("y")`.
		// Classify the declaration by inspecting the RHS shape; nameNode is
		// only consulted when there's a known type kind to emit.
		if kind := zigDeclKind(node); kind != "" {
			state.emitDecl(kind, node, firstChildOfKind(node, "identifier"), src)
		}
		if target := zigImportTarget(node, src); target != "" {
			state.addImport(target)
		}
	}
	walkChildren(cursor, func() { walkZig(cursor, src, state) })
}

// zigDeclKind returns "struct"/"enum"/"union" if the variable_declaration's
// RHS is one of those container forms, or "" otherwise. Done in a single
// pass to avoid three CGO-heavy firstChildOfKind calls.
func zigDeclKind(node *sitter.Node) string {
	n := node.ChildCount()
	for i := uint(0); i < n; i++ {
		switch node.Child(i).Kind() {
		case "struct_declaration":
			return "struct"
		case "enum_declaration":
			return "enum"
		case "union_declaration":
			return "union"
		}
	}
	return ""
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
