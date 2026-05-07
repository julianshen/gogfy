package extract

import (
	"github.com/julianshen/gogfy/internal/schema"
	sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_javascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
)

// JavaScriptExtractor extracts module/function/class nodes and import edges
// from JavaScript sources.
type JavaScriptExtractor struct{}

type jsState struct {
	filePath string
	nodes    []schema.Node
	edges    []schema.Edge
}

func (JavaScriptExtractor) Extract(path string) (Result, error) {
	pf, err := parseFile(path, tree_sitter_javascript.Language())
	if err != nil {
		return Result{}, err
	}
	defer pf.cleanup()
	state := &jsState{filePath: pf.absPath}
	emitModule(&state.nodes, "js", state.filePath, pf.cursor.Node())
	walkJS(pf.cursor, pf.src, state)
	return Result{Nodes: state.nodes, Edges: state.edges}, nil
}

func walkJS(cursor *sitter.TreeCursor, src []byte, state *jsState) {
	node := cursor.Node()
	switch node.Kind() {
	case "function_declaration":
		emitDecl(&state.nodes, "js", "function", state.filePath, node.ChildByFieldName("name"), node, src)
	case "class_declaration":
		emitDecl(&state.nodes, "js", "class", state.filePath, node.ChildByFieldName("name"), node, src)
	case "import_statement":
		if target := importStringSource(node, src); target != "" {
			addImportEdge(&state.nodes, &state.edges, "js", state.filePath, target)
		}
	}
	walkChildren(cursor, func() { walkJS(cursor, src, state) })
}

