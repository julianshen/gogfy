package extract

import (
	sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_erlang "github.com/julianshen/tree-sitter-erlang/bindings/go"
)

// ErlangExtractor handles Erlang sources. Erlang's surface shapes don't
// quite match any other grammar in this package:
//
//   - `-module(name).` is parsed as `module_attribute` and gives the file
//     its display label (analogous to Go's package decl rewriting).
//   - `-import(Mod, [Fn/Arity, ...]).` is `import_attribute`; the first
//     atom is the imported module. We don't enumerate the function list —
//     graphs key on module-level imports.
//   - Function decls live under `fun_decl > function_clause`. The first
//     atom child of the clause is the function name.
//   - Local calls are `call > atom expr_args`. Qualified calls
//     (`io:format(...)`) are `remote > remote_module > call`; the
//     `remote` wraps the call and provides the module qualifier.
type ErlangExtractor struct{}

func (ErlangExtractor) Extract(path string) (Result, error) {
	return runExtraction(path, tree_sitter_erlang.Language(), "erlang", walkErlang)
}

func walkErlang(cursor *sitter.TreeCursor, src []byte, state *extractState) {
	node := cursor.Node()
	switch node.Kind() {
	case "module_attribute":
		if a := firstChildOfKind(node, "atom"); a != nil {
			rewriteModuleLabel(state, a.Utf8Text(src))
		}
	case "import_attribute":
		// First atom child is the imported module name; the bracketed
		// FA list that follows is structural detail we don't model.
		if a := firstChildOfKind(node, "atom"); a != nil {
			state.addImport(a.Utf8Text(src))
		}
	case "fun_decl":
		if fc := firstChildOfKind(node, "function_clause"); fc != nil {
			if name := firstChildOfKind(fc, "atom"); name != nil {
				state.emitDecl("function", node, name, src)
				state.walkFnScope("function", name, src, cursor, walkErlang)
				return
			}
		}
	case "remote":
		// Qualified call `Module:Fn(args)`. Emit one edge to "Mod:Fn"
		// and let walkChildren still recurse so any nested calls inside
		// the argument list are picked up. The inner `call` node's
		// emission is suppressed below by the parent-kind check.
		mod := erlangAtomChild(firstChildOfKind(node, "remote_module"), src)
		fn := erlangAtomChild(firstChildOfKind(node, "call"), src)
		if fn != "" {
			target := fn
			if mod != "" {
				target = mod + ":" + fn
			}
			state.addCall(target)
		}
	case "call":
		// Skip if this `call` is the inner half of a `remote` — the
		// remote case above already emitted the qualified edge.
		if p := node.Parent(); p != nil && p.Kind() == "remote" {
			break
		}
		if name := firstChildOfKind(node, "atom"); name != nil {
			state.addCall(name.Utf8Text(src))
		}
	}
	walkChildren(cursor, func() { walkErlang(cursor, src, state) })
}

// erlangAtomChild returns the text of the first `atom` child of n, or
// "" if n is nil or has no atom child.
func erlangAtomChild(n *sitter.Node, src []byte) string {
	if n == nil {
		return ""
	}
	if a := firstChildOfKind(n, "atom"); a != nil {
		return a.Utf8Text(src)
	}
	return ""
}
