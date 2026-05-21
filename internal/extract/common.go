package extract

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/julianshen/gogfy/internal/schema"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// trimQuotes strips leading/trailing string-literal delimiters that
// tree-sitter includes verbatim in a node's text. Covers single quotes,
// double quotes, and backticks — the three delimiters that wrap string
// literals in nearly every grammar. Idempotent on already-bare strings.
//
// Intentionally does NOT trim angle brackets: those appear in TypeScript
// generics, HTML/JSX content, and arbitrary path strings, where stripping
// them would mangle the value. Callers that need angle-bracket handling
// (e.g., C `#include <foo.h>`) should do that strip explicitly.
func trimQuotes(s string) string {
	return strings.Trim(s, `"'`+"`")
}

// nextSectionID mints a section LangID using the per-Extract sectionSeq
// counter so two sections with identical labels in the same document get
// distinct IDs (graph.Builder.AddNode keeps the first node per ID, so a
// label-only key would silently fold later sections — and their inbound
// edges — into the first). Centralizing the format here keeps the four
// document extractors from drifting.
func nextSectionID(state *extractState, lang, path, label string) string {
	state.sectionSeq++
	return schema.LangID(lang, "section",
		fmt.Sprintf("%s:n%d:%s", path, state.sectionSeq, slugify(label)))
}

// extractState is the per-file accumulator shared across all language
// extractors. The `lang` prefix is woven into every emitted ID so different
// languages don't collide on identical names.
//
// fnStack tracks the IDs of nested enclosing function/method nodes so call
// edges can be sourced from the innermost caller. Walkers push when entering
// a function-decl node and pop after walking its children.
type extractState struct {
	lang        string
	filePath    string
	nodes       []schema.Node
	edges       []schema.Edge
	fnStack     []string
	callTargets map[string]struct{} // dedup set for emitted call-target nodes
	sectionSeq  int                 // see nextSectionID
	// methodRefs dedups synthetic receiver-type-ref nodes emitted by
	// addMethodOf so a package with many methods on one type doesn't
	// emit the typeref node N times.
	methodRefs map[string]struct{}
}

// addMethodOf records a method → receiver-type ownership intent. It
// emits a synthetic `<lang>:typeref:<receiverName>` node (deduped)
// and a `method_of` edge from the method to it. resolve.MethodOwnership
// later rewrites this to a `type contains method` edge against the
// real type node in the same package directory — handling both
// same-file and cross-file-same-package receivers uniformly, the
// same way call targets are resolved.
func (s *extractState) addMethodOf(methodID, receiverName string) {
	if methodID == "" || receiverName == "" {
		return
	}
	ref := schema.LangID(s.lang, "typeref", receiverName)
	if s.methodRefs == nil {
		s.methodRefs = map[string]struct{}{}
	}
	if _, dup := s.methodRefs[ref]; !dup {
		s.methodRefs[ref] = struct{}{}
		s.nodes = append(s.nodes, schema.Node{ID: ref, Label: receiverName})
	}
	s.edges = append(s.edges, schema.Edge{
		Source:     methodID,
		Target:     ref,
		Relation:   "method_of",
		Confidence: schema.Extracted,
	})
}

// pushFn records that subsequent walk steps are inside the function with id.
func (s *extractState) pushFn(id string) { s.fnStack = append(s.fnStack, id) }

// popFn unwinds one level of function nesting. Safe on an empty stack so a
// malformed AST doesn't panic the walker.
func (s *extractState) popFn() {
	if len(s.fnStack) > 0 {
		s.fnStack = s.fnStack[:len(s.fnStack)-1]
	}
}

// callSource returns the ID a call edge should originate from: the innermost
// enclosing function if any, otherwise the file's module node.
func (s *extractState) callSource() string {
	if n := len(s.fnStack); n > 0 {
		return s.fnStack[n-1]
	}
	return schema.LangID(s.lang, "module", s.filePath)
}

