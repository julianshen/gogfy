package extract

import (
	"archive/zip"
	"bytes"
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
// is non-empty, only relationships whose Type ends with it are returned.
// For hyperlink rels we additionally require TargetMode="External": OOXML
// hyperlinks can target bookmarks or internal parts, and emitting those
// as cross-document references would pollute the graph the same way the
// body walker's w:anchor skip prevents.
func parseOOXMLRels(data []byte, wantSuffix string) map[string]string {
	out := map[string]string{}
	if len(data) == 0 {
		return out
	}
	type relationship struct {
		ID         string `xml:"Id,attr"`
		Type       string `xml:"Type,attr"`
		Target     string `xml:"Target,attr"`
		TargetMode string `xml:"TargetMode,attr"`
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
		if wantSuffix == relTypeHyperlink && r.TargetMode != "External" {
			continue
		}
		out[r.ID] = r.Target
	}
	return out
}

// walkDocxBody streams document.xml, accumulating per-paragraph text
// (with hyperlink runs interleaved) and per-paragraph hyperlink rIDs.
// On paragraph-end it inspects the paragraph's pStyle: Heading1/2/3 →
// emit a section node and push it as the current scope; Title → record
// for module label. Hyperlinks are then flushed *after* the section
// has been pushed, so a hyperlink that lives inside a Heading1 paragraph
// is correctly attributed to that heading's section node — not to the
// previous section or the module, which is what flushing on
// </w:hyperlink> (mid-paragraph) used to produce.
//
// Returns (titleParagraphText, firstHeading1Text) for module-label
// resolution after the walk completes.
func walkDocxBody(data []byte, rels map[string]string, path string, state *extractState) (string, string, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	var (
		titleText        string
		firstH1          string
		paraText         strings.Builder
		paraStyle        string
		moduleID         = state.fnStack[0]
		paraHyperlinkIDs []string
	)
	emitLinks := func() {
		source := state.fnStack[len(state.fnStack)-1]
		for _, rID := range paraHyperlinkIDs {
			url, ok := rels[rID]
			if !ok || url == "" {
				continue
			}
			state.edges = append(state.edges, schema.Edge{
				Source:     source,
				Target:     schema.LangID("docx", "link", url),
				Relation:   "references",
				Confidence: schema.Extracted,
			})
		}
		paraHyperlinkIDs = paraHyperlinkIDs[:0]
	}
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
				paraHyperlinkIDs = paraHyperlinkIDs[:0]
			case "pStyle":
				paraStyle = attrValue(t.Attr, "val")
			case "t":
				if cd, ok := nextCharData(dec); ok {
					paraText.WriteString(cd)
				}
			case "tab":
				paraText.WriteByte('\t')
			case "br":
				paraText.WriteByte('\n')
			case "hyperlink":
				// r:id collapses to "id" under Go's xml decoder; we collect
				// every rID seen in the paragraph and resolve+emit at </w:p>
				// once we know whether the paragraph created a new section.
				if rID := attrValue(t.Attr, "id"); rID != "" {
					paraHyperlinkIDs = append(paraHyperlinkIDs, rID)
				}
			}
		case xml.EndElement:
			if t.Name.Local != "p" {
				continue
			}
			text := strings.TrimSpace(paraText.String())
			level := docxHeadingLevel(paraStyle)
			switch {
			case text == "":
				// no-op; still flush any hyperlinks below
			case level == 0 && isDocxTitleStyle(paraStyle):
				if titleText == "" {
					titleText = text
				}
			case level >= 1 && level <= 3:
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
			emitLinks()
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
