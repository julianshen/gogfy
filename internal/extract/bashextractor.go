package extract

import (
	sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_bash "github.com/tree-sitter/tree-sitter-bash/bindings/go"
)

// BashExtractor extracts module/function nodes and source/dot edges from
// shell scripts.
type BashExtractor struct{}

func (BashExtractor) Extract(path string) (Result, error) {
	return runExtraction(path, tree_sitter_bash.Language(), "bash", walkBash)
}

func walkBash(cursor *sitter.TreeCursor, src []byte, state *extractState) {
	node := cursor.Node()
	switch node.Kind() {
	case "function_definition":
		// Function name is the first `word` child; `function bye { … }` and
		// `bye() { … }` both surface as function_definition with leading word.
		state.emitDecl("function", node, firstChildOfKind(node, "word"), src)
	case "command":
		// `source ./lib.sh` and `. ./other.sh` are commands whose first child
		// is `command_name`. The argument that follows is the imported path.
		name := firstChildOfKind(node, "command_name")
		if name == nil {
			break
		}
		nameText := name.Utf8Text(src)
		if nameText != "source" && nameText != "." {
			break
		}
		// Pick the first `word` after command_name.
		n := node.ChildCount()
		for i := uint(0); i < n; i++ {
			c := node.Child(i)
			if c.Kind() == "word" {
				state.addImport(c.Utf8Text(src))
				break
			}
		}
	}
	walkChildren(cursor, func() { walkBash(cursor, src, state) })
}
