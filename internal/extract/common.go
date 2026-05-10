package extract

import (
	"fmt"
	"path/filepath"

	"github.com/julianshen/gogfy/internal/schema"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

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

// firstCallee returns the first child of a call-expression node that looks
// like a callee (a bare identifier or a member-access chain). Languages
// without a `function` field name (Kotlin, Scala, Zig, Julia, Lua) pair this
// with callTargetName to emit `<lang>:call:<name>` edges.
func firstCallee(call *sitter.Node) *sitter.Node {
	return firstChildOfKind(call,
		"identifier", "field_identifier",
		"selector_expression", "member_expression", "attribute",
		"scoped_identifier", "field_access", "field_expression",
		"dot_index_expression",
	)
}

// callTargetName returns the human-readable callee identifier from a call
// expression's `function` (or equivalent) child. Handles the three shapes
// most grammars share: a bare `identifier`, a member-access wrapper
// (selector_expression / member_expression / attribute / scoped_identifier)
// where we use the last identifier, and unknown shapes which we fall back
// to printing verbatim.
func callTargetName(fn *sitter.Node, src []byte) string {
	if fn == nil {
		return ""
	}
	switch fn.Kind() {
	case "identifier", "field_identifier", "name", "variable", "value_name", "simple_identifier":
		// Bare-identifier call shapes. Haskell uses `variable`, OCaml uses
		// `value_name`, Swift uses `simple_identifier`; all are simple
		// wrappers over the textual name.
		return fn.Utf8Text(src)
	case "navigation_expression":
		// Swift: `obj.foo` → navigation_expression with the trailing
		// `navigation_suffix` carrying the suffix-field identifier.
		if suf := lastChildOfKind(fn, "navigation_suffix"); suf != nil {
			if name := suf.ChildByFieldName("suffix"); name != nil {
				return name.Utf8Text(src)
			}
		}
	case "selector_expression", "member_expression", "attribute",
		"scoped_identifier", "field_access", "field_expression",
		"dot_index_expression", "member_access_expression",
		"qualified_variable", "value_path":
		// The last identifier-bearing child is the called name (the
		// receiver/qualifier comes first).
		if id := lastChildOfKind(fn, "identifier", "field_identifier", "property_identifier", "variable", "value_name", "simple_identifier"); id != nil {
			return id.Utf8Text(src)
		}
	}
	// Drop unknown shapes — `(a + b)()`, `arr[0]()`, IIFEs etc. don't have
	// a meaningful textual name. Falling back to verbatim source text would
	// pollute the graph with noisy targets like `call:(a + b)`.
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
func (s *extractState) emitDecl(kind string, declNode, nameNode *sitter.Node, src []byte) {
	name := ""
	if nameNode != nil {
		name = nameNode.Utf8Text(src)
	}
	label := name
	if label == "" {
		label = "<anonymous>"
	}
	s.nodes = append(s.nodes, schema.Node{
		ID:             schema.LangID(s.lang, kind, s.filePath+":"+name),
		Label:          label,
		SourceFile:     s.filePath,
		SourceLocation: nodeLocation(declNode),
	})
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
