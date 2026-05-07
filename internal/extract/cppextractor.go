package extract

import (
	"strings"

	"github.com/julianshen/gogfy/internal/schema"
	sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_cpp "github.com/tree-sitter/tree-sitter-cpp/bindings/go"
)

// CppExtractor extracts module/namespace/class/function nodes and #include
// edges from C++ sources.
type CppExtractor struct{}

type cppState struct {
	filePath string
	nodes    []schema.Node
	edges    []schema.Edge
}

func (CppExtractor) Extract(path string) (Result, error) {
	pf, err := parseFile(path, tree_sitter_cpp.Language())
	if err != nil {
		return Result{}, err
	}
	defer pf.cleanup()
	state := &cppState{filePath: pf.absPath}
	emitModule(&state.nodes, "cpp", state.filePath, pf.cursor.Node())
	walkCpp(pf.cursor, pf.src, state)
	return Result{Nodes: state.nodes, Edges: state.edges}, nil
}

func walkCpp(cursor *sitter.TreeCursor, src []byte, state *cppState) {
	node := cursor.Node()
	switch node.Kind() {
	case "preproc_include":
		for i := uint(0); i < node.ChildCount(); i++ {
			c := node.Child(i)
			text := c.Utf8Text(src)
			switch c.Kind() {
			case "system_lib_string":
				addImportEdge(&state.nodes, &state.edges, "cpp", state.filePath, strings.Trim(text, "<>"))
			case "string_literal":
				addImportEdge(&state.nodes, &state.edges, "cpp", state.filePath, strings.Trim(text, `"`))
			}
		}
	case "namespace_definition":
		emitDecl(&state.nodes, "cpp", "namespace", state.filePath, node.ChildByFieldName("name"), node, src)
	case "class_specifier":
		emitDecl(&state.nodes, "cpp", "class", state.filePath, node.ChildByFieldName("name"), node, src)
	case "struct_specifier":
		emitDecl(&state.nodes, "cpp", "struct", state.filePath, node.ChildByFieldName("name"), node, src)
	case "function_definition":
		emitDecl(&state.nodes, "cpp", "function", state.filePath, cFunctionName(node), node, src)
	}
	walkChildren(cursor, func() { walkCpp(cursor, src, state) })
}
