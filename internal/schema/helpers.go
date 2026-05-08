// Package schema defines the core data types for the gogfy graph model.
package schema

import (
	"fmt"
	"sort"
	"strings"
)

// FormatLocation formats a tree-sitter row:col (zero-based) into a 1-based "row:col" string.
func FormatLocation(row, col uint) string {
	return fmt.Sprintf("%d:%d", row+1, col+1)
}

// LangID composes a deterministic node ID under a language prefix. Used by
// per-language extractors that share a common scheme: "<lang>:<kind>:<key>".
func LangID(lang, kind, key string) string {
	return fmt.Sprintf("%s:%s:%s", lang, kind, key)
}

// ParseLangID is the inverse of LangID. Returns (lang, kind, key, true) when
// id has the "<lang>:<kind>:<key>" shape, otherwise ("", "", "", false). The
// key may itself contain colons (e.g., Rust's `std::collections::HashMap`)
// since only the first two `:` are split on.
func ParseLangID(id string) (lang, kind, key string, ok bool) {
	first := strings.IndexByte(id, ':')
	if first < 0 {
		return "", "", "", false
	}
	rest := id[first+1:]
	second := strings.IndexByte(rest, ':')
	if second < 0 {
		return "", "", "", false
	}
	return id[:first], rest[:second], rest[second+1:], true
}

// SortNodesByID sorts nodes in-place by ID for deterministic output.
func SortNodesByID(nodes []Node) {
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].ID < nodes[j].ID
	})
}
