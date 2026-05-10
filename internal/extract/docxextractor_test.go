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

func TestDocxExtractorSkipsNonExternalHyperlinks(t *testing.T) {
	// Hyperlink rels without TargetMode="External" are intra-document
	// (bookmark navigation, internal part references). They MUST NOT
	// produce reference edges — same reason w:anchor links are skipped.
	// Without this filter, a docx with a TOC bookmark target would
	// pollute the graph with edges to "#bookmark1"-style targets.
	dir := t.TempDir()
	docXML := `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"
            xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
 <w:body>
  <w:p>
    <w:hyperlink r:id="rIdInternal"><w:r><w:t>internal</w:t></w:r></w:hyperlink>
    <w:hyperlink r:id="rIdExternal"><w:r><w:t>external</w:t></w:r></w:hyperlink>
  </w:p>
 </w:body>
</w:document>`
	relsXML := `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rIdInternal" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink" Target="bookmark1"/>
  <Relationship Id="rIdExternal" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink" Target="https://example.com" TargetMode="External"/>
</Relationships>`
	path := writeMinimalDocx(t, dir, "mixed.docx", docXML, relsXML)
	res, err := DocxExtractor{}.Extract(path)
	if err != nil {
		t.Fatal(err)
	}
	var external, internal int
	for _, e := range res.Edges {
		switch e.Target {
		case "docx:link:https://example.com":
			external++
		case "docx:link:bookmark1":
			internal++
		}
	}
	if external != 1 {
		t.Fatalf("expected exactly one external-link edge, got %d (edges=%+v)", external, res.Edges)
	}
	if internal != 0 {
		t.Fatalf("internal hyperlink rel must not produce an edge, got %d (edges=%+v)", internal, res.Edges)
	}
}

func TestDocxExtractorHyperlinkInsideHeadingAttributedToHeading(t *testing.T) {
	// A hyperlink inside a Heading1 paragraph must source from that
	// heading's section node, not from the previous section. Mirrors
	// the Markdown/HTML extractors' section-scoped attribution.
	//
	// Bug history: edges were emitted on </w:hyperlink> (mid-paragraph)
	// but heading section nodes are created on </w:p> (paragraph end),
	// so heading-paragraph hyperlinks sourced from the *previous*
	// section. Fix defers edge emission to </w:p> after the section
	// has been pushed.
	dir := t.TempDir()
	docXML := `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"
            xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
 <w:body>
  <w:p><w:pPr><w:pStyle w:val="Heading1"/></w:pPr><w:r><w:t>Earlier</w:t></w:r></w:p>
  <w:p><w:pPr><w:pStyle w:val="Heading1"/></w:pPr>
    <w:r><w:t xml:space="preserve">Heading with </w:t></w:r>
    <w:hyperlink r:id="rId1"><w:r><w:t>a link</w:t></w:r></w:hyperlink>
  </w:p>
 </w:body>
</w:document>`
	relsXML := `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink" Target="https://example.com/in-heading" TargetMode="External"/>
</Relationships>`
	path := writeMinimalDocx(t, dir, "headinglink.docx", docXML, relsXML)
	res, err := DocxExtractor{}.Extract(path)
	if err != nil {
		t.Fatal(err)
	}
	// Heading-with-link section paragraph text ends up as "Heading with a link"
	// (the hyperlink's run text concatenates into the paragraph text).
	var headingLinkID, earlierID string
	for _, n := range res.Nodes {
		switch n.Label {
		case "Heading with a link":
			headingLinkID = n.ID
		case "Earlier":
			earlierID = n.ID
		}
	}
	if headingLinkID == "" {
		t.Fatalf("expected 'Heading with a link' section node, got %+v", res.Nodes)
	}
	var found bool
	for _, e := range res.Edges {
		if e.Target == "docx:link:https://example.com/in-heading" {
			if e.Source == earlierID {
				t.Fatalf("hyperlink inside heading misattributed to previous section: %+v", e)
			}
			if e.Source == headingLinkID {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("expected hyperlink edge sourced from the heading section, got edges=%+v", res.Edges)
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
