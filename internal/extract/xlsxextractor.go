package extract

import (
	"archive/zip"
	"encoding/xml"
	"path/filepath"
	"strings"

	"github.com/julianshen/gogfy/internal/schema"
)

// XlsxExtractor handles Excel .xlsx files.
//
// xlsx is a zip with a richer part graph than docx: xl/workbook.xml lists
// sheets by r:id, xl/_rels/workbook.xml.rels resolves those ids to sheet
// paths, and each sheet then has its own _rels file for hyperlinks. So we
// resolve two rels graphs, not one.
//
// Cell content is intentionally not extracted: the shared-string table
// (xl/sharedStrings.xml) would let us inline cell values, but for the
// knowledge-graph use case sheet names + outbound hyperlinks are the
// high-signal extracts; cell text is mostly numeric/tabular noise.
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
			strings.HasPrefix(f.Name, "xl/worksheets/") ||
			// Defined-table parts live under xl/tables/ and contribute
			// table + column structural nodes below.
			strings.HasPrefix(f.Name, "xl/tables/") {
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

	workbookXML, err := readPart("xl/workbook.xml")
	if err != nil {
		return Result{}, err
	}
	if len(workbookXML) == 0 {
		return Result{Nodes: state.nodes}, nil
	}

	relsXML, err := readPart("xl/_rels/workbook.xml.rels")
	if err != nil {
		return Result{}, err
	}
	workbookRels := parseOOXMLRels(relsXML, relTypeWorksheet)
	sheets := parseXlsxSheets(workbookXML)

	for _, s := range sheets {
		// Section emission is unconditional — sheets are declared in
		// workbook.xml and belong in the graph regardless of whether
		// the worksheet part or rels can be resolved. Only hyperlink
		// extraction depends on resolution.
		sectionID := schema.LangID("xlsx", "section", abs+":"+slugify(s.Name))
		state.nodes = append(state.nodes, schema.Node{
			ID:         sectionID,
			Label:      s.Name,
			SourceFile: abs,
		})
		state.edges = append(state.edges, schema.Edge{
			Source:     moduleID,
			Target:     sectionID,
			Relation:   "contains",
			Confidence: schema.Extracted,
		})

		target, ok := workbookRels[s.RelID]
		if !ok {
			continue
		}
		sheetPath := resolveOOXMLPartPath("xl/", target)
		sheetXML, err := readPart(sheetPath)
		if err != nil {
			return Result{}, err
		}
		if len(sheetXML) == 0 {
			continue
		}

		// Per-sheet rels: for "xl/worksheets/sheet1.xml" → "xl/worksheets/_rels/sheet1.xml.rels".
		dir, file := filepath.Split(sheetPath)
		sheetRelsXML, err := readPart(dir + "_rels/" + file + ".rels")
		if err != nil {
			return Result{}, err
		}
		sheetRels := parseOOXMLRels(sheetRelsXML, relTypeHyperlink)

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

		// Structural extraction: defined Excel tables and their
		// columns. Tables are linked from the worksheet via tableParts
		// → table rels → xl/tables/tableN.xml. Each table has a name
		// and a list of <tableColumn name="..."/> entries — exactly
		// the structural signal upstream graphify emits for XLSX.
		tableRels := parseOOXMLRels(sheetRelsXML, relTypeTable)
		for _, rid := range parseXlsxTableRefs(sheetXML) {
			tablePartRel, ok := tableRels[rid]
			if !ok || tablePartRel == "" {
				continue
			}
			tablePartPath := resolveOOXMLPartPath(dir, tablePartRel)
			tableXML, err := readPart(tablePartPath)
			if err != nil {
				return Result{}, err
			}
			if len(tableXML) == 0 {
				continue
			}
			tbl := parseXlsxTable(tableXML)
			if tbl.Name == "" {
				continue
			}
			tableID := schema.LangID("xlsx", "table", abs+":"+slugify(s.Name)+":"+slugify(tbl.Name))
			state.nodes = append(state.nodes, schema.Node{
				ID:         tableID,
				Label:      tbl.Name,
				SourceFile: abs,
			})
			state.edges = append(state.edges, schema.Edge{
				Source:     sectionID,
				Target:     tableID,
				Relation:   "contains",
				Confidence: schema.Extracted,
			})
			for _, col := range tbl.Columns {
				if col == "" {
					continue
				}
				colID := schema.LangID("xlsx", "column", abs+":"+slugify(s.Name)+":"+slugify(tbl.Name)+":"+slugify(col))
				state.nodes = append(state.nodes, schema.Node{
					ID:         colID,
					Label:      col,
					SourceFile: abs,
				})
				state.edges = append(state.edges, schema.Edge{
					Source:     tableID,
					Target:     colID,
					Relation:   "contains",
					Confidence: schema.Extracted,
				})
			}
		}
	}

	return Result{Nodes: state.nodes, Edges: state.edges}, nil
}

type xlsxSheet struct {
	Name  string
	RelID string
}

func parseXlsxSheets(data []byte) []xlsxSheet {
	if len(data) == 0 {
		return nil
	}
	type sheet struct {
		Name  string `xml:"name,attr"`
		RelID string `xml:"id,attr"` // r:id — Go's xml decoder matches namespaced attrs by local name
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

// parseXlsxTableRefs extracts the relationship ids of tableParts
// embedded in a worksheet XML. Each id refers to a relationship
// entry in the sheet's _rels file that resolves to xl/tables/tableN.xml.
func parseXlsxTableRefs(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	type tablePart struct {
		RelID string `xml:"id,attr"`
	}
	type worksheet struct {
		Parts []tablePart `xml:"tableParts>tablePart"`
	}
	var ws worksheet
	if err := xml.Unmarshal(data, &ws); err != nil {
		return nil
	}
	out := make([]string, 0, len(ws.Parts))
	for _, p := range ws.Parts {
		if p.RelID != "" {
			out = append(out, p.RelID)
		}
	}
	return out
}

// xlsxTable bundles a defined-Excel-table's name + column headers.
// Range / data-only / total-row info is intentionally dropped —
// the graph cares about structure (this table has these columns),
// not range coordinates.
type xlsxTable struct {
	Name    string
	Columns []string
}

func parseXlsxTable(data []byte) xlsxTable {
	if len(data) == 0 {
		return xlsxTable{}
	}
	type column struct {
		Name string `xml:"name,attr"`
	}
	type table struct {
		Name        string   `xml:"name,attr"`
		DisplayName string   `xml:"displayName,attr"`
		Columns     []column `xml:"tableColumns>tableColumn"`
	}
	var t table
	if err := xml.Unmarshal(data, &t); err != nil {
		return xlsxTable{}
	}
	name := t.Name
	if name == "" {
		name = t.DisplayName
	}
	out := xlsxTable{Name: name}
	for _, c := range t.Columns {
		out.Columns = append(out.Columns, c.Name)
	}
	return out
}

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
