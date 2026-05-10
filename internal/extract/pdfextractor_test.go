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
	for i, body := range objs {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, body)
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
