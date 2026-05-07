package extract

import (
	"strings"

	"github.com/julianshen/gogfy/internal/schema"
	sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_ruby "github.com/tree-sitter/tree-sitter-ruby/bindings/go"
)

// RubyExtractor extracts module/class/method nodes and require edges from
// Ruby sources.
type RubyExtractor struct{}

type rubyState struct {
	filePath string
	nodes    []schema.Node
	edges    []schema.Edge
}

func (RubyExtractor) Extract(path string) (Result, error) {
	pf, err := parseFile(path, tree_sitter_ruby.Language())
	if err != nil {
		return Result{}, err
	}
	defer pf.cleanup()
	state := &rubyState{filePath: pf.absPath}
	emitModule(&state.nodes, "ruby", state.filePath, pf.cursor.Node())
	walkRuby(pf.cursor, pf.src, state)
	return Result{Nodes: state.nodes, Edges: state.edges}, nil
}

func walkRuby(cursor *sitter.TreeCursor, src []byte, state *rubyState) {
	node := cursor.Node()
	switch node.Kind() {
	case "module":
		emitDecl(&state.nodes, "ruby", "module", state.filePath, rubyName(node), node, src)
	case "class":
		emitDecl(&state.nodes, "ruby", "class", state.filePath, rubyName(node), node, src)
	case "method":
		emitDecl(&state.nodes, "ruby", "method", state.filePath, node.ChildByFieldName("name"), node, src)
	case "call":
		// `require 'x'` and `require_relative 'x'` are method calls in Ruby's grammar.
		method := node.ChildByFieldName("method")
		if method == nil {
			break
		}
		name := method.Utf8Text(src)
		if name != "require" && name != "require_relative" {
			break
		}
		args := node.ChildByFieldName("arguments")
		if args == nil {
			break
		}
		for i := uint(0); i < args.ChildCount(); i++ {
			c := args.Child(i)
			if c.Kind() == "string" {
				addImportEdge(&state.nodes, &state.edges, "ruby", state.filePath, strings.Trim(c.Utf8Text(src), `"'`))
			}
		}
	}
	walkChildren(cursor, func() { walkRuby(cursor, src, state) })
}

// rubyName returns the constant child of a module/class node (the type name).
func rubyName(node *sitter.Node) *sitter.Node {
	for i := uint(0); i < node.ChildCount(); i++ {
		c := node.Child(i)
		if c.Kind() == "constant" {
			return c
		}
	}
	return nil
}
