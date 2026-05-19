// Package ingest fetches a URL via safefetch and converts the
// response into a corpus-shaped file: a markdown body plus a path
// derived from the URL so downstream extractors (markdown, semantic)
// pick it up without special-casing.
//
// HTML conversion is intentionally minimal in v1 — strip tags, keep
// text + heading structure. Readability-style article extraction
// (drop nav/footer/sidebars) is a follow-up; the v1 shape is good
// enough to feed semantic.Extract with most blog/docs content.
package ingest

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/julianshen/gogfy/internal/fsutil"
	"github.com/julianshen/gogfy/internal/safefetch"
)

// Ingest fetches `rawURL`, writes a sidecar to outDir, and returns
// the written path. The sidecar extension depends on the response:
// PDF bodies (detected via magic bytes) are saved as `.pdf` so the
// existing PDF extractor processes them; HTML/text bodies are
// converted to markdown and saved as `.md`. arXiv `abs/` URLs are
// rewritten to their PDF equivalent before fetching so paper
// ingestion always lands as a real PDF rather than the abstract page.
//
// Idempotent: the content-hashed filename means the same URL maps to
// the same path across runs; re-running skips the fetch.
func Ingest(ctx context.Context, rawURL, outDir string, opts safefetch.Options) (string, error) {
	fetchURL := rewriteArxivURL(rawURL)
	// Skip-on-exists is checked twice: once eagerly with the .md
	// guess for the common HTML case, and once after the fetch for
	// the PDF case (which we can't know until we see the body).
	mdPath := SidecarPath(outDir, rawURL)
	if _, err := os.Stat(mdPath); err == nil {
		return mdPath, nil
	}
	pdfPath := pdfSidecarPath(outDir, rawURL)
	if _, err := os.Stat(pdfPath); err == nil {
		return pdfPath, nil
	}
	body, _, err := safefetch.Fetch(ctx, fetchURL, opts)
	if err != nil {
		return "", fmt.Errorf("ingest: %w", err)
	}
	if isPDF(body, fetchURL) {
		if err := fsutil.WriteFileAtomic(pdfPath, body, 0644); err != nil {
			return "", fmt.Errorf("ingest: write %s: %w", pdfPath, err)
		}
		return pdfPath, nil
	}
	md := htmlToMarkdown(body)
	out := fmt.Sprintf("---\nsource_url: %q\n---\n\n%s\n", rawURL, md)
	if err := fsutil.WriteFileAtomic(mdPath, []byte(out), 0644); err != nil {
		return "", fmt.Errorf("ingest: write %s: %w", mdPath, err)
	}
	return mdPath, nil
}

// arxivAbsRe matches arXiv abstract URLs and captures the paper id
// (with optional version) so we can rewrite to the PDF endpoint.
// Examples: https://arxiv.org/abs/2401.12345, .../abs/2401.12345v2,
// .../abs/cs/0601001 (pre-2007 cross-list ids).
var arxivAbsRe = regexp.MustCompile(`^(https?://arxiv\.org)/abs/([^?#]+)`)

// rewriteArxivURL converts an arXiv abstract URL into its PDF
// equivalent so ingest always lands the paper itself, not the HTML
// landing page. Non-arXiv URLs pass through unchanged.
func rewriteArxivURL(rawURL string) string {
	if m := arxivAbsRe.FindStringSubmatch(rawURL); m != nil {
		id := strings.TrimSuffix(m[2], ".pdf")
		return m[1] + "/pdf/" + id + ".pdf"
	}
	return rawURL
}

// isPDF reports whether the response should be treated as a PDF.
// Checks the magic-bytes prefix (authoritative) plus a URL-suffix
// fallback (catches servers that serve PDFs with octet-stream or
// generic content types).
func isPDF(body []byte, finalURL string) bool {
	if len(body) >= 5 && string(body[:5]) == "%PDF-" {
		return true
	}
	return strings.HasSuffix(strings.ToLower(finalURL), ".pdf")
}

// pdfSidecarPath mirrors SidecarPath but lands the URL under a `.pdf`
// extension so schema.ClassifyFile tags it as a paper and the PDF
// extractor (not the markdown reader) picks it up.
func pdfSidecarPath(outDir, rawURL string) string {
	h := sha1.Sum([]byte(rawURL))
	hex := hex.EncodeToString(h[:8])
	stem := urlStem(rawURL)
	if stem == "" {
		stem = "url"
	}
	return filepath.Join(outDir, "ingested", stem+"-"+hex+".pdf")
}

// SidecarPath returns where a URL's ingested markdown lives. Uses a
// content-hashed filename so the path is stable across runs and
// idempotent — re-ingesting the same URL doesn't multiply files in
// the corpus.
func SidecarPath(outDir, rawURL string) string {
	h := sha1.Sum([]byte(rawURL))
	hex := hex.EncodeToString(h[:8]) // 16 chars — plenty to avoid collisions
	stem := urlStem(rawURL)
	if stem == "" {
		stem = "url"
	}
	return filepath.Join(outDir, "ingested", stem+"-"+hex+".md")
}

// urlStem builds a human-readable filename prefix from the URL's
// host + last path segment, sanitized for filesystem safety.
func urlStem(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	last := filepath.Base(strings.TrimSuffix(u.Path, "/"))
	if last == "." || last == "/" || last == "" {
		last = u.Host
	}
	return safeStem(strings.ReplaceAll(u.Host+"-"+last, ".", "-"))
}

