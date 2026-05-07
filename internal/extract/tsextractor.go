package extract

import (
	"github.com/julianshen/gogfy/internal/schema"
	sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

// TypeScriptExtractor extracts module/function/class/interface/type nodes
// and import edges from TypeScript sources.
type TypeScriptExtractor struct {
	// TSX, when true, parses input as TSX. Default (false) parses as plain TS.
	TSX bool
}

type tsState struct {
	filePath string
	nodes    []schema.Node
	edges    []schema.Edge
}

func (e TypeScriptExtractor) Extract(path string) (Result, error) {
	lang := tree_sitter_typescript.LanguageTypescript()
	if e.TSX {
		lang = tree_sitter_typescript.LanguageTSX()
	}
	pf, err := parseFile(path, lang)
	if err != nil {
		return Result{}, err
	}
	defer pf.cleanup()
	state := &tsState{filePath: pf.absPath}
	emitModule(&state.nodes, "ts", state.filePath, pf.cursor.Node())
	walkTS(pf.cursor, pf.src, state)
	return Result{Nodes: state.nodes, Edges: state.edges}, nil
}

func walkTS(cursor *sitter.TreeCursor, src []byte, state *tsState) {
	node := cursor.Node()
	switch node.Kind() {
	case "function_declaration":
		emitDecl(&state.nodes, "ts", "function", state.filePath, node.ChildByFieldName("name"), node, src)
	case "class_declaration":
		emitDecl(&state.nodes, "ts", "class", state.filePath, node.ChildByFieldName("name"), node, src)
	case "interface_declaration":
		emitDecl(&state.nodes, "ts", "interface", state.filePath, node.ChildByFieldName("name"), node, src)
	case "type_alias_declaration":
		emitDecl(&state.nodes, "ts", "type", state.filePath, node.ChildByFieldName("name"), node, src)
	case "import_statement":
		if target := importStringSource(node, src); target != "" {
			addImportEdge(&state.nodes, &state.edges, "ts", state.filePath, target)
		}
	}
	walkChildren(cursor, func() { walkTS(cursor, src, state) })
}
