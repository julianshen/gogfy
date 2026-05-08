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
	fnStack    []string
}

func (s *pythonExtractState) pushFn(id string) { s.fnStack = append(s.fnStack, id) }
func (s *pythonExtractState) popFn() {
	if len(s.fnStack) > 0 {
		s.fnStack = s.fnStack[:len(s.fnStack)-1]
	}
}

// callSource returns the innermost enclosing function ID, or the module
// node when the call is at top-level.
func (s *pythonExtractState) callSource(filePath string) string {
	if n := len(s.fnStack); n > 0 {
		return s.fnStack[n-1]
	}
	return schema.PythonModuleID(filePath)
}

func (s *pythonExtractState) addCall(filePath, callee string) {
	if callee == "" || s.moduleName == "" {
		return
	}
	target := schema.LangID("py", "call", callee)
	s.nodes = append(s.nodes, schema.Node{ID: target, Label: callee})
	s.edges = append(s.edges, schema.Edge{
		Source:     s.callSource(filePath),
		Target:     target,
		Relation:   "calls",
		Confidence: schema.Extracted,
	})
}

// Extract parses the Python source file at path and returns the extracted graph Result.
func (PythonExtractor) Extract(path string) (Result, error) {
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
			SourceLocation: nodeLocation(node),
		})
	case "import_statement":
		if state.moduleName == "" {
			break
		}
		n := node.ChildCount()
		for i := uint(0); i < n; i++ {
			child := node.Child(i)
			switch child.Kind() {
			case "dotted_name":
				addPyImport(state, filePath, child.Utf8Text(src))
			case "aliased_import":
				m := child.ChildCount()
				for j := uint(0); j < m; j++ {
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
		n := node.ChildCount()
		for i := uint(0); i < n; i++ {
			child := node.Child(i)
			switch child.Kind() {
			case "dotted_name":
				if moduleName == "" {
					moduleName = child.Utf8Text(src)
				} else {
					addPyImport(state, filePath, moduleName+"."+child.Utf8Text(src))
				}
			case "aliased_import":
				if moduleName == "" {
					continue
				}
				m := child.ChildCount()
				for j := uint(0); j < m; j++ {
					grandchild := child.Child(j)
					if grandchild.Kind() == "dotted_name" {
						addPyImport(state, filePath, moduleName+"."+grandchild.Utf8Text(src))
						break
					}
				}
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
		funcID := schema.PythonFuncID(filePath, funcName)
		state.nodes = append(state.nodes, schema.Node{
			ID:             funcID,
			Label:          label,
			SourceFile:     filePath,
			SourceLocation: nodeLocation(node),
		})
		state.pushFn(funcID)
		walkChildren(cursor, func() { walkPython(cursor, src, filePath, state) })
		state.popFn()
		return
	case "call":
		fn := node.ChildByFieldName("function")
		state.addCall(filePath, callTargetName(fn, src))
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
			SourceLocation: nodeLocation(node),
		})
	}

	walkChildren(cursor, func() { walkPython(cursor, src, filePath, state) })
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
