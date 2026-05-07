package extract

import (
	sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_rust "github.com/tree-sitter/tree-sitter-rust/bindings/go"
)

// RustExtractor extracts module/struct/function/trait/impl nodes and use-edges
// from Rust sources.
type RustExtractor struct{}

func (RustExtractor) Extract(path string) (Result, error) {
	pf, err := parseFile(path, tree_sitter_rust.Language())
	if err != nil {
		return Result{}, err
	}
	defer pf.cleanup()
	state := &extractState{lang: "rust", filePath: pf.absPath}
	state.emitModule(pf.cursor.Node())
	walkRust(pf.cursor, pf.src, state)
	return Result{Nodes: state.nodes, Edges: state.edges}, nil
}

func walkRust(cursor *sitter.TreeCursor, src []byte, state *extractState) {
	node := cursor.Node()
	switch node.Kind() {
	case "use_declaration":
		if id := firstChildOfKind(node, "scoped_identifier", "identifier"); id != nil {
			state.addImport(id.Utf8Text(src))
		}
	case "function_item":
		state.emitDecl("function", node, node.ChildByFieldName("name"), src)
	case "struct_item":
		state.emitDecl("struct", node, node.ChildByFieldName("name"), src)
	case "enum_item":
		state.emitDecl("enum", node, node.ChildByFieldName("name"), src)
	case "trait_item":
		state.emitDecl("trait", node, node.ChildByFieldName("name"), src)
	case "mod_item":
		state.emitDecl("mod", node, node.ChildByFieldName("name"), src)
	}
	walkChildren(cursor, func() { walkRust(cursor, src, state) })
}