// addCall appends a "call:<callee>" target node and a `calls` edge from the
// current scope (enclosing function or module). Cross-file resolution of the
// callee to a concrete function node is the next phase's job; for now every
// emitted edge is EXTRACTED — we observed the call in source.
//
// Repeated calls to the same callee within one file emit a single target
// node (so state.nodes doesn't bloat with N copies for N call sites) but
// every edge — each `bar() bar() bar()` is a distinct call event.
func (s *extractState) addCall(callee string) {
	if callee == "" {
		return
	}
	target := schema.LangID(s.lang, "call", callee)
	if !s.seenCall(target) {
		s.nodes = append(s.nodes, schema.Node{ID: target, Label: callee})
	}
	s.edges = append(s.edges, schema.Edge{
		Source:     s.callSource(),
		Target:     target,
		Relation:   "calls",
		Confidence: schema.Extracted,
	})
}

// seenCall reports whether target was already emitted by this state, marking
// it as seen on the first call.
func (s *extractState) seenCall(target string) bool {
	if s.callTargets == nil {
		s.callTargets = map[string]struct{}{}
	}
	if _, ok := s.callTargets[target]; ok {
		return true
	}
	s.callTargets[target] = struct{}{}
	return false
}

// emitCall is a shorthand for the recurring `addCall(callTargetName(firstCallee))`
// chain. Languages without a `function` field name (Kotlin, Scala, Zig,
// Julia, Lua) use this to keep their walkers terse.
func (s *extractState) emitCall(call *sitter.Node, src []byte) {
	s.addCall(callTargetName(firstCallee(call), src))
}

// calleeShape classifies the AST shape of a call-expression's callee child.
// Each grammar uses different node names for the same semantic — bare
// identifiers, dotted/scoped/namespaced member access, Swift's
// navigation_expression — and the extraction strategy differs per shape.
type calleeShape int

const (
	shapeBare         calleeShape = iota // node text is the callee name
	shapeMemberAccess                    // last identifier-bearing leaf is the name
	shapeNavigation                      // navigation_suffix's suffix field is the name
)

// calleeKinds is the SINGLE source of truth for what AST node kinds count
// as a "callee" across grammars. firstCallee scans for any of these, and
// callTargetName dispatches on the shape to extract the name.
//
// Adding a new grammar with a novel callee node: add ONE entry. Without
// this discipline, the two functions could disagree — a kind in
// firstCallee but not in callTargetName silently produces empty edges
// (graph edges to "" target), and the reverse is invisible because emit
// paths that route through firstCallee never reach the kind. R's
// namespace_operator / extract_operator shipped broken by exactly this
// out-of-sync class until the maps were unified.
var calleeKinds = map[string]calleeShape{
	// Bare-identifier shapes. Haskell uses `variable`, OCaml `value_name`,
	// Swift `simple_identifier` (free-fn calls) and `type_identifier`
	// (constructor calls like `MyClass()`); all carry the name as text.
	"identifier":        shapeBare,
	"field_identifier":  shapeBare,
	"name":              shapeBare,
	"variable":          shapeBare,
	"value_name":        shapeBare,
	"simple_identifier": shapeBare,
	"type_identifier":   shapeBare,

	// Member-access wrappers — receiver.callee. The qualifier appears
	// first; the called name is the last identifier-bearing child.
	"selector_expression":      shapeMemberAccess,
	"member_expression":        shapeMemberAccess,
	"attribute":                shapeMemberAccess,
	"scoped_identifier":        shapeMemberAccess,
	"field_access":             shapeMemberAccess,
	"field_expression":         shapeMemberAccess,
	"dot_index_expression":     shapeMemberAccess,
	"member_access_expression": shapeMemberAccess,
	"qualified_variable":       shapeMemberAccess,
	"value_path":               shapeMemberAccess,
	"namespace_operator":       shapeMemberAccess, // R: dplyr::mutate, base:::internal
	"extract_operator":         shapeMemberAccess, // R: foo$bar, foo@slot

	// Navigation — Swift's `obj.foo` parses as navigation_expression with
	// the trailing navigation_suffix carrying the suffix-field identifier.
	"navigation_expression": shapeNavigation,
}

// memberLeafKinds is the set of identifier-bearing AST leaves that can
// appear as the trailing child of a member-access wrapper. Includes
// JS/TS's `property_identifier` which only ever appears at this position.
var memberLeafKinds = []string{
	"identifier", "field_identifier", "property_identifier",
	"variable", "value_name", "simple_identifier", "type_identifier",
}

