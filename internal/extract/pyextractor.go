// Package extract implements source-code extraction using tree-sitter.
package extract

import (
	"os"
	"path/filepath"

	"github.com/julianshen/gogfy/internal/schema"
	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/python"
)

// PythonExtractor uses tree-sitter-python to extract module/class/function nodes
// and import edges.
type PythonExtractor struct{}

type pythonExtractState struct {
	moduleName string
	nodes      []schema.Node
	edges      []schema.Edge
}

// Extract parses the Python source file at path and returns the extracted graph Result.
func (pe *PythonExtractor) Extract(path string) (Result, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return Result{}, err
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return Result{}, err
	}

	parser := sitter.NewParser()
	parser.SetLanguage(python.GetLanguage())
	tree := parser.Parse(nil, src)
	defer tree.Close()

	root := tree.RootNode()
	cursor := sitter.NewTreeCursor(root)
	defer cursor.Close()

	state := &pythonExtractState{moduleName: filepath.Base(absPath)}
	walkPython(cursor, src, absPath, state)

	return Result{Nodes: state.nodes, Edges: state.edges}, nil
}

func walkPython(cursor *sitter.TreeCursor, src []byte, filePath string, state *pythonExtractState) {
	node := cursor.CurrentNode()
	switch node.Type() {
	case "module":
		// Module node - add as a package-like node
		state.nodes = append(state.nodes, schema.Node{
			ID:             schema.PythonModuleID(filePath),
			Label:          state.moduleName,
			SourceFile:     filePath,
			SourceLocation: schema.FormatLocation(node.StartPoint().Row, node.StartPoint().Column),
		})
	case "import_statement":
		// import x or import x as y
		if state.moduleName == "" {
			break
		}
		for i := 0; i < int(node.ChildCount()); i++ {
			child := node.Child(i)
			if child.Type() == "dotted_name" {
				imp := child.Content(src)
				state.nodes = append(state.nodes, schema.Node{
					ID:    schema.PythonImportID(imp),
					Label: imp,
				})
				state.edges = append(state.edges, schema.Edge{
					Source:     schema.PythonModuleID(filePath),
					Target:     schema.PythonImportID(imp),
					Relation:   "imports",
					Confidence: schema.Extracted,
				})
			} else if child.Type() == "aliased_import" {
				// aliased_import contains a dotted_name child
				for j := 0; j < int(child.ChildCount()); j++ {
					grandchild := child.Child(j)
					if grandchild.Type() == "dotted_name" {
						imp := grandchild.Content(src)
						state.nodes = append(state.nodes, schema.Node{
							ID:    schema.PythonImportID(imp),
							Label: imp,
						})
						state.edges = append(state.edges, schema.Edge{
							Source:     schema.PythonModuleID(filePath),
							Target:     schema.PythonImportID(imp),
							Relation:   "imports",
							Confidence: schema.Extracted,
						})
						break
					}
				}
			}
		}
	case "import_from_statement":
		// from x import y
		if state.moduleName == "" {
			break
		}
		var moduleName string
		for i := 0; i < int(node.ChildCount()); i++ {
			child := node.Child(i)
			if child.Type() == "dotted_name" && moduleName == "" {
				moduleName = child.Content(src)
			} else if child.Type() == "dotted_name" && moduleName != "" {
				// This is the imported name
				imp := moduleName + "." + child.Content(src)
				state.nodes = append(state.nodes, schema.Node{
					ID:    schema.PythonImportID(imp),
					Label: imp,
				})
				state.edges = append(state.edges, schema.Edge{
					Source:     schema.PythonModuleID(filePath),
					Target:     schema.PythonImportID(imp),
					Relation:   "imports",
					Confidence: schema.Extracted,
				})
			}
		}
	case "function_definition":
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
			ID:             schema.PythonFuncID(filePath, funcName),
			Label:          label,
			SourceFile:     filePath,
			SourceLocation: schema.FormatLocation(node.StartPoint().Row, node.StartPoint().Column),
		})
	case "class_definition":
		nameNode := node.ChildByFieldName("name")
		className := ""
		if nameNode != nil {
			className = nameNode.Content(src)
		}
		state.nodes = append(state.nodes, schema.Node{
			ID:             schema.PythonClassID(filePath, className),
			Label:          className,
			SourceFile:     filePath,
			SourceLocation: schema.FormatLocation(node.StartPoint().Row, node.StartPoint().Column),
		})
	}

	if cursor.GoToFirstChild() {
		for {
			walkPython(cursor, src, filePath, state)
			if !cursor.GoToNextSibling() {
				break
			}
		}
		cursor.GoToParent()
	}
}
