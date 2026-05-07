package extract

import (
	"github.com/julianshen/gogfy/internal/schema"
	sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_rust "github.com/tree-sitter/tree-sitter-rust/bindings/go"
)

// RustExtractor extracts module/struct/function/trait/impl nodes and use-edges
// from Rust sources.
type RustExtractor struct{}

type rustState struct {
	filePath string
	nodes    []schema.Node
	edges    []schema.Edge
}

func (RustExtractor) Extract(path string) (Result, error) {
	pf, err := parseFile(path, tree_sitter_rust.Language())
	if err != nil {
		return Result{}, err
	}
	defer pf.cleanup()
	state := &rustState{filePath: pf.absPath}
	emitModule(&state.nodes, "rust", state.filePath, pf.cursor.Node())
	walkRust(pf.cursor, pf.src, state)
	return Result{Nodes: state.nodes, Edges: state.edges}, nil
}

func walkRust(cursor *sitter.TreeCursor, src []byte, state *rustState) {
	node := cursor.Node()
	switch node.Kind() {
	case "use_declaration":
		// Take the first scoped_identifier or identifier child as the imported path.
		for i := uint(0); i < node.ChildCount(); i++ {
			c := node.Child(i)
			if c.Kind() == "scoped_identifier" || c.Kind() == "identifier" {
				addImportEdge(&state.nodes, &state.edges, "rust", state.filePath, c.Utf8Text(src))
				break
			}
		}
	case "function_item":
		emitDecl(&state.nodes, "rust", "function", state.filePath, node.ChildByFieldName("name"), node, src)
	case "struct_item":
		emitDecl(&state.nodes, "rust", "struct", state.filePath, node.ChildByFieldName("name"), node, src)
	case "enum_item":
		emitDecl(&state.nodes, "rust", "enum", state.filePath, node.ChildByFieldName("name"), node, src)
	case "trait_item":
		emitDecl(&state.nodes, "rust", "trait", state.filePath, node.ChildByFieldName("name"), node, src)
	case "mod_item":
		emitDecl(&state.nodes, "rust", "mod", state.filePath, node.ChildByFieldName("name"), node, src)
	}
	walkChildren(cursor, func() { walkRust(cursor, src, state) })
}
