package extract

import (
	sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_swift "github.com/julianshen/tree-sitter-swift/bindings/go"
)

// SwiftExtractor extracts class/struct/protocol/function nodes plus import
// edges and call edges from Swift sources.
//
// Backed by julianshen/tree-sitter-swift (a fork of alex-pinkus/tree-sitter-swift
// that commits the generated parser.c — upstream's .gitignore excludes it
// and Go consumers can't run `tree-sitter generate` at install time).
type SwiftExtractor struct{}

func (SwiftExtractor) Extract(path string) (Result, error) {
	return runExtraction(path, tree_sitter_swift.Language(), "swift", walkSwift)
}

func walkSwift(cursor *sitter.TreeCursor, src []byte, state *extractState) {
	node := cursor.Node()
	switch node.Kind() {
	case "import_declaration":
		// `import Foundation` / `import os.log` / `import struct Swift.Array`.
		// Trailing identifier child is the imported module path.
		if id := lastChildOfKind(node, "identifier", "simple_identifier"); id != nil {
			state.addImport(id.Utf8Text(src))
		}
	case "class_declaration":
		// Covers class / struct / actor — all share the `name` field.
		nameNode := node.ChildByFieldName("name")
		id := state.emitDecl("class", node, nameNode, src)
		goEmitSwiftBases(state, node, id, src)
	case "protocol_declaration":
		id := state.emitDecl("protocol", node, node.ChildByFieldName("name"), src)
		goEmitSwiftBases(state, node, id, src)
	case "function_declaration":
		nameNode := node.ChildByFieldName("name")
		if nameNode == nil {
			// Nameless function (parse error or unhandled grammar shape):
			// anonymous scope keyed on position avoids the empty-name
			// graph-node collision that plain emitDecl would produce.
			state.walkAnonFnScope("function", node, src, cursor, walkSwift)
			return
		}
		state.emitDecl("function", node, nameNode, src)
		state.walkFnScope("function", nameNode, src, cursor, walkSwift)
		return
	case "lambda_literal":
		state.walkAnonFnScope("function", node, src, cursor, walkSwift)
		return
	case "call_expression":
		if first := node.Child(0); first != nil {
			state.addCall(callTargetName(first, src))
		}
	}
	walkChildren(cursor, func() { walkSwift(cursor, src, state) })
}
