package extract

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func writeMinimalDocx(t *testing.T, dir, name, documentXML, relsXML string) string {
	t.Helper()
	parts := map[string]string{"word/document.xml": documentXML}
	if relsXML != "" {
		parts["word/_rels/document.xml.rels"] = relsXML
	}
	return writeZipFixture(t, dir, name, parts)
}

func TestDocxExtractorTitleHeadingsAndHyperlink(t *testing.T) {
	dir := t.TempDir()
	docXML := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"
            xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
 <w:body>
  <w:p><w:pPr><w:pStyle w:val="Title"/></w:pPr><w:r><w:t>The Project</w:t></w:r></w:p>
  <w:p><w:pPr><w:pStyle w:val="Heading1"/></w:pPr><w:r><w:t>Overview</w:t></w:r></w:p>
  <w:p>
    <w:r><w:t xml:space="preserve">See </w:t></w:r>
    <w:hyperlink r:id="rId7"><w:r><w:t>the docs</w:t></w:r></w:hyperlink>
    <w:r><w:t xml:space="preserve"> for details.</w:t></w:r>
  </w:p>
  <w:p><w:pPr><w:pStyle w:val="Heading2"/></w:pPr><w:r><w:t>Installation</w:t></w:r></w:p>
  <w:p><w:r><w:t>Body text.</w:t></w:r></w:p>
 </w:body>
</w:document>`
	relsXML := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
 <Relationship Id="rId7" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink" Target="https://example.com/docs" TargetMode="External"/>
</Relationships>`
	path := writeMinimalDocx(t, dir, "report.docx", docXML, relsXML)

	res, err := DocxExtractor{}.Extract(path)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	labels := map[string]bool{}
	for _, n := range res.Nodes {
		labels[n.Label] = true
	}
	if !labels["The Project"] {
		t.Fatalf("module label should be Title-style paragraph text, got labels=%v", labels)
	}
	for _, want := range []string{"Overview", "Installation"} {
		if !labels[want] {
			t.Fatalf("missing section %q in %v", want, labels)
		}
	}

	// Hyperlink edge should be sourced from the enclosing section
	// (Overview) and target the resolved URL.
	var found bool
	for _, e := range res.Edges {
		if e.Relation == "references" && e.Target == "docx:link:https://example.com/docs" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing hyperlink reference edge, got edges=%+v", res.Edges)
	}
}

func TestDocxExtractorFallsBackThroughTitleH1Basename(t *testing.T) {
	dir := t.TempDir()
	// No Title style, but Heading1 exists → should pick the first H1.
	docXML := `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
 <w:body>
  <w:p><w:pPr><w:pStyle w:val="Heading1"/></w:pPr><w:r><w:t>First Heading</w:t></w:r></w:p>
  <w:p><w:pPr><w:pStyle w:val="Heading1"/></w:pPr><w:r><w:t>Second Heading</w:t></w:r></w:p>
 </w:body>
</w:document>`
	path := writeMinimalDocx(t, dir, "h1only.docx", docXML, "")
	res, err := DocxExtractor{}.Extract(path)
	if err != nil {
		t.Fatal(err)
	}
	if res.Nodes[0].Label != "First Heading" {
		t.Fatalf("expected first H1 as module label, got %q", res.Nodes[0].Label)
	}

	// Now an empty body → falls all the way back to basename.
	emptyXML := `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body></w:body></w:document>`
	path2 := writeMinimalDocx(t, dir, "empty.docx", emptyXML, "")
	res2, err := DocxExtractor{}.Extract(path2)
	if err != nil {
		t.Fatal(err)
	}
	if res2.Nodes[0].Label != "empty.docx" {
		t.Fatalf("expected basename label, got %q", res2.Nodes[0].Label)
	}
}

func TestDocxExtractorSkipsAnchorOnlyHyperlinks(t *testing.T) {
	// A hyperlink with w:anchor (intra-document) and no r:id must NOT
	// produce an edge — those are navigation, not cross-references.
	dir := t.TempDir()
	docXML := `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"
            xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
 <w:body>
  <w:p><w:pPr><w:pStyle w:val="Heading1"/></w:pPr><w:r><w:t>Top</w:t></w:r></w:p>
  <w:p>
    <w:hyperlink w:anchor="bookmark1"><w:r><w:t>jump</w:t></w:r></w:hyperlink>
  </w:p>
 </w:body>
</w:document>`
	path := writeMinimalDocx(t, dir, "anchor.docx", docXML, "")
	res, err := DocxExtractor{}.Extract(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range res.Edges {
		if e.Relation == "references" {
			t.Fatalf("anchor-only hyperlink should not produce a reference edge, got %+v", e)
		}
	}
}

func TestDocxExtractorMissingDocumentXMLReturnsBareModule(t *testing.T) {
	// An empty zip (no word/document.xml) should still produce a module
	// node so the file appears in the graph rather than erroring out.
	dir := t.TempDir()
	path := filepath.Join(dir, "weird.docx")
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	if _, err := zw.Create("dummy.txt"); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}

	res, err := DocxExtractor{}.Extract(path)
	if err != nil {
		t.Fatalf("extract should not error on docx-shaped-but-empty zip: %v", err)
	}
	if len(res.Nodes) != 1 || res.Nodes[0].Label != "weird.docx" {
		t.Fatalf("expected single basename-labeled module node, got %+v", res.Nodes)
	}
}
