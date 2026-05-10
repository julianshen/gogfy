package extract

import (
	sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_haskell "github.com/tree-sitter/tree-sitter-haskell/bindings/go"
)

// HaskellExtractor extracts module/function/import nodes from Haskell sources.
//
// Haskell's call semantics don't have a syntactic call expression — function
// application is juxtaposition, which the grammar surfaces as `apply`. We
// emit a `calls:<name>` edge for each apply whose head is a bare variable
// identifier; partial-application chains and operator sections aren't
// followed exhaustively (out of scope for AST-only extraction).
type HaskellExtractor struct{}

func (HaskellExtractor) Extract(path string) (Result, error) {
	return runExtraction(path, tree_sitter_haskell.Language(), "haskell", walkHaskell)
}

func walkHaskell(cursor *sitter.TreeCursor, src []byte, state *extractState) {
	node := cursor.Node()
	switch node.Kind() {
	case "header":
		// `module Foo.Bar where` — `module` field is the declared name.
		if id := node.ChildByFieldName("module"); id != nil {
			rewriteModuleLabel(state, id.Utf8Text(src))
		}
	case "import":
		// `import Foo.Bar (...)` — `module` field is the imported name.
		if id := node.ChildByFieldName("module"); id != nil {
			state.addImport(id.Utf8Text(src))
		}
	case "function", "bind":
		// Top-level value/function bindings. `function` covers `f x = ...`;
		// `bind` covers `f = ...`. Both have a `name` field naming the
		// bound identifier.
		nameNode := node.ChildByFieldName("name")
		if nameNode == nil {
			break
		}
		state.emitDecl("function", node, nameNode, src)
		state.walkFnScope("function", nameNode, src, cursor, walkHaskell)
		return
	case "apply":
		// `f x` in expression position. We surface the head as a call so
		// graphs include the most common form of cross-function reference.
		if head := node.Child(0); head != nil {
			state.addCall(callTargetName(head, src))
		}
	}
	walkChildren(cursor, func() { walkHaskell(cursor, src, state) })
}
