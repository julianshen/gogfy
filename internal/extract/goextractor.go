// Package extract implements source-code extraction using tree-sitter.
package extract

import (
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

// Extract parses the Go source file at path and returns the extracted graph Result.
func (ge *GoExtractor) Extract(path string) (Result, error) {
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
			ID:             schema.PackageID(filePath, state.pkgName),
			Label:          state.pkgName,
			SourceFile:     filePath,
			SourceLocation: schema.FormatLocation(node.StartPoint().Row, node.StartPoint().Column),
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
			ID:             schema.FuncID(filePath, state.pkgName, funcName),
			Label:          label,
			SourceFile:     filePath,
			SourceLocation: schema.FormatLocation(node.StartPoint().Row, node.StartPoint().Column),
		})
	case "import_spec":
		if state.pkgName == "" {
			break
		}
		pathNode := node.ChildByFieldName("path")
		if pathNode != nil {
			imp := strings.Trim(pathNode.Content(src), `"`)
			state.nodes = append(state.nodes, schema.Node{
				ID:    schema.ImportID(imp),
				Label: imp,
			})
			state.edges = append(state.edges, schema.Edge{
				Source:     schema.PackageID(filePath, state.pkgName),
				Target:     schema.ImportID(imp),
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
