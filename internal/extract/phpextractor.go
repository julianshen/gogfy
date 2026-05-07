package extract

import (
	sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_php "github.com/tree-sitter/tree-sitter-php/bindings/go"
)

// PHPExtractor extracts module/class/function nodes and namespace-use edges
// from PHP sources.
type PHPExtractor struct{}

func (PHPExtractor) Extract(path string) (Result, error) {
	return runExtraction(path, tree_sitter_php.LanguagePHP(), "php", walkPHP)
}

func walkPHP(cursor *sitter.TreeCursor, src []byte, state *extractState) {
	node := cursor.Node()
	switch node.Kind() {
	case "namespace_use_declaration":
		// Drill into namespace_use_clause -> qualified_name (or name).
		if clause := firstChildOfKind(node, "namespace_use_clause"); clause != nil {
			if name := firstChildOfKind(clause, "qualified_name", "name"); name != nil {
				state.addImport(name.Utf8Text(src))
			}
		}
	case "class_declaration":
		state.emitDecl("class", node, node.ChildByFieldName("name"), src)
	case "interface_declaration":
		state.emitDecl("interface", node, node.ChildByFieldName("name"), src)
	case "trait_declaration":
		state.emitDecl("trait", node, node.ChildByFieldName("name"), src)
	case "enum_declaration":
		state.emitDecl("enum", node, node.ChildByFieldName("name"), src)
	case "function_definition":
		state.emitDecl("function", node, node.ChildByFieldName("name"), src)
	case "method_declaration":
		state.emitDecl("method", node, node.ChildByFieldName("name"), src)
	}
	walkChildren(cursor, func() { walkPHP(cursor, src, state) })
}
