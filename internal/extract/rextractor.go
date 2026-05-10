package extract

import (
	sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_r "github.com/julianshen/tree-sitter-r/bindings/go"
)

// RExtractor handles R sources. R has no dedicated function_declaration
// node — function "decls" are assignments whose rhs is a function literal
// (`add <- function(x, y) x + y` is a binary_operator with rhs=function_definition).
// Same shape covers `=` and `<<-`, all valid R assignments.
type RExtractor struct{}

func (RExtractor) Extract(path string) (Result, error) {
	return runExtraction(path, tree_sitter_r.Language(), "r", walkR)
}

func walkR(cursor *sitter.TreeCursor, src []byte, state *extractState) {
	node := cursor.Node()
	switch node.Kind() {
	case "binary_operator":
		rhs := node.ChildByFieldName("rhs")
		if rhs != nil && rhs.Kind() == "function_definition" {
			lhs := node.ChildByFieldName("lhs")
			if lhs != nil && lhs.Kind() == "identifier" {
				state.emitDecl("function", node, lhs, src)
				state.walkFnScope("function", lhs, src, cursor, walkR)
				return
			}
		}
	case "call":
		if pkg, ok := rImportTarget(node, src); ok {
			state.addImport(pkg)
			return
		}
		state.emitCall(node, src)
	}
	walkChildren(cursor, func() { walkR(cursor, src, state) })
}

// rImportTarget detects library/require/requireNamespace calls and returns
// the package name. R accepts both unquoted (`library(dplyr)`, evaluated
// via NSE — the callee captures the bare identifier) and quoted
// (`library("dplyr")`) forms; both are common.
func rImportTarget(call *sitter.Node, src []byte) (string, bool) {
	fn := call.ChildByFieldName("function")
	if fn == nil || fn.Kind() != "identifier" {
		return "", false
	}
	name := fn.Utf8Text(src)
	if name != "library" && name != "require" && name != "requireNamespace" {
		return "", false
	}
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return "", false
	}
	// NamedChild iteration skips anonymous tokens like the surrounding
	// parens. tree-sitter-r exposes `comma` as a named child of
	// `arguments`, so the kind-check below still has to discard those.
	n := args.NamedChildCount()
	for i := uint(0); i < n; i++ {
		a := args.NamedChild(i)
		if a.Kind() != "argument" {
			continue
		}
		v := a.ChildByFieldName("value")
		if v == nil {
			continue
		}
		switch v.Kind() {
		case "identifier":
			return v.Utf8Text(src), true
		case "string":
			return trimQuotes(v.Utf8Text(src)), true
		}
	}
	return "", false
}
