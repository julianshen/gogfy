package extract

import (
	sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_yaml "github.com/tree-sitter-grammars/tree-sitter-yaml/bindings/go"
)

// YAMLExtractor emits a file-as-module node plus a "key" node per top-level
// mapping key, with a "contains" edge from the file to each key.
//
// YAML/TOML are data files rather than code, so the extracted graph is a
// structural map (file → keys) rather than imports/calls.
type YAMLExtractor struct{}

func (YAMLExtractor) Extract(path string) (Result, error) {
	pf, err := parseFile(path, tree_sitter_yaml.Language())
	if err != nil {
		return Result{}, err
	}
	defer pf.cleanup()
	state := &extractState{lang: "yaml", filePath: pf.absPath}
	state.emitModule(pf.cursor.Node())
	walkYAML(pf.cursor, pf.src, state, 0)
	return Result{Nodes: state.nodes, Edges: state.edges}, nil
}

// walkYAML emits "key" nodes only for top-level mapping pairs (mappingDepth==1).
// Nested keys are skipped to keep the per-file graph topology comparable to
// other extractors' "module → import" shape.
func walkYAML(cursor *sitter.TreeCursor, src []byte, state *extractState, mappingDepth int) {
	node := cursor.Node()
	if node.Kind() == "block_mapping_pair" && mappingDepth == 1 {
		if key := node.ChildByFieldName("key"); key != nil {
			state.emitDataKey(key.Utf8Text(src), node)
		}
	}
	nextDepth := mappingDepth
	if node.Kind() == "block_mapping" || node.Kind() == "flow_mapping" {
		nextDepth++
	}
	walkChildren(cursor, func() { walkYAML(cursor, src, state, nextDepth) })
}
