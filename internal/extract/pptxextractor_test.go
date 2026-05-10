package extract

import (
	"testing"
)

func TestPPTXExtractorSlidesAndHyperlinks(t *testing.T) {
	dir := t.TempDir()
	parts := map[string]string{
		"ppt/presentation.xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:presentation xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"
                xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <p:sldIdLst>
    <p:sldId id="256" r:id="rId1"/>
    <p:sldId id="257" r:id="rId2"/>
  </p:sldIdLst>
</p:presentation>`,
		"ppt/_rels/presentation.xml.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide1.xml"/>
  <Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide2.xml"/>
</Relationships>`,
		"ppt/slides/slide1.xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"
       xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"
       xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <p:cSld><p:spTree>
    <p:sp>
      <p:nvSpPr><p:nvPr><p:ph type="title"/></p:nvPr></p:nvSpPr>
      <p:txBody><a:p><a:r><a:t>Quarterly Review</a:t></a:r></a:p></p:txBody>
    </p:sp>
    <p:sp>
      <p:txBody><a:p><a:r><a:rPr><a:hlinkClick r:id="rIdLink1"/></a:rPr><a:t>see docs</a:t></a:r></a:p></p:txBody>
    </p:sp>
  </p:spTree></p:cSld>
</p:sld>`,
		"ppt/slides/_rels/slide1.xml.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rIdLink1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink" Target="https://example.com/q3" TargetMode="External"/>
</Relationships>`,
		"ppt/slides/slide2.xml": `<p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"
       xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main">
  <p:cSld><p:spTree>
    <p:sp>
      <p:nvSpPr><p:nvPr><p:ph type="ctrTitle"/></p:nvPr></p:nvSpPr>
      <p:txBody><a:p><a:r><a:t>Closing Slide</a:t></a:r></a:p></p:txBody>
    </p:sp>
  </p:spTree></p:cSld>
</p:sld>`,
	}
	path := writeZipFixture(t, dir, "deck.pptx", parts)
	res, err := PPTXExtractor{}.Extract(path)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if res.Nodes[0].Label != "deck.pptx" {
		t.Fatalf("module label should be basename, got %q", res.Nodes[0].Label)
	}
	labels := map[string]bool{}
	for _, n := range res.Nodes {
		labels[n.Label] = true
	}
	for _, want := range []string{"Quarterly Review", "Closing Slide"} {
		if !labels[want] {
			t.Fatalf("missing slide section %q in %v", want, labels)
		}
	}
	var found bool
	for _, e := range res.Edges {
		if e.Target == "pptx:link:https://example.com/q3" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing hyperlink reference edge, got edges=%+v", res.Edges)
	}
}

func TestPPTXExtractorHyperlinkAttributedToOwnSlide(t *testing.T) {
	dir := t.TempDir()
	parts := map[string]string{
		"ppt/presentation.xml": `<p:presentation xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"
                xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <p:sldIdLst>
    <p:sldId id="256" r:id="rId1"/>
    <p:sldId id="257" r:id="rId2"/>
  </p:sldIdLst>
</p:presentation>`,
		"ppt/_rels/presentation.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide1.xml"/>
  <Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide2.xml"/>
</Relationships>`,
		"ppt/slides/slide1.xml": `<p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"
       xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main">
  <p:cSld><p:spTree>
    <p:sp>
      <p:nvSpPr><p:nvPr><p:ph type="title"/></p:nvPr></p:nvSpPr>
      <p:txBody><a:p><a:r><a:t>Alpha</a:t></a:r></a:p></p:txBody>
    </p:sp>
  </p:spTree></p:cSld>
</p:sld>`,
		"ppt/slides/slide2.xml": `<p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"
       xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"
       xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <p:cSld><p:spTree>
    <p:sp>
      <p:nvSpPr><p:nvPr><p:ph type="title"/></p:nvPr></p:nvSpPr>
      <p:txBody><a:p><a:r><a:t>Beta</a:t></a:r></a:p></p:txBody>
    </p:sp>
    <p:sp>
      <p:txBody><a:p><a:r><a:rPr><a:hlinkClick r:id="rIdBeta"/></a:rPr><a:t>link</a:t></a:r></a:p></p:txBody>
    </p:sp>
  </p:spTree></p:cSld>
</p:sld>`,
		"ppt/slides/_rels/slide2.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rIdBeta" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink" Target="https://example.com/beta" TargetMode="External"/>
</Relationships>`,
	}
	path := writeZipFixture(t, dir, "deck.pptx", parts)
	res, err := PPTXExtractor{}.Extract(path)
	if err != nil {
		t.Fatal(err)
	}
	var betaID string
	for _, n := range res.Nodes {
		if n.Label == "Beta" {
			betaID = n.ID
		}
	}
	if betaID == "" {
		t.Fatalf("Beta section not found in %+v", res.Nodes)
	}
	for _, e := range res.Edges {
		if e.Target == "pptx:link:https://example.com/beta" && e.Source != betaID {
			t.Fatalf("hyperlink misattributed: source=%s, want=%s", e.Source, betaID)
		}
	}
}

func TestPPTXExtractorSlideWithoutTitleFallsBackToSlideN(t *testing.T) {
	// A slide whose title placeholder is empty (or absent) should still
	// produce a section node, labeled "Slide N" so the structural
	// information from presentation.xml isn't lost.
	dir := t.TempDir()
	parts := map[string]string{
		"ppt/presentation.xml": `<p:presentation xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"
                xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <p:sldIdLst><p:sldId id="256" r:id="rId1"/></p:sldIdLst>
</p:presentation>`,
		"ppt/_rels/presentation.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide1.xml"/>
</Relationships>`,
		"ppt/slides/slide1.xml": `<p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"
       xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main">
  <p:cSld><p:spTree>
    <p:sp><p:txBody><a:p><a:r><a:t>Just body text, no title placeholder</a:t></a:r></a:p></p:txBody></p:sp>
  </p:spTree></p:cSld>
</p:sld>`,
	}
	path := writeZipFixture(t, dir, "untitled.pptx", parts)
	res, err := PPTXExtractor{}.Extract(path)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, n := range res.Nodes {
		if n.Label == "Slide 1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected 'Slide 1' fallback label, got %+v", res.Nodes)
	}
}

func TestPPTXExtractorMissingPresentationReturnsBareModule(t *testing.T) {
	dir := t.TempDir()
	path := writeZipFixture(t, dir, "weird.pptx", map[string]string{
		"dummy.txt": "",
	})
	res, err := PPTXExtractor{}.Extract(path)
	if err != nil {
		t.Fatalf("extract should not error on pptx-shaped-but-empty zip: %v", err)
	}
	if len(res.Nodes) != 1 || res.Nodes[0].Label != "weird.pptx" {
		t.Fatalf("expected single basename-labeled module node, got %+v", res.Nodes)
	}
}
