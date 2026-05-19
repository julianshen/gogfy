package extract

import (
	"testing"
)

func TestXlsxExtractorSheetsAndHyperlinks(t *testing.T) {
	dir := t.TempDir()
	parts := map[string]string{
		"xl/workbook.xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"
          xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <sheets>
    <sheet name="Customers" sheetId="1" r:id="rId1"/>
    <sheet name="Notes" sheetId="2" r:id="rId2"/>
  </sheets>
</workbook>`,
		"xl/_rels/workbook.xml.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/>
  <Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet2.xml"/>
</Relationships>`,
		"xl/worksheets/sheet1.xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"
           xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <sheetData/>
  <hyperlinks>
    <hyperlink ref="A1" r:id="rIdHL1"/>
    <hyperlink ref="A2" r:id="rIdHL2"/>
  </hyperlinks>
</worksheet>`,
		"xl/worksheets/_rels/sheet1.xml.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rIdHL1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink" Target="https://example.com/customer/1" TargetMode="External"/>
  <Relationship Id="rIdHL2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink" Target="https://example.com/customer/2" TargetMode="External"/>
</Relationships>`,
		"xl/worksheets/sheet2.xml": `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData/></worksheet>`,
	}
	path := writeZipFixture(t, dir, "book.xlsx", parts)

	res, err := XlsxExtractor{}.Extract(path)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	labels := map[string]bool{}
	for _, n := range res.Nodes {
		labels[n.Label] = true
	}
	if res.Nodes[0].Label != "book.xlsx" {
		t.Fatalf("module label should be basename, got %q", res.Nodes[0].Label)
	}
	for _, want := range []string{"Customers", "Notes"} {
		if !labels[want] {
			t.Fatalf("missing sheet section %q in %v", want, labels)
		}
	}

	targets := map[string]bool{}
	for _, e := range res.Edges {
		if e.Relation == "references" {
			targets[e.Target] = true
		}
	}
	for _, want := range []string{
		"xlsx:link:https://example.com/customer/1",
		"xlsx:link:https://example.com/customer/2",
	} {
		if !targets[want] {
			t.Fatalf("missing reference target %q in %v", want, targets)
		}
	}
}

func TestXlsxExtractorHyperlinkAttributedToOwnSheet(t *testing.T) {
	// A hyperlink in sheet2 must source from sheet2's section node, not
	// sheet1's. Tests that we don't accidentally collapse all sheet links
	// onto the workbook module or onto the first sheet.
	dir := t.TempDir()
	parts := map[string]string{
		"xl/workbook.xml": `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"
          xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <sheets>
    <sheet name="Alpha" sheetId="1" r:id="rId1"/>
    <sheet name="Beta" sheetId="2" r:id="rId2"/>
  </sheets>
</workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/>
  <Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet2.xml"/>
</Relationships>`,
		"xl/worksheets/sheet1.xml": `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData/></worksheet>`,
		"xl/worksheets/sheet2.xml": `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"
           xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <sheetData/>
  <hyperlinks><hyperlink ref="A1" r:id="rIdBeta"/></hyperlinks>
</worksheet>`,
		"xl/worksheets/_rels/sheet2.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rIdBeta" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink" Target="https://example.com/beta" TargetMode="External"/>
</Relationships>`,
	}
	path := writeZipFixture(t, dir, "book.xlsx", parts)
	res, err := XlsxExtractor{}.Extract(path)
	if err != nil {
		t.Fatal(err)
	}

	// Find the Beta section node's ID, then verify the edge sources from it.
	var betaID string
	for _, n := range res.Nodes {
		if n.Label == "Beta" {
			betaID = n.ID
		}
	}
	if betaID == "" {
		t.Fatalf("Beta section node not found, got nodes=%+v", res.Nodes)
	}
	var found bool
	for _, e := range res.Edges {
		if e.Target == "xlsx:link:https://example.com/beta" && e.Source == betaID {
			found = true
		}
	}
	if !found {
		t.Fatalf("hyperlink edge should be sourced from Beta section, got edges=%+v", res.Edges)
	}
}

func TestXlsxExtractorSheetSectionEmittedWhenRelsUnresolvable(t *testing.T) {
	// A sheet declared in xl/workbook.xml must still produce a section
	// node even if its worksheet part or rels file is missing/incomplete.
	// Section nodes carry the workbook's structural shape; only hyperlink
	// extraction depends on rels resolution.
	dir := t.TempDir()
	parts := map[string]string{
		"xl/workbook.xml": `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"
          xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <sheets>
    <sheet name="Resolved" sheetId="1" r:id="rId1"/>
    <sheet name="Orphaned" sheetId="2" r:id="rIdMissing"/>
  </sheets>
</workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/>
</Relationships>`,
		"xl/worksheets/sheet1.xml": `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData/></worksheet>`,
	}
	path := writeZipFixture(t, dir, "partial.xlsx", parts)
	res, err := XlsxExtractor{}.Extract(path)
	if err != nil {
		t.Fatal(err)
	}
	labels := map[string]bool{}
	for _, n := range res.Nodes {
		labels[n.Label] = true
	}
	for _, want := range []string{"Resolved", "Orphaned"} {
		if !labels[want] {
			t.Fatalf("expected sheet section %q (rels resolution should not gate node emission), got labels=%v", want, labels)
		}
	}
}

func TestXlsxExtractorAcceptsAbsoluteWorksheetTargets(t *testing.T) {
	// presentation.xml.rels Target attributes can be either part-
	// relative ("worksheets/sheet1.xml" — Excel emits this) or package-
	// absolute ("/xl/worksheets/sheet1.xml" — Open XML SDK emits this).
	// Treating absolute as relative previously produced
	// "xl/xl/worksheets/..." and silently dropped every sheet's
	// hyperlink edges under SDK-produced workbooks.
	dir := t.TempDir()
	parts := map[string]string{
		"xl/workbook.xml": `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"
          xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <sheets><sheet name="Data" sheetId="1" r:id="rId1"/></sheets>
</workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="/xl/worksheets/sheet1.xml"/>
</Relationships>`,
		"xl/worksheets/sheet1.xml": `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"
           xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <sheetData/>
  <hyperlinks><hyperlink ref="A1" r:id="rIdHL"/></hyperlinks>
</worksheet>`,
		"xl/worksheets/_rels/sheet1.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rIdHL" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink" Target="https://example.com/data" TargetMode="External"/>
</Relationships>`,
	}
	path := writeZipFixture(t, dir, "absolute.xlsx", parts)
	res, err := XlsxExtractor{}.Extract(path)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, e := range res.Edges {
		if e.Target == "xlsx:link:https://example.com/data" {
			found = true
		}
	}
	if !found {
		t.Fatalf("absolute /xl/... target should resolve and yield hyperlink edge, got %+v", res.Edges)
	}
	// Independently pin the section node — proves the absolute target
	// also resolved into the workbook's structural section emission, not
	// just the downstream hyperlink lookup. A future refactor that fed
	// the raw target into one path but not the other would hide here.
	var hasSheet bool
	for _, n := range res.Nodes {
		if n.Label == "Data" {
			hasSheet = true
		}
	}
	if !hasSheet {
		t.Fatalf("absolute target should still produce 'Data' sheet section, got %+v", res.Nodes)
	}
}

