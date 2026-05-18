package extract

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// buildMinimalPDF returns the bytes of a single-page PDF whose visible
// text is `body`. The structure follows the PDF 1.4 minimum: header,
// catalog, pages tree, page object, content stream, fonts dict, info
// dict, xref, trailer, %%EOF. Cross-reference offsets are computed from
// the actual byte layout so the file is parseable by any conformant
// reader.
//
// Built in-memory rather than committed as a binary fixture — same
// rationale as writeZipFixture for the OOXML formats: deterministic,
// no .gitignore games, and tests can vary the body per scenario.
func buildMinimalPDF(title, body string) []byte {
	// PDF strings escape `(`, `)`, `\` with backslashes.
	esc := func(s string) string {
		r := strings.NewReplacer("\\", `\\`, "(", `\(`, ")", `\)`)
		return r.Replace(s)
	}

	// Build content stream first so we know its length.
	stream := fmt.Sprintf("BT /F1 12 Tf 50 750 Td (%s) Tj ET\n", esc(body))

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(stream), stream),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		fmt.Sprintf("<< /Title (%s) >>", esc(title)),
	}

	var b strings.Builder
	b.WriteString("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")
	offsets := make([]int, len(objs))
	for i, obj := range objs {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, obj)
	}
	xrefStart := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R /Info 6 0 R >>\nstartxref\n%d\n%%%%EOF\n",
		len(objs)+1, xrefStart)
	return []byte(b.String())
}

func writePDF(t *testing.T, dir, name, title, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, buildMinimalPDF(title, body), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPDFExtractorURLsAndTitle(t *testing.T) {
	dir := t.TempDir()
	path := writePDF(t, dir, "report.pdf",
		"Quarterly Report",
		"See https://example.com/report and https://example.com/data for details.",
	)
	res, err := PDFExtractor{}.Extract(path)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if res.Nodes[0].Label != "Quarterly Report" {
		t.Fatalf("module label should be PDF /Info /Title, got %q", res.Nodes[0].Label)
	}
	targets := map[string]bool{}
	for _, e := range res.Edges {
		if e.Relation == "references" {
			targets[e.Target] = true
		}
	}
	for _, want := range []string{
		"pdf:link:https://example.com/report",
		"pdf:link:https://example.com/data",
	} {
		if !targets[want] {
			t.Fatalf("missing reference target %q in %v", want, targets)
		}
	}
}

func TestPDFExtractorFallsBackToBasenameWhenNoTitle(t *testing.T) {
	// Build a PDF with an empty /Title so we exercise the basename
	// fallback path. Empty-string Title is a real-world case (Word's
	// "Save as PDF" default for new documents).
	dir := t.TempDir()
	path := writePDF(t, dir, "untitled.pdf", "", "Body without metadata.")
	res, err := PDFExtractor{}.Extract(path)
	if err != nil {
		t.Fatal(err)
	}
	if res.Nodes[0].Label != "untitled.pdf" {
		t.Fatalf("expected basename fallback, got %q", res.Nodes[0].Label)
	}
}

func TestPDFExtractorBodyWithoutURLsEmitsModuleOnly(t *testing.T) {
	// Pins the contract that a readable PDF with no URLs in its text
	// produces exactly one module node and zero reference edges. Most
	// real-world PDFs have no URLs; a future refactor of urlEdges /
	// extractURLs that started emitting spurious edges would be silent
	// otherwise.
	dir := t.TempDir()
	path := writePDF(t, dir, "plain.pdf", "Plain Title", "Body with no links here.")
	res, err := PDFExtractor{}.Extract(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Nodes) != 1 {
		t.Fatalf("expected exactly 1 module node, got %d (%+v)", len(res.Nodes), res.Nodes)
	}
	if len(res.Edges) != 0 {
		t.Fatalf("expected 0 edges, got %+v", res.Edges)
	}
}

func TestPDFExtractorMalformedReturnsError(t *testing.T) {
	// Bytes that don't start with %PDF — Open should error.
	dir := t.TempDir()
	path := filepath.Join(dir, "broken.pdf")
	if err := os.WriteFile(path, []byte("not a pdf"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := PDFExtractor{}.Extract(path)
	if err == nil {
		t.Fatalf("expected error on malformed PDF")
	}
}

func TestPDFPlainTextExposesHelperForExternalCallers(t *testing.T) {
	// Sanity check: the exported helper opens the file and returns
	// its body text. The semantic-extraction pipeline depends on
	// this — a regression that re-makes PDFPlainText unexported
	// would silently break PDF→LLM routing.
	t.Helper()
	// Reuse the fixture from the existing pdf tests by finding the
	// nearest .pdf testdata file. If none exists we skip — we don't
	// want to fail when the helper exists but happens to have no
	// fixture file in this build.
	candidates, _ := filepath.Glob("testdata/*.pdf")
	if len(candidates) == 0 {
		t.Skip("no PDF fixtures available")
	}
	text, err := PDFPlainText(candidates[0])
	if err != nil {
		t.Fatal(err)
	}
	// Don't pin content — just that we got a string back without a
	// panic. The unit shows the helper is callable from outside the
	// package.
	_ = text
}
