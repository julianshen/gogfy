// Package report renders analysis results as a Markdown report.
package report

import (
	"bytes"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/julianshen/gogfy/internal/analyze"
	"github.com/julianshen/gogfy/internal/schema"
)

// Options carries data the rich-report sections need beyond the analyze
// summary. Old callers can stay on Render(r) and get the trimmed report;
// callers with raw graph access should switch to RenderWithOptions for
// Communities / Ambiguous Edges / Knowledge Gaps / Corpus coverage.
type Options struct {
	Nodes []schema.Node
	Edges []schema.Edge
	// BuiltAtCommit, when non-empty, emits a "Graph Freshness" section so
	// readers can tell which commit a stale report belongs to. Callers
	// should pass a short SHA; we don't truncate (caller's policy).
	BuiltAtCommit string
	// ThinCommunityMin filters tiny "communities" out of the Communities
	// section. Zero defaults to 2 (single-node clusters are noise from
	// extraction, not real groupings). Set to 1 to disable filtering.
	ThinCommunityMin int
	// CommunityLabels maps community ID → human-readable name (from
	// labels.Load). Empty/missing entries fall back to "Community <id>".
	CommunityLabels map[string]string
}

const defaultThinCommunityMin = 2

// Render produces the trimmed report (no data-dependent sections) and
// preserves the original Render(r) call shape.
func Render(r analyze.Report) ([]byte, error) {
	return RenderWithOptions(r, Options{})
}

// RenderWithOptions emits the full report. Sections appear in the order
// upstream graphify uses; sections with no data are silently omitted.
func RenderWithOptions(r analyze.Report, opts Options) ([]byte, error) {
	if opts.ThinCommunityMin == 0 {
		opts.ThinCommunityMin = defaultThinCommunityMin
	}
	// Shared across writeCommunityHubs / writeKnowledgeGaps so we don't
	// scan the edge slice twice for the same map.
	degree := buildDegree(opts.Edges)
	var b bytes.Buffer
	fmt.Fprintf(&b, "# Graph Report\n\n")

	writeSummary(&b, r, opts)
	writeCorpus(&b, opts)
	writeGraphFreshness(&b, opts)
	writeCommunityHubs(&b, opts, degree)

	fmt.Fprintf(&b, "## God Nodes\n")
	if len(r.GodNodes) == 0 {
		fmt.Fprintf(&b, "_None found_\n")
	} else {
		for _, n := range r.GodNodes {
			fmt.Fprintf(&b, "- %s\n", escapeMarkdown(n.Label))
		}
	}

	fmt.Fprintf(&b, "\n## Surprising Links\n")
	if len(r.SurprisingLinks) == 0 {
		fmt.Fprintf(&b, "_None found_\n")
	} else {
		for _, e := range r.SurprisingLinks {
			fmt.Fprintf(&b, "- %s -> %s (%s)\n", escapeMarkdown(e.Source), escapeMarkdown(e.Target), escapeMarkdown(e.Relation))
		}
	}

	writeCommunities(&b, opts)
	writeAmbiguousEdges(&b, opts)
	writeKnowledgeGaps(&b, r, opts, degree)

	if len(r.ConfidenceSummary) > 0 {
		fmt.Fprintf(&b, "\n## Confidence\n")
		for _, c := range []schema.Confidence{schema.Extracted, schema.Inferred, schema.Ambiguous} {
			fmt.Fprintf(&b, "- %s: %d\n", c, r.ConfidenceSummary[c])
		}
	}

	fmt.Fprintf(&b, "\n## Exploration Questions\n")
	if len(r.ExplorationQuestions) == 0 {
		fmt.Fprintf(&b, "_None found_\n")
	} else {
		for _, q := range r.ExplorationQuestions {
			fmt.Fprintf(&b, "- %s\n", q)
		}
	}

	return b.Bytes(), nil
}

func writeSummary(b *bytes.Buffer, r analyze.Report, opts Options) {
	if len(opts.Nodes) == 0 && len(opts.Edges) == 0 {
		return
	}
	communities := map[string]struct{}{}
	for _, n := range opts.Nodes {
		if n.Community != "" {
			communities[n.Community] = struct{}{}
		}
	}
	fmt.Fprintf(b, "## Summary\n")
	fmt.Fprintf(b, "- %d nodes, %d edges, %d communities, %d god nodes\n",
		len(opts.Nodes), len(opts.Edges), len(communities), len(r.GodNodes))
	if len(r.ConfidenceSummary) > 0 {
		fmt.Fprintf(b, "- Confidence: %d extracted / %d inferred / %d ambiguous\n",
			r.ConfidenceSummary[schema.Extracted],
			r.ConfidenceSummary[schema.Inferred],
			r.ConfidenceSummary[schema.Ambiguous])
	}
	b.WriteString("\n")
}

