// Package export serializes graph data to JSON and HTML formats.
package export

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/julianshen/gogfy/internal/schema"
)

// CurrentSchemaVersion is the value ExportJSON stamps on every new
// graph.json. Bump when the schema changes in a way older readers
// can't transparently handle (e.g. new required fields, renamed
// keys). Additive optional fields (the historical pattern — see
// Hyperedges below) DON'T require a bump because old readers just
// see the zero value.
//
// Versioning policy:
//   - "v1"  — pre-versioning baseline. Implicit when SchemaVersion
//             is missing in a loaded file.
//   - "v2"  — adds Hyperedges (PR #120). Old "v1" readers see nil
//             Hyperedges and behave correctly; new code may rely on
//             the field being populated.
//
// LoadJSON warns when it encounters a NEWER version than this
// constant — the file might use fields the current build can't
// interpret. Older versions are accepted silently and transparently
// upgraded in-memory.
const CurrentSchemaVersion = "v2"

// GraphExport is the serializable representation of a graph for export.
type GraphExport struct {
	// SchemaVersion identifies the on-disk schema. Always emitted as
	// the FIRST JSON field so consumers can route on it via a
	// streaming reader without parsing the whole file. omitempty
	// would defeat that — explicit value with no omitempty.
	SchemaVersion string        `json:"schema_version"`
	Nodes         []schema.Node `json:"nodes"`
	Edges         []schema.Edge `json:"edges"`
	// Hyperedges are N-ary relationships (3+ participants) emitted
	// by semantic extraction. omitempty so existing graph.json files
	// without the field round-trip cleanly.
	Hyperedges []schema.Hyperedge `json:"hyperedges,omitempty"`
	// BuiltAtCommit, when non-empty, records the short git SHA of HEAD
	// at the time graph.json was written. Lets a cross-tool consumer
	// (wiki regenerator, downstream analysis script) detect a stale
	// graph against a fresh repo. omitempty so existing graph.json
	// files without the field still round-trip cleanly.
	BuiltAtCommit string `json:"built_at_commit,omitempty"`
}

// SchemaVersionWarner receives a one-shot warning when LoadJSON
// reads a file from a NEWER schema than the current build. Tests
// override to assert; production leaves the default (os.Stderr).
var SchemaVersionWarner io.Writer = os.Stderr

// ExportJSON returns the graph as indented JSON bytes. Stamps the
// CurrentSchemaVersion on the result unless the caller has already
// set a non-empty value (lets downstream tools rewrite a graph at
// an older version explicitly, e.g. for backwards-compat fixtures).
func ExportJSON(g GraphExport) ([]byte, error) {
	if g.SchemaVersion == "" {
		g.SchemaVersion = CurrentSchemaVersion
	}
	return json.MarshalIndent(g, "", "  ")
}

// LoadJSON reads and decodes a graph.json file. The "missing file"
// error wraps os.ErrNotExist so callers can detect the first-run case
// via errors.Is.
//
// Schema-version handling:
//   - Missing/empty version → treat as "v1" (pre-versioning baseline)
//     and continue. The historical files all parse cleanly under the
//     additive-field rule.
//   - Newer-than-current version → emit a one-shot warning on
//     SchemaVersionWarner but DO NOT error. Old code reading a new
//     graph still extracts what it understands; refusing to parse
//     would create a worse failure mode than degraded output.
func LoadJSON(path string) (GraphExport, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return GraphExport{}, fmt.Errorf("export: read %s: %w", path, err)
	}
	var g GraphExport
	if err := json.Unmarshal(data, &g); err != nil {
		return GraphExport{}, fmt.Errorf("export: parse %s: %w", path, err)
	}
	if g.SchemaVersion == "" {
		g.SchemaVersion = "v1"
	}
	if isNewerSchema(g.SchemaVersion, CurrentSchemaVersion) {
		warnNewerSchemaOnce(path, g.SchemaVersion)
	}
	return g, nil
}

// isNewerSchema returns true when `got` represents a schema strictly
// newer than `want`. Both values are expected to follow the "vN"
// convention; non-conforming strings compare lexicographically as a
// safety net so a misformed version doesn't crash.
func isNewerSchema(got, want string) bool {
	gn, gok := parseSchemaVersion(got)
	wn, wok := parseSchemaVersion(want)
	if gok && wok {
		return gn > wn
	}
	return got > want
}

func parseSchemaVersion(s string) (int, bool) {
	if len(s) < 2 || s[0] != 'v' {
		return 0, false
	}
	n := 0
	for i := 1; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
		n = n*10 + int(s[i]-'0')
	}
	return n, true
}

// warnNewerSchemaOnce prints the warning at most once per (path,
// version) pair so a re-run loop doesn't spam stderr.
var (
	warnedSchemasMu sync.Mutex
	warnedSchemas   = map[string]struct{}{}
)

func warnNewerSchemaOnce(path, version string) {
	key := path + "|" + version
	warnedSchemasMu.Lock()
	defer warnedSchemasMu.Unlock()
	if _, seen := warnedSchemas[key]; seen {
		return
	}
	warnedSchemas[key] = struct{}{}
	fmt.Fprintf(SchemaVersionWarner,
		"gogfy: %s has schema_version %q — newer than this build (%q). Some fields may be ignored.\n",
		path, version, CurrentSchemaVersion)
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
	switch {
	case maxNodes < 0:
		// Negative is the opt-out sentinel — callers who want the full
		// fidelity even on huge graphs (offline analysis, CI artifact
		// captures) can pass -1 and accept the renderer slowdown.
		maxNodes = 0
	case maxNodes == 0:
		maxNodes = defaultMaxNodes
	}
	if maxNodes > 0 && len(g.Nodes) > maxNodes {
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
// the meta-graph) — a 1000-edge community renders as one edgeless
// meta-node; full detail is preserved in the wiki + Obsidian artifacts.
// Nodes without a Community keep an empty community key internally so
// a user-defined community literally named "_" doesn't collide.
func aggregateByCommunity(g GraphExport) GraphExport {
	type pair struct{ a, b string }

	nodeCommunity := make(map[string]string, len(g.Nodes))
	memberCount := map[string]int{}
	for _, n := range g.Nodes {
		nodeCommunity[n.ID] = n.Community // empty string for unassigned
		memberCount[n.Community]++
	}

	crossCount := map[pair]int{}
	for _, e := range g.Edges {
		cu, oku := nodeCommunity[e.Source]
		cv, okv := nodeCommunity[e.Target]
		if !oku || !okv || cu == cv {
			// Same community (including both-unassigned: cu == cv == "")
			// is intra; dangling-ref edges (!ok) have no place to land.
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
		id := "community:" + c
		if c == "" {
			label = "(unassigned)"
			id = "community:__unassigned__"
		}
		out.Nodes = append(out.Nodes, schema.Node{
			ID:        id,
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
	idFor := func(c string) string {
		if c == "" {
			return "community:__unassigned__"
		}
		return "community:" + c
	}
	for _, k := range keys {
		w := crossCount[k]
		out.Edges = append(out.Edges, schema.Edge{
			Source: idFor(k.a),
			Target: idFor(k.b),
			Relation: fmt.Sprintf("%d cross-community edges", w),
			// Meta-edges aggregate multiple primary edges — they're not
			// observed directly in source, so INFERRED is the closest
			// fit in the existing confidence enum (no "aggregated"
			// value exists today).
			Confidence: schema.Inferred,
		})
	}
	return out
}
