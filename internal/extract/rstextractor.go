package extract

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/julianshen/gogfy/internal/schema"
)

// RSTExtractor handles reStructuredText (.rst) — the format used by
// Sphinx and most Python-project docs. We don't depend on docutils
// (Python); instead we run heuristics over the raw text:
//
//   - Section headings: a non-blank line followed by a line of the same
//     length composed of one of the canonical RST adornment characters
//     (= - ~ ^ ' " * + # : .). The line above the underline is the
//     heading text. Levels are inferred by adornment-character order
//     of first appearance, but we only emit nodes for the first three
//     levels (matches Markdown/HTML extractors).
//   - References: inline target form `\`text <url>\`_` plus bare URLs
//     in prose. Same `references` relation as Markdown/HTML so the
//     three formats compose into one cross-format graph.
//
// This is a heuristic, not a full parser. A doc that lays out tables
// or directives in unusual ways won't fully extract — that's OK for
// a first cut, since the goal is "get the prominent headings and
// links into the graph" not "full RST fidelity."
type RSTExtractor struct{}

func (RSTExtractor) Extract(path string) (Result, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Result{}, err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return Result{}, err
	}
	moduleID := schema.LangID("rst", "module", abs)
	state := &extractState{lang: "rst", filePath: abs}
	state.nodes = append(state.nodes, schema.Node{
		ID:    moduleID,
		Label: filepath.Base(abs),
	})
	state.fnStack = append(state.fnStack, moduleID)

	lines := strings.Split(string(data), "\n")
	headingLevels := map[byte]int{} // adornment char → level (1, 2, 3, …)
	nextLevel := 1
	for i := 0; i+1 < len(lines); i++ {
		title := strings.TrimRight(lines[i], " \t")
		under := strings.TrimRight(lines[i+1], " \t")
		if title == "" || under == "" || len(title) != len(under) {
			continue
		}
		if !isRSTAdornmentLine(under) {
			continue
		}
		ch := under[0]
		level, ok := headingLevels[ch]
		if !ok {
			level = nextLevel
			headingLevels[ch] = level
			nextLevel++
		}
		if level >= 1 && level <= 3 {
			id := nextSectionID(state, "rst", abs, title)
			state.nodes = append(state.nodes, schema.Node{
				ID:         id,
				Label:      title,
				SourceFile: abs,
			})
			state.emitContainsFromModule(id)
			// Reset to module + push this section so links inside source
			// from here.
			state.fnStack = []string{moduleID, id}
		}
		// First line was the title; skip the underline on the next iter.
		i++
		// Track first-H1 fallback for the doc label. The very first
		// adornment-char encountered defines level 1; if its title hasn't
		// been used as the module label yet, adopt it.
		if level == 1 && state.nodes[0].Label == filepath.Base(abs) {
			state.nodes[0].Label = title
		}
	}

	// Inline-target form: `text <url>`_
	for _, m := range rstInlineTargetRE.FindAllStringSubmatch(string(data), -1) {
		state.edges = append(state.edges, schema.Edge{
			Source:     state.fnStack[len(state.fnStack)-1],
			Target:     schema.LangID("rst", "link", m[1]),
			Relation:   "references",
			Confidence: schema.Extracted,
		})
	}
	// Bare URLs in prose (deduped against inline-target hits via the
	// same target string — addEdge is idempotent at the (source, target,
	// relation, confidence) tuple level via the resolver, but we use a
	// local set to keep emitted edges minimal here).
	seen := map[string]bool{}
	for _, e := range state.edges {
		if e.Relation == "references" {
			seen[e.Target] = true
		}
	}
	for _, u := range extractURLs(string(data)) {
		target := schema.LangID("rst", "link", u)
		if seen[target] {
			continue
		}
		seen[target] = true
		state.edges = append(state.edges, schema.Edge{
			Source:     moduleID, // module-attributed since we don't track which section the bare URL was inside
			Target:     target,
			Relation:   "references",
			Confidence: schema.Extracted,
		})
	}
	return Result{Nodes: state.nodes, Edges: state.edges}, nil
}

// rstAdornmentChars are the RST-canonical characters that can underline
// (or overline) a section heading. Per the spec any printable
// non-alphanumeric ASCII character can be used; in practice these eight
// cover essentially all real-world docs.
const rstAdornmentChars = "=-~^'\"*+#:."

func isRSTAdornmentLine(s string) bool {
	if s == "" {
		return false
	}
	ch := s[0]
	if !strings.ContainsRune(rstAdornmentChars, rune(ch)) {
		return false
	}
	for i := 1; i < len(s); i++ {
		if s[i] != ch {
			return false
		}
	}
	return true
}

// rstInlineTargetRE matches `text <url>`_ (single-underscore form) and
// `text <url>`__ (anonymous form). The URL is captured; the link text
// is discarded since we don't model anchor labels.
var rstInlineTargetRE = regexp.MustCompile("`[^`]*?<(https?://[^>]+)>`__?")
