package extract

import (
	"github.com/julianshen/gogfy/internal/schema"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// nodeLocation formats a node's start position. Hoisting StartPosition() into
// a single call avoids two CGO crossings per emitted node.
func nodeLocation(n *sitter.Node) string {
	p := n.StartPosition()
	return schema.FormatLocation(p.Row, p.Column)
}

// walkChildren recurses into the cursor's current node's children, invoking
// visit at each one, then restores the cursor to the original node.
func walkChildren(cursor *sitter.TreeCursor, visit func()) {
	if !cursor.GotoFirstChild() {
		return
	}
	for {
		visit()
		if !cursor.GotoNextSibling() {
			break
		}
	}
	cursor.GotoParent()
}
