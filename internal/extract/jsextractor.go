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
		state.emitDecl("function", node, node.ChildByFieldName("name"), src)
	case "class_declaration":
		state.emitDecl("class", node, node.ChildByFieldName("name"), src)
	case "import_statement":
		if target := importStringSource(node, src); target != "" {
			state.addImport(target)
		}
	}
	walkChildren(cursor, func() { walkJS(cursor, src, state) })
}
