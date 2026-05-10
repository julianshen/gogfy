package extract

import (
	sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_ocaml "github.com/tree-sitter/tree-sitter-ocaml/bindings/go"
)

// OCamlExtractor extracts module/value/function nodes plus open-statement
// imports and application-expression calls from OCaml sources.
//
// OCaml's value definitions cover both data values and named functions
// (`let foo x = ...`); we emit them all as `function` nodes so the graph
// captures cross-function references. Anonymous `fun` lambdas push a new
// scope so calls inside them attribute to the lambda's enclosing definition.
type OCamlExtractor struct{}

func (OCamlExtractor) Extract(path string) (Result, error) {
	return runExtraction(path, tree_sitter_ocaml.LanguageOCaml(), "ocaml", walkOCaml)
}

func walkOCaml(cursor *sitter.TreeCursor, src []byte, state *extractState) {
	node := cursor.Node()
	switch node.Kind() {
	case "open_module":
		// `open Foo.Bar` brings module names into scope.
		if id := firstChildOfKind(node, "module_path", "module_name"); id != nil {
			state.addImport(id.Utf8Text(src))
		}
	case "module_definition":
		state.emitDecl("module", node, firstChildOfKind(node, "module_name"), src)
	case "value_definition", "let_binding":
		// `let foo x = ...` — the bound name is the `pattern` field for
		// let_binding, or buried deeper for value_definition. Use the first
		// value_name we find as the declared identifier.
		nameNode := firstChildOfKind(node, "value_name")
		if nameNode == nil {
			break
		}
		state.emitDecl("function", node, nameNode, src)
		state.walkFnScope("function", nameNode, src, cursor, walkOCaml)
		return
	case "fun_expression":
		state.walkAnonFnScope("function", node, src, cursor, walkOCaml)
		return
	case "application_expression":
		// `f x` — first child is the applied function.
		if head := node.Child(0); head != nil {
			state.addCall(callTargetName(head, src))
		}
	}
	walkChildren(cursor, func() { walkOCaml(cursor, src, state) })
}
