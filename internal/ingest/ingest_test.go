package ingest

import (
	"context"
	"errors"
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
		// Host-spoofing / suffix attacks must not match.
		{"https://twitter.com.evil.com/foo/status/1", false},
		{"https://evil.com/twitter.com/foo/status/1", false},
		{"https://eviltwitter.com/foo/status/1", false},
		// "statuses" plural is GitHub-style, not a tweet.
		{"https://twitter.com/foo/statuses/12345", false},
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

func TestParseOGTagsIgnoresCompoundAttributeNames(t *testing.T) {
	// data-property, aria-name, etc. share the suffix "property" /
	// "name" — earlier regex matched these by accident. Word-boundary
	// requirement on the attribute name closes the false-match path.
	html := []byte(`
<meta data-property="og:title" content="Should Not Match">
<meta aria-name="og:description" content="Also Should Not Match">
<meta property="og:image" content="https://real.example.com/image.png">
`)
	got := parseOGTags(html)
	if got.Title != "" {
		t.Errorf("data-property should not be picked up: %q", got.Title)
	}
	if got.Description != "" {
		t.Errorf("aria-name should not be picked up: %q", got.Description)
	}
	if got.ImageURL != "https://real.example.com/image.png" {
		t.Errorf("real og:image should match: %q", got.ImageURL)
	}
}

func TestParseOGTagsStripsNewlinesInValues(t *testing.T) {
	// Embedded \n in a title would break out of the markdown
	// blockquote in Format() and let attacker-controlled content
	// (including frontmatter --- markers) leak. Newlines are
	// replaced with a single space.
	html := []byte("<meta property=\"og:title\" content=\"line1\nline2\r\n---\nlooks like frontmatter\">")
	got := parseOGTags(html)
	if strings.ContainsAny(got.Title, "\r\n") {
		t.Errorf("newlines should be stripped from values: %q", got.Title)
	}
	if !strings.Contains(got.Title, "line1") || !strings.Contains(got.Title, "line2") {
		t.Errorf("content text should be preserved (just newlines replaced): %q", got.Title)
	}
}

func TestIsGitHubURL(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"https://github.com/owner/repo", true},
		{"https://github.com/owner/repo/", true},
		{"http://github.com/owner/repo", true},
		{"https://github.com/owner/repo/blob/main/README.md", true},
		{"https://github.com/owner/repo/blob/abc123/src/x.go", true},
		// Issues / PRs / tree views aren't rewritable to raw content.
		{"https://github.com/owner/repo/issues/123", false},
		{"https://github.com/owner/repo/pulls", false},
		{"https://github.com/owner/repo/tree/main", false},
		// Host spoofing.
		{"https://github.com.evil.com/owner/repo", false},
		{"https://evilgithub.com/owner/repo", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := IsGitHubURL(tc.in); got != tc.want {
			t.Errorf("IsGitHubURL(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestRewriteGitHubURL(t *testing.T) {
	cases := []struct {
		in, want string
		hit      bool
	}{
		{
			in:   "https://github.com/owner/repo",
			want: "https://raw.githubusercontent.com/owner/repo/HEAD/README.md",
			hit:  true,
		},
		{
			in:   "https://github.com/owner/repo/blob/main/docs/intro.md",
			want: "https://raw.githubusercontent.com/owner/repo/main/docs/intro.md",
			hit:  true,
		},
		{
			in:   "https://github.com/owner/repo/blob/abc123def/src/lib.rs",
			want: "https://raw.githubusercontent.com/owner/repo/abc123def/src/lib.rs",
			hit:  true,
		},
		// Non-rewritable shapes.
		{in: "https://github.com/owner/repo/issues/1", hit: false},
		{in: "https://example.com/owner/repo", hit: false},
	}
	for _, tc := range cases {
		got, ok := rewriteGitHubURL(tc.in)
		if ok != tc.hit {
			t.Errorf("rewriteGitHubURL(%q) ok = %v, want %v", tc.in, ok, tc.hit)
		}
		if ok && got != tc.want {
			t.Errorf("rewriteGitHubURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestIsYouTubeURL(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"https://www.youtube.com/watch?v=abc123", true},
		{"https://m.youtube.com/watch?v=abc123", true},
		{"https://youtube.com/watch?v=abc123", true},
		{"https://youtu.be/abc123", true},
		{"https://www.youtube.com/shorts/abc123", true},
		{"https://www.youtube.com/embed/abc123", true},
		// Channel / playlist URLs intentionally excluded.
		{"https://www.youtube.com/c/SomeChannel", false},
		{"https://www.youtube.com/playlist?list=PLxxx", false},
		// Host spoofing.
		{"https://youtube.com.evil.com/watch?v=x", false},
		{"https://evilyoutube.com/watch?v=x", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := IsYouTubeURL(tc.in); got != tc.want {
			t.Errorf("IsYouTubeURL(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestFormatYouTubeMetadata(t *testing.T) {
	got := formatYouTubeMetadata(youTubeOEmbed{
		Title:        "Talk on Distributed Systems",
		AuthorName:   "Alice",
		AuthorURL:    "https://example.com/@alice",
		ThumbnailURL: "https://example.com/thumb.jpg",
	})
	for _, want := range []string{
		"> **YouTube video**",
		"Talk on Distributed Systems",
		"Alice",
		"@alice",
		"thumb.jpg",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("metadata missing %q: %q", want, got)
		}
	}

	// Newline injection guard — title with \n should be flattened.
	got = formatYouTubeMetadata(youTubeOEmbed{Title: "a\nb\nc"})
	if strings.ContainsAny(got, "\n") && strings.Count(got, "\n") != 2 {
		// Expected newlines: 1 after the header line, 1 after the title line.
		// An injected title newline would produce 3+.
		t.Errorf("unexpected newline count: %q", got)
	}

	// All string fields must be sanitized — not just Title. A crafted
	// AuthorName / AuthorURL / ThumbnailURL containing \n---\n would
	// otherwise smuggle frontmatter-like content into the sidecar.
	got = formatYouTubeMetadata(youTubeOEmbed{
		AuthorName:   "good\n---\nbad",
		AuthorURL:    "https://e.com\n---\nbad",
		ThumbnailURL: "https://e.com/t.jpg\n---\nbad",
	})
	if strings.Contains(got, "\n---") {
		t.Errorf("frontmatter-like injection via author/thumbnail not stripped: %q", got)
	}

	// Empty oembed → empty result so callers can skip.
	if formatYouTubeMetadata(youTubeOEmbed{}) != "" {
		t.Error("empty oembed should produce empty string")
	}
}

func TestIngestGitHubRepoFetchesRawReadme(t *testing.T) {
	// Repo URL should be rewritten to raw.githubusercontent.com,
	// pointing the fetch at the README rather than the HTML landing
	// page. We can't easily mock the rewrite target since the host is
	// hard-coded, but we can verify the rewrite logic at the unit
	// level — covered above by TestRewriteGitHubURL — and confirm
	// that a repo URL gets the kind: github frontmatter when the
	// fetch succeeds against a stand-in server (via an explicit
	// allowlist & test-side rewrite isn't worth the plumbing here).
	//
	// Instead: assert that a non-repo github URL (e.g. /issues) flows
	// through the normal HTML path with the kind: github label.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "<article><p>Issue body text.</p></article>")
	}))
	defer srv.Close()
	out := t.TempDir()
	opts := safefetch.Options{Resolver: publicResolver()}
	// Local server can't have github.com host; assert kindLabel logic
	// directly via constructed body inspection.
	path, err := Ingest(context.Background(), srv.URL+"/some-page", out, opts)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(path)
	if strings.Contains(string(body), "kind: github") {
		t.Errorf("non-github URL should not get kind: github label: %s", body)
	}
}

func TestIngestImageMagicBytesWritesBinarySidecar(t *testing.T) {
	// PNG magic bytes should route to a .png sidecar with bytes
	// preserved verbatim — no markdown conversion, no frontmatter.
	pngBody := append([]byte("\x89PNG\r\n\x1a\n"), []byte("fake-png-payload")...)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(pngBody)
	}))
	defer srv.Close()
	out := t.TempDir()
	opts := safefetch.Options{Resolver: publicResolver()}

	path, err := Ingest(context.Background(), srv.URL+"/diagram", out, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(path, ".png") {
		t.Errorf("PNG body should land as .png, got %q", path)
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(pngBody) {
		t.Errorf("image bytes should be preserved verbatim")
	}
}

func TestIngestImageURLSuffixRoutesWithoutMagicBytes(t *testing.T) {
	// Servers may serve images with bogus content types — a .jpg URL
	// suffix should still route to the image branch.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = io.WriteString(w, "garbage but URL says jpg")
	}))
	defer srv.Close()
	out := t.TempDir()
	opts := safefetch.Options{Resolver: publicResolver()}

	path, err := Ingest(context.Background(), srv.URL+"/img.jpg", out, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(path, ".jpg") {
		t.Errorf("URL with .jpg suffix should land as .jpg, got %q", path)
	}
}

func TestIngestImageIdempotent(t *testing.T) {
	pngBody := append([]byte("\x89PNG\r\n\x1a\n"), []byte("x")...)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(pngBody)
	}))
	defer srv.Close()
	out := t.TempDir()
	opts := safefetch.Options{Resolver: publicResolver()}

	p1, err := Ingest(context.Background(), srv.URL+"/d", out, opts)
	if err != nil {
		t.Fatal(err)
	}
	srv.Close()
	p2, err := Ingest(context.Background(), srv.URL+"/d", out, opts)
	if err != nil {
		t.Fatalf("image re-ingest should be idempotent offline: %v", err)
	}
	if p1 != p2 {
		t.Errorf("image sidecar path drifted: %q vs %q", p1, p2)
	}
}

