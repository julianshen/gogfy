// Package extract implements source-code extraction using tree-sitter.
package extract

import (
	sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_python "github.com/tree-sitter/tree-sitter-python/bindings/go"
)

// PythonExtractor uses tree-sitter-python to extract module/class/function
// nodes, import edges, and call edges from Python sources.
type PythonExtractor struct{}

func (PythonExtractor) Extract(path string) (Result, error) {
	return runExtraction(path, tree_sitter_python.Language(), "py", walkPython)
}

// walkPython emits one module node per file (label is the file's basename),
// function/class nodes for each definition, import edges from the module
// to each imported name, and call edges from the innermost enclosing
// function/lambda (or the module for top-level calls) to a synthetic
// `py:call:<callee>` target.
func walkPython(cursor *sitter.TreeCursor, src []byte, state *extractState) {
	node := cursor.Node()
	switch node.Kind() {
	case "import_statement":
		emitPyImports(state, node, src)
	case "import_from_statement":
		emitPyFromImports(state, node, src)
	case "function_definition":
		nameNode := node.ChildByFieldName("name")
		state.emitDecl("function", node, nameNode, src)
		state.walkFnScope("function", nameNode, src, cursor, walkPython)
		return
	case "lambda":
		state.walkAnonFnScope("function", node, src, cursor, walkPython)
		return
	case "call":
		state.addCall(callTargetName(node.ChildByFieldName("function"), src))
	case "class_definition":
		state.emitDecl("class", node, node.ChildByFieldName("name"), src)
	}
	walkChildren(cursor, func() { walkPython(cursor, src, state) })
}

// emitPyImports handles `import a` / `import a as b` / `import a, b`
// statements. Each top-level identifier (or aliased_import's wrapped name)
// becomes one import edge.
func emitPyImports(state *extractState, node *sitter.Node, src []byte) {
	n := node.ChildCount()
	for i := uint(0); i < n; i++ {
		child := node.Child(i)
		switch child.Kind() {
		case "dotted_name":
			state.addImport(child.Utf8Text(src))
		case "aliased_import":
			if d := firstChildOfKind(child, "dotted_name"); d != nil {
				state.addImport(d.Utf8Text(src))
			}
		}
	}
}

// emitPyFromImports handles `from M import a, b as c, ...` shapes. The first
// dotted_name is the module; subsequent ones (or aliased_import wrappers)
// each emit a `M.name` edge.
func emitPyFromImports(state *extractState, node *sitter.Node, src []byte) {
	var moduleName string
	n := node.ChildCount()
	for i := uint(0); i < n; i++ {
		child := node.Child(i)
		switch child.Kind() {
		case "dotted_name":
			if moduleName == "" {
				moduleName = child.Utf8Text(src)
			} else {
				state.addImport(moduleName + "." + child.Utf8Text(src))
			}
		case "aliased_import":
			if moduleName == "" {
				continue
			}
			if d := firstChildOfKind(child, "dotted_name"); d != nil {
				state.addImport(moduleName + "." + d.Utf8Text(src))
			}
		}
	}
}
