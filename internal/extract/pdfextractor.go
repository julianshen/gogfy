package extract

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/julianshen/gogfy/internal/schema"
	"github.com/ledongthuc/pdf"
)

// PDFExtractor reads .pdf files via github.com/ledongthuc/pdf (pure-Go,
// no cgo). Module label prefers /Info /Title over basename — same
// rationale as HTML's <title>-over-<h1> precedence, since PDFs
// frequently carry a title separately from visible body text.
//
// Annotation /A /URI actions are not parsed; URLs are recovered only
// from extracted text. PDFs whose link annotations carry visible
// "click here"-style anchor text will under-report references — that's
// a known scope limitation, not a coverage claim about real-world docs.
//
// Section nodes are not emitted: a PDF without an explicit Outline has
// no reliable structural markers, and font-size heading heuristics are
// expensive and false-positive-prone. Adding Outline-derived sections
// when present is a future change.
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

	// Encrypted PDFs, malformed content streams, and font-mapping
	// failures all degrade to empty body → bare module node, so a
	// pathological PDF doesn't fail a 10k-file batched run. Failures
	// log to stderr (see pdfPlainText) so the operator can still see
	// which files degraded.
	body := pdfPlainText(r, abs)
	state.edges = append(state.edges, urlEdges("pdf", moduleID, body)...)
	return Result{Nodes: state.nodes, Edges: state.edges}, nil
}

// pdfModuleLabel reads /Info /Title from the PDF trailer, falling back
// to the file basename if the key is absent, empty, or the library
// panics walking the Info dict. .Text() handles both UTF-16-encoded
// "text strings" and PDFDocEncoding — no extra decoding needed here.
func pdfModuleLabel(r *pdf.Reader, abs string) (label string) {
	label = filepath.Base(abs)
	defer func() {
		if rec := recover(); rec != nil {
			fmt.Fprintf(os.Stderr, "pdf: title read recovered from panic on %s: %v\n", abs, rec)
			label = filepath.Base(abs)
		}
	}()
	title := strings.TrimSpace(r.Trailer().Key("Info").Key("Title").Text())
	if title == "" {
		return filepath.Base(abs)
	}
	return title
}

// PDFPlainText opens path, returns the concatenated plain text of
// every page. Exposed so the semantic-extraction pipeline can feed
// PDF bodies into an LLM without re-implementing the panic-tolerant
// read pattern. Returns "" on any read failure (the PDF library
// panics on some uncommon font encodings — callers want
// best-effort, not pipeline-aborts).
func PDFPlainText(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	f, r, err := pdf.Open(abs)
	if err != nil {
		return "", err
	}
	defer f.Close()
	return pdfPlainText(r, abs), nil
}

// pdfPlainText returns the concatenated text of every page, or "" on
// any failure (read error or library panic on uncommon font encodings
// / content-stream operators). The caller treats empty text as "no
// URLs to extract." All silent-failure paths log to stderr so the
// operator can tell which files degraded — unlike the docx/xlsx
// extractors which propagate every parse error, the PDF library is
// known to panic on inputs we'd still rather not abort the run for.
func pdfPlainText(r *pdf.Reader, abs string) (text string) {
	defer func() {
		if rec := recover(); rec != nil {
			fmt.Fprintf(os.Stderr, "pdf: text extraction recovered from panic on %s: %v\n", abs, rec)
			text = ""
		}
	}()
	rd, err := r.GetPlainText()
	if err != nil {
		fmt.Fprintf(os.Stderr, "pdf: text extraction failed on %s: %v\n", abs, err)
		return ""
	}
	b, err := io.ReadAll(rd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pdf: read failed on %s: %v\n", abs, err)
		return ""
	}
	return string(b)
}
