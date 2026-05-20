// Package site bundles the wiki markdown output into a self-contained
// static HTML site with client-side full-text search. Output is a
// directory that can be served as-is (file://, github-pages, S3,
// netlify, anywhere — no server-side process needed).
//
// Layout produced:
//
//	<out>/
//	  index.html         — landing page with TOC + search box
//	  search-index.json  — pre-built inverted token→file map
//	  pages/             — one .html per source .md
//	    community-0.html
//	    community-1.html
//	    god-node-X.html
//	    ...
//	  style.css          — minimal embedded styling
//	  search.js          — client-side search (no deps)
//
// Search is intentionally simple: substring + token match against a
// pre-built JSON inverted index. For corpus sizes typical of gogfy
// output (hundreds of pages, low-thousands at most) this is plenty.
// Heavyweight search libraries (Bleve, Tantivy) would be overkill and
// dramatically expand the dep footprint.
package site

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/julianshen/gogfy/internal/fsutil"
)

// Generate reads the wiki markdown directory and writes a static
// site under outDir. Existing files in outDir are NOT cleaned —
// callers wanting a fresh build should rm-rf the dir first.
//
// Returns the number of pages emitted (excluding index.html) so the
// caller can print a useful "wrote N pages to <dir>" status line.
func Generate(wikiDir, outDir string) (int, error) {
	entries, err := os.ReadDir(wikiDir)
	if err != nil {
		return 0, fmt.Errorf("site: read wiki dir %s: %w", wikiDir, err)
	}

	pagesDir := filepath.Join(outDir, "pages")
	if err := os.MkdirAll(pagesDir, 0755); err != nil {
		return 0, fmt.Errorf("site: mkdir %s: %w", pagesDir, err)
	}

	var pages []pageMeta
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".md" {
			continue
		}
		mdPath := filepath.Join(wikiDir, e.Name())
		data, err := os.ReadFile(mdPath)
		if err != nil {
			return len(pages), fmt.Errorf("site: read %s: %w", mdPath, err)
		}
		title := extractTitle(data, strings.TrimSuffix(e.Name(), ".md"))
		htmlBody := renderMarkdown(data)
		page := pageMeta{
			SourceMD: e.Name(),
			HTMLFile: strings.TrimSuffix(e.Name(), ".md") + ".html",
			Title:    title,
			Body:     htmlBody,
			Text:     stripHTMLTags(htmlBody), // for search index
		}
		pages = append(pages, page)
	}

	// Determinism: walking os.ReadDir is alpha-sorted on most platforms
	// but not guaranteed. Sort explicitly so search-index.json and
	// index.html byte-stable across runs.
	sort.Slice(pages, func(i, j int) bool { return pages[i].SourceMD < pages[j].SourceMD })

	// Write per-page HTML.
	for _, p := range pages {
		body := wrapPage(p)
		if err := fsutil.WriteFileAtomic(filepath.Join(pagesDir, p.HTMLFile), []byte(body), 0644); err != nil {
			return len(pages), fmt.Errorf("site: write %s: %w", p.HTMLFile, err)
		}
	}

	// Build + write the search index. Token → file list (deduped,
	// sorted). Each entry is just the filename; the JS side resolves
	// titles from the page list.
	idx := buildIndex(pages)
	idxJSON, err := json.Marshal(idx)
	if err != nil {
		return len(pages), fmt.Errorf("site: marshal index: %w", err)
	}
	if err := fsutil.WriteFileAtomic(filepath.Join(outDir, "search-index.json"), idxJSON, 0644); err != nil {
		return len(pages), err
	}

	if err := fsutil.WriteFileAtomic(filepath.Join(outDir, "style.css"), []byte(styleCSS), 0644); err != nil {
		return len(pages), err
	}
	if err := fsutil.WriteFileAtomic(filepath.Join(outDir, "search.js"), []byte(searchJS), 0644); err != nil {
		return len(pages), err
	}
	if err := fsutil.WriteFileAtomic(filepath.Join(outDir, "index.html"), []byte(landingPage(pages)), 0644); err != nil {
		return len(pages), err
	}
	return len(pages), nil
}

