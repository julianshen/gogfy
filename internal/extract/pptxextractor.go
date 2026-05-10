package extract

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/julianshen/gogfy/internal/schema"
)

// PPTXExtractor handles PowerPoint .pptx files. Architecture mirrors
// XlsxExtractor — see that file for the OOXML zip-of-XML rationale and
// the unconditional-section-emission policy. Slide titles, hyperlinks,
// and paragraph separators are collected in one token-walk because
// struct-tag unmarshalling can't express several cross-cutting
// concerns cleanly: descendant-only title-shape gating, first-title-
// wins semantics, hyperlinks at arbitrary depth, inter-paragraph spacing.
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
		return Result{Nodes: state.nodes, Edges: state.edges}, nil
	}

	relsXML, err := readPart("ppt/_rels/presentation.xml.rels")
	if err != nil {
		return Result{}, err
	}
	presRels := parseOOXMLRels(relsXML, relTypeSlide)
	slideRIDs := parsePresentationSlideRIDs(presXML, abs)

	for i, rid := range slideRIDs {
		title := fmt.Sprintf("Slide %d", i+1)
		var slideRels map[string]string
		var hyperlinkRIDs []string

		if target, ok := presRels[rid]; ok {
			slidePath := resolveOOXMLPartPath("ppt/", target)
			slideXML, err := readPart(slidePath)
			if err != nil {
				return Result{}, err
			}
			if len(slideXML) > 0 {
				t, links := pptxSlideContent(slideXML, abs, i+1)
				if t != "" {
					title = t
				}
				hyperlinkRIDs = links
				dir, file := filepath.Split(slidePath)
				rxml, err := readPart(dir + "_rels/" + file + ".rels")
				if err != nil {
					return Result{}, err
				}
				slideRels = parseOOXMLRels(rxml, relTypeHyperlink)
			}
		}

		// Slide ordinal is part of the key: PowerPoint allows duplicate
		// slide titles (multiple "Appendix" slides is common) and a
		// title-only key would collapse them into one node, dropping
		// every later slide's hyperlinks under the first slide's section.
		sectionID := schema.LangID("pptx", "section",
			fmt.Sprintf("%s:slide%d:%s", abs, i+1, slugify(title)))
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
// ppt/presentation.xml's <p:sldIdLst>, in document order. Malformed
// presentation XML degrades to nil + stderr log: distinguishes "broken
// file" from "legitimately empty deck" for batch operators.
func parsePresentationSlideRIDs(data []byte, abs string) []string {
	if len(data) == 0 {
		return nil
	}
	type sldID struct {
		// Explicit namespace required: <p:sldId> carries BOTH `id` (the
		// internal numeric slide id) and `r:id` (the rel id) with the
		// same local name. Without the relationships-namespace URL, Go's
		// xml decoder picks whichever attribute appears first in source —
		// some producers emit `r:id` before `id`, which captured the
		// wrong value and silently broke slide-rel resolution.
		RelID string `xml:"http://schemas.openxmlformats.org/officeDocument/2006/relationships id,attr"`
	}
	type presentation struct {
		Slides []sldID `xml:"sldIdLst>sldId"`
	}
	var p presentation
	if err := xml.Unmarshal(data, &p); err != nil {
		fmt.Fprintf(os.Stderr, "pptx: presentation parse failed on %s: %v\n", abs, err)
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

// pptxSlideContent walks slide XML once, capturing the first title-
// placeholder's text and every <a:hlinkClick> rId in the slide.
// Truncated/malformed XML logs to stderr and returns whatever was
// collected so far — a partial slide is more useful than dropping it.
func pptxSlideContent(data []byte, abs string, slideNum int) (title string, hyperlinkRIDs []string) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	var (
		inShape, inTxBody, inText, sawTitlePh bool
		titleDone                             bool
		text                                  strings.Builder
		paraSep                               bool
	)
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "pptx: slide %d parse failed on %s: %v\n", slideNum, abs, err)
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
