// Package callflow renders a self-contained call-flow architecture HTML
// document from a graphify-style graph: a Mermaid LR overview at the
// section/community level + per-section flowcharts. Modeled after
// graphify's callflow_html.py (English-only v1; no labels/sections-file
// or GRAPH_REPORT integration yet).
package callflow

import (
	"errors"
	"fmt"
	"html"
	"path"
	"sort"
	"strings"

	"github.com/julianshen/gogfy/internal/schema"
)

// Options controls rendering. Zero/negative fields select package defaults.
type Options struct {
	MaxSections        int     // default 15
	MaxNodesPerSection int     // default 18
	MaxEdgesPerSection int     // default 24
	DiagramScale       float64 // default 1.0 (passed to mermaid init)
	ProjectName        string  // appears in <title> and header; default "Project"
}

const (
	defaultMaxSections  = 15
	defaultMaxNodes     = 18
	defaultMaxEdges     = 24
	defaultDiagramScale = 1.0
	defaultProjectName  = "Project"
	overviewEdgesCap    = 12
	uncategorizedID     = "uncategorized"
)

// Generate returns the full HTML document. Empty input is an error
// rather than an empty page so the caller can warn the user (a
// callflow doc with zero sections is not useful).
func Generate(nodes []schema.Node, edges []schema.Edge, opts Options) (string, error) {
	if len(nodes) == 0 {
		return "", errors.New("callflow: graph has no nodes; nothing to chart")
	}
	o := normalizeOptions(opts)

	sections := buildSections(nodes, o.MaxSections)
	if len(sections) == 0 {
		return "", errors.New("callflow: no non-empty sections derived from communities")
	}

	sectionOfNode := make(map[string]string, len(nodes))
	for _, sec := range sections {
		for _, n := range sec.Nodes {
			sectionOfNode[n.ID] = sec.ID
		}
	}
	cross := make(map[crossKey]*crossInfo)
	intra := make(map[string][]schema.Edge)
	for _, e := range edges {
		ssrc, sok := sectionOfNode[e.Source]
		stgt, tok := sectionOfNode[e.Target]
		if !sok || !tok {
			continue // endpoint in a dropped section or missing node
		}
		if ssrc == stgt {
			intra[ssrc] = append(intra[ssrc], e)
			continue
		}
		key := crossKey{src: ssrc, tgt: stgt}
		ci, ok := cross[key]
		if !ok {
			ci = &crossInfo{relations: make(map[string]int)}
			cross[key] = ci
		}
		ci.count++
		ci.relations[e.Relation]++
	}

	var b strings.Builder
	writeHead(&b, o)
	writeHeader(&b, o, sections)
	writeOverview(&b, sections, cross, o)
	for i, sec := range sections {
		writeSection(&b, i+2, sec, intra[sec.ID], o)
	}
	writeFooter(&b)
	return b.String(), nil
}

type Section struct {
	ID    string
	Name  string
	Nodes []schema.Node
}

type crossKey struct{ src, tgt string }

type crossInfo struct {
	count     int
	relations map[string]int // relation → count, for picking the most-common label
}

func normalizeOptions(o Options) Options {
	if o.MaxSections <= 0 {
		o.MaxSections = defaultMaxSections
	}
	if o.MaxNodesPerSection <= 0 {
		o.MaxNodesPerSection = defaultMaxNodes
	}
	if o.MaxEdgesPerSection <= 0 {
		o.MaxEdgesPerSection = defaultMaxEdges
	}
	if o.DiagramScale <= 0 {
		o.DiagramScale = defaultDiagramScale
	}
	if o.ProjectName == "" {
		o.ProjectName = defaultProjectName
	}
	return o
}

// buildSections groups nodes by Community. Nodes with empty Community
// pool into a single "uncategorized" section so they're not silently
// dropped.
func buildSections(nodes []schema.Node, maxSections int) []Section {
	byComm := make(map[string][]schema.Node)
	for _, n := range nodes {
		c := n.Community
		if c == "" {
			c = uncategorizedID
		}
		byComm[c] = append(byComm[c], n)
	}
	sections := make([]Section, 0, len(byComm))
	for cid, members := range byComm {
		sections = append(sections, Section{ID: cid, Nodes: members})
	}
	sort.SliceStable(sections, func(i, j int) bool {
		if len(sections[i].Nodes) != len(sections[j].Nodes) {
			return len(sections[i].Nodes) > len(sections[j].Nodes)
		}
		return sections[i].ID < sections[j].ID
	})
	if len(sections) > maxSections {
		sections = sections[:maxSections]
	}
	// Sort surviving sections' members and name them only after
	// truncation — skipping the cost for sections that get cut.
	for i := range sections {
		sort.SliceStable(sections[i].Nodes, func(a, b int) bool {
			return sections[i].Nodes[a].ID < sections[i].Nodes[b].ID
		})
		sections[i].Name = sectionName(sections[i].ID, sections[i].Nodes)
	}
	return sections
}

