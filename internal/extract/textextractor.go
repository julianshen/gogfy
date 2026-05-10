package extract

import (
	"os"
	"path/filepath"

	"github.com/julianshen/gogfy/internal/schema"
)

// TextExtractor handles plain text files (.txt). No structure to parse —
// just emit a module node and one reference edge per HTTP(S) URL found
// in the body. Useful for getting README-adjacent files (CHANGELOG,
// LICENSE, NOTES) into the graph alongside structured docs.
type TextExtractor struct{}

func (TextExtractor) Extract(path string) (Result, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Result{}, err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return Result{}, err
	}
	moduleID := schema.LangID("text", "module", abs)
	state := &extractState{lang: "text", filePath: abs}
	state.nodes = append(state.nodes, schema.Node{
		ID:    moduleID,
		Label: filepath.Base(abs),
	})
	for _, u := range extractURLs(string(data)) {
		state.edges = append(state.edges, schema.Edge{
			Source:     moduleID,
			Target:     schema.LangID("text", "link", u),
			Relation:   "references",
			Confidence: schema.Extracted,
		})
	}
	return Result{Nodes: state.nodes, Edges: state.edges}, nil
}
