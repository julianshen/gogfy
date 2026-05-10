package extract

import (
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_r "github.com/julianshen/tree-sitter-r/bindings/go"
)

// RExtractor extracts function declarations, library/require imports,
// and call edges from R sources. R's grammar models top-level shapes
// quite differently from C-family languages:
//
//   - Function "declarations" are just assignments where the rhs is a
//     function literal: `add <- function(x, y) x + y` is a binary_operator
//     with lhs=identifier and rhs=function_definition. The same shape
//     covers `=` and `<<-` assignment, all valid in R.
//   - `library(pkg)` and `require(pkg)` are ordinary calls, distinguished
//     only by the function name. We emit them as imports rather than as
//     call edges; the package name comes from the first argument, which
//     can be either an identifier (NSE: `library(dplyr)`) or a string
//     literal (`library("dplyr")`).
type RExtractor struct{}

func (RExtractor) Extract(path string) (Result, error) {
	return runExtraction(path, tree_sitter_r.Language(), "r", walkR)
}

func walkR(cursor *sitter.TreeCursor, src []byte, state *extractState) {
	node := cursor.Node()
	switch node.Kind() {
	case "binary_operator":
		// Function definition shape: lhs is an identifier, rhs is a
		// function_definition. R has no dedicated `function_declaration`
		// node — assignments are the declarations.
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

// rImportTarget detects library(pkg) / require(pkg) calls and returns
// the package name. R accepts both `library(dplyr)` (NSE — argument is
// an identifier) and `library("dplyr")` (string) and both forms are
// common in practice.
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
	n := args.ChildCount()
	for i := uint(0); i < n; i++ {
		a := args.Child(i)
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
			return strings.Trim(v.Utf8Text(src), `"'`), true
		}
	}
	return "", false
}
