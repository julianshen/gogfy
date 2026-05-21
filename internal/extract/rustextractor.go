package extract

import (
	sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_rust "github.com/tree-sitter/tree-sitter-rust/bindings/go"
)

// RustExtractor extracts module/struct/function/trait/impl nodes and use-edges
// from Rust sources.
type RustExtractor struct{}

func (RustExtractor) Extract(path string) (Result, error) {
	return runExtraction(path, tree_sitter_rust.Language(), "rust", walkRust)
}

func walkRust(cursor *sitter.TreeCursor, src []byte, state *extractState) {
	node := cursor.Node()
	switch node.Kind() {
	case "use_declaration":
		emitRustUseTargets(state, node, src)
	case "function_item":
		nameNode := node.ChildByFieldName("name")
		state.emitDecl("function", node, nameNode, src)
		state.walkFnScope("function", nameNode, src, cursor, walkRust)
		return
	case "closure_expression":
		state.walkAnonFnScope("function", node, src, cursor, walkRust)
		return
	case "call_expression":
		state.addCall(callTargetName(node.ChildByFieldName("function"), src))
	case "macro_invocation":
		// Macros like `println!("...")` are very common; treat them as calls
		// so they show up in the graph. The macro name is the first
		// identifier or scoped_identifier child.
		if id := firstChildOfKind(node, "identifier", "scoped_identifier"); id != nil {
			state.addCall(id.Utf8Text(src) + "!")
		}
	case "impl_item":
		// `impl Trait for Type` → Type implements Trait. Both endpoints are
		// named here, not declared, so resolve.Inheritance resolves both.
		// `impl Type {}` (inherent impl, no trait) produces no edge.
		if tr := node.ChildByFieldName("trait"); tr != nil {
			if ty := node.ChildByFieldName("type"); ty != nil {
				state.addInheritsByName(simpleTypeName(ty.Utf8Text(src)), simpleTypeName(tr.Utf8Text(src)), "implements")
			}
		}
	case "struct_item":
		state.emitDecl("struct", node, node.ChildByFieldName("name"), src)
	case "enum_item":
		state.emitDecl("enum", node, node.ChildByFieldName("name"), src)
	case "trait_item":
		state.emitDecl("trait", node, node.ChildByFieldName("name"), src)
	case "mod_item":
		state.emitDecl("mod", node, node.ChildByFieldName("name"), src)
	}
	walkChildren(cursor, func() { walkRust(cursor, src, state) })
}

// emitRustUseTargets handles the four shapes a use_declaration takes in the
// Rust grammar:
//
//	use foo::bar;                    // scoped_identifier or identifier child
//	use foo::{bar, baz};             // scoped_use_list: path + use_list
//	use foo as bar;                  // use_as_clause: emit the original path
//	use foo::*;                      // use_wildcard: emit the path
//
// The first form matches the original implementation; the other three were
// silently producing zero edges before this fix.
func emitRustUseTargets(state *extractState, node *sitter.Node, src []byte) {
	for i := uint(0); i < node.ChildCount(); i++ {
		c := node.Child(i)
		switch c.Kind() {
		case "identifier", "scoped_identifier":
			state.addImport(c.Utf8Text(src))
		case "use_as_clause":
			// First identifier-bearing child is the original path; the alias
			// follows `as` and is discarded for the import edge.
			if id := firstChildOfKind(c, "scoped_identifier", "identifier"); id != nil {
				state.addImport(id.Utf8Text(src))
			}
		case "use_wildcard":
			if id := firstChildOfKind(c, "scoped_identifier", "identifier"); id != nil {
				state.addImport(id.Utf8Text(src))
			}
		case "scoped_use_list":
			emitRustUseList(state, c, src)
		}
	}
}

// emitRustUseList expands `use prefix::{a, b, c};` into one import edge per
// leaf, joined to the prefix as `prefix::a`, `prefix::b`, etc.
func emitRustUseList(state *extractState, node *sitter.Node, src []byte) {
	prefix := ""
	if id := firstChildOfKind(node, "scoped_identifier", "identifier"); id != nil {
		prefix = id.Utf8Text(src)
	}
	list := firstChildOfKind(node, "use_list")
	if list == nil {
		return
	}
	for i := uint(0); i < list.ChildCount(); i++ {
		c := list.Child(i)
		if c.Kind() != "identifier" && c.Kind() != "scoped_identifier" {
			continue
		}
		leaf := c.Utf8Text(src)
		if prefix == "" {
			state.addImport(leaf)
		} else {
			state.addImport(prefix + "::" + leaf)
		}
	}
}
