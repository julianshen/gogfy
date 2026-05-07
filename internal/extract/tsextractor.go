package extract

import (
	sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

// TypeScriptExtractor extracts module/function/class/interface/type nodes
// and import edges from TypeScript sources.
type TypeScriptExtractor struct {
	// TSX, when true, parses input as TSX. Default (false) parses as plain TS.
	TSX bool
}

func (e TypeScriptExtractor) Extract(path string) (Result, error) {
	lang := tree_sitter_typescript.LanguageTypescript()
	if e.TSX {
		lang = tree_sitter_typescript.LanguageTSX()
	}
	return runExtraction(path, lang, "ts", walkTS)
}

func walkTS(cursor *sitter.TreeCursor, src []byte, state *extractState) {
	node := cursor.Node()
	switch node.Kind() {
	case "function_declaration":
		state.emitDecl("function", node, node.ChildByFieldName("name"), src)
	case "class_declaration":
		state.emitDecl("class", node, node.ChildByFieldName("name"), src)
	case "interface_declaration":
		state.emitDecl("interface", node, node.ChildByFieldName("name"), src)
	case "type_alias_declaration":
		state.emitDecl("type", node, node.ChildByFieldName("name"), src)
	case "import_statement":
		if target := importStringSource(node, src); target != "" {
			state.addImport(target)
		}
	}
	walkChildren(cursor, func() { walkTS(cursor, src, state) })
}
