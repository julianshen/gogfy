// Package resolve upgrades call edges from synthetic "<lang>:call:<name>"
// targets to real function nodes when the project contains a function whose
// label matches. Resolved edges are marked INFERRED (or AMBIGUOUS when
// multiple candidates exist), implementing SPEC §2's typed confidence model
// for cross-file calls.
//
// Known limitations:
//
//   - Method calls lose their receiver context upstream (extractors strip
//     `obj.foo()` to a `<lang>:call:foo` target). When multiple types in the
//     project define a method named `foo`, the resolver fans out to all of
//     them as AMBIGUOUS — receiver-aware resolution would need type
//     inference, which is out of scope for AST-only extraction.
//   - Resolution is language-scoped: `go:call:foo` won't match a Python
//     `foo` even if a project mixes both languages. Cross-language calls
//     (FFI, RPC, subprocess invocations) stay EXTRACTED with the synthetic
//     target preserved.
package resolve

import (
	"sort"

	"github.com/julianshen/gogfy/internal/schema"
)

// Calls returns a new (nodes, edges) pair where every "calls" edge whose
// target matches `<lang>:call:<name>` and which has resolvable function
// candidates has been upgraded:
//
//   - 0 candidates → edge is unchanged (target remains the synthetic call node).
//   - 1 candidate  → edge's target is rewritten to the function's ID, and
//     Confidence is set to INFERRED.
//   - N candidates → the original edge is replaced with N AMBIGUOUS edges,
//     one per candidate (sorted for deterministic output). The synthetic
//     call node is preserved as an anchor for the original-callee identity
//     so the edge expansion doesn't erase the "we observed this name" fact.
//
// Synthetic call-target nodes that no longer have any incoming edges (i.e.,
// the only edge pointing at them got upgraded to a real function ID) are
// pruned from the node list.
func Calls(nodes []schema.Node, edges []schema.Edge) ([]schema.Node, []schema.Edge) {
	idx := buildFunctionIndex(nodes)

	out := make([]schema.Edge, 0, len(edges))
	referenced := make(map[string]bool, len(nodes))
	for _, e := range edges {
		if e.Relation != "calls" {
			out = append(out, e)
			referenced[e.Target] = true
			continue
		}
		lang, name, ok := parseCallTarget(e.Target)
		if !ok {
			out = append(out, e)
			referenced[e.Target] = true
			continue
		}
		candidates := idx[langLabel{lang, name}]
		switch len(candidates) {
		case 0:
			out = append(out, e)
			referenced[e.Target] = true
		case 1:
			upgraded := e
			upgraded.Target = candidates[0]
			upgraded.Confidence = schema.Inferred
			out = append(out, upgraded)
			referenced[upgraded.Target] = true
		default:
			// candidates was sorted once at index-build time, so the
			// emitted edges are already deterministic without per-edge
			// re-sorting.
			for _, cand := range candidates {
				out = append(out, schema.Edge{
					Source:     e.Source,
					Target:     cand,
					Relation:   e.Relation,
					Confidence: schema.Ambiguous,
				})
				referenced[cand] = true
			}
			// Anchor: keep the synthetic call: node alive so the original
			// callee identity is still discoverable in the graph.
			referenced[e.Target] = true
		}
	}

	prunedNodes := make([]schema.Node, 0, len(nodes))
	for _, n := range nodes {
		if isSyntheticCallTarget(n.ID) && !referenced[n.ID] {
			continue
		}
		prunedNodes = append(prunedNodes, n)
	}
	return prunedNodes, out
}

type langLabel struct{ lang, label string }

// buildFunctionIndex maps (lang, label) → [function-node IDs] across the
// graph so the resolver can find call candidates by name. Each bucket is
// sorted once here so the AMBIGUOUS edge expansion in Calls doesn't need
// to re-sort per call site.
func buildFunctionIndex(nodes []schema.Node) map[langLabel][]string {
	idx := map[langLabel][]string{}
	for _, n := range nodes {
		lang, ok := functionNodeLang(n.ID)
		if !ok {
			continue
		}
		key := langLabel{lang, n.Label}
		idx[key] = append(idx[key], n.ID)
	}
	for k := range idx {
		sort.Strings(idx[k])
	}
	return idx
}

// Legacy ID-prefix constants for the Go and Python extractors, which predate
// the shared LangID scheme. Centralizing them here means a scheme change
// breaks compilation rather than silently mis-resolving in functionNodeLang.
const (
	legacyGoFnPrefix = "fn:"
	legacyPyFnPrefix = "py:fn:"
)

// functionNodeLang returns the language prefix and true if id refers to a
// function or method node, otherwise ("", false). Handles the legacy Go and
// Python ID schemes plus the shared "<lang>:function:..." / "<lang>:method:..."
// form (every other language).
func functionNodeLang(id string) (string, bool) {
	switch {
	case hasPrefix(id, legacyPyFnPrefix):
		return "py", true
	case hasPrefix(id, legacyGoFnPrefix):
		return "go", true
	}
	lang, kind, _, ok := schema.ParseLangID(id)
	if !ok {
		return "", false
	}
	if kind == "function" || kind == "method" {
		return lang, true
	}
	return "", false
}

// parseCallTarget splits a "<lang>:call:<name>" ID into its parts. Returns
// ok=false for any other shape.
func parseCallTarget(id string) (lang, name string, ok bool) {
	lang, kind, name, ok := schema.ParseLangID(id)
	if !ok || kind != "call" {
		return "", "", false
	}
	return lang, name, true
}

// isSyntheticCallTarget reports whether id is a synthetic call-target node
// (eligible for pruning when no edges reference it).
func isSyntheticCallTarget(id string) bool {
	_, _, ok := parseCallTarget(id)
	return ok
}

// hasPrefix is a tiny helper so functionNodeLang's prefix table reads cleanly
// and we don't pull strings just for that one call site.
func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
