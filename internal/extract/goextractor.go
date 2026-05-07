// Package extract implements source-code extraction using tree-sitter.
package extract

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/julianshen/gogfy/internal/schema"
	sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_go "github.com/tree-sitter/tree-sitter-go/bindings/go"
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
func (GoExtractor) Extract(path string) (Result, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return Result{}, err
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return Result{}, err
	}

	parser := sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(sitter.NewLanguage(tree_sitter_go.Language())); err != nil {
		return Result{}, err
	}
	tree := parser.Parse(src, nil)
	defer tree.Close()

	cursor := tree.Walk()
	defer cursor.Close()

	state := &extractState{}
	walk(cursor, src, absPath, state)

	return Result{Nodes: state.nodes, Edges: state.edges}, nil
}

func walk(cursor *sitter.TreeCursor, src []byte, filePath string, state *extractState) {
	node := cursor.Node()
	switch node.Kind() {
	case "package_clause":
		nameNode := node.ChildByFieldName("name")
		if nameNode == nil {
			n := node.ChildCount()
			for i := uint(0); i < n; i++ {
				child := node.Child(i)
				if child.Kind() == "package_identifier" {
					nameNode = child
					break
				}
			}
		}
		if nameNode == nil {
			break
		}
		state.pkgName = nameNode.Utf8Text(src)
		state.nodes = append(state.nodes, schema.Node{
			ID:             schema.PackageID(filePath, state.pkgName),
			Label:          state.pkgName,
			SourceFile:     filePath,
			SourceLocation: nodeLocation(node),
		})
	case "function_declaration":
		nameNode := node.ChildByFieldName("name")
		funcName := ""
		if nameNode != nil {
			funcName = nameNode.Utf8Text(src)
		}
		label := funcName
		if label == "" {
			label = "<anonymous>"
		}
		state.nodes = append(state.nodes, schema.Node{
			ID:             schema.FuncID(filePath, state.pkgName, funcName),
			Label:          label,
			SourceFile:     filePath,
			SourceLocation: nodeLocation(node),
		})
	case "import_spec":
		if state.pkgName == "" {
			break
		}
		pathNode := node.ChildByFieldName("path")
		if pathNode != nil {
			imp := strings.Trim(pathNode.Utf8Text(src), `"`)
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

	walkChildren(cursor, func() { walk(cursor, src, filePath, state) })
}
