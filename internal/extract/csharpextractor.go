package extract

import (
	sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_csharp "github.com/tree-sitter/tree-sitter-c-sharp/bindings/go"
)

// CSharpExtractor extracts class/method/namespace nodes plus using-directive
// and method-invocation edges from C# sources.
type CSharpExtractor struct{}

func (CSharpExtractor) Extract(path string) (Result, error) {
	return runExtraction(path, tree_sitter_csharp.Language(), "csharp", walkCSharp)
}

func walkCSharp(cursor *sitter.TreeCursor, src []byte, state *extractState) {
	node := cursor.Node()
	switch node.Kind() {
	case "using_directive":
		// `using Foo.Bar;` — the qualified name is the trailing identifier-
		// bearing child. Skip past the `using` and any `static` modifier.
		if id := lastChildOfKind(node, "qualified_name", "identifier"); id != nil {
			state.addImport(id.Utf8Text(src))
		}
	case "namespace_declaration", "file_scoped_namespace_declaration":
		// Override the runExtraction-emitted module label so
		// `csharp:module:<file>` carries the namespace name.
		nameNode := node.ChildByFieldName("name")
		if nameNode != nil {
			rewriteModuleLabel(state, nameNode.Utf8Text(src))
		}
	case "class_declaration", "record_declaration":
		// Records (C# 9+) are declared with the same `name` field as
		// classes; treat them as classes for graph purposes.
		id := state.emitDecl("class", node, node.ChildByFieldName("name"), src)
		goEmitCSharpBases(state, node, id, src)
	case "interface_declaration":
		id := state.emitDecl("interface", node, node.ChildByFieldName("name"), src)
		goEmitCSharpBases(state, node, id, src)
	case "struct_declaration":
		id := state.emitDecl("struct", node, node.ChildByFieldName("name"), src)
		goEmitCSharpBases(state, node, id, src)
	case "enum_declaration":
		state.emitDecl("enum", node, node.ChildByFieldName("name"), src)
	case "method_declaration", "constructor_declaration":
		kind := "method"
		if node.Kind() == "constructor_declaration" {
			kind = "constructor"
		}
		nameNode := node.ChildByFieldName("name")
		state.emitDecl(kind, node, nameNode, src)
		state.walkFnScope(kind, nameNode, src, cursor, walkCSharp)
		return
	case "local_function_statement":
		nameNode := node.ChildByFieldName("name")
		state.emitDecl("function", node, nameNode, src)
		state.walkFnScope("function", nameNode, src, cursor, walkCSharp)
		return
	case "lambda_expression":
		state.walkAnonFnScope("function", node, src, cursor, walkCSharp)
		return
	case "invocation_expression":
		// Callee is in the `function` field. callTargetName strips the
		// receiver from `obj.foo()` so it surfaces as `calls:foo`.
		state.addCall(callTargetName(node.ChildByFieldName("function"), src))
	}
	walkChildren(cursor, func() { walkCSharp(cursor, src, state) })
}
