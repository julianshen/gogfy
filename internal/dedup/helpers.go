// Package dedup implements three-pass entity deduplication for graph nodes.
package dedup

import (
	"math"
	"regexp"
	"strings"

	"github.com/julianshen/gogfy/internal/schema"
)

var nonAlphaNum = regexp.MustCompile(`[^a-z0-9]+`)

// normalize collapses a label to lowercase alphanumeric with single-space separators.
func normalize(label string) string {
	s := nonAlphaNum.ReplaceAllString(strings.ToLower(label), " ")
	return strings.TrimSpace(s)
}

// nodeKind extracts the kind segment of a `<lang>:<kind>:<key>` node
// ID (e.g. "module", "type", "function", "method"). Returns "" when
// the ID doesn't parse.
func nodeKind(id string) string {
	_, kind, _, ok := schema.ParseLangID(id)
	if !ok {
		return ""
	}
	return kind
}

// structuralKinds are container / synthetic node kinds that must only
// merge within their own kind — never with a named code/concept
// entity. A Go `package graph` (module, label "graph") and its
// `type Graph` (label "Graph") both normalize to "graph"; without
// this isolation pass-1 would merge the type INTO the module,
// collapsing the type and destroying every `type contains method`
// ownership edge. `package foo` + `type Foo` is ubiquitous in Go.
var structuralKinds = map[string]bool{
	"module":  true,
	"section": true,
	"import":  true,
	"call":    true,
	"typeref": true,
	"link":    true,
	"key":     true,
}

// mergeBucket groups node kinds for dedup. Each structural kind is its
// own bucket (kind-scoped — no cross-kind merge). All remaining
// "entity" kinds (function, method, type, class, field, const, var,
// and the semantic-extraction `entity`) share one bucket so the
// intended cross-source dedup still fires — an AST `function:Auth`
// and a semantic `entity:Auth` are the same real thing and should
// merge (code wins via pickWinner's FileType rank). The container
// kinds stay isolated so the harmful module↔type collapse can't happen.
func mergeBucket(id string) string {
	k := nodeKind(id)
	if structuralKinds[k] {
		return "k:" + k
	}
	return "entity"
}

// entropy computes Shannon entropy in bits per character.
func entropy(s string) float64 {
	if len(s) == 0 {
		return 0
	}
	freq := make(map[rune]int, len(s))
	for _, r := range s {
		freq[r]++
	}
	var h float64
	n := float64(len(s))
	for _, count := range freq {
		p := float64(count) / n
		h -= p * math.Log2(p)
	}
	return h
}

// shingles returns character k-grams for the given text.
// Spaces are stripped before shingling to match upstream behavior.
func shingles(text string, k int) []string {
	text = strings.ReplaceAll(text, " ", "")
	if len(text) < k {
		return []string{text}
	}
	seen := make(map[string]struct{})
	var result []string
	for i := 0; i <= len(text)-k; i++ {
		s := text[i : i+k]
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			result = append(result, s)
		}
	}
	return result
}

var chunkSuffix = regexp.MustCompile(`_c\d+$`)

// pickWinner selects the best representative from a group of duplicate nodes.
// Preference: no chunk suffix, then shorter ID.
func pickWinner(nodes []schema.Node) schema.Node {
	if len(nodes) == 0 {
		return schema.Node{}
	}
	winner := nodes[0]
	ws := winnerScore(winner)
	for i := 1; i < len(nodes); i++ {
		s := winnerScore(nodes[i])
		if s.less(ws) {
			ws = s
			winner = nodes[i]
		}
	}
	return winner
}

// fileTypeRank biases pickWinner toward AST-grounded nodes when a
// fuzzy merge pulls in a semantic-extracted one. The authoritative
// IDs are code-derived (gogfy_<lang>:function:...) — they're stable
// across runs and grounded in real source positions, so they should
// outlive an LLM-emitted "doc:entity:..." duplicate when the labels
// fuzzy-match. Lower rank wins.
var fileTypeRank = map[schema.FileType]int{
	schema.FileTypeCode:     0,
	schema.FileTypePaper:    1,
	schema.FileTypeDocument: 2,
	schema.FileTypeImage:    3,
	schema.FileTypeVideo:    4,
	"rationale":             5,
}

const fileTypeRankUnknown = 6

// winnerScore returns a tuple-like score where lower is better.
// Ordering: (fileTypeRank, hasChunkSuffix, len(ID)).
// FileType-rank dominates so code beats document on a fuzzy merge;
// within the same FileType the chunk-suffix and ID-length rules
// preserve the prior tie-break shape.
func winnerScore(n schema.Node) score {
	rank, ok := fileTypeRank[n.FileType]
	if !ok {
		rank = fileTypeRankUnknown
	}
	hasSuffix := 0
	if chunkSuffix.MatchString(n.ID) {
		hasSuffix = 1
	}
	return score{rank, hasSuffix, len(n.ID)}
}

// score is a tuple for lexicographic comparison.
type score struct {
	fileTypeRank int
	hasSuffix    int
	idLen     int
}

// less returns true if s is better (lower) than other.
// Ordering: FileType-rank first so AST-grounded nodes (Code) outrank
// LLM-emitted ones (Document); then chunk-suffix; then ID length.
func (s score) less(other score) bool {
	if s.fileTypeRank != other.fileTypeRank {
		return s.fileTypeRank < other.fileTypeRank
	}
	if s.hasSuffix != other.hasSuffix {
		return s.hasSuffix < other.hasSuffix
	}
	return s.idLen < other.idLen
}
