package extract

import (
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_scala "github.com/tree-sitter/tree-sitter-scala/bindings/go"
)

// ScalaExtractor extracts module/class/object/trait/function nodes and import
// edges from Scala sources.
type ScalaExtractor struct{}

func (ScalaExtractor) Extract(path string) (Result, error) {
	return runExtraction(path, tree_sitter_scala.Language(), "scala", walkScala)
}

func walkScala(cursor *sitter.TreeCursor, src []byte, state *extractState) {
	node := cursor.Node()
	switch node.Kind() {
	case "import_declaration":
		// Scala dotted imports surface as a flat sequence of identifier/`.`
		// children rather than a wrapped path node — collect identifiers.
		var parts []string
		n := node.ChildCount()
		for i := uint(0); i < n; i++ {
			c := node.Child(i)
			if c.Kind() == "identifier" {
				parts = append(parts, c.Utf8Text(src))
			}
		}
		if len(parts) > 0 {
			state.addImport(strings.Join(parts, "."))
		}
	case "class_definition":
		state.emitDecl("class", node, firstChildOfKind(node, "identifier"), src)
	case "object_definition":
		state.emitDecl("object", node, firstChildOfKind(node, "identifier"), src)
	case "trait_definition":
		state.emitDecl("trait", node, firstChildOfKind(node, "identifier"), src)
	case "function_definition":
		state.emitDecl("function", node, firstChildOfKind(node, "identifier"), src)
	}
	walkChildren(cursor, func() { walkScala(cursor, src, state) })
}
