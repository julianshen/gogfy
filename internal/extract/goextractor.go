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

type extractState struct {
	pkgName string
	nodes   []schema.Node
	edges   []schema.Edge
}

func (g *GoExtractor) Extract(path string) (Result, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return Result{}, err
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return Result{}, err
	}

	parser := sitter.NewParser()
	parser.SetLanguage(golang.GetLanguage())
	tree := parser.Parse(nil, src)
	defer tree.Close()

	root := tree.RootNode()
	cursor := sitter.NewTreeCursor(root)
	defer cursor.Close()

	state := &extractState{}
	walk(cursor, src, absPath, state)

	return Result{Nodes: state.nodes, Edges: state.edges}, nil
}

func walk(cursor *sitter.TreeCursor, src []byte, filePath string, state *extractState) {
	node := cursor.CurrentNode()
	switch node.Type() {
	case "package_clause":
		nameNode := node.ChildByFieldName("name")
		if nameNode == nil {
			// Fallback: find package_identifier child
			for i := 0; i < int(node.ChildCount()); i++ {
				child := node.Child(i)
				if child.Type() == "package_identifier" {
					nameNode = child
					break
				}
			}
		}
		if nameNode == nil {
			break
		}
		state.pkgName = nameNode.Content(src)
		state.nodes = append(state.nodes, schema.Node{
			ID:             fmt.Sprintf("pkg:%s:%s", filePath, state.pkgName),
			Label:          state.pkgName,
			SourceFile:     filePath,
			SourceLocation: fmt.Sprintf("%d:%d", node.StartPoint().Row+1, node.StartPoint().Column+1),
		})
	case "function_declaration":
		nameNode := node.ChildByFieldName("name")
		funcName := ""
		if nameNode != nil {
			funcName = nameNode.Content(src)
		}
		label := funcName
		if label == "" {
			label = "<anonymous>"
		}
		state.nodes = append(state.nodes, schema.Node{
			ID:             fmt.Sprintf("fn:%s:%s.%s", filePath, state.pkgName, funcName),
			Label:          label,
			SourceFile:     filePath,
			SourceLocation: fmt.Sprintf("%d:%d", node.StartPoint().Row+1, node.StartPoint().Column+1),
		})
	case "import_spec":
		if state.pkgName == "" {
			break
		}
		pathNode := node.ChildByFieldName("path")
		if pathNode != nil {
			imp := strings.Trim(pathNode.Content(src), `"`)
			state.edges = append(state.edges, schema.Edge{
				Source:     fmt.Sprintf("pkg:%s:%s", filePath, state.pkgName),
				Target:     fmt.Sprintf("pkg:import:%s", imp),
				Relation:   "imports",
				Confidence: schema.Extracted,
			})
		}
	}

	if cursor.GoToFirstChild() {
		for {
			walk(cursor, src, filePath, state)
			if !cursor.GoToNextSibling() {
				break
			}
		}
		cursor.GoToParent()
	}
}
