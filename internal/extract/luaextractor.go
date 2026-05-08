package extract

import (
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_lua "github.com/tree-sitter-grammars/tree-sitter-lua/bindings/go"
)

// LuaExtractor extracts module/function nodes and require edges from Lua sources.
type LuaExtractor struct{}

func (LuaExtractor) Extract(path string) (Result, error) {
	return runExtraction(path, tree_sitter_lua.Language(), "lua", walkLua)
}

func walkLua(cursor *sitter.TreeCursor, src []byte, state *extractState) {
	node := cursor.Node()
	switch node.Kind() {
	case "function_declaration":
		nameNode := luaFunctionName(node)
		state.emitDecl("function", node, nameNode, src)
		state.walkFnScope("function", nameNode, src, cursor, walkLua)
		return
	case "function_call":
		// `require("x")` and `require "x"` show up as function_call
		// expressions; route those to imports first, then emit a `calls`
		// edge for any other callee.
		if target := luaRequireTarget(node, src); target != "" {
			state.addImport(target)
		} else {
			state.emitCall(node, src)
		}
	}
	walkChildren(cursor, func() { walkLua(cursor, src, state) })
}

// luaFunctionName drills into the identifier or dot_index_expression that
// follows the `function` keyword. For `function M.hello() end` the first
// dot_index_expression's last identifier is the function name.
func luaFunctionName(node *sitter.Node) *sitter.Node {
	if id := firstChildOfKind(node, "identifier"); id != nil {
		return id
	}
	if dotted := firstChildOfKind(node, "dot_index_expression"); dotted != nil {
		// Last identifier is the field name (e.g., "hello" in "M.hello").
		return lastChildOfKind(dotted, "identifier")
	}
	return nil
}

// luaRequireTarget returns the unquoted module argument from a `require(...)`
// or `require "..."` call, or "" for any other call.
func luaRequireTarget(call *sitter.Node, src []byte) string {
	fn := firstChildOfKind(call, "identifier")
	if fn == nil || fn.Utf8Text(src) != "require" {
		return ""
	}
	// Argument may appear as arguments(string) or string_literal directly.
	if args := firstChildOfKind(call, "arguments"); args != nil {
		if str := firstChildOfKind(args, "string"); str != nil {
			return strings.Trim(str.Utf8Text(src), `"'`)
		}
	}
	if str := firstChildOfKind(call, "string"); str != nil {
		return strings.Trim(str.Utf8Text(src), `"'`)
	}
	return ""
}