// writeCorpus surfaces file/language counts so readers can detect a
// shrinking corpus before they read deeper into the report.
func writeCorpus(b *bytes.Buffer, opts Options) {
	files := map[string]struct{}{}
	exts := map[string]struct{}{}
	types := map[schema.FileType]int{}
	for _, n := range opts.Nodes {
		if n.SourceFile == "" {
			continue
		}
		if _, dup := files[n.SourceFile]; !dup {
			// Classify from SourceFile rather than n.FileType so the
			// tally still works for synthetic nodes whose FileType was
			// never populated (resolve.Calls fan-outs, dedup merges).
			// Counts unique files, not nodes — otherwise a file with
			// many functions would dominate the breakdown.
			if t := schema.ClassifyFile(n.SourceFile); t != "" {
				types[t]++
			}
		}
		files[n.SourceFile] = struct{}{}
		if e := filepath.Ext(n.SourceFile); e != "" {
			exts[e] = struct{}{}
		}
	}
	if len(files) == 0 {
		return
	}
	// Tiny-corpus warning: under this size, the source code fits in a
	// single context window and the graph adds more friction than value.
	// Aligned in spirit with graphify's 50K-word threshold; gogfy uses a
	// file-count proxy since we don't track per-file word counts.
	const tinyCorpusFileThreshold = 5
	fmt.Fprintf(b, "## Corpus\n- %d files", len(files))
	if len(exts) > 0 {
		es := make([]string, 0, len(exts))
		for e := range exts {
			es = append(es, e)
		}
		sort.Strings(es)
		fmt.Fprintf(b, " across %d language(s): %s", len(exts), strings.Join(es, ", "))
	}
	b.WriteString("\n")
	if len(types) > 0 {
		kinds := make([]schema.FileType, 0, len(types))
		for k := range types {
			kinds = append(kinds, k)
		}
		sort.Slice(kinds, func(i, j int) bool { return string(kinds[i]) < string(kinds[j]) })
		parts := make([]string, 0, len(kinds))
		for _, k := range kinds {
			parts = append(parts, fmt.Sprintf("%d %s", types[k], k))
		}
		fmt.Fprintf(b, "- File types: %s\n", strings.Join(parts, ", "))
	}
	if len(files) < tinyCorpusFileThreshold {
		fmt.Fprintf(b, "- _Tiny corpus (%d files) — likely fits in a single context window; a graph may add more friction than value._\n", len(files))
	}
	b.WriteString("\n")
}

func writeGraphFreshness(b *bytes.Buffer, opts Options) {
	if opts.BuiltAtCommit == "" {
		return
	}
	fmt.Fprintf(b, "## Graph Freshness\n- Built at commit `%s`\n\n", opts.BuiltAtCommit)
}

// writeCommunityHubs picks the highest-degree node per community as the
// hub — the same heuristic the labels package uses, kept aligned so the
// hub and the community label are usually the same node (no drift).
func writeCommunityHubs(b *bytes.Buffer, opts Options, degree map[string]int) {
	if len(opts.Nodes) == 0 || len(opts.Edges) == 0 {
		return
	}
	type hub struct {
		cid    string
		label  string
		degree int
	}
	bestPer := map[string]hub{}
	for _, n := range opts.Nodes {
		if n.Community == "" {
			continue
		}
		d := degree[n.ID]
		cur, seen := bestPer[n.Community]
		if !seen || d > cur.degree || (d == cur.degree && n.Label < cur.label) {
			bestPer[n.Community] = hub{cid: n.Community, label: labelOrID(n), degree: d}
		}
	}
	if len(bestPer) == 0 {
		return
	}
	cids := make([]string, 0, len(bestPer))
	for cid := range bestPer {
		cids = append(cids, cid)
	}
	sort.Strings(cids)
	fmt.Fprintf(b, "## Community Hubs\n")
	for _, cid := range cids {
		h := bestPer[cid]
		fmt.Fprintf(b, "- %s: %s (degree %d)\n", communityName(cid, opts.CommunityLabels), escapeMarkdown(h.label), h.degree)
	}
	b.WriteString("\n")
}

func writeCommunities(b *bytes.Buffer, opts Options) {
	members := map[string][]schema.Node{}
	for _, n := range opts.Nodes {
		if n.Community == "" {
			continue
		}
		members[n.Community] = append(members[n.Community], n)
	}
	if len(members) == 0 {
		return
	}
	type rec struct {
		cid   string
		nodes []schema.Node
	}
	rs := make([]rec, 0, len(members))
	for cid, ns := range members {
		if len(ns) < opts.ThinCommunityMin {
			continue
		}
		rs = append(rs, rec{cid: cid, nodes: ns})
	}
	if len(rs) == 0 {
		return
	}
	sort.Slice(rs, func(i, j int) bool { return rs[i].cid < rs[j].cid })
	fmt.Fprintf(b, "\n## Communities\n")
	for _, r := range rs {
		// Sort members by label so the digest is reproducible.
		ms := append([]schema.Node(nil), r.nodes...)
		sort.Slice(ms, func(i, j int) bool { return labelOrID(ms[i]) < labelOrID(ms[j]) })
		fmt.Fprintf(b, "### %s (%d members)\n", communityName(r.cid, opts.CommunityLabels), len(ms))
		const sample = 5
		for i, n := range ms {
			if i >= sample {
				fmt.Fprintf(b, "- _… %d more_\n", len(ms)-sample)
				break
			}
			fmt.Fprintf(b, "- %s\n", escapeMarkdown(labelOrID(n)))
		}
	}
	b.WriteString("\n")
}

