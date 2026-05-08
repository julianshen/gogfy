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

type goExtractState struct {
	pkgName     string
	nodes       []schema.Node
	edges       []schema.Edge
	fnStack     []string // enclosing function IDs; top is the current scope
	callTargets map[string]struct{}
}

func (s *goExtractState) pushFn(id string) { s.fnStack = append(s.fnStack, id) }
func (s *goExtractState) popFn() {
	if len(s.fnStack) > 0 {
		s.fnStack = s.fnStack[:len(s.fnStack)-1]
	}
}

// callSource returns the innermost enclosing function ID, or the package
// node when the call is at top-level (init expressions, top-level vars).
func (s *goExtractState) callSource(filePath string) string {
	if n := len(s.fnStack); n > 0 {
		return s.fnStack[n-1]
	}
	return schema.PackageID(filePath, s.pkgName)
}

// addCall emits a call edge from the current scope to a synthetic
// "go:call:<callee>" target. Repeated calls to the same callee within a
// file emit one target node and N edges. Cross-file resolution of
// callee → real function node is left to the analyze layer.
func (s *goExtractState) addCall(filePath, callee string) {
	if callee == "" || s.pkgName == "" {
		return
	}
	target := schema.LangID("go", "call", callee)
	if s.callTargets == nil {
		s.callTargets = map[string]struct{}{}
	}
	if _, seen := s.callTargets[target]; !seen {
		s.callTargets[target] = struct{}{}
		s.nodes = append(s.nodes, schema.Node{ID: target, Label: callee})
	}
	s.edges = append(s.edges, schema.Edge{
		Source:     s.callSource(filePath),
		Target:     target,
		Relation:   "calls",
		Confidence: schema.Extracted,
	})
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
	warnIfParseError(absPath, tree)

	cursor := tree.Walk()
	defer cursor.Close()

	state := &goExtractState{}
	walk(cursor, src, absPath, state)

	return Result{Nodes: state.nodes, Edges: state.edges}, nil
}

func walk(cursor *sitter.TreeCursor, src []byte, filePath string, state *goExtractState) {
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
	case "function_declaration", "method_declaration":
		nameNode := node.ChildByFieldName("name")
		funcName := ""
		if nameNode != nil {
			funcName = nameNode.Utf8Text(src)
		}
		label := funcName
		if label == "" {
			label = "<anonymous>"
		}
		funcID := schema.FuncID(filePath, state.pkgName, funcName)
		state.nodes = append(state.nodes, schema.Node{
			ID:             funcID,
			Label:          label,
			SourceFile:     filePath,
			SourceLocation: nodeLocation(node),
		})
		state.pushFn(funcID)
		walkChildren(cursor, func() { walk(cursor, src, filePath, state) })
		state.popFn()
		return
	case "call_expression":
		fn := node.ChildByFieldName("function")
		state.addCall(filePath, callTargetName(fn, src))
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