// pageMeta is the per-page data the renderer threads from source
// markdown through to landing-page + per-page HTML emission.
type pageMeta struct {
	SourceMD string `json:"source_md"`
	HTMLFile string `json:"html_file"`
	Title    string `json:"title"`
	Body     string `json:"-"` // rendered HTML — not in the search payload
	Text     string `json:"-"` // search-only plain text
}

// extractTitle pulls the first `# heading` from a markdown document.
// Falls back to the supplied default (typically the slug) when no
// H1 is present — index.md's title would otherwise become an empty
// string under that fallback path.
func extractTitle(md []byte, fallback string) string {
	s := string(md)
	for _, line := range strings.SplitN(s, "\n", 50) {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(line[2:])
		}
	}
	return fallback
}

// renderMarkdown is a deliberately small md→html converter: enough
// for what the wiki package emits (headings, paragraphs, lists,
// links, inline code, code blocks). NOT a full CommonMark renderer —
// pulling in goldmark/blackfriday would double the dep footprint
// for a 95% feature subset we don't need.
//
// Conversion rules:
//   - `# H1` → <h1>, `## H2` → <h2>, …
//   - `[text](url)` → <a>
//   - `` `code` `` → <code>
//   - ```` ```block``` ```` → <pre><code>
//   - `- item` / `* item` → <ul><li>
//   - blank line → paragraph break
func renderMarkdown(md []byte) string {
	lines := strings.Split(string(md), "\n")
	var b strings.Builder
	inCode := false
	inList := false
	flushList := func() {
		if inList {
			b.WriteString("</ul>\n")
			inList = false
		}
	}
	for _, line := range lines {
		// Fenced code blocks — pass through raw with HTML-escape.
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			flushList()
			if inCode {
				b.WriteString("</code></pre>\n")
				inCode = false
			} else {
				b.WriteString("<pre><code>")
				inCode = true
			}
			continue
		}
		if inCode {
			b.WriteString(html.EscapeString(line) + "\n")
			continue
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			flushList()
			b.WriteString("\n")
			continue
		}
		// Headings.
		if level, text, ok := heading(trimmed); ok {
			flushList()
			fmt.Fprintf(&b, "<h%d>%s</h%d>\n", level, renderInline(text), level)
			continue
		}
		// Lists.
		if rest, ok := listItem(trimmed); ok {
			if !inList {
				b.WriteString("<ul>\n")
				inList = true
			}
			fmt.Fprintf(&b, "<li>%s</li>\n", renderInline(rest))
			continue
		}
		// Plain paragraph line.
		flushList()
		fmt.Fprintf(&b, "<p>%s</p>\n", renderInline(trimmed))
	}
	if inCode {
		b.WriteString("</code></pre>\n")
	}
	flushList()
	return b.String()
}

func heading(s string) (int, string, bool) {
	for level := 6; level >= 1; level-- {
		prefix := strings.Repeat("#", level) + " "
		if strings.HasPrefix(s, prefix) {
			return level, strings.TrimSpace(s[len(prefix):]), true
		}
	}
	return 0, "", false
}

func listItem(s string) (string, bool) {
	for _, marker := range []string{"- ", "* "} {
		if strings.HasPrefix(s, marker) {
			return strings.TrimSpace(s[len(marker):]), true
		}
	}
	return "", false
}

// linkRe matches `[text](url)` Markdown links. RE2 (no
// backreferences) is fine here — both groups are simple.
var linkRe = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)

// codeRe matches `` `inline code` `` — single backticks, no
// nesting. The wiki output doesn't use nested backticks.
var codeRe = regexp.MustCompile("`([^`]+)`")

// renderInline runs the inline transformations (links + inline
// code) on a paragraph/heading/li body. HTML-escapes the rest so
// `<unsafe>` content from upstream sources stays inert.
func renderInline(s string) string {
	// Order matters: escape FIRST so the regex captures see escaped
	// content, then re-inject markup we explicitly want.
	out := html.EscapeString(s)
	// Markdown links — rewrite .md targets to .html so cross-page
	// links work in the site. Other URLs pass through.
	out = linkRe.ReplaceAllStringFunc(out, func(m string) string {
		sub := linkRe.FindStringSubmatch(m)
		if len(sub) != 3 {
			return m
		}
		target := sub[2]
		if strings.HasSuffix(target, ".md") {
			target = strings.TrimSuffix(target, ".md") + ".html"
		}
		return fmt.Sprintf(`<a href="%s">%s</a>`, target, sub[1])
	})
	out = codeRe.ReplaceAllString(out, "<code>$1</code>")
	return out
}

