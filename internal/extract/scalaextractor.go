package extract

import (
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_scala "github.com/tree-sitter/tree-sitter-scala/bindings/go"
)

// ScalaExtractor extracts module/class/object/trait/function nodes and import
// edges from Scala sources.
type ScalaExtractor struct{}

func (ScalaExtractor) Extract(path string) (Result, error) {
	return runExtraction(path, tree_sitter_scala.Language(), "scala", walkScala)
}

func walkScala(cursor *sitter.TreeCursor, src []byte, state *extractState) {
	node := cursor.Node()
	switch node.Kind() {
	case "import_declaration":
		emitScalaImports(state, node, src)
	case "class_definition":
		id := state.emitDecl("class", node, firstChildOfKind(node, "identifier"), src)
		goEmitScalaBases(state, node, id, src)
	case "object_definition":
		id := state.emitDecl("object", node, firstChildOfKind(node, "identifier"), src)
		goEmitScalaBases(state, node, id, src)
	case "trait_definition":
		id := state.emitDecl("trait", node, firstChildOfKind(node, "identifier"), src)
		goEmitScalaBases(state, node, id, src)
	case "function_definition":
		nameNode := firstChildOfKind(node, "identifier")
		state.emitDecl("function", node, nameNode, src)
		state.walkFnScope("function", nameNode, src, cursor, walkScala)
		return
	case "call_expression":
		state.emitCall(node, src)
	case "lambda_expression":
		// `xs.map(x => inner(x))` — calls inside should source from the
		// anonymous lambda scope, not the enclosing method.
		state.walkAnonFnScope("function", node, src, cursor, walkScala)
		return
	}
	walkChildren(cursor, func() { walkScala(cursor, src, state) })
}

// emitScalaImports handles Scala's three import shapes:
//
//	import a.b.C                   // flat identifier sequence ending in C
//	import a.b.{Foo, Bar}          // namespace_selectors with leaf identifiers
//	import a.b.{Foo => Bar}        // namespace_selectors with arrow_renamed_identifier
//
// The grammar exposes the dotted prefix as flat identifier/`.` children
// rather than a wrapped path node, so we accumulate the prefix and then
// expand any selectors against it.
func emitScalaImports(state *extractState, node *sitter.Node, src []byte) {
	var prefix []string
	var selectors *sitter.Node
	n := node.ChildCount()
	for i := uint(0); i < n; i++ {
		c := node.Child(i)
		switch c.Kind() {
		case "identifier":
			prefix = append(prefix, c.Utf8Text(src))
		case "namespace_selectors":
			selectors = c
		}
	}
	pfx := strings.Join(prefix, ".")
	if selectors == nil {
		if pfx != "" {
			state.addImport(pfx)
		}
		return
	}
	m := selectors.ChildCount()
	for i := uint(0); i < m; i++ {
		c := selectors.Child(i)
		var leaf string
		switch c.Kind() {
		case "identifier":
			leaf = c.Utf8Text(src)
		case "arrow_renamed_identifier":
			// Original symbol is the first identifier; the alias is discarded
			// for the import edge.
			if id := firstChildOfKind(c, "identifier"); id != nil {
				leaf = id.Utf8Text(src)
			}
		}
		if leaf == "" {
			continue
		}
		if pfx == "" {
			state.addImport(leaf)
		} else {
			state.addImport(pfx + "." + leaf)
		}
	}
}
