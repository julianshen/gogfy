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

func TestHTMLToMarkdownDropsBoilerplateTags(t *testing.T) {
	html := []byte(`
<nav><a href="/">Home</a> <a href="/about">About</a></nav>
<header>Site Header Logo</header>
<p>Real article paragraph.</p>
<aside>Related Posts</aside>
<footer>Copyright 2025</footer>
`)
	got := htmlToMarkdown(html)
	if !strings.Contains(got, "Real article paragraph") {
		t.Errorf("article text dropped: %q", got)
	}
	for _, junk := range []string{"Home", "About", "Site Header", "Related Posts", "Copyright"} {
		if strings.Contains(got, junk) {
			t.Errorf("boilerplate %q survived conversion: %q", junk, got)
		}
	}
}

func TestHTMLToMarkdownNarrowsToArticle(t *testing.T) {
	// Sidebar/comments outside <article> should be dropped even
	// without explicit boilerplate tags — narrowing to <article> is
	// the bigger lift than nav-stripping for typical blog markup.
	html := []byte(`
<div class="sidebar"><p>Sponsor: Buy Now!</p></div>
<article>
  <h1>How Distributed Consensus Works</h1>
  <p>Paxos is a family of protocols for solving consensus.</p>
</article>
<div class="comments"><p>nice post +1</p></div>
`)
	got := htmlToMarkdown(html)
	if !strings.Contains(got, "How Distributed Consensus Works") {
		t.Errorf("article heading dropped: %q", got)
	}
	if !strings.Contains(got, "Paxos") {
		t.Errorf("article body dropped: %q", got)
	}
	for _, junk := range []string{"Sponsor", "nice post"} {
		if strings.Contains(got, junk) {
			t.Errorf("out-of-article noise %q survived: %q", junk, got)
		}
	}
}

func TestHTMLToMarkdownFallsBackToMainThenWholeDoc(t *testing.T) {
	mainOnly := []byte(`
<div class="ads">Buy Now</div>
<main>
  <p>Body text.</p>
</main>
<footer>foot</footer>
`)
	got := htmlToMarkdown(mainOnly)
	if !strings.Contains(got, "Body text") {
		t.Errorf("main body dropped: %q", got)
	}
	if strings.Contains(got, "Buy Now") {
		t.Errorf("pre-main ad survived: %q", got)
	}

	// Plain HTML without article/main should still convert (existing
	// behavior). This is a regression guard.
	plain := []byte(`<p>Hello</p>`)
	if got := htmlToMarkdown(plain); !strings.Contains(got, "Hello") {
		t.Errorf("plain HTML regressed: %q", got)
	}
}

func TestNarrowToArticlePicksLargestBlock(t *testing.T) {
	// Some sites use <article> for both the main post and "card"
	// previews of related posts. Picking the largest block avoids
	// narrowing to a thumbnail caption.
	html := `
<article>Card preview short.</article>
<article>This is the full article body with substantially more content than any of the related-post cards on the page.</article>
<article>Another small card.</article>
`
	got := narrowToArticle(html)
	if !strings.Contains(got, "substantially more content") {
		t.Errorf("expected largest article block, got %q", got)
	}
	if strings.Contains(got, "Card preview") || strings.Contains(got, "Another small") {
		t.Errorf("smaller blocks should not be selected: %q", got)
	}
}

func TestIsTweetURL(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"https://twitter.com/foo/status/12345", true},
		{"https://x.com/foo/status/12345", true},
		{"https://www.x.com/foo/status/12345", true},
		{"https://mobile.twitter.com/foo/status/12345", true},
		{"http://X.com/foo/status/1", true},
		{"https://twitter.com/foo", false},        // profile, not status
		{"https://example.com/status/12345", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := IsTweetURL(tc.in); got != tc.want {
			t.Errorf("IsTweetURL(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestParseOGTagsBothAttributeOrderings(t *testing.T) {
	// Some pages emit property-first, others content-first. Both
	// orderings must be picked up so we don't miss the metadata on a
	// site with non-standard ordering.
	html := []byte(`
<meta property="og:title" content="Property First Title"/>
<meta content="Content First Description" name="og:description">
<meta property="og:image" content="https://example.com/cover.png">
`)
	got := parseOGTags(html)
	if got.Title != "Property First Title" {
		t.Errorf("title: %q", got.Title)
	}
	if got.Description != "Content First Description" {
		t.Errorf("description: %q", got.Description)
	}
	if got.ImageURL != "https://example.com/cover.png" {
		t.Errorf("image: %q", got.ImageURL)
	}
}

func TestParseOGTagsTwitterCardFallback(t *testing.T) {
	// twitter:* used when og:* missing for the same field.
	html := []byte(`
<meta name="twitter:title" content="Tweet Title">
<meta name="twitter:description" content="Tweet desc">
`)
	got := parseOGTags(html)
	if got.Title != "Tweet Title" {
		t.Errorf("twitter:title not picked: %+v", got)
	}
	if got.Description != "Tweet desc" {
		t.Errorf("twitter:description not picked: %+v", got)
	}
}

func TestParseOGTagsOGWinsOverTwitter(t *testing.T) {
	// When both og:title and twitter:title are present, og:title is
	// canonical per OpenGraph spec.
	html := []byte(`
<meta property="og:title" content="OG Title">
<meta name="twitter:title" content="Twitter Title">
`)
	got := parseOGTags(html)
	if got.Title != "OG Title" {
		t.Errorf("og: should win over twitter:, got %q", got.Title)
	}
}

func TestOGTagsEmpty(t *testing.T) {
	if !(OGTags{}).Empty() {
		t.Error("zero value should be empty")
	}
	if (OGTags{Title: "x"}).Empty() {
		t.Error("any non-empty field means not empty")
	}
}

func TestIngestPrependsOGMetadataBlock(t *testing.T) {
	// OG meta tags survive even after readability narrowing because
	// parseOGTags scans the full body (not the narrowed region).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `
<html><head>
<meta property="og:title" content="Alice posted: Hello, world">
<meta property="og:description" content="A delightful greeting.">
</head><body><article><p>Body text.</p></article></body></html>`)
	}))
	defer srv.Close()

	out := t.TempDir()
	opts := safefetch.Options{Resolver: publicResolver()}
	path, err := Ingest(context.Background(), srv.URL+"/article", out, opts)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(path)
	for _, want := range []string{
		"> **OpenGraph metadata**",
		"Alice posted: Hello, world",
		"delightful greeting",
		"Body text",
	} {
		if !strings.Contains(string(body), want) {
			t.Errorf("expected %q in sidecar, got: %s", want, body)
		}
	}
}
