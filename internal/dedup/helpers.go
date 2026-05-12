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

// winnerScore returns a tuple-like score where lower is better.
// (hasChunkSuffix, len(ID)) — no suffix < suffix, shorter < longer.
func winnerScore(n schema.Node) score {
	hasSuffix := 0
	if chunkSuffix.MatchString(n.ID) {
		hasSuffix = 1
	}
	return score{hasSuffix, len(n.ID)}
}

// score is a tuple for lexicographic comparison.
type score struct {
	hasSuffix int
	idLen     int
}

// less returns true if s is better (lower) than other.
func (s score) less(other score) bool {
	if s.hasSuffix != other.hasSuffix {
		return s.hasSuffix < other.hasSuffix
	}
	return s.idLen < other.idLen
}
