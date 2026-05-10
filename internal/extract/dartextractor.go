package extract

import (
	"strings"

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
		// `import 'package:foo/bar.dart';` — the URI is in a `configurable_uri`
		// child whose first identifier-bearing descendant is a `uri` node
		// (a string literal). Strip quotes.
		if u := firstChildOfKind(node, "configurable_uri", "uri"); u != nil {
			if uri := firstChildOfKind(u, "uri"); uri != nil {
				state.addImport(strings.Trim(uri.Utf8Text(src), `'"`))
			} else {
				state.addImport(strings.Trim(u.Utf8Text(src), `'"`))
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
		// `void foo(int x)` — name is an identifier child. Walks fn scope so
		// inner calls attribute correctly.
		nameNode := firstChildOfKind(node, "identifier")
		if nameNode != nil {
			state.emitDecl("function", node, nameNode, src)
			state.walkFnScope("function", nameNode, src, cursor, walkDart)
			return
		}
	case "method_signature":
		// Method inside a class. Same shape as function_signature for our purposes.
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
