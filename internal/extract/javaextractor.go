package extract

import (
	sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_java "github.com/tree-sitter/tree-sitter-java/bindings/go"
)

// JavaExtractor extracts module/class/method nodes and import edges from
// Java sources.
type JavaExtractor struct{}

func (JavaExtractor) Extract(path string) (Result, error) {
	return runExtraction(path, tree_sitter_java.Language(), "java", walkJava)
}

func walkJava(cursor *sitter.TreeCursor, src []byte, state *extractState) {
	node := cursor.Node()
	switch node.Kind() {
	case "import_declaration":
		// Take the trailing identifier-bearing child to skip past `import` and
		// any `static` modifier.
		if id := lastChildOfKind(node, "scoped_identifier", "identifier"); id != nil {
			state.addImport(id.Utf8Text(src))
		}
	case "class_declaration":
		state.emitDecl("class", node, node.ChildByFieldName("name"), src)
	case "interface_declaration":
		state.emitDecl("interface", node, node.ChildByFieldName("name"), src)
	case "enum_declaration":
		state.emitDecl("enum", node, node.ChildByFieldName("name"), src)
	case "method_declaration":
		state.emitDecl("method", node, node.ChildByFieldName("name"), src)
	}
	walkChildren(cursor, func() { walkJava(cursor, src, state) })
}
