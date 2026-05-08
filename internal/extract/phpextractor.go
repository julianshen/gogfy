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
		state.emitDecl("function", node, node.ChildByFieldName("name"), src)
	case "method_declaration":
		state.emitDecl("method", node, node.ChildByFieldName("name"), src)
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
