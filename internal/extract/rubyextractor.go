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
	pf, err := parseFile(path, tree_sitter_ruby.Language())
	if err != nil {
		return Result{}, err
	}
	defer pf.cleanup()
	state := &extractState{lang: "ruby", filePath: pf.absPath}
	state.emitModule(pf.cursor.Node())
	walkRuby(pf.cursor, pf.src, state)
	return Result{Nodes: state.nodes, Edges: state.edges}, nil
}

func walkRuby(cursor *sitter.TreeCursor, src []byte, state *extractState) {
	node := cursor.Node()
	switch node.Kind() {
	case "module":
		state.emitDecl("module", node, firstChildOfKind(node, "constant"), src)
	case "class":
		state.emitDecl("class", node, firstChildOfKind(node, "constant"), src)
	case "method":
		state.emitDecl("method", node, node.ChildByFieldName("name"), src)
	case "call":
		if target := rubyRequireTarget(node, src); target != "" {
			state.addImport(target)
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
	switch method.Utf8Text(src) {
	case "require", "require_relative":
	default:
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
