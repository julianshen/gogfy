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
	case "function_declaration", "method_declaration":
		kind := "function"
		if node.Kind() == "method_declaration" {
			kind = "method"
		}
		nameNode := node.ChildByFieldName("name")
		state.emitDecl(kind, node, nameNode, src)
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
