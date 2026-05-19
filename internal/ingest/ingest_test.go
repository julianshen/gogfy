package ingest

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/julianshen/gogfy/internal/safefetch"
)

func publicResolver() func(ctx context.Context, host string) ([]net.IP, error) {
	return func(_ context.Context, _ string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("8.8.8.8")}, nil
	}
}

func TestHTMLToMarkdownPreservesHeadingsAndParagraphs(t *testing.T) {
	in := []byte(`<html><head><title>x</title><script>evil()</script><style>.x{}</style></head>
<body>
<h1>Title</h1>
<p>First paragraph with <b>bold</b> and <a href="x">link</a>.</p>
<h2>Sub</h2>
<ul><li>one</li><li>two</li></ul>
</body></html>`)
	got := htmlToMarkdown(in)
	for _, want := range []string{"# Title", "First paragraph with bold and link.", "## Sub", "- one", "- two"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in output:\n%s", want, got)
		}
	}
	if strings.Contains(got, "evil()") || strings.Contains(got, ".x{}") {
		t.Errorf("script/style not stripped:\n%s", got)
	}
}

func TestHTMLToMarkdownDecodesBasicEntities(t *testing.T) {
	got := htmlToMarkdown([]byte("<p>Foo &amp; Bar &mdash; baz &lt;x&gt;</p>"))
	if !strings.Contains(got, "Foo & Bar — baz <x>") {
		t.Errorf("entity decode broken: %q", got)
	}
}

func TestIngestWritesSidecarAndIsIdempotent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "<h1>Hello</h1><p>World</p>")
	}))
	defer srv.Close()
	out := t.TempDir()
	opts := safefetch.Options{Resolver: publicResolver()}

	path, err := Ingest(context.Background(), srv.URL+"/article", out, opts)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(path)
	if !strings.Contains(string(body), "# Hello") || !strings.Contains(string(body), "World") {
		t.Errorf("sidecar body wrong: %s", body)
	}
	if !strings.Contains(string(body), "source_url:") {
		t.Errorf("sidecar should record source URL in frontmatter: %s", body)
	}

	// Second call returns the same path without re-fetching.
	srv.Close() // ensure no network would succeed
	path2, err := Ingest(context.Background(), srv.URL+"/article", out, opts)
	if err != nil {
		t.Fatalf("idempotent re-ingest should succeed offline: %v", err)
	}
	if path != path2 {
		t.Errorf("paths differ across runs: %q vs %q", path, path2)
	}
}

func TestSidecarPathStableForSameURL(t *testing.T) {
	a := SidecarPath("/out", "https://example.com/x")
	b := SidecarPath("/out", "https://example.com/x")
	if a != b {
		t.Errorf("non-deterministic path: %q vs %q", a, b)
	}
}

func TestSidecarPathDiffersForDifferentURLs(t *testing.T) {
	a := SidecarPath("/out", "https://example.com/x")
	b := SidecarPath("/out", "https://example.com/y")
	if a == b {
		t.Errorf("different URLs collided to same path: %s", a)
	}
}

func TestIngestSurfacesSSRFGuardError(t *testing.T) {
	// safefetch will reject a private-IP target. Ingest must propagate.
	out := t.TempDir()
	opts := safefetch.Options{
		Resolver: func(_ context.Context, _ string) ([]net.IP, error) {
			return []net.IP{net.ParseIP("10.0.0.1")}, nil
		},
	}
	_, err := Ingest(context.Background(), "https://internal.example/x", out, opts)
	if err == nil {
		t.Fatal("expected SSRF error to propagate")
	}
}

func TestIngestPDFContentWritesPDFSidecar(t *testing.T) {
	// PDF magic-bytes prefix should route to a .pdf sidecar with the
	// original bytes preserved (no markdown conversion, no frontmatter
	// — PDF parsers reject anything before %PDF-).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = io.WriteString(w, "%PDF-1.4\nsome bytes\n%%EOF")
	}))
	defer srv.Close()
	out := t.TempDir()
	opts := safefetch.Options{Resolver: publicResolver()}

	path, err := Ingest(context.Background(), srv.URL+"/paper", out, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(path, ".pdf") {
		t.Errorf("PDF body should land as .pdf, got %q", path)
	}
	body, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(body), "%PDF-") {
		t.Errorf("PDF bytes should be preserved verbatim, got %q", body[:min(20, len(body))])
	}
}

func TestIngestPDFUrlSuffixRoutesEvenWithoutMagicBytes(t *testing.T) {
	// Servers sometimes serve PDFs with octet-stream or other
	// generic content types — fall back to URL-suffix detection.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = io.WriteString(w, "garbage but URL says it's a pdf")
	}))
	defer srv.Close()
	out := t.TempDir()
	opts := safefetch.Options{Resolver: publicResolver()}

	path, err := Ingest(context.Background(), srv.URL+"/paper.pdf", out, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(path, ".pdf") {
		t.Errorf("URL with .pdf suffix should land as .pdf, got %q", path)
	}
}

func TestIngestPDFIdempotent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = io.WriteString(w, "%PDF-1.4\nfoo")
	}))
	defer srv.Close()
	out := t.TempDir()
	opts := safefetch.Options{Resolver: publicResolver()}

	p1, err := Ingest(context.Background(), srv.URL+"/paper", out, opts)
	if err != nil {
		t.Fatal(err)
	}
	srv.Close()
	p2, err := Ingest(context.Background(), srv.URL+"/paper", out, opts)
	if err != nil {
		t.Fatalf("PDF re-ingest should be idempotent offline: %v", err)
	}
	if p1 != p2 {
		t.Errorf("PDF sidecar path drifted: %q vs %q", p1, p2)
	}
}

func TestRewriteArxivURL(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"https://arxiv.org/abs/2401.12345", "https://arxiv.org/pdf/2401.12345.pdf"},
		{"https://arxiv.org/abs/2401.12345v2", "https://arxiv.org/pdf/2401.12345v2.pdf"},
		{"http://arxiv.org/abs/cs/0601001", "http://arxiv.org/pdf/cs/0601001.pdf"},
		{"https://arxiv.org/abs/2401.12345.pdf", "https://arxiv.org/pdf/2401.12345.pdf"},
		// Non-arXiv URLs untouched.
		{"https://example.com/abs/x", "https://example.com/abs/x"},
		{"https://arxiv.org/pdf/2401.12345.pdf", "https://arxiv.org/pdf/2401.12345.pdf"},
	}
	for _, tc := range cases {
		if got := rewriteArxivURL(tc.in); got != tc.want {
			t.Errorf("rewriteArxivURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
