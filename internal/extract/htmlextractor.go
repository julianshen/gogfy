package extract

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/julianshen/gogfy/internal/schema"
	"golang.org/x/net/html"
)

// HTMLExtractor turns HTML files into graph nodes and edges using
// golang.org/x/net/html (pure-Go; no cgo). Mirrors MarkdownExtractor's
// schema so cross-format graphs (a Markdown doc linking into an HTML
// section, or vice versa) compose cleanly without translation.
//
// Schema:
//
//   - Document node, kind=module, label = <title> if present, else
//     first H1 text, else basename.
//   - Section nodes, kind=section, for each h1/h2/h3.
//   - Reference edges (relation=references) for every <a href="…"> link,
//     sourced from the enclosing section (or document if no section yet),
//     target = `html:link:<href>`.
//
// Limitations (deliberate, for the first cut):
//
//   - <link rel="canonical"> and other meta-only references aren't
//     emitted; could become opt-in later.
//   - <iframe src="…">, <script src="…">, etc. aren't surfaced as
//     reference edges — they're loaded resources, not document links
//     in the knowledge-graph sense.
//   - Inline-script content isn't parsed (would require a JS sub-parser).
type HTMLExtractor struct{}

func (HTMLExtractor) Extract(path string) (Result, error) {
	f, err := os.Open(path)
	if err != nil {
		return Result{}, err
	}
	defer f.Close()
	abs, err := filepath.Abs(path)
	if err != nil {
		return Result{}, err
	}
	root, err := html.Parse(f)
	if err != nil {
		return Result{}, err
	}

	state := &extractState{lang: "html", filePath: abs}
	moduleID := schema.LangID("html", "module", abs)
	state.nodes = append(state.nodes, schema.Node{
		ID:    moduleID,
		Label: filepath.Base(abs),
	})
	state.fnStack = append(state.fnStack, moduleID)

	// Pre-pass: prefer <title> over the first h1, mirroring how browsers
	// and screen-readers identify the document.
	if t := firstTitleText(root); t != "" {
		state.nodes[0].Label = t
	} else if h := firstHeadingText(root, "h1"); h != "" {
		state.nodes[0].Label = h
	}

	walkHTML(root, abs, state)
	return Result{Nodes: state.nodes, Edges: state.edges}, nil
}

func walkHTML(n *html.Node, path string, state *extractState) {
	if n.Type == html.ElementNode {
		switch n.Data {
		case "h1", "h2", "h3":
			label := strings.TrimSpace(elementText(n))
			if label != "" {
				id := nextSectionID(state, "html", path, label)
				state.nodes = append(state.nodes, schema.Node{
					ID:         id,
					Label:      label,
					SourceFile: path,
				})
				state.emitContainsFromModule(id)
				// Reset to module + push this section, same shape as
				// the markdown walker.
				state.fnStack = []string{state.fnStack[0]}
				state.fnStack = append(state.fnStack, id)
			}
		case "a":
			if href := attr(n, "href"); href != "" && !isFragmentOnly(href) {
				state.edges = append(state.edges, schema.Edge{
					Source:     state.fnStack[len(state.fnStack)-1],
					Target:     schema.LangID("html", "link", href),
					Relation:   "references",
					Confidence: schema.Extracted,
				})
			}
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walkHTML(c, path, state)
	}
}

func firstTitleText(n *html.Node) string {
	if n.Type == html.ElementNode && n.Data == "title" {
		return strings.TrimSpace(elementText(n))
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if t := firstTitleText(c); t != "" {
			return t
		}
	}
	return ""
}

func firstHeadingText(n *html.Node, tag string) string {
	if n.Type == html.ElementNode && n.Data == tag {
		return strings.TrimSpace(elementText(n))
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if t := firstHeadingText(c, tag); t != "" {
			return t
		}
	}
	return ""
}

// elementText collects all TextNode descendants of n, separated by single
// spaces. Used for headings and <title>; ignores attribute values and
// HTML comments.
func elementText(n *html.Node) string {
	var b strings.Builder
	collectText(n, &b)
	return strings.Join(strings.Fields(b.String()), " ")
}

func collectText(n *html.Node, b *strings.Builder) {
	if n.Type == html.TextNode {
		b.WriteString(n.Data)
		b.WriteByte(' ')
		return
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		collectText(c, b)
	}
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

// isFragmentOnly skips `href="#section"` self-links — they're navigation
// within the same document, not cross-references worth a graph edge.
// (Same-document anchors that ALSO carry a path, like `./other.html#sec`,
// are kept; only pure `#…` is dropped.)
func isFragmentOnly(href string) bool {
	return strings.HasPrefix(href, "#")
}
