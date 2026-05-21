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
		bare := lastImportSegment(tkey)
		if bare == "" {
			continue
		}
		if out[key] == nil {
			out[key] = map[string]struct{}{}
		}
		// Two entries per import: the bare name gates `imports[calledName]`
		// so call narrowing fires, and the dotted/sliced root lets the
		// candidate file-stem match (auth.py for `from auth import login`).
		out[key][bare] = struct{}{}
		if root := firstImportSegment(tkey); root != "" && root != bare {
			out[key][root] = struct{}{}
		}
	}
	return out
}

// lastImportSegment extracts the user-facing name from an import-target
// string. Handles three conventions:
//
//   - Python / Java dotted: `auth.login`, `com.example.Foo` → last
//     dot-separated segment (`login`, `Foo`).
//   - Go path-style: `github.com/foo/bar` → last slash-separated
//     segment (`bar`), which is what unaliased Go imports bind as
//     the package name at call sites.
//   - Mixed (`gopkg.in/yaml.v2`): the slash wins over the dot —
//     callers actually write `yaml.Marshal(...)`, not `v2.Marshal(...)`,
//     because Go's package name is `yaml` regardless of the major
//     version suffix in the path. So we slash-split first, then
//     dot-split the trailing segment and drop the suffix.
func lastImportSegment(s string) string {
	if i := strings.LastIndexByte(s, '/'); i >= 0 {
		s = s[i+1:]
	}
	if i := strings.LastIndexByte(s, '.'); i >= 0 {
		// `yaml.v2` → `yaml` (Go-style versioned-package convention).
		// `auth.login` → `login` (Python attribute access convention).
		// Disambiguate: if the suffix looks like a major-version tag
		// (`v\d+`), strip it; otherwise keep the suffix as the bare
		// name.
		tail := s[i+1:]
		if isVersionTag(tail) {
			s = s[:i]
		} else {
			s = tail
		}
	}
	return s
}

// firstImportSegment returns the leftmost segment under the same
// rules — useful for file-stem narrowing where the import target
// shape is `dir/file.py` style and the matchable hint is `dir`.
func firstImportSegment(s string) string {
	if i := strings.IndexByte(s, '/'); i > 0 {
		return s[:i]
	}
	if i := strings.IndexByte(s, '.'); i > 0 {
		return s[:i]
	}
	return ""
}

