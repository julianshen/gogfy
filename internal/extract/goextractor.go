package extract

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/julianshen/gogfy/internal/schema"
	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/golang"
)

// GoExtractor uses tree-sitter-go to extract package/function nodes
// and import/call edges.
type GoExtractor struct{}

func (g *GoExtractor) Extract(path string) (Result, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return Result{}, err
	}

	parser := sitter.NewParser()
	parser.SetLanguage(golang.GetLanguage())
	tree := parser.Parse(nil, src)
	defer tree.Close()

	root := tree.RootNode()
	absPath, _ := filepath.Abs(path)

	var nodes []schema.Node
	var edges []schema.Edge

	pkgName := ""
	cursor := sitter.NewTreeCursor(root)
	defer cursor.Close()

	walk(cursor, src, absPath, &pkgName, &nodes, &edges)

	return Result{Nodes: nodes, Edges: edges}, nil
}

func walk(cursor *sitter.TreeCursor, src []byte, filePath string, pkgName *string, nodes *[]schema.Node, edges *[]schema.Edge) {
	node := cursor.CurrentNode()
	switch node.Type() {
	case "package_clause":
		nameNode := node.ChildByFieldName("name")
		if nameNode == nil {
			break
		}
		*pkgName = strings.TrimSpace(nameNode.Content(src))
		*nodes = append(*nodes, schema.Node{
			ID:             fmt.Sprintf("pkg:%s:%s", filePath, *pkgName),
			Label:          *pkgName,
			SourceFile:     filePath,
			SourceLocation: fmt.Sprintf("%d:%d", node.StartPoint().Row+1, node.StartPoint().Column+1),
		})
	case "function_declaration":
		nameNode := node.ChildByFieldName("name")
		funcName := ""
		if nameNode != nil {
			funcName = nameNode.Content(src)
		}
		*nodes = append(*nodes, schema.Node{
			ID:             fmt.Sprintf("fn:%s:%s.%s", filePath, *pkgName, funcName),
			Label:          funcName,
			SourceFile:     filePath,
			SourceLocation: fmt.Sprintf("%d:%d", node.StartPoint().Row+1, node.StartPoint().Column+1),
		})
	case "import_spec":
		pathNode := node.ChildByFieldName("path")
		if pathNode != nil {
			imp := strings.Trim(pathNode.Content(src), `"`)
			*edges = append(*edges, schema.Edge{
				Source:     fmt.Sprintf("pkg:%s:%s", filePath, *pkgName),
				Target:     fmt.Sprintf("pkg:import:%s", imp),
				Relation:   "imports",
				Confidence: schema.Extracted,
			})
		}
	}

	if cursor.GoToFirstChild() {
		for {
			walk(cursor, src, filePath, pkgName, nodes, edges)
			if !cursor.GoToNextSibling() {
				break
			}
		}
		cursor.GoToParent()
	}
}
