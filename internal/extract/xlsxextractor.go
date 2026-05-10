package extract

import (
	"archive/zip"
	"encoding/xml"
	"path/filepath"
	"strings"

	"github.com/julianshen/gogfy/internal/schema"
)

// XlsxExtractor handles Excel .xlsx files. Like .docx, OOXML is a zip of
// XML — but the part graph is richer:
//
//   - xl/workbook.xml: lists sheets as `<sheet name="…" r:id="rIdN"/>`.
//   - xl/_rels/workbook.xml.rels: resolves each sheet's r:id → relative
//     path (e.g. `worksheets/sheet1.xml`).
//   - xl/worksheets/sheetN.xml: per-sheet body. Contains
//     `<hyperlinks><hyperlink ref="A1" r:id="rIdN"/></hyperlinks>` for
//     external links.
//   - xl/worksheets/_rels/sheetN.xml.rels: per-sheet rels file that
//     resolves *that sheet's* hyperlink r:ids → URL Targets.
//
// Schema (same shape as the other doc extractors):
//
//   - Workbook → module node, label = basename. (xlsx has no canonical
//     "title" the way docx does; sheet names are user-meaningful but
//     a workbook with one untitled "Sheet1" shouldn't hijack the label.)
//   - Each sheet → section node, label = sheet name.
//   - Each external hyperlink → reference edge sourced from its sheet
//     section, target = `xlsx:link:<URL>`.
//
// Cell text isn't extracted: the shared-string table (xl/sharedStrings.xml)
// would let us inline cell values, but for the knowledge-graph use case
// the high-signal extracts are sheet names and outbound hyperlinks. Cell
// content is mostly numeric/tabular and would mostly add noise.
type XlsxExtractor struct{}

func (XlsxExtractor) Extract(path string) (Result, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return Result{}, err
	}
	zr, err := zip.OpenReader(abs)
	if err != nil {
		return Result{}, err
	}
	defer zr.Close()

	// Index *zip.File pointers (not buffered bytes) so worksheet content
	// is read on demand. Workbooks with hundreds of sheets would otherwise
	// hold every sheet's XML in memory simultaneously.
	files := map[string]*zip.File{}
	for _, f := range zr.File {
		if f.Name == "xl/workbook.xml" ||
			f.Name == "xl/_rels/workbook.xml.rels" ||
			strings.HasPrefix(f.Name, "xl/worksheets/") {
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

	state := &extractState{lang: "xlsx", filePath: abs}
	moduleID := schema.LangID("xlsx", "module", abs)
	state.nodes = append(state.nodes, schema.Node{
		ID:    moduleID,
		Label: filepath.Base(abs),
	})
	state.fnStack = append(state.fnStack, moduleID)

	workbookXML, err := readPart("xl/workbook.xml")
	if err != nil {
		return Result{}, err
	}
	if len(workbookXML) == 0 {
		// Not an xlsx we recognize — bare module node so the file still
		// appears in the graph.
		return Result{Nodes: state.nodes}, nil
	}

	relsXML, err := readPart("xl/_rels/workbook.xml.rels")
	if err != nil {
		return Result{}, err
	}
	// Workbook rels point to worksheets — not hyperlinks. Use the general
	// helper with the worksheet-type suffix.
	workbookRels := parseOOXMLRels(relsXML, "/worksheet")
	sheets := parseXlsxSheets(workbookXML)

	for _, s := range sheets {
		// Sheet section node is emitted unconditionally — every sheet
		// declared in xl/workbook.xml is part of the workbook's structure
		// regardless of whether its worksheet part or rels can be resolved.
		// Hyperlink extraction is what depends on those lookups; gating
		// the section on them would silently drop sheets from the graph.
		sectionID := schema.LangID("xlsx", "section", abs+":"+slugify(s.Name))
		state.nodes = append(state.nodes, schema.Node{
			ID:         sectionID,
			Label:      s.Name,
			SourceFile: abs,
		})

		// Resolve sheet path via the workbook rels. The Target is
		// relative to xl/ (e.g. "worksheets/sheet1.xml" → "xl/worksheets/sheet1.xml").
		target, ok := workbookRels[s.RelID]
		if !ok {
			continue
		}
		sheetPath := "xl/" + strings.TrimPrefix(target, "/")
		sheetXML, err := readPart(sheetPath)
		if err != nil {
			return Result{}, err
		}
		if len(sheetXML) == 0 {
			continue
		}

		// Per-sheet rels live next to the sheet. For "xl/worksheets/sheet1.xml"
		// the rels path is "xl/worksheets/_rels/sheet1.xml.rels".
		dir, file := filepath.Split(sheetPath)
		sheetRelsXML, err := readPart(dir + "_rels/" + file + ".rels")
		if err != nil {
			return Result{}, err
		}
		sheetRels := parseOOXMLRels(sheetRelsXML, "/hyperlink")

		for _, rid := range parseXlsxHyperlinks(sheetXML) {
			url, ok := sheetRels[rid]
			if !ok || url == "" {
				continue
			}
			state.edges = append(state.edges, schema.Edge{
				Source:     sectionID,
				Target:     schema.LangID("xlsx", "link", url),
				Relation:   "references",
				Confidence: schema.Extracted,
			})
		}
	}

	return Result{Nodes: state.nodes, Edges: state.edges}, nil
}

type xlsxSheet struct {
	Name  string
	RelID string
}

// parseXlsxSheets parses xl/workbook.xml's <sheets><sheet name="…" r:id="…"/></sheets>.
// Sheet order is preserved so the section nodes appear in workbook order
// (matters for deterministic graph output).
func parseXlsxSheets(data []byte) []xlsxSheet {
	if len(data) == 0 {
		return nil
	}
	type sheet struct {
		Name  string `xml:"name,attr"`
		RelID string `xml:"id,attr"` // namespaced as r:id; Go collapses to local name
	}
	type sheets struct {
		Sheets []sheet `xml:"sheets>sheet"`
	}
	var wb sheets
	if err := xml.Unmarshal(data, &wb); err != nil {
		return nil
	}
	out := make([]xlsxSheet, 0, len(wb.Sheets))
	for _, s := range wb.Sheets {
		if s.Name == "" || s.RelID == "" {
			continue
		}
		out = append(out, xlsxSheet{Name: s.Name, RelID: s.RelID})
	}
	return out
}

// parseXlsxHyperlinks returns the r:id of every <hyperlink> child of the
// worksheet's <hyperlinks> block. The actual URL is in the per-sheet
// rels; we just collect the IDs here.
func parseXlsxHyperlinks(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	type hyperlink struct {
		RelID string `xml:"id,attr"`
	}
	type worksheet struct {
		Links []hyperlink `xml:"hyperlinks>hyperlink"`
	}
	var ws worksheet
	if err := xml.Unmarshal(data, &ws); err != nil {
		return nil
	}
	out := make([]string, 0, len(ws.Links))
	for _, l := range ws.Links {
		if l.RelID != "" {
			out = append(out, l.RelID)
		}
	}
	return out
}
