package extract

import (
	"archive/zip"
	"encoding/xml"
	"io"
	"path/filepath"
	"strings"

	"github.com/julianshen/gogfy/internal/schema"
)

// DocxExtractor handles Word .docx files. OOXML is a zip of XML; we read
// just the two parts we need:
//
//   - word/document.xml: the body, as paragraphs (`w:p`) of runs (`w:r`)
//     of text (`w:t`). Heading paragraphs carry `<w:pStyle w:val="Heading1">`
//     (and Heading2/3, plus "Title" which we use as the doc label).
//   - word/_rels/document.xml.rels: a flat list of `<Relationship Id="rId5"
//     Type="…/hyperlink" Target="https://…"/>` tuples. Hyperlink elements
//     in the body reference their URL by Id rather than carrying it
//     inline, so we resolve via this map.
//
// Same module + section + reference schema as Markdown/HTML/RST/Text so
// cross-format graphs compose. No third-party docx dependency — every
// usable Go docx library we surveyed is built around document *editing*
// (Run/Paragraph/Style structs designed for mutation), and reading
// styled paragraphs + hyperlinks via the standard library is ~150 lines.
//
// Limitations (deliberate, first cut):
//
//   - We don't recurse into headers, footers, footnotes, or comments.
//     The body covers the prominent content; auxiliary parts can be
//     added later if the graph signal warrants it.
//   - Tables are flattened: cell text contributes to the current
//     paragraph's text, but table structure isn't modeled.
//   - Internal anchor hyperlinks (`w:anchor` instead of `r:id`) are
//     skipped — they're intra-document navigation, not cross-references.
type DocxExtractor struct{}

func (DocxExtractor) Extract(path string) (Result, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return Result{}, err
	}
	zr, err := zip.OpenReader(abs)
	if err != nil {
		return Result{}, err
	}
	defer zr.Close()

	var docXML, relsXML []byte
	for _, f := range zr.File {
		switch f.Name {
		case "word/document.xml":
			docXML, err = readZipFile(f)
		case "word/_rels/document.xml.rels":
			relsXML, err = readZipFile(f)
		}
		if err != nil {
			return Result{}, err
		}
	}
	if len(docXML) == 0 {
		// Not a Word document we recognize — return a bare module node so
		// the file still appears in the graph.
		return docxBareModule(abs), nil
	}

	rels := parseOOXMLRels(relsXML, relTypeHyperlink)

	state := &extractState{lang: "docx", filePath: abs}
	moduleID := schema.LangID("docx", "module", abs)
	state.nodes = append(state.nodes, schema.Node{
		ID:    moduleID,
		Label: filepath.Base(abs),
	})
	state.fnStack = append(state.fnStack, moduleID)

	titleFromTitleStyle, firstH1, err := walkDocxBody(docXML, rels, abs, state)
	if err != nil {
		return Result{}, err
	}
	// Title-style paragraph wins; else first Heading1; else basename
	// (already set). Mirrors HTML's <title> > h1 > basename precedence.
	switch {
	case titleFromTitleStyle != "":
		state.nodes[0].Label = titleFromTitleStyle
	case firstH1 != "":
		state.nodes[0].Label = firstH1
	}

	return Result{Nodes: state.nodes, Edges: state.edges}, nil
}

func docxBareModule(abs string) Result {
	moduleID := schema.LangID("docx", "module", abs)
	return Result{
		Nodes: []schema.Node{{ID: moduleID, Label: filepath.Base(abs)}},
	}
}

