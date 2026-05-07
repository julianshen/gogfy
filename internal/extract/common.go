package extract

import (
	"path/filepath"

	"github.com/julianshen/gogfy/internal/schema"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// extractState is the per-file accumulator shared across all language
// extractors. The `lang` prefix is woven into every emitted ID so different
// languages don't collide on identical names.
type extractState struct {
	lang     string
	filePath string
	nodes    []schema.Node
	edges    []schema.Edge
}

// fileBase returns the file's basename, used as the default module-node label.
func fileBase(absPath string) string {
	return filepath.Base(absPath)
}

// emitModule appends the file-as-module node, used as the per-file root.
func (s *extractState) emitModule(root *sitter.Node) {
	s.nodes = append(s.nodes, schema.Node{
		ID:             schema.LangID(s.lang, "module", s.filePath),
		Label:          fileBase(s.filePath),
		SourceFile:     s.filePath,
		SourceLocation: nodeLocation(root),
	})
}

// emitDecl appends a "<lang>:<kind>:<filePath:name>" declaration node. Empty
// names get an "<anonymous>" label so schema.Node.Validate accepts them.
func (s *extractState) emitDecl(kind string, declNode, nameNode *sitter.Node, src []byte) {
	name := ""
	if nameNode != nil {
		name = nameNode.Utf8Text(src)
	}
	label := name
	if label == "" {
		label = "<anonymous>"
	}
	s.nodes = append(s.nodes, schema.Node{
		ID:             schema.LangID(s.lang, kind, s.filePath+":"+name),
		Label:          label,
		SourceFile:     s.filePath,
		SourceLocation: nodeLocation(declNode),
	})
}

// addImport appends an import-target node and an "imports" edge from the
// file's module node. Languages share this shape regardless of how their
// import statement is spelled.
func (s *extractState) addImport(target string) {
	s.nodes = append(s.nodes, schema.Node{
		ID:    schema.LangID(s.lang, "import", target),
		Label: target,
	})
	s.edges = append(s.edges, schema.Edge{
		Source:     schema.LangID(s.lang, "module", s.filePath),
		Target:     schema.LangID(s.lang, "import", target),
		Relation:   "imports",
		Confidence: schema.Extracted,
	})
}

// emitDataKey appends a "key" node + "contains" edge from the module. Used
// by data-file extractors (YAML, TOML) to surface document structure.
func (s *extractState) emitDataKey(key string, declNode *sitter.Node) {
	s.nodes = append(s.nodes, schema.Node{
		ID:             schema.LangID(s.lang, "key", s.filePath+":"+key),
		Label:          key,
		SourceFile:     s.filePath,
		SourceLocation: nodeLocation(declNode),
	})
	s.edges = append(s.edges, schema.Edge{
		Source:     schema.LangID(s.lang, "module", s.filePath),
		Target:     schema.LangID(s.lang, "key", s.filePath+":"+key),
		Relation:   "contains",
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
	n := source.ChildCount()
	for i := uint(0); i < n; i++ {
		c := source.Child(i)
		if c.Kind() == "string_fragment" {
			return c.Utf8Text(src)
		}
	}
	return ""
}

// firstChildOfKind returns the first direct child of node whose Kind matches
// any of the given kinds, or nil. Used by extractors that need to drill past
// punctuation/keyword children to reach the named token.
func firstChildOfKind(node *sitter.Node, kinds ...string) *sitter.Node {
	n := node.ChildCount()
	for i := uint(0); i < n; i++ {
		c := node.Child(i)
		k := c.Kind()
		for _, want := range kinds {
			if k == want {
				return c
			}
		}
	}
	return nil
}

// firstDescendantIdentifier does a BFS through node's descendants and returns
// the first `identifier` (or `type_identifier`) it finds. Used by grammars
// that wrap declaration names inside intermediate `signature`/`call_expression`
// subtrees rather than as direct children.
func firstDescendantIdentifier(node *sitter.Node) *sitter.Node {
	queue := []*sitter.Node{node}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		if n != node {
			k := n.Kind()
			if k == "identifier" || k == "type_identifier" {
				return n
			}
		}
		c := n.ChildCount()
		for i := uint(0); i < c; i++ {
			queue = append(queue, n.Child(i))
		}
	}
	return nil
}

// lastChildOfKind is firstChildOfKind's right-leaning cousin — returns the
// last matching child rather than the first. Java import_declaration uses it
// to pick the trailing scoped_identifier (skipping the `static` modifier).
func lastChildOfKind(node *sitter.Node, kinds ...string) *sitter.Node {
	var last *sitter.Node
	n := node.ChildCount()
	for i := uint(0); i < n; i++ {
		c := node.Child(i)
		k := c.Kind()
		for _, want := range kinds {
			if k == want {
				last = c
			}
		}
	}
	return last
}