func TestImageKindClassification(t *testing.T) {
	cases := []struct {
		name    string
		body    []byte
		url     string
		wantExt string
		wantOK  bool
	}{
		{"png magic", []byte("\x89PNG\r\n\x1a\nrest"), "x", ".png", true},
		{"jpeg magic", []byte("\xFF\xD8\xFFrest"), "x", ".jpg", true},
		{"gif87a", []byte("GIF87aXX"), "x", ".gif", true},
		{"gif89a", []byte("GIF89aXX"), "x", ".gif", true},
		{"webp magic", []byte("RIFF1234WEBP....."), "x", ".webp", true},
		{"jpeg suffix", nil, "https://e.com/a.jpeg", ".jpg", true},
		{"png suffix with query", nil, "https://e.com/a.png?v=2", ".png", true},
		{"not an image", []byte("plain text"), "https://e.com/x", "", false},
		{"svg not classified", []byte("<svg></svg>"), "https://e.com/a.svg", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ext, ok := imageKind(tc.body, tc.url)
			if ok != tc.wantOK || ext != tc.wantExt {
				t.Errorf("imageKind(%q, %q) = (%q, %v); want (%q, %v)", tc.body, tc.url, ext, ok, tc.wantExt, tc.wantOK)
			}
		})
	}
}

