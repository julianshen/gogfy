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
	// Slide with no title placeholder at all should still produce a
	// section node labeled "Slide N" so structural info from
	// presentation.xml isn't lost.
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

func TestPPTXExtractorEmptyTitlePlaceholderFallsBackToSlideN(t *testing.T) {
	// A title placeholder containing only whitespace must not produce
	// a label of "" or " " — it should fall back to "Slide N", same as
	// the no-placeholder case.
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
    <p:sp>
      <p:nvSpPr><p:nvPr><p:ph type="title"/></p:nvPr></p:nvSpPr>
      <p:txBody><a:p><a:r><a:t>   </a:t></a:r></a:p></p:txBody>
    </p:sp>
  </p:spTree></p:cSld>
</p:sld>`,
	}
	path := writeZipFixture(t, dir, "blanktitle.pptx", parts)
	res, err := PPTXExtractor{}.Extract(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range res.Nodes {
		if n.Label == "" || n.Label == " " {
			t.Fatalf("blank/space label leaked: %+v", n)
		}
	}
	var found bool
	for _, n := range res.Nodes {
		if n.Label == "Slide 1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("whitespace-only title should fall back to 'Slide 1', got %+v", res.Nodes)
	}
}

func TestPPTXExtractorFirstTitleShapeWins(t *testing.T) {
	// A slide with two title-typed shapes — the first wins; the second
	// must NOT overwrite the section label. Pins the titleDone flag's
	// purpose so a future refactor that drops it would be caught.
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
    <p:sp>
      <p:nvSpPr><p:nvPr><p:ph type="title"/></p:nvPr></p:nvSpPr>
      <p:txBody><a:p><a:r><a:t>First Title</a:t></a:r></a:p></p:txBody>
    </p:sp>
    <p:sp>
      <p:nvSpPr><p:nvPr><p:ph type="title"/></p:nvPr></p:nvSpPr>
      <p:txBody><a:p><a:r><a:t>Second Title</a:t></a:r></a:p></p:txBody>
    </p:sp>
  </p:spTree></p:cSld>
</p:sld>`,
	}
	path := writeZipFixture(t, dir, "twotitle.pptx", parts)
	res, err := PPTXExtractor{}.Extract(path)
	if err != nil {
		t.Fatal(err)
	}
	labels := map[string]bool{}
	for _, n := range res.Nodes {
		labels[n.Label] = true
	}
	if !labels["First Title"] {
		t.Fatalf("expected first title to win, got %+v", labels)
	}
	if labels["Second Title"] {
		t.Fatalf("second title shape should not produce a section, got %+v", labels)
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

func TestPPTXExtractorParsesRIdWhenAttributeOrderReversed(t *testing.T) {
	// <p:sldId> carries both `id` (numeric internal id) and `r:id`
	// (rel reference). With xml:"id,attr" alone, Go's decoder picks
	// the first attribute by source order — when r:id is emitted
	// before id, the wrong value is captured. Some producers do this.
	dir := t.TempDir()
	parts := map[string]string{
		"ppt/presentation.xml": `<p:presentation xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"
                xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <p:sldIdLst><p:sldId r:id="rId1" id="256"/></p:sldIdLst>
</p:presentation>`,
		"ppt/_rels/presentation.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide1.xml"/>
</Relationships>`,
		"ppt/slides/slide1.xml": `<p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"
       xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main">
  <p:cSld><p:spTree>
    <p:sp>
      <p:nvSpPr><p:nvPr><p:ph type="title"/></p:nvPr></p:nvSpPr>
      <p:txBody><a:p><a:r><a:t>Real Title</a:t></a:r></a:p></p:txBody>
    </p:sp>
  </p:spTree></p:cSld>
</p:sld>`,
	}
	path := writeZipFixture(t, dir, "reversed.pptx", parts)
	res, err := PPTXExtractor{}.Extract(path)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, n := range res.Nodes {
		if n.Label == "Real Title" {
			found = true
		}
	}
	if !found {
		t.Fatalf("title resolution failed when r:id appears before id (rel parse picked wrong attr); got %+v", res.Nodes)
	}
}

func TestPPTXExtractorDuplicateSlideTitlesProduceDistinctNodes(t *testing.T) {
	// Real decks commonly have repeated titles ("Agenda", "Summary",
	// "Appendix"). Without slide-ordinal in the section ID, all such
	// slides would collapse onto one node and later slides' hyperlinks
	// would be misattributed to the first.
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
      <p:txBody><a:p><a:r><a:t>Appendix</a:t></a:r></a:p></p:txBody>
    </p:sp>
  </p:spTree></p:cSld>
</p:sld>`,
		"ppt/slides/slide2.xml": `<p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"
       xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main">
  <p:cSld><p:spTree>
    <p:sp>
      <p:nvSpPr><p:nvPr><p:ph type="title"/></p:nvPr></p:nvSpPr>
      <p:txBody><a:p><a:r><a:t>Appendix</a:t></a:r></a:p></p:txBody>
    </p:sp>
  </p:spTree></p:cSld>
</p:sld>`,
	}
	path := writeZipFixture(t, dir, "dup.pptx", parts)
	res, err := PPTXExtractor{}.Extract(path)
	if err != nil {
		t.Fatal(err)
	}
	// Count section nodes labeled "Appendix"; both slides must produce
	// distinct nodes (different IDs) even though labels match.
	ids := map[string]bool{}
	for _, n := range res.Nodes {
		if n.Label == "Appendix" {
			ids[n.ID] = true
		}
	}
	if len(ids) != 2 {
		t.Fatalf("two 'Appendix' slides should produce two distinct section IDs, got %d (%+v)", len(ids), ids)
	}
}

func TestPPTXExtractorAcceptsAbsoluteSlideTargets(t *testing.T) {
	// presentation.xml.rels Target attributes can be either part-
	// relative ("slides/slide1.xml") or package-absolute
	// ("/ppt/slides/slide1.xml" — Open XML SDK style). Treating
	// absolute as relative previously produced "ppt/ppt/slides/..."
	// and missed every slide.
	dir := t.TempDir()
	parts := map[string]string{
		"ppt/presentation.xml": `<p:presentation xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"
                xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <p:sldIdLst><p:sldId id="256" r:id="rId1"/></p:sldIdLst>
</p:presentation>`,
		"ppt/_rels/presentation.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="/ppt/slides/slide1.xml"/>
</Relationships>`,
		"ppt/slides/slide1.xml": `<p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"
       xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main">
  <p:cSld><p:spTree>
    <p:sp>
      <p:nvSpPr><p:nvPr><p:ph type="title"/></p:nvPr></p:nvSpPr>
      <p:txBody><a:p><a:r><a:t>SDK Title</a:t></a:r></a:p></p:txBody>
    </p:sp>
  </p:spTree></p:cSld>
</p:sld>`,
	}
	path := writeZipFixture(t, dir, "absolute.pptx", parts)
	res, err := PPTXExtractor{}.Extract(path)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, n := range res.Nodes {
		if n.Label == "SDK Title" {
			found = true
		}
	}
	if !found {
		t.Fatalf("absolute /ppt/... target should resolve; got %+v", res.Nodes)
	}
}
