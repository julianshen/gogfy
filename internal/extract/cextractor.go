package extract

import (
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_c "github.com/tree-sitter/tree-sitter-c/bindings/go"
)

// CExtractor extracts module/function/struct nodes and #include edges from C
// sources.
type CExtractor struct{}

func (CExtractor) Extract(path string) (Result, error) {
	return runExtraction(path, tree_sitter_c.Language(), "c", walkC)
}

func walkC(cursor *sitter.TreeCursor, src []byte, state *extractState) {
	node := cursor.Node()
	switch node.Kind() {
	case "preproc_include":
		emitInclude(state, node, src)
	case "function_definition":
		state.emitDecl("function", node, cFunctionName(node), src)
	case "struct_specifier":
		state.emitDecl("struct", node, node.ChildByFieldName("name"), src)
	}
	walkChildren(cursor, func() { walkC(cursor, src, state) })
}

// emitInclude is shared by C and C++; preproc_include children are
// system_lib_string (`<…>`) or string_literal (`"…"`).
func emitInclude(state *extractState, node *sitter.Node, src []byte) {
	n := node.ChildCount()
	for i := uint(0); i < n; i++ {
		c := node.Child(i)
		text := c.Utf8Text(src)
		switch c.Kind() {
		case "system_lib_string":
			state.addImport(strings.Trim(text, "<>"))
		case "string_literal":
			state.addImport(strings.Trim(text, `"`))
		}
	}
}

// cFunctionName drills into function_definition.declarator → function_declarator.declarator
// to find the bare identifier. Pointer/array declarators wrap the identifier further.
// Shared with the C++ extractor since the declarator chain is the same.
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
