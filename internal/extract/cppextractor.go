package extract

import (
	sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_cpp "github.com/tree-sitter/tree-sitter-cpp/bindings/go"
)

// CppExtractor extracts module/namespace/class/function nodes and #include
// edges from C++ sources. Shares emitInclude/cFunctionName with CExtractor.
type CppExtractor struct{}

func (CppExtractor) Extract(path string) (Result, error) {
	pf, err := parseFile(path, tree_sitter_cpp.Language())
	if err != nil {
		return Result{}, err
	}
	defer pf.cleanup()
	state := &extractState{lang: "cpp", filePath: pf.absPath}
	state.emitModule(pf.cursor.Node())
	walkCpp(pf.cursor, pf.src, state)
	return Result{Nodes: state.nodes, Edges: state.edges}, nil
}

func walkCpp(cursor *sitter.TreeCursor, src []byte, state *extractState) {
	node := cursor.Node()
	switch node.Kind() {
	case "preproc_include":
		emitInclude(state, node, src)
	case "namespace_definition":
		state.emitDecl("namespace", node, node.ChildByFieldName("name"), src)
	case "class_specifier":
		state.emitDecl("class", node, node.ChildByFieldName("name"), src)
	case "struct_specifier":
		state.emitDecl("struct", node, node.ChildByFieldName("name"), src)
	case "function_definition":
		state.emitDecl("function", node, cFunctionName(node), src)
	}
	walkChildren(cursor, func() { walkCpp(cursor, src, state) })
}
