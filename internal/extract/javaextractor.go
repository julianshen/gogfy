package extract

import (
	"github.com/julianshen/gogfy/internal/schema"
	sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_java "github.com/tree-sitter/tree-sitter-java/bindings/go"
)

// JavaExtractor extracts module/class/method nodes and import edges from
// Java sources.
type JavaExtractor struct{}

type javaState struct {
	filePath string
	pkg      string
	nodes    []schema.Node
	edges    []schema.Edge
}

func (JavaExtractor) Extract(path string) (Result, error) {
	pf, err := parseFile(path, tree_sitter_java.Language())
	if err != nil {
		return Result{}, err
	}
	defer pf.cleanup()
	state := &javaState{filePath: pf.absPath}
	emitModule(&state.nodes, "java", state.filePath, pf.cursor.Node())
	walkJava(pf.cursor, pf.src, state)
	return Result{Nodes: state.nodes, Edges: state.edges}, nil
}

func walkJava(cursor *sitter.TreeCursor, src []byte, state *javaState) {
	node := cursor.Node()
	switch node.Kind() {
	case "package_declaration":
		// Use the scoped_identifier child (skip the "package" keyword and ";").
		for i := uint(0); i < node.ChildCount(); i++ {
			c := node.Child(i)
			if c.Kind() == "scoped_identifier" || c.Kind() == "identifier" {
				state.pkg = c.Utf8Text(src)
				break
			}
		}
	case "import_declaration":
		// Take the last identifier-bearing child as the import target.
		var target string
		for i := uint(0); i < node.ChildCount(); i++ {
			c := node.Child(i)
			if c.Kind() == "scoped_identifier" || c.Kind() == "identifier" {
				target = c.Utf8Text(src)
			}
		}
		if target != "" {
			addImportEdge(&state.nodes, &state.edges, "java", state.filePath, target)
		}
	case "class_declaration", "interface_declaration", "enum_declaration":
		kind := "class"
		if node.Kind() == "interface_declaration" {
			kind = "interface"
		} else if node.Kind() == "enum_declaration" {
			kind = "enum"
		}
		emitDecl(&state.nodes, "java", kind, state.filePath, node.ChildByFieldName("name"), node, src)
	case "method_declaration":
		emitDecl(&state.nodes, "java", "method", state.filePath, node.ChildByFieldName("name"), node, src)
	}
	walkChildren(cursor, func() { walkJava(cursor, src, state) })
}