// firstCallee returns the first child of a call expression whose kind is
// in calleeKinds. Languages that use a `function` field name (Go, Python,
// Rust, etc.) reach callTargetName via ChildByFieldName directly; this
// helper covers grammars without that convention (Kotlin, Scala, Zig,
// Julia, Lua, R).
func firstCallee(call *sitter.Node) *sitter.Node {
	n := call.ChildCount()
	for i := uint(0); i < n; i++ {
		c := call.Child(i)
		if _, ok := calleeKinds[c.Kind()]; ok {
			return c
		}
	}
	return nil
}

// callTargetName returns the human-readable callee identifier from a call
// expression's `function` (or equivalent) child. Returns "" for unknown
// shapes — `(a + b)()`, `arr[0]()`, IIFEs etc. don't have a meaningful
// textual name and falling back to source text would pollute the graph.
func callTargetName(fn *sitter.Node, src []byte) string {
	if fn == nil {
		return ""
	}
	shape, ok := calleeKinds[fn.Kind()]
	if !ok {
		return ""
	}
	switch shape {
	case shapeBare:
		return fn.Utf8Text(src)
	case shapeMemberAccess:
		if id := lastChildOfKind(fn, memberLeafKinds...); id != nil {
			return id.Utf8Text(src)
		}
	case shapeNavigation:
		if suf := lastChildOfKind(fn, "navigation_suffix"); suf != nil {
			if name := suf.ChildByFieldName("suffix"); name != nil {
				return name.Utf8Text(src)
			}
		}
	}
	return ""
}

// fileBase returns the file's basename, used as the default module-node label.
func fileBase(absPath string) string {
	return filepath.Base(absPath)
}

// rewriteModuleLabel updates the most recently emitted module node's Label
// in place. Used by Go and Python after the package/module name is parsed
// out of the source to give the module node a more meaningful label than
// the raw filename.
func rewriteModuleLabel(s *extractState, label string) {
	want := schema.LangID(s.lang, "module", s.filePath)
	for i := len(s.nodes) - 1; i >= 0; i-- {
		if s.nodes[i].ID == want {
			s.nodes[i].Label = label
			return
		}
	}
}

// emitModule appends the file-as-module node, used as the per-file root.
func (s *extractState) emitModule(root *sitter.Node) {
	s.nodes = append(s.nodes, schema.Node{
		ID:             schema.LangID(s.lang, "module", s.filePath),
		Label:          fileBase(s.filePath),
		SourceFile:     s.filePath,
		SourceLocation: nodeLocation(root),
	})
}

// declID returns the deterministic ID emitDecl would assign without actually
// emitting a node. Used by walkers that need to know the ID of a function/
// method they're about to descend into (so they can pushFn before children
// are walked) without duplicating the ID-format string.
func declID(lang, kind, filePath string, nameNode *sitter.Node, src []byte) string {
	return schema.LangID(lang, kind, filePath+":"+nodeNameOrEmpty(nameNode, src))
}

// nodeNameOrEmpty returns nameNode.Utf8Text(src), or "" if nameNode is nil.
func nodeNameOrEmpty(nameNode *sitter.Node, src []byte) string {
	if nameNode == nil {
		return ""
	}
	return nameNode.Utf8Text(src)
}

// walkFnScope is the canonical "enter a named function/method, walk its
// children with the function on the scope stack, then leave" helper.
// Eliminates the easy-to-forget pattern of pushFn/walkChildren/popFn/return
// duplicated across every extractor.
func (s *extractState) walkFnScope(kind string, nameNode *sitter.Node, src []byte, cursor *sitter.TreeCursor, recurse func(*sitter.TreeCursor, []byte, *extractState)) {
	s.pushFn(declID(s.lang, kind, s.filePath, nameNode, src))
	walkChildren(cursor, func() { recurse(cursor, src, s) })
	s.popFn()
}

