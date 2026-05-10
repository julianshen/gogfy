package extract

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// writeMinimalXlsx builds a small in-memory .xlsx and returns its path.
// Each entry in `parts` becomes a file inside the zip; missing parts
// (workbook.xml, sheet, rels) let tests probe degraded inputs.
func writeMinimalXlsx(t *testing.T, dir, name string, parts map[string]string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for n, body := range parts {
		w, err := zw.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

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
	path := writeMinimalXlsx(t, dir, "book.xlsx", parts)

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
	path := writeMinimalXlsx(t, dir, "book.xlsx", parts)
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
	path := writeMinimalXlsx(t, dir, "partial.xlsx", parts)
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

func TestXlsxExtractorMissingWorkbookReturnsBareModule(t *testing.T) {
	dir := t.TempDir()
	path := writeMinimalXlsx(t, dir, "weird.xlsx", map[string]string{
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
	path := writeMinimalXlsx(t, dir, "plain.xlsx", parts)
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
