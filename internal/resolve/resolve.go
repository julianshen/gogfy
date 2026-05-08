// Package resolve upgrades call edges from synthetic "<lang>:call:<name>"
// targets to real function nodes when the project contains a function whose
// label matches. Resolved edges are marked INFERRED (or AMBIGUOUS when
// multiple candidates exist), implementing SPEC §2's typed confidence model
// for cross-file calls.
package resolve

import (
	"sort"
	"strings"

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
//     one per candidate. The synthetic call node is preserved so the
//     original-callee identity is still discoverable.
//
// Synthetic call-target nodes that no longer have any incoming edges are
// pruned from the node list.
func Calls(nodes []schema.Node, edges []schema.Edge) ([]schema.Node, []schema.Edge) {
	idx := buildFunctionIndex(nodes)

	out := make([]schema.Edge, 0, len(edges))
	referenced := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		_ = n
	}
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
			// Sort for determinism so AMBIGUOUS edges appear in stable order.
			sorted := append([]string(nil), candidates...)
			sort.Strings(sorted)
			for _, cand := range sorted {
				out = append(out, schema.Edge{
					Source:     e.Source,
					Target:     cand,
					Relation:   e.Relation,
					Confidence: schema.Ambiguous,
				})
				referenced[cand] = true
			}
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
// graph so the resolver can find call candidates by name.
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
	return idx
}

// functionNodeLang returns the language prefix and true if id refers to a
// function or method node, otherwise ("", false). Handles the legacy Go and
// Python ID schemes (which predate LangID) plus the shared
// "<lang>:function:..." / "<lang>:method:..." form.
func functionNodeLang(id string) (string, bool) {
	switch {
	case strings.HasPrefix(id, "fn:"):
		return "go", true
	case strings.HasPrefix(id, "py:fn:"):
		return "py", true
	}
	parts := strings.SplitN(id, ":", 3)
	if len(parts) < 3 {
		return "", false
	}
	if parts[1] == "function" || parts[1] == "method" {
		return parts[0], true
	}
	return "", false
}

// parseCallTarget splits a "<lang>:call:<name>" ID into its parts. Returns
// ok=false for any other shape so the resolver can leave it alone.
func parseCallTarget(id string) (lang, name string, ok bool) {
	parts := strings.SplitN(id, ":", 3)
	if len(parts) != 3 || parts[1] != "call" {
		return "", "", false
	}
	return parts[0], parts[2], true
}

// isSyntheticCallTarget reports whether id is a synthetic call-target node
// (eligible for pruning when no edges reference it).
func isSyntheticCallTarget(id string) bool {
	_, _, ok := parseCallTarget(id)
	return ok
}