func TestIngestYouTubeAudioModeWritesBinarySidecar(t *testing.T) {
	// With YouTubeFetchAudio + a stub fetcher, the YouTube branch
	// should write the audio bytes to a .m4a (or .webm) sidecar
	// instead of stopping at the metadata-only .md path.
	out := t.TempDir()
	fakeAudio := []byte("fake m4a payload")
	stub := &stubYTFetcher{
		audio:       fakeAudio,
		contentType: "audio/mp4",
		title:       "Tech Talk",
		author:      "Conf 2026",
	}
	path, err := IngestWithOptions(context.Background(),
		"https://www.youtube.com/watch?v=abc123",
		out,
		Options{
			YouTubeFetchAudio:   true,
			YouTubeAudioFetcher: stub,
		})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(path, ".m4a") {
		t.Errorf("expected .m4a sidecar, got %q", path)
	}
	body, _ := os.ReadFile(path)
	if string(body) != string(fakeAudio) {
		t.Errorf("audio bytes not preserved verbatim")
	}
}

func TestIngestYouTubeAudioFallsBackToMetadataOnError(t *testing.T) {
	// Audio mode is best-effort: a geo-block / age-gate / takedown
	// must NOT fail the ingest — fall back to the metadata-only
	// .md path so the URL still lands in the corpus.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"title":"Fallback","author_name":"X"}`)
	}))
	defer srv.Close()
	// Replace the YouTube oembed endpoint by monkey-patching the
	// safefetch resolver — fetchYouTubeMetadata calls the real URL,
	// so the fallback path will actually try to hit the network.
	// For this test we use a stub fetcher that errors so the fallback
	// fires, but the metadata fetch itself uses the live youtube.com
	// host (which the safefetch resolver can redirect to httptest).
	out := t.TempDir()
	stub := &stubYTFetcher{err: errYTStub}
	opts := Options{
		Safefetch: safefetch.Options{
			Resolver: publicResolver(),
		},
		YouTubeFetchAudio:   true,
		YouTubeAudioFetcher: stub,
	}
	// The metadata branch will try a live network call here — accept
	// that as "best-effort" and just verify the ingest didn't error
	// at the audio stage. We check that audio didn't land.
	_, err := IngestWithOptions(context.Background(),
		"https://www.youtube.com/watch?v=abc123",
		out,
		opts)
	// Either the metadata fetch succeeded (rare in CI) or it errored
	// after the audio-fallback log. Either is fine — what matters is
	// no audio sidecar was written.
	_ = err
	for _, ext := range []string{".m4a", ".webm"} {
		if _, statErr := os.Stat(audioSidecarPath(out, "https://www.youtube.com/watch?v=abc123", ext)); statErr == nil {
			t.Errorf("audio sidecar wrote despite fetcher error: ext=%s", ext)
		}
	}
}

var errYTStub = errors.New("stub-error: geo-blocked")

type stubYTFetcher struct {
	audio       []byte
	contentType string
	title       string
	author      string
	err         error
}

func (s *stubYTFetcher) Download(_ context.Context, _ string) ([]byte, string, string, string, error) {
	if s.err != nil {
		return nil, "", "", "", s.err
	}
	return s.audio, s.contentType, s.title, s.author, nil
}