// walkAnonFnScope is the analogue for arrow functions / function expressions
// / lambdas — anonymous by construction. The scope ID is keyed by source
// position so two arrow functions in the same file don't collide on a
// single "anon" bucket. The synthetic node is also emitted (so call edges
// from within have a real source).
func (s *extractState) walkAnonFnScope(kind string, node *sitter.Node, src []byte, cursor *sitter.TreeCursor, recurse func(*sitter.TreeCursor, []byte, *extractState)) {
	p := node.StartPosition()
	posKey := fmt.Sprintf("anon@%d:%d", p.Row, p.Column)
	id := schema.LangID(s.lang, kind, s.filePath+":"+posKey)
	s.nodes = append(s.nodes, schema.Node{
		ID:             id,
		Label:          "<anonymous>",
		SourceFile:     s.filePath,
		SourceLocation: nodeLocation(node),
	})
	s.pushFn(id)
	walkChildren(cursor, func() { recurse(cursor, src, s) })
	s.popFn()
}

// emitDecl appends a "<lang>:<kind>:<filePath:name>" declaration node. Empty
// names get an "<anonymous>" label so schema.Node.Validate accepts them.
// Returns the minted declaration-node ID so callers can attach further
// edges from it (e.g. inheritance edges via addInherits).
func (s *extractState) emitDecl(kind string, declNode, nameNode *sitter.Node, src []byte) string {
	name := ""
	if nameNode != nil {
		name = nameNode.Utf8Text(src)
	}
	label := name
	if label == "" {
		label = "<anonymous>"
	}
	declID := schema.LangID(s.lang, kind, s.filePath+":"+name)
	s.nodes = append(s.nodes, schema.Node{
		ID:             declID,
		Label:          label,
		SourceFile:     s.filePath,
		SourceLocation: nodeLocation(declNode),
	})
	// Containment backbone: link the declaration to its enclosing
	// scope — the innermost nesting function if any, otherwise the
	// file's module node. This is the structural hierarchy graphify
	// emits as `contains` edges (module → top-level decls, function →
	// nested closures). Without it, an uncalled function or a method
	// with no resolvable call sites floats as a zero-edge singleton
	// community; with it, every declaration is reachable from its
	// file, which is what the wiki / get_neighbors / traverse tools
	// want when answering "what's defined in here?".
	//
	// callSource() returns the enclosing function only while we're
	// walking its body; emitDecl for the function itself runs BEFORE
	// walkFnScope pushes it, so a top-level function correctly gets
	// `module contains func`, not a self-loop.
	container := s.callSource()
	if container != declID { // guard against a degenerate self-link
		s.edges = append(s.edges, schema.Edge{
			Source:     container,
			Target:     declID,
			Relation:   "contains",
			Confidence: schema.Extracted,
		})
	}
	return declID
}

// addInherits records a subtype → base-type relationship (class
// inheritance, interface implementation, or Go struct/interface
// embedding). It emits a synthetic `<lang>:typeref:<baseName>` node
// (deduped, shared with addMethodOf) and a `relation` edge from the
// subtype node to it. resolve.Inheritance later rewrites the typeref
// target to the real base type/class node when one exists in the
// corpus — narrowed by import scope across packages, since base types
// frequently live in other files or modules. Unresolved bases (an
// external framework class like `Exception`) keep the typeref as an
// observed "extends this" fact.
//
// relation is the edge label: "inherits", "implements", or "embeds".
func (s *extractState) addInherits(subtypeID, baseName, relation string) {
	if subtypeID == "" || baseName == "" || relation == "" {
		return
	}
	s.edges = append(s.edges, schema.Edge{
		Source:     subtypeID,
		Target:     s.ensureTypeRef(baseName),
		Relation:   relation,
		Confidence: schema.Extracted,
	})
}

// addInheritsByName records inheritance where BOTH endpoints are named
// rather than declared at this site — the shape of Rust's
// `impl Trait for Type` (and trait bounds), where neither the type nor
// the trait is defined in the impl block. Both become synthetic typeref
// nodes; resolve.Inheritance rewrites each to its real declaration when
// one exists in the corpus.
func (s *extractState) addInheritsByName(subtypeName, baseName, relation string) {
	if subtypeName == "" || baseName == "" {
		return
	}
	s.addInherits(s.ensureTypeRef(subtypeName), baseName, relation)
}

// ensureTypeRef returns the `<lang>:typeref:<name>` node ID, emitting the
// node once (deduped, shared with addMethodOf/addInherits).
func (s *extractState) ensureTypeRef(name string) string {
	ref := schema.LangID(s.lang, "typeref", name)
	if s.methodRefs == nil {
		s.methodRefs = map[string]struct{}{}
	}
	if _, dup := s.methodRefs[ref]; !dup {
		s.methodRefs[ref] = struct{}{}
		s.nodes = append(s.nodes, schema.Node{ID: ref, Label: name})
	}
	return ref
}

