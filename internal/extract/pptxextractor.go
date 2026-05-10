package extract

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/julianshen/gogfy/internal/schema"
)

// PPTXExtractor handles PowerPoint .pptx files. Same OOXML-zip approach
// as docx/xlsx: ppt/presentation.xml lists slide r:ids, the presentation
// rels resolve those to slide part paths, and each slide has its own
// rels file for hyperlinks.
//
// Schema mirrors XlsxExtractor: presentation → module node (label =
// basename); each slide → section node (label = slide title placeholder
// text, or "Slide N" if the title shape has no text); external hyperlinks
// → reference edges sourced from the owning slide section.
type PPTXExtractor struct{}

const relTypeSlide = "/slide"

func (PPTXExtractor) Extract(path string) (Result, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return Result{}, err
	}
	zr, err := zip.OpenReader(abs)
	if err != nil {
		return Result{}, err
	}
	defer zr.Close()

	files := map[string]*zip.File{}
	for _, f := range zr.File {
		if f.Name == "ppt/presentation.xml" ||
			f.Name == "ppt/_rels/presentation.xml.rels" ||
			strings.HasPrefix(f.Name, "ppt/slides/") {
			files[f.Name] = f
		}
	}
	readPart := func(name string) ([]byte, error) {
		f, ok := files[name]
		if !ok {
			return nil, nil
		}
		return readZipFile(f)
	}

	state := &extractState{lang: "pptx", filePath: abs}
	moduleID := schema.LangID("pptx", "module", abs)
	state.nodes = append(state.nodes, schema.Node{
		ID:    moduleID,
		Label: filepath.Base(abs),
	})

	presXML, err := readPart("ppt/presentation.xml")
	if err != nil {
		return Result{}, err
	}
	if len(presXML) == 0 {
		return Result{Nodes: state.nodes}, nil
	}

	relsXML, err := readPart("ppt/_rels/presentation.xml.rels")
	if err != nil {
		return Result{}, err
	}
	presRels := parseOOXMLRels(relsXML, relTypeSlide)
	slideRIDs := parsePresentationSlideRIDs(presXML)

	for i, rid := range slideRIDs {
		// Section emission is unconditional; structural data shouldn't
		// depend on rels resolution. Title text is best-effort — if the
		// slide part is unreadable, we fall back to "Slide N" so the
		// section still appears.
		title := fmt.Sprintf("Slide %d", i+1)
		var sheetRels map[string]string
		var hyperlinkRIDs []string

		if target, ok := presRels[rid]; ok {
			slidePath := "ppt/" + strings.TrimPrefix(target, "/")
			if slideXML, err := readPart(slidePath); err == nil && len(slideXML) > 0 {
				if t := pptxSlideTitle(slideXML); t != "" {
					title = t
				}
				hyperlinkRIDs = pptxSlideHyperlinks(slideXML)
				dir, file := filepath.Split(slidePath)
				if rxml, err := readPart(dir + "_rels/" + file + ".rels"); err == nil {
					sheetRels = parseOOXMLRels(rxml, relTypeHyperlink)
				}
			} else if err != nil {
				return Result{}, err
			}
		}

		sectionID := schema.LangID("pptx", "section", abs+":"+slugify(title))
		state.nodes = append(state.nodes, schema.Node{
			ID:         sectionID,
			Label:      title,
			SourceFile: abs,
		})
		for _, hrid := range hyperlinkRIDs {
			url, ok := sheetRels[hrid]
			if !ok || url == "" {
				continue
			}
			state.edges = append(state.edges, schema.Edge{
				Source:     sectionID,
				Target:     schema.LangID("pptx", "link", url),
				Relation:   "references",
				Confidence: schema.Extracted,
			})
		}
	}

	return Result{Nodes: state.nodes, Edges: state.edges}, nil
}

// parsePresentationSlideRIDs returns the r:id of each slide listed in
// ppt/presentation.xml's <p:sldIdLst>, in document order. The numeric
// /id attribute is internal and not used; slide ordering is what binds
// the section labels to "Slide N" fallbacks.
func parsePresentationSlideRIDs(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	type sldID struct {
		RelID string `xml:"id,attr"` // r:id — Go matches namespaced attrs by local name
	}
	type presentation struct {
		Slides []sldID `xml:"sldIdLst>sldId"`
	}
	var p presentation
	if err := xml.Unmarshal(data, &p); err != nil {
		return nil
	}
	out := make([]string, 0, len(p.Slides))
	for _, s := range p.Slides {
		if s.RelID != "" {
			out = append(out, s.RelID)
		}
	}
	return out
}

// pptxSlideTitle returns the concatenated text of the slide's title
// placeholder (<p:ph type="title"/> or "ctrTitle"), or "" if the slide
// has no title shape. Walks the slide XML token-by-token rather than
// unmarshalling into a struct tree because the title shape is buried
// inside <p:sp>/<p:nvSpPr>/<p:nvPr>/<p:ph> and Go's xml decoder doesn't
// have a clean way to express "any descendant ph element" via struct
// tags without flattening the surrounding shape structure.
func pptxSlideTitle(data []byte) string {
	dec := xml.NewDecoder(strings.NewReader(string(data)))
	var (
		inShape, inTxBody, inText  bool
		inTitleShape, sawTitlePh   bool
		text                       strings.Builder
		paraSep                    bool // emit a space between paragraphs
	)
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "sp":
				inShape = true
				sawTitlePh = false
			case "ph":
				if inShape {
					phType := attrValue(t.Attr, "type")
					if phType == "title" || phType == "ctrTitle" {
						sawTitlePh = true
					}
				}
			case "txBody":
				if inShape && sawTitlePh {
					inTxBody = true
					inTitleShape = true
				}
			case "p":
				if inTxBody {
					if paraSep && text.Len() > 0 {
						text.WriteByte(' ')
					}
					paraSep = false
				}
			case "t":
				if inTitleShape {
					inText = true
				}
			}
		case xml.CharData:
			if inText {
				text.Write(t)
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "sp":
				inShape = false
				sawTitlePh = false
				inTitleShape = false
			case "txBody":
				inTxBody = false
				inTitleShape = false
				if text.Len() > 0 {
					return strings.TrimSpace(text.String())
				}
			case "p":
				if inTxBody {
					paraSep = true
				}
			case "t":
				inText = false
			}
		}
	}
	return strings.TrimSpace(text.String())
}

// pptxSlideHyperlinks returns the r:id of every <a:hlinkClick> element
// in the slide. Hyperlinks live inside run properties and click actions
// on shapes; we don't care which run, just which rIDs need URL lookup.
func pptxSlideHyperlinks(data []byte) []string {
	dec := xml.NewDecoder(strings.NewReader(string(data)))
	var out []string
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if se.Name.Local != "hlinkClick" {
			continue
		}
		if rid := attrValue(se.Attr, "id"); rid != "" {
			out = append(out, rid)
		}
	}
	return out
}
