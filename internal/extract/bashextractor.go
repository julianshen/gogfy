package extract

import (
	"strings"

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
		// `source ./lib.sh`, `source "lib.sh"`, `. 'other.sh'` are all
		// commands whose first child is `command_name`. The first `word`,
		// `string`, or `raw_string` sibling is the imported path.
		name := firstChildOfKind(node, "command_name")
		if name == nil {
			break
		}
		if t := name.Utf8Text(src); t != "source" && t != "." {
			break
		}
		if target := bashSourceTarget(node, src); target != "" {
			state.addImport(target)
		}
	}
	walkChildren(cursor, func() { walkBash(cursor, src, state) })
}

// bashSourceTarget returns the imported path from a `source`/`.` command's
// first argument node. Quoted forms (`source "lib.sh"`) wrap the path in a
// `string` whose `string_content` child holds the literal; single-quoted
// forms surface as a `raw_string` containing the quotes verbatim.
func bashSourceTarget(cmd *sitter.Node, src []byte) string {
	n := cmd.ChildCount()
	for i := uint(0); i < n; i++ {
		c := cmd.Child(i)
		switch c.Kind() {
		case "word":
			return c.Utf8Text(src)
		case "string":
			if content := firstChildOfKind(c, "string_content"); content != nil {
				return content.Utf8Text(src)
			}
			return strings.Trim(c.Utf8Text(src), `"`)
		case "raw_string":
			return strings.Trim(c.Utf8Text(src), `'`)
		}
	}
	return ""
}
