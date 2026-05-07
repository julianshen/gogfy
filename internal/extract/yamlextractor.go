package extract

import (
	"github.com/julianshen/gogfy/internal/schema"
	sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_yaml "github.com/tree-sitter-grammars/tree-sitter-yaml/bindings/go"
)

// YAMLExtractor extracts a file-as-module node plus a "key" node per top-level
// mapping key, with a "contains" edge from the file to each key.
//
// YAML/TOML are data files rather than code, so the extracted graph is a
// structural map (file → keys) rather than imports/calls.
type YAMLExtractor struct{}

type yamlState struct {
	filePath string
	nodes    []schema.Node
	edges    []schema.Edge
}

func (YAMLExtractor) Extract(path string) (Result, error) {
	pf, err := parseFile(path, tree_sitter_yaml.Language())
	if err != nil {
		return Result{}, err
	}
	defer pf.cleanup()
	state := &yamlState{filePath: pf.absPath}
	emitModule(&state.nodes, "yaml", state.filePath, pf.cursor.Node())
	walkYAML(pf.cursor, pf.src, state, 0)
	return Result{Nodes: state.nodes, Edges: state.edges}, nil
}

// walkYAML emits "key" nodes only for top-level mapping pairs (depth==0
// relative to the first block_mapping). Nested keys are skipped to keep the
// graph shape comparable to other extractors' "module → import" topology.
func walkYAML(cursor *sitter.TreeCursor, src []byte, state *yamlState, mappingDepth int) {
	node := cursor.Node()
	if node.Kind() == "block_mapping_pair" && mappingDepth == 1 {
		key := node.ChildByFieldName("key")
		if key != nil {
			emitDataKey(&state.nodes, &state.edges, "yaml", state.filePath, key.Utf8Text(src), node)
		}
	}
	nextDepth := mappingDepth
	if node.Kind() == "block_mapping" || node.Kind() == "flow_mapping" {
		nextDepth = mappingDepth + 1
	}
	walkChildren(cursor, func() { walkYAML(cursor, src, state, nextDepth) })
}

// emitDataKey appends a key node + contains edge for YAML/TOML extractors.
func emitDataKey(nodes *[]schema.Node, edges *[]schema.Edge, lang, filePath, key string, declNode *sitter.Node) {
	*nodes = append(*nodes, schema.Node{
		ID:             schema.LangID(lang, "key", filePath+":"+key),
		Label:          key,
		SourceFile:     filePath,
		SourceLocation: nodeLocation(declNode),
	})
	*edges = append(*edges, schema.Edge{
		Source:     schema.LangID(lang, "module", filePath),
		Target:     schema.LangID(lang, "key", filePath+":"+key),
		Relation:   "contains",
		Confidence: schema.Extracted,
	})
}