// sectionName picks the most-common parent directory among the
// section's source files as a human-readable label. Falls back to
// "Community <id>" when no directory dominates.
func sectionName(communityID string, members []schema.Node) string {
	if communityID == uncategorizedID {
		return "Uncategorized"
	}
	// Most common directory among the member source files; gives the
	// section a name like "auth" or "api/routes" without needing a
	// separate labels file.
	dirCounts := make(map[string]int)
	for _, n := range members {
		if d := topDir(n.SourceFile); d != "" {
			dirCounts[d]++
		}
	}
	best, bestCount := "", 0
	for d, c := range dirCounts {
		if c > bestCount || (c == bestCount && d < best) {
			best, bestCount = d, c
		}
	}
	if best != "" {
		return best
	}
	return "Community " + communityID
}

// topDir returns the immediate parent directory name of a POSIX-style
// source-file path. For "/r/auth/login.go" → "auth".
func topDir(p string) string {
	if p == "" {
		return ""
	}
	dir := path.Dir(strings.TrimRight(p, "/"))
	if dir == "/" || dir == "." {
		return ""
	}
	return path.Base(dir)
}

// ─────────────────────────────────────────────────────────────────────────────
// HTML emission

func writeHead(b *strings.Builder, o Options) {
	title := o.ProjectName + " — Call Flow & Architecture"
	fmt.Fprintf(b, `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>%s</title>
<script src="https://cdn.jsdelivr.net/npm/mermaid@11/dist/mermaid.min.js"></script>
<style>
%s
</style>
</head>
<body>
<div class="container">
`, html.EscapeString(title), inlineCSS)
}

func writeHeader(b *strings.Builder, o Options, sections []Section) {
	fmt.Fprintf(b, "<header>\n<h1>%s</h1>\n", html.EscapeString(o.ProjectName))
	b.WriteString(`<nav><ul>`)
	b.WriteString(`<li><a href="#overview">Overview</a></li>`)
	for _, sec := range sections {
		fmt.Fprintf(b, `<li><a href="#section-%s">%s</a></li>`,
			html.EscapeString(slugify(sec.ID)), html.EscapeString(sec.Name))
	}
	b.WriteString("</ul></nav>\n</header>\n")
}

func writeOverview(b *strings.Builder, sections []Section, cross map[crossKey]*crossInfo, o Options) {
	b.WriteString(`<section id="overview">` + "\n")
	b.WriteString(`<h2>1. Architecture Overview</h2>` + "\n")
	b.WriteString(`<div class="mermaid">` + "\n")
	b.WriteString(mermaidInit(o.DiagramScale))
	b.WriteString("\nflowchart LR\n")
	for _, sec := range sections {
		nid := sectionMermaidID(sec.ID)
		label := fmt.Sprintf("%s<br/><small>%d nodes</small>",
			safeMermaidText(sec.Name), len(sec.Nodes))
		fmt.Fprintf(b, "    %s(\"%s\")\n", nid, label)
	}
	keys := make([]crossKey, 0, len(cross))
	for k := range cross {
		keys = append(keys, k)
	}
	sort.SliceStable(keys, func(i, j int) bool {
		if cross[keys[i]].count != cross[keys[j]].count {
			return cross[keys[i]].count > cross[keys[j]].count
		}
		if keys[i].src != keys[j].src {
			return keys[i].src < keys[j].src
		}
		return keys[i].tgt < keys[j].tgt
	})
	if len(keys) > overviewEdgesCap {
		keys = keys[:overviewEdgesCap]
	}
	for _, k := range keys {
		info := cross[k]
		rel := mostCommonRelation(info.relations)
		label := safeMermaidText(rel)
		if info.count > 1 {
			label = fmt.Sprintf("%s x%d", label, info.count)
		}
		fmt.Fprintf(b, "    %s -->|%s| %s\n",
			sectionMermaidID(k.src), label, sectionMermaidID(k.tgt))
	}
	b.WriteString("</div>\n</section>\n")
}

func writeSection(b *strings.Builder, headingNum int, sec Section, sectionEdges []schema.Edge, o Options) {
	fmt.Fprintf(b, "<section id=\"section-%s\">\n",
		html.EscapeString(slugify(sec.ID)))
	fmt.Fprintf(b, "<h2>%d. %s</h2>\n", headingNum, html.EscapeString(sec.Name))
	fmt.Fprintf(b, "<p class=\"summary\">%d nodes, %d intra-section edges.</p>\n",
		len(sec.Nodes), len(sectionEdges))

	selected := selectDiagramNodes(sec.Nodes, sectionEdges, o.MaxNodesPerSection)
	selectedSet := make(map[string]bool, len(selected))
	for _, n := range selected {
		selectedSet[n.ID] = true
	}

	b.WriteString(`<div class="mermaid">` + "\n")
	b.WriteString(mermaidInit(o.DiagramScale))
	b.WriteString("\nflowchart LR\n")
	if len(selected) == 0 {
		fmt.Fprintf(b, "    empty(\"%s - no nodes\")\n", safeMermaidText(sec.Name))
		b.WriteString("</div>\n</section>\n")
		return
	}
	for _, n := range selected {
		label := n.Label
		if label == "" {
			label = n.ID
		}
		fmt.Fprintf(b, "    %s(\"%s\")\n",
			nodeMermaidID(n.ID), safeMermaidText(label))
	}
	emitted := 0
	for _, e := range sectionEdges {
		if emitted >= o.MaxEdgesPerSection {
			break
		}
		if !selectedSet[e.Source] || !selectedSet[e.Target] {
			continue
		}
		fmt.Fprintf(b, "    %s -->|%s| %s\n",
			nodeMermaidID(e.Source), safeMermaidText(e.Relation), nodeMermaidID(e.Target))
		emitted++
	}
	omittedNodes := max0(len(sec.Nodes) - len(selected))
	if omittedNodes > 0 {
		fmt.Fprintf(b, "    %%%% Omitted for readability: %d nodes\n", omittedNodes)
	}
	b.WriteString("</div>\n</section>\n")
}

