// Package extract defines the extraction interface and result type for source-code analysis.
package extract

import "github.com/julianshen/gogfy/internal/schema"

// Result holds the nodes and edges extracted from a single source file.
type Result struct {
	Nodes []schema.Node
	Edges []schema.Edge
}

// Extractor is the interface for extracting a graph from a source file.
type Extractor interface {
	Extract(path string) (Result, error)
}