func readZipFile(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

// OOXML relationship-type discriminators. Full URLs are
// "http://schemas.openxmlformats.org/officeDocument/2006/relationships/<kind>"
// — we only care about the trailing suffix for filtering.
const (
	relTypeHyperlink = "/hyperlink"
	relTypeWorksheet = "/worksheet"
)

// parseOOXMLRels parses an OOXML _rels file into Id→Target. If wantSuffix
// is non-empty, only relationships whose Type ends with it are returned —
// keeps image/embed relationships out of hyperlink lookups.
func parseOOXMLRels(data []byte, wantSuffix string) map[string]string {
	out := map[string]string{}
	if len(data) == 0 {
		return out
	}
	type relationship struct {
		ID     string `xml:"Id,attr"`
		Type   string `xml:"Type,attr"`
		Target string `xml:"Target,attr"`
	}
	type relationships struct {
		Items []relationship `xml:"Relationship"`
	}
	var rs relationships
	if err := xml.Unmarshal(data, &rs); err != nil {
		return out
	}
	for _, r := range rs.Items {
		if wantSuffix != "" && !strings.HasSuffix(r.Type, wantSuffix) {
			continue
		}
		out[r.ID] = r.Target
	}
	return out
}

// walkDocxBody streams document.xml, accumulating per-paragraph text
// (with hyperlink runs interleaved). On paragraph-end it inspects the
// paragraph's pStyle. Heading1/2/3 → emit a section node; Title → record
// for module label. Hyperlinks anywhere in the body → emit a reference
// edge sourced from the current section (or module).
//
// Returns (titleParagraphText, firstHeading1Text) for module-label
// resolution after the walk completes.
func walkDocxBody(data []byte, rels map[string]string, path string, state *extractState) (string, string, error) {
	dec := xml.NewDecoder(strings.NewReader(string(data)))
	var (
		titleText  string
		firstH1    string
		paraText   strings.Builder
		paraStyle  string
		moduleID   = state.fnStack[0]
		inHyperlnk bool
		// hyperlinkRId resolves the *current* hyperlink's r:id; we emit
		// the edge on hyperlink end so anchor-only links (no r:id) are
		// dropped naturally.
		hyperlinkRID string
	)
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", "", err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "p":
				paraText.Reset()
				paraStyle = ""
			case "pStyle":
				paraStyle = attrValue(t.Attr, "val")
			case "t":
				// w:t — text run content. Read the next CharData token.
				if cd, ok := nextCharData(dec); ok {
					paraText.WriteString(cd)
				}
			case "tab":
				paraText.WriteByte('\t')
			case "br":
				paraText.WriteByte('\n')
			case "hyperlink":
				inHyperlnk = true
				hyperlinkRID = attrValue(t.Attr, "id") // r:id collapses to "id" under Go's xml decoder
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "hyperlink":
				if inHyperlnk && hyperlinkRID != "" {
					if url, ok := rels[hyperlinkRID]; ok && url != "" {
						state.edges = append(state.edges, schema.Edge{
							Source:     state.fnStack[len(state.fnStack)-1],
							Target:     schema.LangID("docx", "link", url),
							Relation:   "references",
							Confidence: schema.Extracted,
						})
					}
				}
				inHyperlnk = false
				hyperlinkRID = ""
			case "p":
				text := strings.TrimSpace(paraText.String())
				if text == "" {
					break
				}
				switch docxHeadingLevel(paraStyle) {
				case 0:
					if isDocxTitleStyle(paraStyle) && titleText == "" {
						titleText = text
					}
				case 1, 2, 3:
					level := docxHeadingLevel(paraStyle)
					if level == 1 && firstH1 == "" {
						firstH1 = text
					}
					id := schema.LangID("docx", "section", path+":"+slugify(text))
					state.nodes = append(state.nodes, schema.Node{
						ID:         id,
						Label:      text,
						SourceFile: path,
					})
					state.fnStack = []string{moduleID, id}
				}
			}
		}
	}
	return titleText, firstH1, nil
}

// nextCharData consumes tokens until it finds a CharData (or hits a
// non-text token). Used when the caller has just observed `<w:t>` and
// wants the run's text payload.
func nextCharData(dec *xml.Decoder) (string, bool) {
	tok, err := dec.Token()
	if err != nil {
		return "", false
	}
	if cd, ok := tok.(xml.CharData); ok {
		return string(cd), true
	}
	return "", false
}

func attrValue(attrs []xml.Attr, name string) string {
	for _, a := range attrs {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

// docxHeadingLevel returns the Word heading level (1-9) from a pStyle
// value, or 0 if the style isn't a heading. Word uses both "Heading1"
// (en-US) and locale variants; we accept the canonical English form
// which is what nearly every editor emits regardless of UI locale.
func docxHeadingLevel(style string) int {
	const prefix = "Heading"
	if !strings.HasPrefix(style, prefix) {
		return 0
	}
	rest := style[len(prefix):]
	if len(rest) != 1 {
		return 0
	}
	c := rest[0]
	if c < '1' || c > '9' {
		return 0
	}
	return int(c - '0')
}

func isDocxTitleStyle(style string) bool {
	return style == "Title"
}
