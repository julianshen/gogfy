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
		// Module-level import: `-import(lists, [map/2, foldl/3]).` adds
		// `lists` plus one selector edge per FA pair (`lists:map`,
		// `lists:foldl`). Mirrors Julia's selected-import handling.
		erlangEmitImport(state, node, src)
	case "fun_decl":
		if fc := firstChildOfKind(node, "function_clause"); fc != nil {
			if name := firstChildOfKind(fc, "atom"); name != nil {
				state.emitDecl("function", node, name, src)
				state.walkFnScope("function", name, src, cursor, walkErlang)
				return
			}
		}
	case "external_fun":
		// `fun mymod:func/1` — a function reference. Emits a call edge
		// to "mymod:func" (omitting the arity: graph nodes key on names,
		// not signatures). Without this case, a common Erlang idiom for
		// passing function refs to higher-order functions silently
		// produces no call edge.
		if mod, fn := erlangExternalFunName(node, src); fn != "" {
			if mod != "" {
				state.addCall(mod + ":" + fn)
			} else {
				state.addCall(fn)
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
			if mod != "" {
				state.addCall(mod + ":" + fn)
			} else {
				state.addCall(fn)
			}
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

func erlangAtomChild(n *sitter.Node, src []byte) string {
	if n == nil {
		return ""
	}
	if a := firstChildOfKind(n, "atom"); a != nil {
		return a.Utf8Text(src)
	}
	return ""
}

// erlangEmitImport handles `-import(Mod, [Fn/Arity, ...]).` Adds the
// module itself and one `Mod:Fn` edge per selector — same shape as
// Julia's selected-import handling so cross-language graphs compose.
func erlangEmitImport(state *extractState, node *sitter.Node, src []byte) {
	mod := ""
	if a := firstChildOfKind(node, "atom"); a != nil {
		mod = a.Utf8Text(src)
		state.addImport(mod)
	}
	if mod == "" {
		return
	}
	// Each FA-pair entry is an `fa` node; its first atom child is the
	// function name. Walk all `fa` children directly under the import.
	n := node.ChildCount()
	for i := uint(0); i < n; i++ {
		c := node.Child(i)
		if c.Kind() != "fa" {
			continue
		}
		if fn := firstChildOfKind(c, "atom"); fn != nil {
			state.addImport(mod + ":" + fn.Utf8Text(src))
		}
	}
}

// erlangExternalFunName decomposes a `fun Mod:Fn/N` external_fun node
// into (module, name). Returns ("", "") if the shape is unrecognized.
func erlangExternalFunName(node *sitter.Node, src []byte) (string, string) {
	mod := erlangAtomChild(firstChildOfKind(node, "module"), src)
	// The function-name atom is a direct child of external_fun, after
	// the optional `module` qualifier; lastChildOfKind(...,"atom") would
	// pick the wrong atom if the module qualifier is present (its inner
	// atom is also a child by-DFS-order). We walk children directly and
	// take the LAST top-level atom.
	var fn string
	n := node.ChildCount()
	for i := uint(0); i < n; i++ {
		c := node.Child(i)
		if c.Kind() == "atom" {
			fn = c.Utf8Text(src)
		}
	}
	return mod, fn
}
