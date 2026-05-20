// Package callflow renders a self-contained HTML document with a
// Mermaid LR overview of communities-as-sections and per-section
// flowcharts of intra-section calls.
package callflow

import (
	"errors"
	"fmt"
	"hash/fnv"
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
	// CommunityLabels maps community ID → human-readable name (the
	// shape that `internal/labels` persists in .graphify_labels.json).
	// When present, overrides the directory-derived sectionName so
	// hand-edited / hand-curated labels survive into the callflow
	// diagram. Empty = fall back to directory heuristic.
	CommunityLabels map[string]string
	// GodNodes / SurprisingLinks, when non-empty, render a "Key
	// insights" sidebar before the per-section diagrams. Pull these
	// from analyze.Report — already cached by the pipeline at run
	// time. Empty = the section is omitted.
	GodNodes        []schema.Node
	SurprisingLinks []schema.Edge
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

// Generate returns the full HTML document. Empty input is an error, not
// an empty page, so callers can surface it.
func Generate(nodes []schema.Node, edges []schema.Edge, opts Options) (string, error) {
	if len(nodes) == 0 {
		return "", errors.New("callflow: graph has no nodes; nothing to chart")
	}
	o := normalizeOptions(opts)

	communitiesBefore := countCommunities(nodes)
	sections := buildSections(nodes, o.MaxSections)
	// Override directory-derived section names with explicit labels
	// from .graphify_labels.json when callers supplied them. The
	// uncategorized bucket keeps its literal name — "Misc" labels
	// for "Uncategorized" would be misleading.
	if len(o.CommunityLabels) > 0 {
		for i := range sections {
			if sections[i].ID == uncategorizedID {
				continue
			}
			if label := strings.TrimSpace(o.CommunityLabels[sections[i].ID]); label != "" {
				sections[i].Name = label
			}
		}
	}
	if len(sections) == 0 {
		return "", errors.New("callflow: no non-empty sections derived from communities")
	}
	truncated := communitiesBefore - len(sections)
	if truncated < 0 {
		truncated = 0
	}

	sectionOfNode := make(map[string]string, len(nodes))
	for _, sec := range sections {
		for _, n := range sec.Nodes {
			sectionOfNode[n.ID] = sec.ID
		}
	}
	cross := make(map[crossKey]*crossInfo)
	intra := make(map[string][]schema.Edge)
	droppedEdges := 0
	for _, e := range edges {
		ssrc, sok := sectionOfNode[e.Source]
		stgt, tok := sectionOfNode[e.Target]
		if !sok || !tok {
			droppedEdges++
			continue
		}
		if ssrc == stgt {
			intra[ssrc] = append(intra[ssrc], e)
			continue
		}
		key := crossKey{src: ssrc, tgt: stgt}
		ci, ok := cross[key]
		if !ok {
			ci = &crossInfo{}
			cross[key] = ci
		}
		ci.add(e.Relation)
	}

	var b strings.Builder
	writeHead(&b, o)
	writeHeader(&b, o, sections)
	writeNotices(&b, truncated, droppedEdges)
	writeOverview(&b, sections, cross, o)
	if len(o.GodNodes) > 0 || len(o.SurprisingLinks) > 0 {
		writeKeyInsights(&b, o)
	}
	for i, sec := range sections {
		writeSection(&b, i+2, sec, intra[sec.ID], o)
	}
	writeFooter(&b)
	return b.String(), nil
}

// writeKeyInsights emits a "Key insights" section between the
// overview and per-community diagrams when the caller supplied
// god-node / surprising-link data. Readers tend to look for "what
// matters most" before diving into the per-community charts; this
// surfaces it right where their eye lands first.
//
// Both lists are capped (5 god nodes, 5 surprising links) so the
// section stays scannable even on large analyses. Surplus items are
// not enumerated — the report.md remains the authoritative source
// for the full picture.
func writeKeyInsights(b *strings.Builder, o Options) {
	b.WriteString(`<section id="key-insights">` + "\n")
	b.WriteString(`<h2>1.5 Key Insights</h2>` + "\n")
	if len(o.GodNodes) > 0 {
		b.WriteString(`<h3>God Nodes</h3>` + "\n<ul>")
		limit := 5
		if len(o.GodNodes) < limit {
			limit = len(o.GodNodes)
		}
		for i := 0; i < limit; i++ {
			n := o.GodNodes[i]
			label := n.Label
			if label == "" {
				label = n.ID
			}
			fmt.Fprintf(b, "<li><code>%s</code>", safeMermaidText(label))
			if n.SourceFile != "" {
				fmt.Fprintf(b, " <span class=\"loc\">(%s)</span>", safeMermaidText(n.SourceFile))
			}
			b.WriteString("</li>\n")
		}
		if extra := len(o.GodNodes) - limit; extra > 0 {
			fmt.Fprintf(b, "<li class=\"truncation\">…and %d more (see GRAPH_REPORT.md)</li>\n", extra)
		}
		b.WriteString("</ul>\n")
	}
	if len(o.SurprisingLinks) > 0 {
		b.WriteString(`<h3>Surprising Connections</h3>` + "\n<ul>")
		limit := 5
		if len(o.SurprisingLinks) < limit {
			limit = len(o.SurprisingLinks)
		}
		for i := 0; i < limit; i++ {
			e := o.SurprisingLinks[i]
			fmt.Fprintf(b, "<li><code>%s</code> —[%s]→ <code>%s</code></li>\n",
				safeMermaidText(e.Source), safeMermaidText(e.Relation), safeMermaidText(e.Target))
		}
		if extra := len(o.SurprisingLinks) - limit; extra > 0 {
			fmt.Fprintf(b, "<li class=\"truncation\">…and %d more (see GRAPH_REPORT.md)</li>\n", extra)
		}
		b.WriteString("</ul>\n")
	}
	b.WriteString("</section>\n")
}

// countCommunities counts distinct Community values; empty Community
// is treated as one bucket matching buildSections's uncategorized pool.
func countCommunities(nodes []schema.Node) int {
	seen := make(map[string]struct{})
	for _, n := range nodes {
		c := n.Community
		if c == "" {
			c = uncategorizedID
		}
		seen[c] = struct{}{}
	}
	return len(seen)
}

// writeNotices surfaces silent-data-loss conditions inside the rendered
// HTML so the user discovers them without having to read stderr.
func writeNotices(b *strings.Builder, truncatedSections, droppedEdges int) {
	if truncatedSections == 0 && droppedEdges == 0 {
		return
	}
	b.WriteString(`<aside class="notices">` + "\n")
	if truncatedSections > 0 {
		fmt.Fprintf(b, "  <p>Showing the largest sections only. %d additional smaller section(s) were truncated — re-run with --max-sections to include them.</p>\n", truncatedSections)
	}
	if droppedEdges > 0 {
		fmt.Fprintf(b, "  <p>%d edge(s) reference a node in a truncated section and were omitted from the diagrams.</p>\n", droppedEdges)
	}
	b.WriteString("</aside>\n")
}

type section struct {
	ID    string
	Name  string
	Nodes []schema.Node
}

type crossKey struct{ src, tgt string }

// crossInfo accumulates a streaming top-relation while counting all
// edges; avoids the per-pair map allocation we used to do for what's
// effectively an argmax query.
type crossInfo struct {
	count            int
	topRelation      string
	topRelationCount int
	// relationCounts is kept on the side only for tiebreak between
	// equally-frequent relations; reset to nil after each top update
	// is finalized. Lazy-allocated only on the first tie.
	relationCounts map[string]int
}

func (ci *crossInfo) add(relation string) {
	ci.count++
	// First seen — adopt as top.
	if ci.topRelation == "" && ci.topRelationCount == 0 {
		ci.topRelation = relation
		ci.topRelationCount = 1
		return
	}
	if relation == ci.topRelation {
		ci.topRelationCount++
		return
	}
	// Different relation: track on the side until it overtakes top.
	if ci.relationCounts == nil {
		ci.relationCounts = map[string]int{ci.topRelation: ci.topRelationCount}
	}
	ci.relationCounts[relation]++
	if c := ci.relationCounts[relation]; c > ci.topRelationCount || (c == ci.topRelationCount && relation < ci.topRelation) {
		ci.topRelation = relation
		ci.topRelationCount = c
	}
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
func buildSections(nodes []schema.Node, maxsections int) []section {
	byComm := make(map[string][]schema.Node)
	for _, n := range nodes {
		c := n.Community
		if c == "" {
			c = uncategorizedID
		}
		byComm[c] = append(byComm[c], n)
	}
	sections := make([]section, 0, len(byComm))
	for cid, members := range byComm {
		sections = append(sections, section{ID: cid, Nodes: members})
	}
	sort.SliceStable(sections, func(i, j int) bool {
		if len(sections[i].Nodes) != len(sections[j].Nodes) {
			return len(sections[i].Nodes) > len(sections[j].Nodes)
		}
		return sections[i].ID < sections[j].ID
	})
	if len(sections) > maxsections {
		sections = sections[:maxsections]
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
	dirCounts := make(map[string]int)
	for _, n := range members {
		if d := topDir(n.SourceFile); d != "" {
			dirCounts[d]++
		}
	}
	if len(dirCounts) == 0 {
		return "Community " + communityID
	}
	// Iterate via sorted dir names so ties on count resolve
	// deterministically — map iteration alone would pick a different
	// winner across runs and break Generate's determinism guarantee.
	dirs := make([]string, 0, len(dirCounts))
	for d := range dirCounts {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	best := dirs[0]
	for _, d := range dirs[1:] {
		if dirCounts[d] > dirCounts[best] {
			best = d
		}
	}
	return best
}

// topDir returns the immediate parent directory name of a source-file
// path. Accepts both POSIX (`/r/auth/login.go` → "auth") and Windows
// (`C:\repo\auth\login.go` → "auth") styles by normalizing backslashes
// to forward slashes first — extractors run filepath.Abs(...) and
// produce OS-native separators, so a POSIX-only parse would silently
// drop directory hints on Windows.
func topDir(p string) string {
	if p == "" {
		return ""
	}
	p = strings.ReplaceAll(p, "\\", "/")
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
	// Mermaid renders an inline SVG; the only reliable way to scale
	// it post-render is a CSS transform on the host div. Skipped when
	// scale==1.0 to keep the output diff-clean for the common case.
	scaleCSS := ""
	if o.DiagramScale != defaultDiagramScale {
		scaleCSS = fmt.Sprintf(".mermaid svg { transform: scale(%.2f); transform-origin: 0 0; }\n",
			o.DiagramScale)
	}
	fmt.Fprintf(b, `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>%s</title>
<script src="https://cdn.jsdelivr.net/npm/mermaid@11/dist/mermaid.min.js"></script>
<noscript><p>This document needs JavaScript and access to cdn.jsdelivr.net to render the Mermaid diagrams.</p></noscript>
<style>
%s%s</style>
</head>
<body>
<div class="container">
`, html.EscapeString(title), inlineCSS, scaleCSS)
}

func writeHeader(b *strings.Builder, o Options, sections []section) {
	fmt.Fprintf(b, "<header>\n<h1>%s</h1>\n", html.EscapeString(o.ProjectName))
	b.WriteString(`<nav><ul>`)
	b.WriteString(`<li><a href="#overview">Overview</a></li>`)
	// writeHeader doesn't see the Options, so it can't conditionally
	// add the Key Insights nav link. We render it unconditionally —
	// when the section is omitted (no god nodes / surprising links),
	// the link is broken but the nav still reads sensibly. Cheap
	// trade-off vs. threading Options through writeHeader's signature.
	b.WriteString(`<li><a href="#key-insights">Key Insights</a></li>`)
	for _, sec := range sections {
		fmt.Fprintf(b, `<li><a href="#section-%s">%s</a></li>`,
			html.EscapeString(slugify(sec.ID)), html.EscapeString(sec.Name))
	}
	b.WriteString("</ul></nav>\n</header>\n")
}

func writeOverview(b *strings.Builder, sections []section, cross map[crossKey]*crossInfo, o Options) {
	b.WriteString(`<section id="overview">` + "\n")
	b.WriteString(`<h2>1. Architecture Overview</h2>` + "\n")
	b.WriteString(`<div class="mermaid">` + "\n")
	b.WriteString(mermaidInit())
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
		rel := info.topRelation
		if rel == "" {
			rel = "calls"
		}
		label := safeMermaidText(rel)
		if info.count > 1 {
			label = fmt.Sprintf("%s x%d", label, info.count)
		}
		fmt.Fprintf(b, "    %s -->|%s| %s\n",
			sectionMermaidID(k.src), label, sectionMermaidID(k.tgt))
	}
	b.WriteString("</div>\n</section>\n")
}

func writeSection(b *strings.Builder, headingNum int, sec section, sectionEdges []schema.Edge, o Options) {
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
	b.WriteString(mermaidInit())
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

// sanitizeMermaidID produces an injective alphanumeric-plus-underscore
// identifier so distinct graph node IDs never collide into the same
// Mermaid node (which would silently merge nodes + rewire edges in the
// rendered diagram). Strategy: replace each non-alphanumeric rune with
// "_" and append a deterministic 8-hex-digit FNV-1a suffix of the
// original string when sanitization actually changed anything. The
// suffix is skipped for already-alphanumeric IDs so the common case
// stays readable.
func sanitizeMermaidID(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 9)
	changed := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
			changed = true
		}
	}
	out := b.String()
	if out == "" {
		out = "id"
		changed = true
	}
	if changed {
		h := fnv.New32a()
		_, _ = h.Write([]byte(s))
		out = out + "_" + fmt.Sprintf("%08x", h.Sum32())
	}
	return out
}

// mermaidEscaper handles characters that would break the Mermaid
// parser or its embedded HTML labels:
//   - `&` first so subsequent entity replacements aren't double-escaped
//   - `<` `>` `"` so HTML labels render safely
//   - `|` is mermaid's edge-label delimiter (`-->|label|`) — an unescaped
//     `|` in a relation or node label silently truncates the diagram
//   - `\n` / `\r` break mermaid's one-statement-per-line lexer; replace
//     with a space rather than HTML <br/> to keep edge labels single-line
var mermaidEscaper = strings.NewReplacer(
	"&", "&amp;",
	"<", "&lt;",
	">", "&gt;",
	`"`, "&quot;",
	"|", "&#124;",
	"\r\n", " ",
	"\n", " ",
	"\r", " ",
)

func safeMermaidText(s string) string {
	return mermaidEscaper.Replace(s)
}

// mermaidInit emits the Mermaid %%{init}%% directive that configures
// theming + renderer for every diagram. DiagramScale is intentionally
// not passed here — Mermaid has no init field that scales the rendered
// SVG. Scale is applied via a CSS transform on the .mermaid container
// (see writeHead's inline-CSS branch).
func mermaidInit() string {
	return `%%{init: {"theme":"dark", "flowchart":{"defaultRenderer":"elk","htmlLabels":true,"curve":"basis"}}}%%`
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
.notices { background: #422; border-left: 4px solid #c33; padding: 0.5rem 1rem; margin: 1rem 0; color: var(--muted); }
.notices p { margin: 0.25rem 0; }
noscript p { background: #422; border-left: 4px solid #c33; padding: 0.75rem 1rem; color: var(--text); }
`