// addImport appends an import-target node and an "imports" edge from the
// file's module node. Languages share this shape regardless of how their
// import statement is spelled.
func (s *extractState) addImport(target string) {
	s.nodes = append(s.nodes, schema.Node{
		ID:    schema.LangID(s.lang, "import", target),
		Label: target,
	})
	s.edges = append(s.edges, schema.Edge{
		Source:     schema.LangID(s.lang, "module", s.filePath),
		Target:     schema.LangID(s.lang, "import", target),
		Relation:   "imports",
		Confidence: schema.Extracted,
	})
}

// emitContainsFromModule links the file's module node to childID with a
// `contains` edge. Used by the document extractors so heading/section
// nodes attach to their file rather than floating as zero-edge
// singletons (which the clusterer would isolate into their own
// communities). The module-node ID is derived the same way the
// extractors mint it, so the edge endpoint always resolves.
func (s *extractState) emitContainsFromModule(childID string) {
	s.edges = append(s.edges, schema.Edge{
		Source:     schema.LangID(s.lang, "module", s.filePath),
		Target:     childID,
		Relation:   "contains",
		Confidence: schema.Extracted,
	})
}

// emitDataKey appends a "key" node + "contains" edge from the module. Used
// by data-file extractors (YAML, TOML) to surface document structure.
func (s *extractState) emitDataKey(key string, declNode *sitter.Node) {
	s.nodes = append(s.nodes, schema.Node{
		ID:             schema.LangID(s.lang, "key", s.filePath+":"+key),
		Label:          key,
		SourceFile:     s.filePath,
		SourceLocation: nodeLocation(declNode),
	})
	s.edges = append(s.edges, schema.Edge{
		Source:     schema.LangID(s.lang, "module", s.filePath),
		Target:     schema.LangID(s.lang, "key", s.filePath+":"+key),
		Relation:   "contains",
		Confidence: schema.Extracted,
	})
}

// importStringSource returns the unquoted string text from an import_statement's
// `source` field. Used by JS/TS, where the source path is the only string child.
func importStringSource(node *sitter.Node, src []byte) string {
	source := node.ChildByFieldName("source")
	if source == nil {
		return ""
	}
	n := source.ChildCount()
	for i := uint(0); i < n; i++ {
		c := source.Child(i)
		if c.Kind() == "string_fragment" {
			return c.Utf8Text(src)
		}
	}
	return ""
}

// firstChildOfKind returns the first direct child of node whose Kind matches
// any of the given kinds, or nil. Used by extractors that need to drill past
// punctuation/keyword children to reach the named token.
func firstChildOfKind(node *sitter.Node, kinds ...string) *sitter.Node {
	n := node.ChildCount()
	for i := uint(0); i < n; i++ {
		c := node.Child(i)
		k := c.Kind()
		for _, want := range kinds {
			if k == want {
				return c
			}
		}
	}
	return nil
}

// firstDescendantIdentifier does a BFS through node's descendants and returns
// the first `identifier` (or `type_identifier`) it finds. Used by grammars
// that wrap declaration names inside intermediate `signature`/`call_expression`
// subtrees rather than as direct children.
func firstDescendantIdentifier(node *sitter.Node) *sitter.Node {
	queue := []*sitter.Node{node}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		if n != node {
			k := n.Kind()
			if k == "identifier" || k == "type_identifier" {
				return n
			}
		}
		c := n.ChildCount()
		for i := uint(0); i < c; i++ {
			queue = append(queue, n.Child(i))
		}
	}
	return nil
}

// lastChildOfKind is firstChildOfKind's right-leaning cousin — returns the
// last matching child rather than the first. Java import_declaration uses it
// to pick the trailing scoped_identifier (skipping the `static` modifier).
func lastChildOfKind(node *sitter.Node, kinds ...string) *sitter.Node {
	var last *sitter.Node
	n := node.ChildCount()
	for i := uint(0); i < n; i++ {
		c := node.Child(i)
		k := c.Kind()
		for _, want := range kinds {
			if k == want {
				last = c
			}
		}
	}
	return last
}
