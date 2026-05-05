package extract

import "github.com/julianshen/gogfy/internal/schema"

type Result struct {
	Nodes []schema.Node
	Edges []schema.Edge
}

type Extractor interface {
	Extract(path string) (Result, error)
}
