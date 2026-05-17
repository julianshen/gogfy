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

// Ingest fetches `rawURL`, writes a markdown sidecar to outDir, and
// returns the written path. Skips re-fetch if the sidecar already
// exists (the URL's content-hash filename means the same URL maps to
// the same path across runs).
//
// Returns the path even on skip so callers can pass it to the next
// pipeline stage.
func Ingest(ctx context.Context, rawURL, outDir string, opts safefetch.Options) (string, error) {
	path := SidecarPath(outDir, rawURL)
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	body, _, err := safefetch.Fetch(ctx, rawURL, opts)
	if err != nil {
		return "", fmt.Errorf("ingest: %w", err)
	}
	md := htmlToMarkdown(body)
	out := fmt.Sprintf("---\nsource_url: %q\n---\n\n%s\n", rawURL, md)
	if err := fsutil.WriteFileAtomic(path, []byte(out), 0644); err != nil {
		return "", fmt.Errorf("ingest: write %s: %w", path, err)
	}
	return path, nil
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