func writeAmbiguousEdges(b *bytes.Buffer, opts Options) {
	nodeMap := indexNodes(opts.Nodes)
	type amb struct{ s, t, r string }
	var ambs []amb
	for _, e := range opts.Edges {
		if e.Confidence != schema.Ambiguous {
			continue
		}
		s := labelOrID(nodeMap[e.Source])
		t := labelOrID(nodeMap[e.Target])
		if s == "" {
			s = e.Source
		}
		if t == "" {
			t = e.Target
		}
		ambs = append(ambs, amb{s, t, e.Relation})
	}
	if len(ambs) == 0 {
		return
	}
	// Deterministic order so reports diff cleanly.
	sort.Slice(ambs, func(i, j int) bool {
		if ambs[i].s != ambs[j].s {
			return ambs[i].s < ambs[j].s
		}
		if ambs[i].t != ambs[j].t {
			return ambs[i].t < ambs[j].t
		}
		return ambs[i].r < ambs[j].r
	})
	fmt.Fprintf(b, "\n## Ambiguous Edges\n")
	const maxAmbig = 25
	for i, a := range ambs {
		if i >= maxAmbig {
			fmt.Fprintf(b, "- _… %d more_\n", len(ambs)-maxAmbig)
			break
		}
		fmt.Fprintf(b, "- %s -> %s (%s)\n", escapeMarkdown(a.s), escapeMarkdown(a.t), escapeMarkdown(a.r))
	}
	b.WriteString("\n")
}

// writeKnowledgeGaps composites three failure signals into one digest so
// a reader can spot extraction problems at a glance: isolated nodes
// (likely extraction misses), thin communities (likely false splits),
// and ambiguous edges (likely heuristic guesses to verify).
func writeKnowledgeGaps(b *bytes.Buffer, r analyze.Report, opts Options, degree map[string]int) {
	if len(opts.Nodes) == 0 && len(opts.Edges) == 0 {
		return
	}
	isolated := 0
	for _, n := range opts.Nodes {
		if degree[n.ID] == 0 {
			isolated++
		}
	}
	members := map[string]int{}
	for _, n := range opts.Nodes {
		if n.Community != "" {
			members[n.Community]++
		}
	}
	thinCount := 0
	for _, c := range members {
		if c > 0 && c < opts.ThinCommunityMin {
			thinCount++
		}
	}
	// Trust the analyze.Report summary when it's populated — a present
	// map with Ambiguous=0 is an authoritative "no ambiguous edges",
	// not missing data. Only re-scan when the summary wasn't computed.
	ambiguous := 0
	if len(r.ConfidenceSummary) > 0 {
		ambiguous = r.ConfidenceSummary[schema.Ambiguous]
	} else {
		for _, e := range opts.Edges {
			if e.Confidence == schema.Ambiguous {
				ambiguous++
			}
		}
	}
	if isolated == 0 && thinCount == 0 && ambiguous == 0 {
		return
	}
	fmt.Fprintf(b, "\n## Knowledge Gaps\n")
	if isolated > 0 {
		fmt.Fprintf(b, "- %d isolated node(s) — likely extraction miss\n", isolated)
	}
	if thinCount > 0 {
		fmt.Fprintf(b, "- %d thin community(s) under %d members — likely false split\n", thinCount, opts.ThinCommunityMin)
	}
	if ambiguous > 0 {
		fmt.Fprintf(b, "- %d ambiguous edge(s) awaiting verification\n", ambiguous)
	}
	b.WriteString("\n")
}

func communityName(cid string, lbls map[string]string) string {
	if name, ok := lbls[cid]; ok && name != "" {
		return name
	}
	return "Community " + cid
}

func buildDegree(edges []schema.Edge) map[string]int {
	d := make(map[string]int, len(edges)*2)
	for _, e := range edges {
		d[e.Source]++
		d[e.Target]++
	}
	return d
}

func indexNodes(ns []schema.Node) map[string]schema.Node {
	m := make(map[string]schema.Node, len(ns))
	for _, n := range ns {
		m[n.ID] = n
	}
	return m
}

func labelOrID(n schema.Node) string {
	if n.Label != "" {
		return n.Label
	}
	return n.ID
}

func escapeMarkdown(s string) string {
	s = strings.ReplaceAll(s, "*", "\\*")
	s = strings.ReplaceAll(s, "_", "\\_")
	return s
}

