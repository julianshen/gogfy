package extract

import (
	sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_toml "github.com/tree-sitter-grammars/tree-sitter-toml/bindings/go"
)

// TOMLExtractor emits a file-as-module node plus a "key" node per top-level
// pair or table, with a "contains" edge from the file to each one.
type TOMLExtractor struct{}

func (TOMLExtractor) Extract(path string) (Result, error) {
	pf, err := parseFile(path, tree_sitter_toml.Language())
	if err != nil {
		return Result{}, err
	}
	defer pf.cleanup()
	state := &extractState{lang: "toml", filePath: pf.absPath}
	state.emitModule(pf.cursor.Node())
	walkTOML(pf.cursor, pf.src, state)
	return Result{Nodes: state.nodes, Edges: state.edges}, nil
}

func walkTOML(cursor *sitter.TreeCursor, src []byte, state *extractState) {
	node := cursor.Node()
	switch node.Kind() {
	case "pair":
		// Top-level pairs only; pairs nested inside [tables] would otherwise
		// drown out the document-level shape we're trying to surface.
		if parent := node.Parent(); parent != nil && parent.Kind() == "document" {
			if key := firstChildOfKind(node, "bare_key"); key != nil {
				state.emitDataKey(key.Utf8Text(src), node)
			}
		}
	case "table", "table_array_element":
		if key := firstChildOfKind(node, "bare_key"); key != nil {
			state.emitDataKey(key.Utf8Text(src), node)
		}
	}
	walkChildren(cursor, func() { walkTOML(cursor, src, state) })
}
