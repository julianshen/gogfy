// Package export serializes graph data to JSON and HTML formats.
package export

import (
	"encoding/json"
	"fmt"

	"github.com/julianshen/gogfy/internal/schema"
)

// GraphExport is the serializable representation of a graph for export.
type GraphExport struct {
	Nodes []schema.Node `json:"nodes"`
	Edges []schema.Edge `json:"edges"`
}

// ExportJSON returns the graph as indented JSON bytes.
func ExportJSON(g GraphExport) ([]byte, error) {
	return json.MarshalIndent(g, "", "  ")
}

// ExportHTML returns a minimal HTML representation of the graph.
func ExportHTML(g GraphExport) ([]byte, error) {
	return []byte(fmt.Sprintf("<html><body>Nodes: %d, Edges: %d</body></html>", len(g.Nodes), len(g.Edges))), nil
}
