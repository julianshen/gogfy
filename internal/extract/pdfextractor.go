package extract

import (
	"io"
	"path/filepath"
	"strings"

	"github.com/julianshen/gogfy/internal/schema"
	"github.com/ledongthuc/pdf"
)

// PDFExtractor turns PDF files into graph nodes and edges using
// github.com/ledongthuc/pdf — a pure-Go reader with no cgo dependency.
//
// Schema:
//
//   - Module node, kind=module, label = PDF /Info /Title metadata if
//     present, else basename. (PDFs commonly carry a title separately
//     from the visible body text; using metadata when available
//     mirrors HTML's <title>-over-h1 precedence.)
//   - Reference edges (relation=references) for every HTTP(S) URL found
//     in the extracted plain text, using the shared extractURLs helper
//     so trailing sentence punctuation is stripped consistently with the
//     other doc extractors.
//
// Section nodes are deliberately not emitted in this first cut: PDFs
// without explicit document outlines have no reliable structural
// markers, and font-size heuristics for heading detection are both
// expensive and false-positive-prone. The PDF Outline / bookmark
// dictionary could become section nodes in a follow-up if it exists.
//
// Annotation links (clickable hyperlinks embedded in the PDF) are not
// extracted separately — the URL regex over the plain text catches the
// same URLs in nearly all real-world PDFs, and annotation-only links
// without visible URL text are rare in technical documents.
type PDFExtractor struct{}

func (PDFExtractor) Extract(path string) (Result, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return Result{}, err
	}
	f, r, err := pdf.Open(abs)
	if err != nil {
		return Result{}, err
	}
	defer f.Close()

	moduleID := schema.LangID("pdf", "module", abs)
	state := &extractState{lang: "pdf", filePath: abs}
	state.nodes = append(state.nodes, schema.Node{
		ID:    moduleID,
		Label: pdfModuleLabel(r, abs),
	})

	// Encrypted PDFs, malformed streams, and font-mapping failures all
	// degrade to "no body" → bare module node, so the file still appears
	// in the graph rather than failing the whole extraction run.
	body := pdfPlainText(r)
	state.edges = append(state.edges, urlEdges("pdf", moduleID, body)...)
	return Result{Nodes: state.nodes, Edges: state.edges}, nil
}

// pdfModuleLabel reads /Info /Title from the PDF trailer and falls back
// to the file basename if absent or unreadable. Some producers emit
// UTF-16-encoded titles (PDF's "text string" type); .Text() handles
// both that and PDFDocEncoding.
func pdfModuleLabel(r *pdf.Reader, abs string) string {
	defer func() {
		// The library panics on certain malformed Info dicts; treat
		// recovery the same as missing metadata.
		_ = recover()
	}()
	if r == nil {
		return filepath.Base(abs)
	}
	title := strings.TrimSpace(r.Trailer().Key("Info").Key("Title").Text())
	if title == "" {
		return filepath.Base(abs)
	}
	return title
}

// pdfPlainText returns the concatenated plain text of every page, or
// "" on any failure (read error, library panic on uncommon font
// encodings or content-stream operators). The caller treats empty
// text as "no URLs to extract."
func pdfPlainText(r *pdf.Reader) (text string) {
	defer func() { _ = recover() }()
	rd, err := r.GetPlainText()
	if err != nil {
		return ""
	}
	b, err := io.ReadAll(rd)
	if err != nil {
		return ""
	}
	return string(b)
}
