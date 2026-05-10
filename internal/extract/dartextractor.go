package extract

import (
	sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_dart "github.com/UserNobody14/tree-sitter-dart/bindings/go"
)

// DartExtractor extracts class/function nodes plus library/import/export
// edges and call edges from Dart sources.
type DartExtractor struct{}

func (DartExtractor) Extract(path string) (Result, error) {
	return runExtraction(path, tree_sitter_dart.Language(), "dart", walkDart)
}

func walkDart(cursor *sitter.TreeCursor, src []byte, state *extractState) {
	node := cursor.Node()
	switch node.Kind() {
	case "library_name":
		// `library foo.bar;` — set the module label to the dotted name.
		if id := firstChildOfKind(node, "dotted_identifier_list", "identifier"); id != nil {
			rewriteModuleLabel(state, id.Utf8Text(src))
		}
	case "import_specification", "library_export":
		// The URI is a string-literal child of a `configurable_uri` wrapper.
		// `configurable_uri` itself has a `uri` child holding the literal.
		if cu := firstChildOfKind(node, "configurable_uri"); cu != nil {
			if uri := firstChildOfKind(cu, "uri"); uri != nil {
				state.addImport(trimQuotes(uri.Utf8Text(src)))
			}
		}
	case "class_definition":
		nameNode := node.ChildByFieldName("name")
		if nameNode == nil {
			nameNode = firstChildOfKind(node, "identifier")
		}
		state.emitDecl("class", node, nameNode, src)
	case "mixin_declaration":
		state.emitDecl("mixin", node, firstChildOfKind(node, "identifier"), src)
	case "extension_declaration":
		state.emitDecl("extension", node, firstChildOfKind(node, "identifier"), src)
	case "enum_declaration":
		state.emitDecl("enum", node, firstChildOfKind(node, "identifier"), src)
	case "function_signature":
		nameNode := firstChildOfKind(node, "identifier")
		if nameNode != nil {
			state.emitDecl("function", node, nameNode, src)
			state.walkFnScope("function", nameNode, src, cursor, walkDart)
			return
		}
	case "method_signature":
		// method_signature wraps a function_signature; reach through.
		if header := firstChildOfKind(node, "function_signature"); header != nil {
			nameNode := firstChildOfKind(header, "identifier")
			if nameNode != nil {
				state.emitDecl("method", node, nameNode, src)
				state.walkFnScope("method", nameNode, src, cursor, walkDart)
				return
			}
		}
	}
	walkChildren(cursor, func() { walkDart(cursor, src, state) })
}
