package extract

import (
	"strings"

	"github.com/julianshen/gogfy/internal/schema"
	sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_c "github.com/tree-sitter/tree-sitter-c/bindings/go"
)

// CExtractor extracts module/function/struct nodes and #include edges from C
// sources.
type CExtractor struct{}

type cState struct {
	filePath string
	nodes    []schema.Node
	edges    []schema.Edge
}

func (CExtractor) Extract(path string) (Result, error) {
	pf, err := parseFile(path, tree_sitter_c.Language())
	if err != nil {
		return Result{}, err
	}
	defer pf.cleanup()
	state := &cState{filePath: pf.absPath}
	emitModule(&state.nodes, "c", state.filePath, pf.cursor.Node())
	walkC(pf.cursor, pf.src, state)
	return Result{Nodes: state.nodes, Edges: state.edges}, nil
}

func walkC(cursor *sitter.TreeCursor, src []byte, state *cState) {
	node := cursor.Node()
	switch node.Kind() {
	case "preproc_include":
		// Children include `#include` and either `system_lib_string` (<…>) or `string_literal` ("…").
		for i := uint(0); i < node.ChildCount(); i++ {
			c := node.Child(i)
			text := c.Utf8Text(src)
			switch c.Kind() {
			case "system_lib_string":
				addImportEdge(&state.nodes, &state.edges, "c", state.filePath, strings.Trim(text, "<>"))
			case "string_literal":
				addImportEdge(&state.nodes, &state.edges, "c", state.filePath, strings.Trim(text, `"`))
			}
		}
	case "function_definition":
		emitDecl(&state.nodes, "c", "function", state.filePath, cFunctionName(node), node, src)
	case "struct_specifier":
		emitDecl(&state.nodes, "c", "struct", state.filePath, node.ChildByFieldName("name"), node, src)
	}
	walkChildren(cursor, func() { walkC(cursor, src, state) })
}

// cFunctionName drills into function_definition.declarator → function_declarator.declarator
// to find the bare identifier. Pointer/array declarators wrap the identifier further.
func cFunctionName(fnDef *sitter.Node) *sitter.Node {
	d := fnDef.ChildByFieldName("declarator")
	for d != nil {
		switch d.Kind() {
		case "identifier", "field_identifier":
			return d
		case "function_declarator", "pointer_declarator", "array_declarator", "parenthesized_declarator":
			d = d.ChildByFieldName("declarator")
		default:
			return nil
		}
	}
	return nil
}
