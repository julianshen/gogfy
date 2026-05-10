package extract

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/julianshen/gogfy/internal/schema"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// MarkdownExtractor turns Markdown files into graph nodes and edges using
// the goldmark parser (pure-Go; no cgo, no external runtime). This is the
// first step toward graphify-style document extraction without taking on
// markitdown's Python dependency.
//
// Schema:
//
//   - Document node, kind=module, label = first H1 text or basename.
//   - One section node per H1/H2/H3 heading (kind=section). Deeper headings
//     fold into their nearest H1-3 ancestor section to avoid noise.
//   - Import-relation edges (target prefix `markdown:link:`) for every
//     [text](url) link, sourced from the enclosing section (or the
//     document if no section yet).
//
// Limitations (deliberate, for a first cut):
//
//   - Reference-style links (`[text][ref]` + `[ref]: url`) are resolved by
//     goldmark before our walk, so the AST presents them as plain links —
//     no separate handling needed.
//   - Setext-style headings (underline form) are matched the same as ATX
//     by goldmark; no special case.
//   - Code-fence languages aren't surfaced as nodes yet; future work.
type MarkdownExtractor struct{}

func (MarkdownExtractor) Extract(path string) (Result, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Result{}, err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return Result{}, err
	}
	parser := goldmark.DefaultParser()
	root := parser.Parse(text.NewReader(data))

	state := &extractState{lang: "markdown", filePath: abs}
	moduleID := schema.LangID("markdown", "module", abs)
	moduleLabel := filepath.Base(abs)

	// Pre-scan for the first H1 to use as the doc's display label.
	// Doing this before the main walk keeps the module node's label
	// stable regardless of where we emit it.
	if h := firstH1Text(root, data); h != "" {
		moduleLabel = h
	}

	state.nodes = append(state.nodes, schema.Node{
		ID:    moduleID,
		Label: moduleLabel,
	})
	state.fnStack = append(state.fnStack, moduleID)

	// Walk the AST. Heading nodes push a new "scope" (treated like fn
	// scope — their ID becomes the source of any link edges emitted while
	// inside the section). Headings of level >3 don't push (too noisy).
	walkMarkdown(root, data, abs, state)
	return Result{Nodes: state.nodes, Edges: state.edges}, nil
}

func walkMarkdown(node ast.Node, src []byte, path string, state *extractState) {
	switch n := node.(type) {
	case *ast.Heading:
		if n.Level >= 1 && n.Level <= 3 {
			label := nodeText(node, src)
			id := nextSectionID(state, "markdown", path, label)
			state.nodes = append(state.nodes, schema.Node{
				ID:         id,
				Label:      label,
				SourceFile: path,
			})
			// Pop any deeper section, push this one. Heading levels are
			// monotonic-ish in practice; we don't need a precise stack
			// since "innermost section" is what each link wants.
			state.fnStack = []string{state.fnStack[0]} // reset to module
			state.fnStack = append(state.fnStack, id)
		}
	case *ast.Link:
		dest := string(n.Destination)
		if dest == "" {
			break
		}
		state.edges = append(state.edges, schema.Edge{
			Source:     state.fnStack[len(state.fnStack)-1],
			Target:     schema.LangID("markdown", "link", dest),
			Relation:   "references",
			Confidence: schema.Extracted,
		})
	case *ast.AutoLink:
		dest := string(n.URL(src))
		if dest == "" {
			break
		}
		state.edges = append(state.edges, schema.Edge{
			Source:     state.fnStack[len(state.fnStack)-1],
			Target:     schema.LangID("markdown", "link", dest),
			Relation:   "references",
			Confidence: schema.Extracted,
		})
	}
	// Recurse.
	for c := node.FirstChild(); c != nil; c = c.NextSibling() {
		walkMarkdown(c, src, path, state)
	}
}

// firstH1Text returns the inline text of the first level-1 heading in the
// document, or "" if none exists.
func firstH1Text(root ast.Node, src []byte) string {
	var found string
	ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		if h, ok := n.(*ast.Heading); ok && h.Level == 1 {
			found = nodeText(h, src)
			return ast.WalkStop, nil
		}
		return ast.WalkContinue, nil
	})
	return found
}

// nodeText flattens an AST subtree to its plain-text content. goldmark's
// inline children (Text, Emphasis, Code, etc.) all expose a Segment that
// indexes into the source.
func nodeText(n ast.Node, src []byte) string {
	var b strings.Builder
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		switch v := c.(type) {
		case *ast.Text:
			b.Write(v.Segment.Value(src))
		case *ast.String:
			b.Write(v.Value)
		default:
			b.WriteString(nodeText(c, src))
		}
	}
	return b.String()
}

// slugify produces a stable, deterministic ID-suffix from a heading label.
// Lowercased, alphanumeric + hyphens; collapses runs of non-alphanumeric
// to a single hyphen. Same shape jekyll/hugo use, so cross-tool linking
// stays predictable.
func slugify(s string) string {
	var b strings.Builder
	prevHyphen := true
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevHyphen = false
		default:
			if !prevHyphen {
				b.WriteByte('-')
				prevHyphen = true
			}
		}
	}
	return strings.TrimSuffix(b.String(), "-")
}
