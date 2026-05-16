// Package export serializes graph data to JSON and HTML formats.
package export

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"sort"
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

// LoadJSON reads and decodes a graph.json file. The "missing file"
// error wraps os.ErrNotExist so callers can detect the first-run case
// via errors.Is.
func LoadJSON(path string) (GraphExport, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return GraphExport{}, fmt.Errorf("export: read %s: %w", path, err)
	}
	var g GraphExport
	if err := json.Unmarshal(data, &g); err != nil {
		return GraphExport{}, fmt.Errorf("export: parse %s: %w", path, err)
	}
	return g, nil
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
	// MaxNodes caps the number of nodes the SVG renderer is asked to draw.
	// When the graph exceeds this, ExportHTML collapses each community
	// into a single meta-node so the visualization stays navigable on
	// large repos. Zero → defaultMaxNodes.
	MaxNodes int
}

// defaultMaxNodes balances "renders smoothly in a browser" against
// "shows enough detail to be useful." The SVG force-layout starts to
// stutter above ~1000 nodes on typical hardware.
const defaultMaxNodes = 1000

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
	maxNodes := opts.MaxNodes
	if maxNodes <= 0 {
		maxNodes = defaultMaxNodes
	}
	if len(g.Nodes) > maxNodes {
		g = aggregateByCommunity(g)
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

// aggregateByCommunity collapses each community into a single node and
// each pair of cross-community edges into a single weighted meta-edge.
// Intra-community edges are dropped (they no longer have endpoints in
// the meta-graph). Nodes without a Community fold into an "_" bucket
// so they still appear in the viz.
func aggregateByCommunity(g GraphExport) GraphExport {
	type pair struct{ a, b string }

	nodeCommunity := make(map[string]string, len(g.Nodes))
	memberCount := map[string]int{}
	for _, n := range g.Nodes {
		c := n.Community
		if c == "" {
			c = "_"
		}
		nodeCommunity[n.ID] = c
		memberCount[c]++
	}

	crossCount := map[pair]int{}
	for _, e := range g.Edges {
		cu := nodeCommunity[e.Source]
		cv := nodeCommunity[e.Target]
		if cu == "" || cv == "" || cu == cv {
			continue
		}
		if cu > cv {
			cu, cv = cv, cu
		}
		crossCount[pair{cu, cv}]++
	}

	// Sorted iteration so the on-screen layout (and the test's
	// substring assertions) stay deterministic across runs.
	cids := make([]string, 0, len(memberCount))
	for c := range memberCount {
		cids = append(cids, c)
	}
	sort.Strings(cids)

	out := GraphExport{
		Nodes: make([]schema.Node, 0, len(cids)),
		Edges: make([]schema.Edge, 0, len(crossCount)),
	}
	for _, c := range cids {
		label := "Community " + c
		if c == "_" {
			label = "(unassigned)"
		}
		out.Nodes = append(out.Nodes, schema.Node{
			ID:        "community:" + c,
			Label:     label,
			Community: c,
		})
	}
	keys := make([]pair, 0, len(crossCount))
	for k := range crossCount {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].a != keys[j].a {
			return keys[i].a < keys[j].a
		}
		return keys[i].b < keys[j].b
	})
	for _, k := range keys {
		w := crossCount[k]
		out.Edges = append(out.Edges, schema.Edge{
			Source:     "community:" + k.a,
			Target:     "community:" + k.b,
			Relation:   fmt.Sprintf("%d cross-community edges", w),
			Confidence: schema.Inferred,
		})
	}
	return out
}
