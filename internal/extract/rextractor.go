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
		// R's binary_operator covers many non-assignment shapes (formula
		// `y ~ ...`, custom infix `%op%`, arithmetic). Only assignments
		// declare functions; without this gate `y ~ function(x) x` would
		// emit a phantom decl named "y".
		if !rIsAssignment(node) {
			break
		}
		rhs := node.ChildByFieldName("rhs")
		if rhs != nil && rhs.Kind() == "function_definition" {
			if lhs := rDeclLHS(node); lhs != nil {
				rEmitDecl(state, node, lhs, src)
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

// rDeclLHS returns the LHS node of a function-assignment binary_operator
// when it names something the graph can use as a declaration. R supports
// three syntactically distinct ways to name what's being assigned:
//
//   - Bare identifier: `add <- function(...)`
//   - Backticked identifier: `` `+.MyClass` <- function(...) `` (S3 operator
//     dispatch). tree-sitter-r parses these as `identifier` with the
//     backticks included in the text — no special handling needed.
//   - String literal: `"opname" <- function(...)`. Less common but valid R;
//     the tree-sitter-r grammar parses the LHS as `string`. Without this
//     case we'd silently drop S3-method registrations of this form.
func rDeclLHS(node *sitter.Node) *sitter.Node {
	lhs := node.ChildByFieldName("lhs")
	if lhs == nil {
		return nil
	}
	switch lhs.Kind() {
	case "identifier", "string":
		return lhs
	}
	return nil
}

// rEmitDecl emits the function-decl node, deriving a clean label from
// the LHS — string-literal LHS like `"opname"` would otherwise carry
// surrounding quotes into the graph label.
func rEmitDecl(state *extractState, declNode, lhs *sitter.Node, src []byte) {
	state.emitDecl("function", declNode, lhs, src)
	if lhs.Kind() == "string" {
		// Patch the label of the just-emitted node to drop quotes; the
		// LangID still uses the raw text for uniqueness.
		clean := trimQuotes(lhs.Utf8Text(src))
		if i := len(state.nodes) - 1; i >= 0 && clean != "" {
			state.nodes[i].Label = clean
		}
	}
}

// rIsAssignment reports whether a binary_operator node is one of R's
// assignment forms (<-, =, <<-). R reuses binary_operator for many other
// shapes — formulas, custom infix operators, arithmetic — none of which
// declare anything.
func rIsAssignment(node *sitter.Node) bool {
	n := node.ChildCount()
	for i := uint(0); i < n; i++ {
		c := node.Child(i)
		if c.IsNamed() {
			continue
		}
		switch c.Kind() {
		case "<-", "=", "<<-":
			return true
		}
	}
	return false
}

// rImportTarget detects library/require/requireNamespace calls and returns
// the package name. The package is either the first POSITIONAL argument
// (no `name=` prefix) or the argument explicitly named `package`. Other
// named arguments must be skipped — `library(help = "stats")` is a help
// lookup, not a package load, and was previously misread as importing
// "stats". Both unquoted (NSE: `library(dplyr)`) and quoted forms accepted.
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
	// `library(pkg, character.only = TRUE)` means the first arg is a
	// variable holding the package name, not the literal package. Skip
	// emission — emitting the variable identifier would produce a fake
	// import edge and hide the real (dynamic) dependency.
	if rHasCharacterOnly(args, src) {
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
		// Named args: argument has a "name" field. Only `package = ...`
		// counts as the package; every other name (`help`, `quietly`,
		// `character.only`, etc.) is unrelated and must not be treated
		// as a positional package name.
		if argName := a.ChildByFieldName("name"); argName != nil {
			if argName.Utf8Text(src) != "package" {
				continue
			}
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
		return "", false
	}
	return "", false
}

// rHasCharacterOnly reports whether an arguments node has an explicit
// `character.only = TRUE` (or `T`) named argument. This signals dynamic
// loading where the package-name argument is a variable; in that case
// no static import edge should be emitted.
func rHasCharacterOnly(args *sitter.Node, src []byte) bool {
	n := args.NamedChildCount()
	for i := uint(0); i < n; i++ {
		a := args.NamedChild(i)
		if a.Kind() != "argument" {
			continue
		}
		argName := a.ChildByFieldName("name")
		if argName == nil || argName.Utf8Text(src) != "character.only" {
			continue
		}
		v := a.ChildByFieldName("value")
		if v == nil {
			continue
		}
		txt := v.Utf8Text(src)
		if txt == "TRUE" || txt == "T" {
			return true
		}
	}
	return false
}
