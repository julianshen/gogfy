// Package extract implements source-code extraction using tree-sitter.
package extract

import (
	"os"
	"path/filepath"

	"github.com/julianshen/gogfy/internal/schema"
	sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_python "github.com/tree-sitter/tree-sitter-python/bindings/go"
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
	defer parser.Close()
	if err := parser.SetLanguage(sitter.NewLanguage(tree_sitter_python.Language())); err != nil {
		return Result{}, err
	}
	tree := parser.Parse(src, nil)
	defer tree.Close()

	cursor := tree.Walk()
	defer cursor.Close()

	state := &pythonExtractState{moduleName: filepath.Base(absPath)}
	walkPython(cursor, src, absPath, state)

	return Result{Nodes: state.nodes, Edges: state.edges}, nil
}

func walkPython(cursor *sitter.TreeCursor, src []byte, filePath string, state *pythonExtractState) {
	node := cursor.Node()
	switch node.Kind() {
	case "module":
		state.nodes = append(state.nodes, schema.Node{
			ID:             schema.PythonModuleID(filePath),
			Label:          state.moduleName,
			SourceFile:     filePath,
			SourceLocation: schema.FormatLocation(uint32(node.StartPosition().Row), uint32(node.StartPosition().Column)),
		})
	case "import_statement":
		if state.moduleName == "" {
			break
		}
		for i := uint(0); i < node.ChildCount(); i++ {
			child := node.Child(i)
			switch child.Kind() {
			case "dotted_name":
				addPyImport(state, filePath, child.Utf8Text(src))
			case "aliased_import":
				for j := uint(0); j < child.ChildCount(); j++ {
					grandchild := child.Child(j)
					if grandchild.Kind() == "dotted_name" {
						addPyImport(state, filePath, grandchild.Utf8Text(src))
						break
					}
				}
			}
		}
	case "import_from_statement":
		if state.moduleName == "" {
			break
		}
		var moduleName string
		for i := uint(0); i < node.ChildCount(); i++ {
			child := node.Child(i)
			if child.Kind() != "dotted_name" {
				continue
			}
			if moduleName == "" {
				moduleName = child.Utf8Text(src)
			} else {
				addPyImport(state, filePath, moduleName+"."+child.Utf8Text(src))
			}
		}
	case "function_definition":
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
			ID:             schema.PythonFuncID(filePath, funcName),
			Label:          label,
			SourceFile:     filePath,
			SourceLocation: schema.FormatLocation(uint32(node.StartPosition().Row), uint32(node.StartPosition().Column)),
		})
	case "class_definition":
		nameNode := node.ChildByFieldName("name")
		className := ""
		if nameNode != nil {
			className = nameNode.Utf8Text(src)
		}
		state.nodes = append(state.nodes, schema.Node{
			ID:             schema.PythonClassID(filePath, className),
			Label:          className,
			SourceFile:     filePath,
			SourceLocation: schema.FormatLocation(uint32(node.StartPosition().Row), uint32(node.StartPosition().Column)),
		})
	}

	if cursor.GotoFirstChild() {
		for {
			walkPython(cursor, src, filePath, state)
			if !cursor.GotoNextSibling() {
				break
			}
		}
		cursor.GotoParent()
	}
}

func addPyImport(state *pythonExtractState, filePath, imp string) {
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
