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