func writeFooter(b *strings.Builder) {
	b.WriteString(`<script>
if (window.mermaid && window.mermaid.initialize) {
  window.mermaid.initialize({ startOnLoad: true, theme: 'dark', securityLevel: 'loose' });
}
</script>
</div>
</body>
</html>
`)
}

// selectDiagramNodes returns the top-cap nodes ranked by intra-section
// degree, ties broken on node ID for determinism.
func selectDiagramNodes(nodes []schema.Node, edges []schema.Edge, cap int) []schema.Node {
	if len(nodes) <= cap {
		return nodes
	}
	deg := make(map[string]int, len(nodes))
	for _, e := range edges {
		deg[e.Source]++
		deg[e.Target]++
	}
	scored := append([]schema.Node(nil), nodes...)
	sort.SliceStable(scored, func(i, j int) bool {
		if deg[scored[i].ID] != deg[scored[j].ID] {
			return deg[scored[i].ID] > deg[scored[j].ID]
		}
		return scored[i].ID < scored[j].ID
	})
	return scored[:cap]
}

// Mermaid identifier + text escaping. Mermaid is picky: node IDs must
// be alphanumeric+underscore; node labels inside quoted strings can't
// contain double-quotes or `<script>` breakouts.

func sectionMermaidID(id string) string {
	return "section_" + sanitizeMermaidID(id)
}

func nodeMermaidID(id string) string {
	return "node_" + sanitizeMermaidID(id)
}

func sanitizeMermaidID(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := b.String()
	if out == "" {
		return "id"
	}
	return out
}

// safeMermaidText escapes a label for use inside a mermaid quoted
// node-text or edge-label. Mermaid renders HTML inside labels, so we
// apply standard HTML-entity escaping for `&`, `<`, `>`, `"` — missing
// `&` would let a label containing a literal `&` mis-parse downstream
// entities; missing `"` would close the quoted-string node early.
var mermaidEscaper = strings.NewReplacer(
	"&", "&amp;",
	"<", "&lt;",
	">", "&gt;",
	`"`, "&quot;",
)

func safeMermaidText(s string) string {
	return mermaidEscaper.Replace(s)
}

func mermaidInit(scale float64) string {
	return fmt.Sprintf(`%%%%{init: {"theme":"dark", "flowchart":{"defaultRenderer":"elk","htmlLabels":true,"curve":"basis"}}}%%%%
%%%% scale: %.2f`, scale)
}

func mostCommonRelation(rels map[string]int) string {
	if len(rels) == 0 {
		return "calls"
	}
	type rk struct {
		rel   string
		count int
	}
	xs := make([]rk, 0, len(rels))
	for r, c := range rels {
		xs = append(xs, rk{r, c})
	}
	sort.SliceStable(xs, func(i, j int) bool {
		if xs[i].count != xs[j].count {
			return xs[i].count > xs[j].count
		}
		return xs[i].rel < xs[j].rel
	})
	return xs[0].rel
}

func slugify(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + 32)
		default:
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "section"
	}
	return out
}

func max0(n int) int {
	if n < 0 {
		return 0
	}
	return n
}

const inlineCSS = `:root {
  --bg: #0f172a; --surface: #1e293b; --border: #334155;
  --text: #e2e8f0; --muted: #94a3b8; --accent: #38bdf8;
}
* { box-sizing: border-box; }
body { margin: 0; background: var(--bg); color: var(--text); font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", system-ui, sans-serif; line-height: 1.6; }
.container { max-width: 1200px; margin: 0 auto; padding: 2rem 1.5rem; }
header h1 { margin: 0 0 1rem; color: var(--accent); font-size: 1.75rem; }
nav ul { list-style: none; padding: 0; display: flex; flex-wrap: wrap; gap: 0.5rem 1rem; border-bottom: 1px solid var(--border); padding-bottom: 1rem; margin-bottom: 2rem; }
nav a { color: var(--accent); text-decoration: none; font-size: 0.9rem; }
nav a:hover { text-decoration: underline; }
section { margin: 2.5rem 0; }
section h2 { color: var(--accent); border-left: 4px solid var(--accent); padding-left: 0.75rem; }
.summary { color: var(--muted); font-size: 0.9rem; }
.mermaid { background: var(--surface); padding: 1rem; border: 1px solid var(--border); border-radius: 6px; overflow-x: auto; }
`
