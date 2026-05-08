package extract

import (
	sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_php "github.com/tree-sitter/tree-sitter-php/bindings/go"
)

// PHPExtractor extracts module/class/function nodes and namespace-use edges
// from PHP sources.
type PHPExtractor struct{}

func (PHPExtractor) Extract(path string) (Result, error) {
	return runExtraction(path, tree_sitter_php.LanguagePHP(), "php", walkPHP)
}

func walkPHP(cursor *sitter.TreeCursor, src []byte, state *extractState) {
	node := cursor.Node()
	switch node.Kind() {
	case "namespace_use_declaration":
		emitPHPImports(state, node, src)
	case "class_declaration":
		state.emitDecl("class", node, node.ChildByFieldName("name"), src)
	case "interface_declaration":
		state.emitDecl("interface", node, node.ChildByFieldName("name"), src)
	case "trait_declaration":
		state.emitDecl("trait", node, node.ChildByFieldName("name"), src)
	case "enum_declaration":
		state.emitDecl("enum", node, node.ChildByFieldName("name"), src)
	case "function_definition":
		nameNode := node.ChildByFieldName("name")
		state.emitDecl("function", node, nameNode, src)
		state.walkFnScope("function", nameNode, src, cursor, walkPHP)
		return
	case "method_declaration":
		nameNode := node.ChildByFieldName("name")
		state.emitDecl("method", node, nameNode, src)
		state.walkFnScope("method", nameNode, src, cursor, walkPHP)
		return
	case "function_call_expression":
		// callTargetName already handles the `name` kind for the bare-callee
		// case; for member-style `$obj->foo()` PHP uses `member_call_expression`
		// which has a `name` field for the method name.
		state.addCall(callTargetName(node.ChildByFieldName("function"), src))
	case "member_call_expression":
		state.addCall(callTargetName(node.ChildByFieldName("name"), src))
	case "scoped_call_expression":
		// `Foo::bar()` — class is the first `name` child, method is the
		// last. Source the call edge against the method, dropping the
		// class context (consistent with how member calls drop the
		// receiver across all extractors).
		state.addCall(callTargetName(lastChildOfKind(node, "name"), src))
	}
	walkChildren(cursor, func() { walkPHP(cursor, src, state) })
}

// emitPHPImports handles PHP's three import-statement shapes:
//
//	use App\Lib\Foo;             // single namespace_use_clause
//	use App\Lib\Foo, App\Lib\Bar; // multiple namespace_use_clause children
//	use App\Lib\{Foo, Bar};       // namespace_use_group prefixed by namespace_name
//
// Group-use puts a leading namespace_name as a sibling of namespace_use_group;
// each clause inside the group is just a leaf name to be joined onto the prefix.
func emitPHPImports(state *extractState, node *sitter.Node, src []byte) {
	var groupPrefix string
	if pre := firstChildOfKind(node, "namespace_name"); pre != nil {
		groupPrefix = pre.Utf8Text(src)
	}
	if group := firstChildOfKind(node, "namespace_use_group"); group != nil {
		m := group.ChildCount()
		for i := uint(0); i < m; i++ {
			c := group.Child(i)
			if c.Kind() != "namespace_use_clause" {
				continue
			}
			if name := firstChildOfKind(c, "qualified_name", "name"); name != nil {
				leaf := name.Utf8Text(src)
				if groupPrefix == "" {
					state.addImport(leaf)
				} else {
					state.addImport(groupPrefix + `\` + leaf)
				}
			}
		}
		return
	}
	n := node.ChildCount()
	for i := uint(0); i < n; i++ {
		c := node.Child(i)
		if c.Kind() != "namespace_use_clause" {
			continue
		}
		if name := firstChildOfKind(c, "qualified_name", "name"); name != nil {
			state.addImport(name.Utf8Text(src))
		}
	}
}
