package site

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeWikiFixture builds a minimal wiki dir on disk for the tests.
// Same layout the real internal/wiki package produces: index.md +
// per-community .md, with cross-document `.md` links.
func writeWikiFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"index.md": "# Wiki Index\n\nNavigate the wiki.\n\n- [Auth Subsystem](auth-subsystem.md)\n- [Storage Layer](storage-layer.md)\n",
		"auth-subsystem.md": "# Auth Subsystem\n\nHandles login + tokens.\n\n## Entities\n\n- `LoginHandler`\n- `TokenStore`\n\nSee [Storage Layer](storage-layer.md) for the persistence side.\n",
		"storage-layer.md":  "# Storage Layer\n\nPostgres-backed persistence.\n\n```sql\nSELECT * FROM users;\n```\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestGenerateProducesExpectedLayout(t *testing.T) {
	wikiDir := writeWikiFixture(t)
	out := t.TempDir()
	pages, err := Generate(wikiDir, out)
	if err != nil {
		t.Fatal(err)
	}
	if pages != 3 {
		t.Errorf("expected 3 pages, got %d", pages)
	}
	// Required files present.
	for _, p := range []string{
		"index.html",
		"style.css",
		"search.js",
		"search-index.json",
		"pages/index.html",
		"pages/auth-subsystem.html",
		"pages/storage-layer.html",
	} {
		if _, err := os.Stat(filepath.Join(out, p)); err != nil {
			t.Errorf("expected %s in output: %v", p, err)
		}
	}
}

func TestGenerateRewritesMDLinksToHTML(t *testing.T) {
	// `[Storage Layer](storage-layer.md)` in source markdown should
	// become `<a href="storage-layer.html">` in rendered HTML —
	// otherwise cross-page links 404 in the deployed site.
	wikiDir := writeWikiFixture(t)
	out := t.TempDir()
	if _, err := Generate(wikiDir, out); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(out, "pages/auth-subsystem.html"))
	if !strings.Contains(string(data), `href="storage-layer.html"`) {
		t.Errorf("expected .md → .html link rewrite, got:\n%s", data)
	}
	if strings.Contains(string(data), `href="storage-layer.md"`) {
		t.Errorf("raw .md link survived — would 404 in deployed site")
	}
}

func TestGenerateFirstHeadingBecomesTitle(t *testing.T) {
	// The H1 from the markdown should appear in the per-page <title>
	// tag AND in the landing-page TOC list.
	wikiDir := writeWikiFixture(t)
	out := t.TempDir()
	if _, err := Generate(wikiDir, out); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(filepath.Join(out, "pages/auth-subsystem.html"))
	if !strings.Contains(string(body), "<title>Auth Subsystem — gogfy</title>") {
		t.Errorf("expected H1 in <title>, got:\n%s", body[:200])
	}
	landing, _ := os.ReadFile(filepath.Join(out, "index.html"))
	if !strings.Contains(string(landing), ">Auth Subsystem<") {
		t.Errorf("landing TOC missing 'Auth Subsystem'")
	}
}

func TestGenerateSearchIndexCoversPageText(t *testing.T) {
	// Tokens from page body should appear in the inverted index.
	// `LoginHandler` (split on case-changes? no — our tokenizer
	// just lowercases and splits on non-word, so `LoginHandler` →
	// `loginhandler` as a single token).
	wikiDir := writeWikiFixture(t)
	out := t.TempDir()
	if _, err := Generate(wikiDir, out); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(out, "search-index.json"))
	var idx map[string][]int
	if err := json.Unmarshal(data, &idx); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"loginhandler", "tokenstore", "postgres", "persistence"} {
		if _, ok := idx[want]; !ok {
			t.Errorf("expected token %q in search index", want)
		}
	}
}

func TestGenerateDeterministicAcrossRuns(t *testing.T) {
	// Two runs over the same wiki should produce byte-identical
	// search-index.json. Map iteration is famously nondeterministic
	// in Go; if we leak iteration order into the JSON the result
	// would drift run-to-run, breaking CI snapshots.
	wikiDir := writeWikiFixture(t)
	out1 := t.TempDir()
	out2 := t.TempDir()
	if _, err := Generate(wikiDir, out1); err != nil {
		t.Fatal(err)
	}
	if _, err := Generate(wikiDir, out2); err != nil {
		t.Fatal(err)
	}
	a, _ := os.ReadFile(filepath.Join(out1, "search-index.json"))
	b, _ := os.ReadFile(filepath.Join(out2, "search-index.json"))
	// JSON object key order ISN'T stable in Go's encoder for
	// map[string][]int by default — but it IS sorted alphabetically
	// by encoding/json for map keys. Pin that here so any future
	// switch to a custom marshaler doesn't silently break
	// reproducibility.
	if string(a) != string(b) {
		t.Errorf("search-index.json differs across runs (Go map iteration leaked?):\nA: %s\nB: %s", a, b)
	}
}

func TestRenderMarkdownInlineCodeAndLinks(t *testing.T) {
	// Smoke test for the renderer — these are the inline forms the
	// wiki package actually uses. Block-level coverage is via the
	// fixtures in TestGenerate*.
	got := renderMarkdown([]byte("Use `gogfy run` to extract. See [docs](other.md)."))
	if !strings.Contains(got, "<code>gogfy run</code>") {
		t.Errorf("inline-code conversion missing: %s", got)
	}
	if !strings.Contains(got, `<a href="other.html">docs</a>`) {
		t.Errorf("link + .md→.html rewrite missing: %s", got)
	}
}

func TestRenderMarkdownFencedCodeBlock(t *testing.T) {
	got := renderMarkdown([]byte("```\nSELECT 1;\n```\n"))
	if !strings.Contains(got, "<pre><code>") || !strings.Contains(got, "SELECT 1;") {
		t.Errorf("fenced block lost code semantics: %s", got)
	}
	// HTML inside code should be escaped, not interpreted.
	got = renderMarkdown([]byte("```\n<script>x()</script>\n```\n"))
	if strings.Contains(got, "<script>") || !strings.Contains(got, "&lt;script&gt;") {
		t.Errorf("HTML in code block not escaped — XSS surface: %s", got)
	}
}

func TestRenderMarkdownEscapesParagraphContent(t *testing.T) {
	// Paragraph content with HTML tags must be escaped so a wiki
	// article can't inject markup into the site shell.
	got := renderMarkdown([]byte("Hello <script>alert(1)</script> world.\n"))
	if strings.Contains(got, "<script>") {
		t.Errorf("paragraph HTML not escaped: %s", got)
	}
	if !strings.Contains(got, "&lt;script&gt;") {
		t.Errorf("expected escaped <script>, got: %s", got)
	}
}
