// Package export serializes graph data to JSON and HTML formats.
package export

import (
	_ "embed"
	"encoding/json"
	"strings"

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

//go:embed template.html
var htmlTemplate string

// htmlPlaceholder is the marker the template reserves for the embedded graph
// payload. The HTML interpolates the JSON literal directly into a JS const,
// which is why the marker lives inside a JS comment.
const htmlPlaceholder = "/*__DATA__*/null"

// ExportHTML returns a self-contained interactive HTML viewer with the graph
// payload inlined as a JS literal. Opens in any modern browser; no external
// network needed. Renders an SVG force-directed layout with search filtering,
// a community legend, and a click-to-inspect panel.
//
// Nil Nodes/Edges are normalized to empty slices before marshaling so the
// viewer's `DATA.nodes.map(...)` doesn't throw `TypeError: null` for
// otherwise-valid empty graphs.
func ExportHTML(g GraphExport) ([]byte, error) {
	if g.Nodes == nil {
		g.Nodes = []schema.Node{}
	}
	if g.Edges == nil {
		g.Edges = []schema.Edge{}
	}
	payload, err := json.Marshal(g)
	if err != nil {
		return nil, err
	}
	return []byte(strings.Replace(htmlTemplate, htmlPlaceholder, string(payload), 1)), nil
}