var unsafeFSChars = regexp.MustCompile(`[^A-Za-z0-9_\-]+`)

func safeStem(s string) string {
	s = unsafeFSChars.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 60 {
		s = s[:60]
	}
	return s
}

// htmlToMarkdown is a v1 best-effort HTML→text converter. It:
//   - drops <script>, <style>, <noscript> blocks entirely
//   - converts <h1-h6> to markdown heading markers
//   - converts <p>, <br>, <li> to paragraph/line breaks
//   - drops all other tags but preserves their text content
//
// Less polished than a real readability extractor but enough to feed
// downstream semantic extraction with sensible prose.
// Go's RE2 disallows backreferences, so we can't share one pattern
// for "<script>...</script>"-style blocks — each tag type needs its
// own regex. Headings keep their level via a numbered capture and a
// post-match map.
var (
	scriptRe     = regexp.MustCompile(`(?is)<script\b[^>]*>.*?</\s*script\s*>`)
	styleRe      = regexp.MustCompile(`(?is)<style\b[^>]*>.*?</\s*style\s*>`)
	noscriptRe   = regexp.MustCompile(`(?is)<noscript\b[^>]*>.*?</\s*noscript\s*>`)
	h1Re         = regexp.MustCompile(`(?is)<h1\b[^>]*>(.*?)</\s*h1\s*>`)
	h2Re         = regexp.MustCompile(`(?is)<h2\b[^>]*>(.*?)</\s*h2\s*>`)
	h3Re         = regexp.MustCompile(`(?is)<h3\b[^>]*>(.*?)</\s*h3\s*>`)
	h4Re         = regexp.MustCompile(`(?is)<h4\b[^>]*>(.*?)</\s*h4\s*>`)
	h5Re         = regexp.MustCompile(`(?is)<h5\b[^>]*>(.*?)</\s*h5\s*>`)
	h6Re         = regexp.MustCompile(`(?is)<h6\b[^>]*>(.*?)</\s*h6\s*>`)
	paragraphRe  = regexp.MustCompile(`(?is)<p\b[^>]*>(.*?)</\s*p\s*>`)
	listItemRe   = regexp.MustCompile(`(?is)<li\b[^>]*>(.*?)</\s*li\s*>`)
	brRe         = regexp.MustCompile(`(?is)<br\s*/?>`)
	tagRe        = regexp.MustCompile(`(?is)<[^>]+>`)
	multiBlankRe = regexp.MustCompile(`\n\s*\n\s*\n+`)
)

// headingLevels indexes h-tag regex → markdown prefix so the heading
// loop in htmlToMarkdown stays a single pass instead of six.
var headingLevels = []struct {
	re     *regexp.Regexp
	prefix string
}{
	{h1Re, "# "}, {h2Re, "## "}, {h3Re, "### "},
	{h4Re, "#### "}, {h5Re, "##### "}, {h6Re, "###### "},
}

func htmlToMarkdown(html []byte) string {
	s := string(html)
	s = scriptRe.ReplaceAllString(s, "")
	s = styleRe.ReplaceAllString(s, "")
	s = noscriptRe.ReplaceAllString(s, "")
	for _, h := range headingLevels {
		prefix := h.prefix
		s = h.re.ReplaceAllStringFunc(s, func(m string) string {
			match := h.re.FindStringSubmatch(m)
			if len(match) < 2 {
				return ""
			}
			return "\n\n" + prefix + stripTags(match[1]) + "\n\n"
		})
	}
	s = paragraphRe.ReplaceAllStringFunc(s, func(m string) string {
		match := paragraphRe.FindStringSubmatch(m)
		if len(match) < 2 {
			return ""
		}
		return "\n\n" + stripTags(match[1]) + "\n\n"
	})
	s = listItemRe.ReplaceAllStringFunc(s, func(m string) string {
		match := listItemRe.FindStringSubmatch(m)
		if len(match) < 2 {
			return ""
		}
		return "\n- " + stripTags(match[1])
	})
	s = brRe.ReplaceAllString(s, "\n")
	s = tagRe.ReplaceAllString(s, "")
	// Decode AFTER the final tagRe strip so an entity-encoded
	// "&lt;x&gt;" survives as "<x>" instead of being decoded mid-pass
	// and then mistakenly stripped as if it were a real HTML tag.
	s = decodeBasicEntities(s)
	s = multiBlankRe.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

// stripTags removes HTML tags from a span without touching entities —
// entity decoding is deferred to htmlToMarkdown's final pass.
func stripTags(s string) string {
	return tagRe.ReplaceAllString(s, "")
}

// decodeBasicEntities handles the most common HTML entities. Not
// exhaustive — the v1 goal is "readable enough for an LLM to parse,"
// not "byte-perfect HTML→text round-trip."
func decodeBasicEntities(s string) string {
	for _, pair := range [][2]string{
		{"&amp;", "&"}, {"&lt;", "<"}, {"&gt;", ">"},
		{"&quot;", `"`}, {"&apos;", "'"}, {"&#39;", "'"},
		{"&nbsp;", " "}, {"&mdash;", "—"}, {"&ndash;", "–"},
	} {
		s = strings.ReplaceAll(s, pair[0], pair[1])
	}
	return s
}