// isVersionTag matches Go's `vN` major-version path suffix
// convention (`yaml.v2`, `mongo.v3`). Anything else stays as the
// bare-name candidate.
func isVersionTag(s string) bool {
	if len(s) < 2 || s[0] != 'v' {
		return false
	}
	for i := 1; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
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

// PruneOrphanCallTargets removes synthetic `<lang>:call:<name>` nodes
// that no edge references. Calls() already prunes these once, but a
// later pass — notably dedup, which remaps and de-duplicates edge
// endpoints — can drop the last edge pointing at a call target,
// re-orphaning it. Run this after dedup to keep unresolved-call
// placeholders from lingering as zero-degree noise nodes (which the
// clusterer would otherwise isolate into singleton communities).
//
// Only synthetic call targets are touched; every real node is kept
// regardless of degree (an isolated function is still a fact about
// the corpus).
func PruneOrphanCallTargets(nodes []schema.Node, edges []schema.Edge) []schema.Node {
	referenced := make(map[string]bool, len(edges)*2)
	for _, e := range edges {
		referenced[e.Source] = true
		referenced[e.Target] = true
	}
	out := make([]schema.Node, 0, len(nodes))
	for _, n := range nodes {
		if isSyntheticCallTarget(n.ID) && !referenced[n.ID] {
			continue
		}
		out = append(out, n)
	}
	return out
}

// MethodOwnership resolves the synthetic `method_of` edges emitted by
// the Go extractor into real `type contains method` edges. Each
// method_of edge points from a method node to a
// `<lang>:typeref:<ReceiverName>` placeholder; this pass rewrites it
// to a `contains` edge from the actual type node declared in the same
// package directory as the method.
//
// Package = directory (Go's model), so this handles both same-file
// and cross-file-same-package receivers — the gap finalize()-based
// per-file linking couldn't close. A typeref that resolves to no
// in-corpus type (receiver from a dependency, or an embedded builtin)
// is dropped along with its edge, leaving no dangling reference. The
// synthetic typeref nodes are pruned once unreferenced.
//
// Mirrors Calls: emit-intent-during-extraction, resolve-against-
// global-view here.
func MethodOwnership(nodes []schema.Node, edges []schema.Edge) ([]schema.Node, []schema.Edge) {
	// Index type nodes by (lang, dir, typeName).
	type typeKey struct{ lang, dir, name string }
	typeByKey := map[typeKey]string{}
	fileByID := buildFileByID(nodes)
	for _, n := range nodes {
		lang, kind, _, ok := schema.ParseLangID(n.ID)
		if !ok || kind != "type" {
			continue
		}
		typeByKey[typeKey{lang, filepath.Dir(n.SourceFile), n.Label}] = n.ID
	}

	out := make([]schema.Edge, 0, len(edges))
	referenced := map[string]bool{}
	for _, e := range edges {
		lang, recv, isMO := parseTypeRef(e.Target)
		if e.Relation != "method_of" || !isMO {
			out = append(out, e)
			referenced[e.Target] = true
			continue
		}
		methodDir := filepath.Dir(fileByID[e.Source])
		typeID, ok := typeByKey[typeKey{lang, methodDir, recv}]
		if !ok {
			// Receiver type not in this package's extracted files —
			// drop the method_of edge (and let the synthetic typeref
			// be pruned). The method keeps its module contains edge.
			continue
		}
		out = append(out, schema.Edge{
			Source:     typeID,
			Target:     e.Source, // type contains method
			Relation:   "contains",
			Confidence: schema.Extracted,
		})
	}

	// Prune synthetic typeref nodes that no surviving edge references.
	pruned := make([]schema.Node, 0, len(nodes))
	for _, n := range nodes {
		if isTypeRef(n.ID) && !referenced[n.ID] {
			continue
		}
		pruned = append(pruned, n)
	}
	return pruned, out
}

// inheritanceRelations are the subtype→base edge labels addInherits
// emits. resolve.Inheritance rewrites their synthetic typeref targets to
// real base type/class nodes.
var inheritanceRelations = map[string]bool{
	"inherits":   true,
	"implements": true,
	"embeds":     true,
}

// typeLikeKinds are the declaration kinds a base type can resolve to,
// across languages: Go `type`, OOP `class`/`interface`, Rust
// `struct`/`enum`/`trait`, Java `enum`, Swift `protocol`, etc.
var typeLikeKinds = map[string]bool{
	"type": true, "class": true, "interface": true, "struct": true,
	"enum": true, "trait": true, "protocol": true, "object": true,
	"record": true, "mixin": true,
}

// Inheritance resolves the synthetic inheritance edges (`inherits`,
// `implements`, `embeds`) emitted by the extractors. Each points from a
// subtype node to a `<lang>:typeref:<BaseName>` placeholder; this pass
// rewrites the target to the real base type/class node when one exists
// in the corpus.
//
// Unlike MethodOwnership (directory-scoped, drop-if-unresolved), base
// types routinely live in other packages, so resolution mirrors Calls:
// a global (lang, name) index over `type` AND `class` nodes, narrowed by
// the subtype file's import scope when several same-named bases exist.
//
//   - 1 candidate  → target rewritten to the base ID, Confidence INFERRED.
//   - N candidates → fanned out to one AMBIGUOUS edge per base (the
//     synthetic typeref is kept as an anchor for the observed name).
//   - 0 candidates → edge is left pointing at the typeref. An external
//     base (a framework class like `Exception`) is a real "extends this"
//     fact worth keeping, so the typeref survives as an observed node.
//
// Run AFTER MethodOwnership: that pass passes inheritance edges through
// untouched (marking their typeref targets referenced, so it won't prune
// them), and by the time this runs every method-receiver typeref has
// already been resolved or pruned — leaving only inheritance typerefs here.
func Inheritance(nodes []schema.Node, edges []schema.Edge) ([]schema.Node, []schema.Edge) {
	// Index all type-like declaration nodes globally by (lang, name).
	idx := map[langLabel][]string{}
	for _, n := range nodes {
		lang, kind, _, ok := schema.ParseLangID(n.ID)
		if !ok || !typeLikeKinds[kind] {
			continue
		}
		key := langLabel{lang, n.Label}
		idx[key] = append(idx[key], n.ID)
	}
	for k := range idx {
		sort.Strings(idx[k])
	}

	importScope := buildImportScope(edges)
	fileByID := buildFileByID(nodes)

	out := make([]schema.Edge, 0, len(edges))
	referenced := make(map[string]bool, len(nodes))
	for _, e := range edges {
		lang, base, isRef := parseTypeRef(e.Target)
		if !inheritanceRelations[e.Relation] || !isRef {
			out = append(out, e)
			referenced[e.Target] = true
			continue
		}
		// Resolve a typeref SOURCE too (Rust `impl Trait for Foo` names
		// both ends). Unique match → rewrite source to the real type;
		// otherwise keep the typeref source (and mark it referenced so it
		// survives pruning).
		if srcLang, srcName, srcIsRef := parseTypeRef(e.Source); srcIsRef {
			if c := idx[langLabel{srcLang, srcName}]; len(c) == 1 {
				e.Source = c[0]
			} else {
				referenced[e.Source] = true
			}
		}
		candidates := idx[langLabel{lang, base}]
		if len(candidates) > 1 {
			if narrowed := narrowByImportScope(candidates, fileByID, importScope, fileByID[e.Source], base); len(narrowed) > 0 {
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
			for _, cand := range candidates {
				out = append(out, schema.Edge{
					Source:     e.Source,
					Target:     cand,
					Relation:   e.Relation,
					Confidence: schema.Ambiguous,
				})
				referenced[cand] = true
			}
			referenced[e.Target] = true // keep anchor
		}
	}

	pruned := make([]schema.Node, 0, len(nodes))
	for _, n := range nodes {
		if isTypeRef(n.ID) && !referenced[n.ID] {
			continue
		}
		pruned = append(pruned, n)
	}
	return pruned, out
}

// parseTypeRef extracts (lang, receiverName) from a synthetic
// `<lang>:typeref:<name>` node ID.
func parseTypeRef(id string) (lang, name string, ok bool) {
	l, kind, n, ok := schema.ParseLangID(id)
	if !ok || kind != "typeref" {
		return "", "", false
	}
	return l, n, true
}

// isTypeRef reports whether id is a synthetic receiver-type-ref node.
func isTypeRef(id string) bool {
	_, _, ok := parseTypeRef(id)
	return ok
}

