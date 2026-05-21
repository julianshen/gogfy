// Package extract implements source-code extraction using tree-sitter.
package extract

import (
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_go "github.com/tree-sitter/tree-sitter-go/bindings/go"
)

// GoExtractor uses tree-sitter-go to extract module/function/method nodes,
// import edges, and call edges from Go sources.
type GoExtractor struct{}

func (GoExtractor) Extract(path string) (Result, error) {
	return runExtraction(path, tree_sitter_go.Language(), "go", walkGo)
}

// walkGo emits one module node per file (label is the Go package name as
// declared by the file's package_clause), function/method nodes for each
// declaration, import edges from the module to each imported path, and
// call edges from the innermost enclosing function (or the module, for
// top-level init expressions) to a synthetic `go:call:<callee>` target.
func walkGo(cursor *sitter.TreeCursor, src []byte, state *extractState) {
	node := cursor.Node()
	switch node.Kind() {
	case "package_clause":
		nameNode := node.ChildByFieldName("name")
		if nameNode == nil {
			nameNode = firstChildOfKind(node, "package_identifier")
		}
		if nameNode != nil {
			// Override the runExtraction-emitted module node's label so
			// `go:module:<file>` carries the package name (more meaningful
			// than the bare filename for Go's per-package model).
			rewriteModuleLabel(state, nameNode.Utf8Text(src))
		}
	case "type_declaration":
		// `type Foo struct{}` / `type Bar interface{}` / `type Baz =
		// Qux`. A single declaration can group multiple specs
		// (`type ( A int; B string )`), so walk every type_spec child.
		// Each becomes a `go:type:` node, registered for later
		// method-ownership linking.
		for i := uint(0); i < node.ChildCount(); i++ {
			ch := node.Child(i)
			if ch.Kind() != "type_spec" && ch.Kind() != "type_alias" {
				continue
			}
			nameNode := ch.ChildByFieldName("name")
			state.emitDecl("type", ch, nameNode, src)
			if nameNode != nil {
				state.recordDeclaredType(nameNode.Utf8Text(src),
					declID(state.lang, "type", state.filePath, nameNode, src))
			}
		}
	case "function_declaration", "method_declaration":
		kind := "function"
		if node.Kind() == "method_declaration" {
			kind = "method"
		}
		nameNode := node.ChildByFieldName("name")
		state.emitDecl(kind, node, nameNode, src)
		// Method ownership: record the receiver type so finalize() can
		// link `type contains method` when the receiver type is
		// declared in this file. Methods on cross-file types keep just
		// the module link emitDecl already added.
		if kind == "method" && nameNode != nil {
			if recv := goReceiverTypeName(node, src); recv != "" {
				state.recordMethodOwner(
					declID(state.lang, "method", state.filePath, nameNode, src), recv)
			}
		}
		state.walkFnScope(kind, nameNode, src, cursor, walkGo)
		return
	case "call_expression":
		state.addCall(callTargetName(node.ChildByFieldName("function"), src))
	case "import_spec":
		pathNode := node.ChildByFieldName("path")
		if pathNode != nil {
			state.addImport(strings.Trim(pathNode.Utf8Text(src), `"`))
		}
	}
	walkChildren(cursor, func() { walkGo(cursor, src, state) })
}

// goReceiverTypeName extracts the receiver type name from a Go
// method_declaration, stripping a leading pointer. For
// `func (r *Repo) Save()` and `func (r Repo) Load()` it returns
// "Repo". Returns "" when the receiver or its type can't be found
// (defensive — a malformed AST shouldn't panic the walker).
//
// AST shape: method_declaration has a `receiver` field = parameter_list
// → parameter_declaration whose `type` field is either a
// `pointer_type` (→ type_identifier) or a bare `type_identifier`.
func goReceiverTypeName(method *sitter.Node, src []byte) string {
	recv := method.ChildByFieldName("receiver")
	if recv == nil {
		return ""
	}
	param := firstChildOfKind(recv, "parameter_declaration")
	if param == nil {
		return ""
	}
	typeNode := param.ChildByFieldName("type")
	if typeNode == nil {
		return ""
	}
	// Unwrap a pointer receiver (*Repo → Repo).
	if typeNode.Kind() == "pointer_type" {
		if id := firstChildOfKind(typeNode, "type_identifier"); id != nil {
			return id.Utf8Text(src)
		}
		return ""
	}
	if typeNode.Kind() == "type_identifier" {
		return typeNode.Utf8Text(src)
	}
	// Generic receiver (`func (r *Repo[T]) ...`) → generic_type wraps
	// the pointer/identifier; dig for the first type_identifier.
	if id := firstChildOfKind(typeNode, "type_identifier"); id != nil {
		return id.Utf8Text(src)
	}
	return ""
}
