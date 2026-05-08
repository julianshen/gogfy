package extract

import (
	sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_kotlin "github.com/tree-sitter-grammars/tree-sitter-kotlin/bindings/go"
)

// KotlinExtractor extracts module/class/function nodes and import edges from
// Kotlin sources.
type KotlinExtractor struct{}

func (KotlinExtractor) Extract(path string) (Result, error) {
	return runExtraction(path, tree_sitter_kotlin.Language(), "kotlin", walkKotlin)
}

func walkKotlin(cursor *sitter.TreeCursor, src []byte, state *extractState) {
	node := cursor.Node()
	switch node.Kind() {
	case "import":
		if id := firstChildOfKind(node, "qualified_identifier", "identifier"); id != nil {
			state.addImport(id.Utf8Text(src))
		}
	case "class_declaration":
		state.emitDecl("class", node, firstChildOfKind(node, "identifier"), src)
	case "function_declaration":
		state.emitDecl("function", node, firstChildOfKind(node, "identifier"), src)
	case "object_declaration":
		state.emitDecl("object", node, firstChildOfKind(node, "identifier"), src)
	}
	walkChildren(cursor, func() { walkKotlin(cursor, src, state) })
}
