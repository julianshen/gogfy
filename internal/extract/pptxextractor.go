package extract

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/julianshen/gogfy/internal/schema"
)

// PPTXExtractor handles PowerPoint .pptx files. Architecture mirrors
// XlsxExtractor — see that file for the OOXML zip-of-XML rationale and
// the unconditional-section-emission policy. The pptx-specific quirk is
// that slide title text is buried inside <p:sp>/<p:nvSpPr>/<p:nvPr>/<p:ph
// type="title">, deep enough that struct-tag unmarshalling can't express
// the descendant-only constraint cleanly — we walk tokens instead.
type PPTXExtractor struct{}

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
		title := fmt.Sprintf("Slide %d", i+1)
		var slideRels map[string]string
		var hyperlinkRIDs []string

		if target, ok := presRels[rid]; ok {
			slidePath := "ppt/" + strings.TrimPrefix(target, "/")
			slideXML, err := readPart(slidePath)
			if err != nil {
				return Result{}, err
			}
			if len(slideXML) > 0 {
				t, links := pptxSlideContent(slideXML)
				if t != "" {
					title = t
				}
				hyperlinkRIDs = links
				dir, file := filepath.Split(slidePath)
				if rxml, err := readPart(dir + "_rels/" + file + ".rels"); err == nil {
					slideRels = parseOOXMLRels(rxml, relTypeHyperlink)
				}
			}
		}

		sectionID := schema.LangID("pptx", "section", abs+":"+slugify(title))
		state.nodes = append(state.nodes, schema.Node{
			ID:         sectionID,
			Label:      title,
			SourceFile: abs,
		})
		for _, hrid := range hyperlinkRIDs {
			url, ok := slideRels[hrid]
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
// ppt/presentation.xml's <p:sldIdLst>, in document order.
func parsePresentationSlideRIDs(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	type sldID struct {
		RelID string `xml:"id,attr"`
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

// pptxSlideContent walks the slide XML once, returning the title-
// placeholder text (first title shape wins; <a:hlinkClick> is collected
// throughout regardless). Single-pass replaces what was two independent
// walks of the same bytes — meaningful on decks with many slides.
func pptxSlideContent(data []byte) (title string, hyperlinkRIDs []string) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	var (
		inShape, inTxBody, inText, sawTitlePh bool
		titleDone                             bool
		text                                  strings.Builder
		paraSep                               bool
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
				if inShape && sawTitlePh && !titleDone {
					inTxBody = true
				}
			case "p":
				if inTxBody && paraSep && text.Len() > 0 {
					text.WriteByte(' ')
				}
				paraSep = false
			case "t":
				if inTxBody {
					inText = true
				}
			case "hlinkClick":
				if rid := attrValue(t.Attr, "id"); rid != "" {
					hyperlinkRIDs = append(hyperlinkRIDs, rid)
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
			case "txBody":
				if inTxBody {
					titleDone = true // first title shape wins; later ones can't override
					inTxBody = false
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
	return strings.TrimSpace(text.String()), hyperlinkRIDs
}