func TestXlsxExtractorMissingWorkbookReturnsBareModule(t *testing.T) {
	dir := t.TempDir()
	path := writeZipFixture(t, dir, "weird.xlsx", map[string]string{
		"dummy.txt": "",
	})
	res, err := XlsxExtractor{}.Extract(path)
	if err != nil {
		t.Fatalf("extract should not error on xlsx-shaped-but-empty zip: %v", err)
	}
	if len(res.Nodes) != 1 || res.Nodes[0].Label != "weird.xlsx" {
		t.Fatalf("expected single basename-labeled module node, got %+v", res.Nodes)
	}
}

func TestXlsxExtractorSheetWithoutHyperlinksProducesNoEdges(t *testing.T) {
	dir := t.TempDir()
	parts := map[string]string{
		"xl/workbook.xml": `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"
          xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <sheets><sheet name="Empty" sheetId="1" r:id="rId1"/></sheets>
</workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/>
</Relationships>`,
		"xl/worksheets/sheet1.xml": `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData/></worksheet>`,
	}
	path := writeZipFixture(t, dir, "plain.xlsx", parts)
	res, err := XlsxExtractor{}.Extract(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Edges) != 0 {
		t.Fatalf("expected no edges on link-free workbook, got %+v", res.Edges)
	}
	// Section node should still exist so the sheet is in the graph.
	var hasEmpty bool
	for _, n := range res.Nodes {
		if n.Label == "Empty" {
			hasEmpty = true
		}
	}
	if !hasEmpty {
		t.Fatalf("expected 'Empty' section node, got %+v", res.Nodes)
	}
}

func TestXlsxExtractorEmitsTableAndColumnNodes(t *testing.T) {
	// Defined Excel tables (the structured Table objects) live in
	// xl/tables/tableN.xml and are linked from the worksheet via
	// <tableParts><tablePart r:id="rIdTbl1"/></tableParts>. Each
	// table should produce a `xlsx:table` node + `contains` edge
	// from its sheet; each <tableColumn> should produce a
	// `xlsx:column` node + `contains` edge from the table.
	dir := t.TempDir()
	parts := map[string]string{
		"xl/workbook.xml": `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"
          xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <sheets><sheet name="Data" sheetId="1" r:id="rId1"/></sheets>
</workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/>
</Relationships>`,
		"xl/worksheets/sheet1.xml": `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"
            xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <sheetData/>
  <tableParts count="1"><tablePart r:id="rIdTbl1"/></tableParts>
</worksheet>`,
		"xl/worksheets/_rels/sheet1.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rIdTbl1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/table" Target="../tables/table1.xml"/>
</Relationships>`,
		"xl/tables/table1.xml": `<table xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"
        id="1" name="Customers" displayName="Customers" ref="A1:C10">
  <tableColumns count="3">
    <tableColumn id="1" name="ID"/>
    <tableColumn id="2" name="Name"/>
    <tableColumn id="3" name="Email"/>
  </tableColumns>
</table>`,
	}
	path := writeZipFixture(t, dir, "structured.xlsx", parts)
	res, err := XlsxExtractor{}.Extract(path)
	if err != nil {
		t.Fatal(err)
	}
	labels := map[string]bool{}
	for _, n := range res.Nodes {
		labels[n.Label] = true
	}
	for _, want := range []string{"Data", "Customers", "ID", "Name", "Email"} {
		if !labels[want] {
			t.Errorf("missing node label %q in %v", want, labels)
		}
	}
	// Verify edge structure: sheet contains table, table contains columns.
	var sheetContainsTable, tableContainsCol bool
	for _, e := range res.Edges {
		if e.Relation == "contains" {
			if containsSub(e.Source, ":section:") && containsSub(e.Target, ":table:") {
				sheetContainsTable = true
			}
			if containsSub(e.Source, ":table:") && containsSub(e.Target, ":column:") {
				tableContainsCol = true
			}
		}
	}
	if !sheetContainsTable {
		t.Errorf("missing sheet→table contains edge: %+v", res.Edges)
	}
	if !tableContainsCol {
		t.Errorf("missing table→column contains edge: %+v", res.Edges)
	}
}

func containsSub(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
