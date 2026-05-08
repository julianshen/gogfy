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

// Template placeholders the embedded HTML reserves for runtime interpolation.
// Both live inside JS comments so the unsubstituted template still parses.
const (
	htmlPlaceholder     = "/*__DATA__*/null"
	directedPlaceholder = "/*__DIRECTED__*/false"
)

// HTMLOptions controls optional rendering behavior of the interactive viewer.
type HTMLOptions struct {
	// Directed, when true, renders arrowheads on every edge so the viewer
	// reflects the source→target direction encoded in the graph.
	Directed bool
}

// ExportHTML returns a self-contained interactive HTML viewer with the graph
// payload inlined as a JS literal. Opens in any modern browser; no external
// network needed. Renders an SVG force-directed layout with search filtering,
// a community legend, and a click-to-inspect panel.
//
// Nil Nodes/Edges are normalized to empty slices before marshaling so the
// viewer's `DATA.nodes.map(...)` doesn't throw `TypeError: null` for
// otherwise-valid empty graphs.
func ExportHTML(g GraphExport, opts HTMLOptions) ([]byte, error) {
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
	directed := "false"
	if opts.Directed {
		directed = "true"
	}
	out := strings.Replace(htmlTemplate, htmlPlaceholder, string(payload), 1)
	out = strings.Replace(out, directedPlaceholder, directed, 1)
	return []byte(out), nil
}
