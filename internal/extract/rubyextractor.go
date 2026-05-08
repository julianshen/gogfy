package extract

import (
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_ruby "github.com/tree-sitter/tree-sitter-ruby/bindings/go"
)

// RubyExtractor extracts module/class/method nodes and require edges from
// Ruby sources.
type RubyExtractor struct{}

func (RubyExtractor) Extract(path string) (Result, error) {
	return runExtraction(path, tree_sitter_ruby.Language(), "ruby", walkRuby)
}

func walkRuby(cursor *sitter.TreeCursor, src []byte, state *extractState) {
	node := cursor.Node()
	switch node.Kind() {
	case "module":
		state.emitDecl("module", node, firstChildOfKind(node, "constant"), src)
	case "class":
		state.emitDecl("class", node, firstChildOfKind(node, "constant"), src)
	case "method":
		nameNode := node.ChildByFieldName("name")
		state.emitDecl("method", node, nameNode, src)
		state.pushFn(declID(state.lang, "method", state.filePath, nameNode, src))
		walkChildren(cursor, func() { walkRuby(cursor, src, state) })
		state.popFn()
		return
	case "call":
		// `require`/`require_relative` are method calls in Ruby's grammar;
		// route them to import edges and skip the calls-edge emission so a
		// require doesn't double as a `calls:require` edge.
		if target := rubyRequireTarget(node, src); target != "" {
			state.addImport(target)
		} else if methodNode := node.ChildByFieldName("method"); methodNode != nil {
			state.addCall(methodNode.Utf8Text(src))
		}
	}
	walkChildren(cursor, func() { walkRuby(cursor, src, state) })
}

// rubyRequireTarget returns the string argument of a `require`/`require_relative`
// call expression, or "" for any other call. Ruby's grammar represents requires
// as plain method calls rather than dedicated import statements.
func rubyRequireTarget(call *sitter.Node, src []byte) string {
	method := call.ChildByFieldName("method")
	if method == nil {
		return ""
	}
	if name := method.Utf8Text(src); name != "require" && name != "require_relative" {
		return ""
	}
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return ""
	}
	if str := firstChildOfKind(args, "string"); str != nil {
		return strings.Trim(str.Utf8Text(src), `"'`)
	}
	return ""
}