// stripHTMLTags returns a plain-text approximation of html for
// search-index purposes. Not perfect — leaves entity references like
// &amp; intact — but good enough for tokenization, where the goal is
// "did the user's search term appear in the page text?"
var tagRe = regexp.MustCompile(`<[^>]+>`)

func stripHTMLTags(s string) string {
	return tagRe.ReplaceAllString(s, " ")
}

// buildIndex constructs a token → []pageIdx inverted map. Tokens
// are lowercased and split on non-word chars. Stop-word filtering is
// intentionally skipped — a 100-page wiki has so much vocabulary
// breadth that stopwords don't bloat the index meaningfully, and
// users sometimes search for "the" inside a string they remember.
//
// Output shape (JSON):
//
//	{ "tokenName": [0, 4, 7] }
//
// where the integers are indices into the pages array embedded
// in landingPage's pageData. The JS side joins them.
func buildIndex(pages []pageMeta) map[string][]int {
	idx := map[string]map[int]struct{}{}
	for i, p := range pages {
		for _, tok := range tokenize(p.Title + " " + p.Text) {
			if idx[tok] == nil {
				idx[tok] = map[int]struct{}{}
			}
			idx[tok][i] = struct{}{}
		}
	}
	out := make(map[string][]int, len(idx))
	for tok, set := range idx {
		ids := make([]int, 0, len(set))
		for id := range set {
			ids = append(ids, id)
		}
		sort.Ints(ids)
		out[tok] = ids
	}
	return out
}

// tokenize splits on non-word characters, lowercases, drops tokens
// shorter than 3 chars (cuts the index size ~40% without losing
// real query value).
var tokenSplitRe = regexp.MustCompile(`[^\p{L}\p{N}]+`)

func tokenize(s string) []string {
	parts := tokenSplitRe.Split(strings.ToLower(s), -1)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if len([]rune(p)) >= 3 {
			out = append(out, p)
		}
	}
	return out
}

// landingPage emits index.html: a list of all pages + the search
// box. Page metadata is JSON-embedded so the search.js code can
// resolve indices to filenames + titles without a second fetch.
func landingPage(pages []pageMeta) string {
	meta, _ := json.Marshal(pages)
	var b strings.Builder
	b.WriteString(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>gogfy site</title>
<link rel="stylesheet" href="style.css">
</head>
<body>
<header><h1>gogfy site</h1></header>
<main>
<div class="search">
  <input type="text" id="q" placeholder="search the site…" autofocus autocomplete="off">
  <ol id="results"></ol>
</div>
<nav class="toc">
<h2>All pages</h2>
<ul>
`)
	for _, p := range pages {
		fmt.Fprintf(&b, `  <li><a href="pages/%s">%s</a></li>`+"\n",
			html.EscapeString(p.HTMLFile), html.EscapeString(p.Title))
	}
	b.WriteString(`</ul>
</nav>
</main>
<script>
window.GOGFY_PAGES = `)
	b.Write(meta)
	b.WriteString(`;
</script>
<script src="search.js"></script>
</body>
</html>
`)
	return b.String()
}

// wrapPage emits a per-page HTML document. Header has a "back to
// index" link so navigation doesn't dead-end on a page.
func wrapPage(p pageMeta) string {
	return fmt.Sprintf(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>%s — gogfy</title>
<link rel="stylesheet" href="../style.css">
</head>
<body>
<header><a href="../index.html">&larr; index</a> | <span>%s</span></header>
<main class="article">
%s
</main>
</body>
</html>
`, html.EscapeString(p.Title), html.EscapeString(p.Title), p.Body)
}

//go:embed style.css
var styleCSS string

//go:embed search.js
var searchJS string
