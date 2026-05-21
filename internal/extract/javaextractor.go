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
		id := state.emitDecl("class", node, node.ChildByFieldName("name"), src)
		goEmitJavaBases(state, node, id, src)
	case "interface_declaration":
		id := state.emitDecl("interface", node, node.ChildByFieldName("name"), src)
		goEmitJavaBases(state, node, id, src)
	case "enum_declaration":
		state.emitDecl("enum", node, node.ChildByFieldName("name"), src)
	case "method_declaration":
		nameNode := node.ChildByFieldName("name")
		state.emitDecl("method", node, nameNode, src)
		state.pushFn(declID(state.lang, "method", state.filePath, nameNode, src))
		walkChildren(cursor, func() { walkJava(cursor, src, state) })
		state.popFn()
		return
	case "method_invocation":
		// Java's call site: callee is in the `name` field; the receiver
		// (if any) is in `object`. We surface just the called name so
		// `obj.foo()` and `foo()` both produce a `calls:foo` edge.
		state.addCall(callTargetName(node.ChildByFieldName("name"), src))
	}
	walkChildren(cursor, func() { walkJava(cursor, src, state) })
}
