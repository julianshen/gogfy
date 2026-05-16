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
	"path/filepath"
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
//     one per candidate (sorted for deterministic output). The synthetic
//     call node is preserved as an anchor for the original-callee identity
//     so the edge expansion doesn't erase the "we observed this name" fact.
//
// Synthetic call-target nodes that no longer have any incoming edges (i.e.,
// the only edge pointing at them got upgraded to a real function ID) are
// pruned from the node list.
func Calls(nodes []schema.Node, edges []schema.Edge) ([]schema.Node, []schema.Edge) {
	idx := buildFunctionIndex(nodes)
	importScope := buildImportScope(edges)
	// Precomputed once: per-edge narrowing would otherwise rebuild this
	// map O(G) times for a G-edge graph.
	fileByID := buildFileByID(nodes)

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
		// Narrow by import scope: if the caller's file imports a module
		// matching one (and only one) of the candidates' source files,
		// prefer that candidate. Avoids fanning out to every module that
		// happens to define a same-named function.
		if len(candidates) > 1 {
			if narrowed := narrowByImportScope(candidates, fileByID, importScope, callerFile(e.Source), name); len(narrowed) > 0 {
				candidates = narrowed
			}
		}
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

// buildImportScope returns filepath → set of bare names the file's
// `imports` edges bring into scope. Extractors emit one import edge per
// imported name with target shaped as either the module ("auth") or
// "module.name" ("auth.login"); the trailing dotted segment is the name
// a call expression would reference (extractors normalize `auth.foo()`
// to a bare `foo` callee, so we don't track dotted forms).
//
// Known limitation: aliased imports (`from M import N as A; A()`) bind
// the alias `A` at the call site, but extractors record only the
// original name `N` in the import edge — the called name `A` therefore
// won't match the scope, and narrowing is skipped.
func buildImportScope(edges []schema.Edge) map[string]map[string]struct{} {
	out := map[string]map[string]struct{}{}
	for _, e := range edges {
		if e.Relation != "imports" {
			continue
		}
		_, kind, key, ok := schema.ParseLangID(e.Source)
		if !ok || kind != "module" {
			continue
		}
		_, tk, tkey, ok := schema.ParseLangID(e.Target)
		if !ok || tk != "import" {
			continue
		}
		bare := tkey
		if i := strings.LastIndexByte(bare, '.'); i >= 0 {
			bare = bare[i+1:]
		}
		if bare == "" {
			continue
		}
		if out[key] == nil {
			out[key] = map[string]struct{}{}
		}
		// Two entries per import: the bare name gates `imports[calledName]`
		// so call narrowing fires, and the dotted root lets the candidate
		// file-stem match (auth.py for `from auth import login`).
		out[key][bare] = struct{}{}
		if i := strings.IndexByte(tkey, '.'); i > 0 {
			out[key][tkey[:i]] = struct{}{}
		}
	}
	return out
}

// callerFile extracts the source file path from a function/method node
// ID of the form `<lang>:<kind>:<filepath>:<scope>:<name>`. Returns "" if
// the ID isn't shaped that way.
func callerFile(id string) string {
	_, kind, key, ok := schema.ParseLangID(id)
	if !ok {
		return ""
	}
	if kind != "function" && kind != "method" {
		return ""
	}
	// key is `<filepath>:<scope>:<name>` — first colon splits the file.
	i := strings.IndexByte(key, ':')
	if i < 0 {
		return key
	}
	return key[:i]
}

// buildFileByID indexes nodes that carry a SourceFile so narrowByImportScope
// can resolve a candidate function node's source file in O(1). Pulled
// out of the inner loop — the per-edge rebuild was O(G·N).
func buildFileByID(nodes []schema.Node) map[string]string {
	out := make(map[string]string, len(nodes))
	for _, n := range nodes {
		if n.SourceFile != "" {
			out[n.ID] = n.SourceFile
		}
	}
	return out
}

// narrowByImportScope returns the subset of candidates whose source file
// stem matches an import bound at callerPath. Conservative: returns nil
// (deferring to outer AMBIGUOUS behavior) whenever the caller has no
// import scope or the called name isn't itself imported — we never
// invent INFERRED edges from missing data.
func narrowByImportScope(candidates []string, fileByID map[string]string, scope map[string]map[string]struct{}, callerPath, calledName string) []string {
	if callerPath == "" {
		return nil
	}
	imports, ok := scope[callerPath]
	if !ok || len(imports) == 0 {
		return nil
	}
	if _, named := imports[calledName]; !named {
		return nil
	}
	var narrowed []string
	for _, cand := range candidates {
		path := fileByID[cand]
		if path == "" {
			continue
		}
		stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		if _, hit := imports[stem]; hit {
			narrowed = append(narrowed, cand)
		}
	}
	return narrowed
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

// functionNodeLang returns the language prefix and true if id refers to a
// function or method node, otherwise ("", false). All extractors now use
// the shared "<lang>:function:..." / "<lang>:method:..." LangID scheme so
// no per-language special cases are needed.
func functionNodeLang(id string) (string, bool) {
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

