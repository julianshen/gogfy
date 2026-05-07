package extract

import (
	"github.com/julianshen/gogfy/internal/schema"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// nodeLocation formats a node's start position, calling StartPosition only once.
func nodeLocation(n *sitter.Node) string {
	p := n.StartPosition()
	return schema.FormatLocation(p.Row, p.Column)
}

// walkChildren visits each child of the cursor's current node and restores
// the cursor to that node before returning. visit MUST leave the cursor on
// the same node it was called with — moving the cursor inside visit will
// silently desync the iteration.
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
