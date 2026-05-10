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
		if id := firstChildOfKind(node, "dotted_identifier_list", "identifier"); id != nil {
			rewriteModuleLabel(state, id.Utf8Text(src))
		}
	case "import_specification", "library_export":
		// Dart's import/export grammar wraps the URI in `configurable_uri`
		// for typical shapes, but the deferred-import form
		// `import 'foo' deferred as Bar;` puts the `uri` child directly
		// under `import_specification`. Handle both.
		if uri := firstChildOfKind(node, "uri"); uri != nil {
			state.addImport(trimQuotes(uri.Utf8Text(src)))
		} else if cu := firstChildOfKind(node, "configurable_uri"); cu != nil {
			if u := firstChildOfKind(cu, "uri"); u != nil {
				state.addImport(trimQuotes(u.Utf8Text(src)))
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
	case "method_signature":
		// method_signature wraps function_signature, so handle the method
		// at THIS level and skip into its inner function_signature without
		// recursing through the function_signature case (which would
		// otherwise emit a duplicate function-kinded decl for every method).
		header := firstChildOfKind(node, "function_signature")
		if header == nil {
			state.walkAnonFnScope("method", node, src, cursor, walkDart)
			return
		}
		nameNode := firstChildOfKind(header, "identifier")
		if nameNode == nil {
			state.walkAnonFnScope("method", node, src, cursor, walkDart)
			return
		}
		state.emitDecl("method", node, nameNode, src)
		state.walkFnScope("method", nameNode, src, cursor, walkDart)
		return
	case "function_signature":
		// Reached only when function_signature is NOT inside a
		// method_signature (free-function declarations). Method-wrapped
		// signatures short-circuit at the method_signature case above.
		if parent := node.Parent(); parent != nil && parent.Kind() == "method_signature" {
			// Defensive: shouldn't happen because the parent's case
			// returns before recursing here, but guard anyway.
			return
		}
		nameNode := firstChildOfKind(node, "identifier")
		if nameNode == nil {
			state.walkAnonFnScope("function", node, src, cursor, walkDart)
			return
		}
		state.emitDecl("function", node, nameNode, src)
		state.walkFnScope("function", nameNode, src, cursor, walkDart)
		return
	}
	walkChildren(cursor, func() { walkDart(cursor, src, state) })
}
