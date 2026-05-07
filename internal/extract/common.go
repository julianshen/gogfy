package extract

import (
	"path/filepath"

	"github.com/julianshen/gogfy/internal/schema"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// fileBase returns the file's basename, used as a default module-node label.
func fileBase(absPath string) string {
	return filepath.Base(absPath)
}

// addImportEdge appends a node+edge pair representing an import relationship
// from the file's module node to the given target name. Languages share the
// same shape: a "<lang>:import:<name>" target node and an "imports" edge.
func addImportEdge(nodes *[]schema.Node, edges *[]schema.Edge, lang, filePath, target string) {
	*nodes = append(*nodes, schema.Node{
		ID:    schema.LangID(lang, "import", target),
		Label: target,
	})
	*edges = append(*edges, schema.Edge{
		Source:     schema.LangID(lang, "module", filePath),
		Target:     schema.LangID(lang, "import", target),
		Relation:   "imports",
		Confidence: schema.Extracted,
	})
}

// importStringSource returns the unquoted string text from an import_statement's
// `source` field. Used by JS/TS, where the source path is the only string child.
func importStringSource(node *sitter.Node, src []byte) string {
	source := node.ChildByFieldName("source")
	if source == nil {
		return ""
	}
	for i := uint(0); i < source.ChildCount(); i++ {
		c := source.Child(i)
		if c.Kind() == "string_fragment" {
			return c.Utf8Text(src)
		}
	}
	return ""
}

// emitDecl appends a "declaration" node (function/class/struct/etc.) using
// the shared <lang>:<kind>:<filePath:name> ID scheme. Empty names get an
// "<anonymous>" label.
func emitDecl(nodes *[]schema.Node, lang, kind, filePath string, nameNode *sitter.Node, declNode *sitter.Node, src []byte) {
	name := ""
	if nameNode != nil {
		name = nameNode.Utf8Text(src)
	}
	label := name
	if label == "" {
		label = "<anonymous>"
	}
	*nodes = append(*nodes, schema.Node{
		ID:             schema.LangID(lang, kind, filePath+":"+name),
		Label:          label,
		SourceFile:     filePath,
		SourceLocation: nodeLocation(declNode),
	})
}

// emitModule appends the file-as-module node for a language. Most extractors
// emit one of these as the root of the per-file subgraph.
func emitModule(nodes *[]schema.Node, lang, filePath string, root *sitter.Node) {
	*nodes = append(*nodes, schema.Node{
		ID:             schema.LangID(lang, "module", filePath),
		Label:          fileBase(filePath),
		SourceFile:     filePath,
		SourceLocation: nodeLocation(root),
	})
}
