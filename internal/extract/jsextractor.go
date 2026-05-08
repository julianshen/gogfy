package extract

import (
	sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_javascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
)

// JavaScriptExtractor extracts module/function/class nodes and import edges
// from JavaScript sources.
type JavaScriptExtractor struct{}

func (JavaScriptExtractor) Extract(path string) (Result, error) {
	return runExtraction(path, tree_sitter_javascript.Language(), "js", walkJS)
}

func walkJS(cursor *sitter.TreeCursor, src []byte, state *extractState) {
	node := cursor.Node()
	switch node.Kind() {
	case "function_declaration":
		nameNode := node.ChildByFieldName("name")
		state.emitDecl("function", node, nameNode, src)
		state.walkFnScope("function", nameNode, src, cursor, walkJS)
		return
	case "method_definition":
		nameNode := node.ChildByFieldName("name")
		state.emitDecl("method", node, nameNode, src)
		state.walkFnScope("method", nameNode, src, cursor, walkJS)
		return
	case "arrow_function", "function_expression", "generator_function":
		// Anonymous-by-construction: source position keys the synthetic ID
		// so calls inside don't all collide on a single anonymous bucket.
		state.walkAnonFnScope("function", node, src, cursor, walkJS)
		return
	case "class_declaration":
		state.emitDecl("class", node, node.ChildByFieldName("name"), src)
	case "import_statement":
		if target := importStringSource(node, src); target != "" {
			state.addImport(target)
		}
	case "call_expression":
		state.addCall(callTargetName(node.ChildByFieldName("function"), src))
	}
	walkChildren(cursor, func() { walkJS(cursor, src, state) })
}
