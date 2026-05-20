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
// Options configures Ingest's behavior beyond the SSRF-guard layer.
// Kept narrow today (YouTube audio mode); expand as new ingest
// branches grow knobs.
type Options struct {
	// Safefetch is the SSRF / size guard configuration passed to
	// safefetch.Fetch. Zero-value uses the default guard.
	Safefetch safefetch.Options
	// YouTubeFetchAudio, when true, fetches the audio stream of a
	// YouTube video URL and writes a binary `.m4a` / `.webm` sidecar
	// so the transcribe pipeline can pick it up. When false (the
	// historical default), YouTube URLs only get an oembed
	// metadata-only `.md` sidecar — no audio bytes touched.
	YouTubeFetchAudio bool
	// YouTubeAudioFetcher is a hook for tests to inject a synthetic
	// audio fetcher without reaching for the live `ytaudio` client.
	// Production callers leave this nil; the package supplies the
	// default `ytaudio.New()` client when audio mode is enabled.
	YouTubeAudioFetcher YouTubeAudioFetcher
}

// YouTubeAudioFetcher is the minimum surface Ingest needs from the
// `ytaudio` package. Pulled into a named interface so the ingest
// package doesn't import `ytaudio` directly (avoids a heavy
// transitive dependency for callers who only need text ingestion).
type YouTubeAudioFetcher interface {
	Download(ctx context.Context, videoURL string) (audio []byte, contentType, title, author string, err error)
}

// Ingest is the historical entry point: text-mode ingest with the
// default safefetch policy. New callers wanting YouTube-audio mode
// should use IngestWithOptions.
func Ingest(ctx context.Context, rawURL, outDir string, opts safefetch.Options) (string, error) {
	return IngestWithOptions(ctx, rawURL, outDir, Options{Safefetch: opts})
}

// IngestWithOptions is the knob-bearing form. Future ingest features
// hang flags here instead of churning Ingest's signature.
func IngestWithOptions(ctx context.Context, rawURL, outDir string, options Options) (string, error) {
	opts := options.Safefetch
	fetchURL := rawURL
	if rewritten, ok := rewriteGitHubURL(rawURL); ok {
		fetchURL = rewritten
	} else {
		fetchURL = rewriteArxivURL(fetchURL)
	}
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
	// Image sidecars use an ext-specific filename; on re-ingest the
	// extension isn't known up front (the body is what told us last
	// time), so probe the four image variants we write. PNG/JPEG/GIF/
	// WebP only — SVG goes through the markdown path.
	for _, ext := range []string{".png", ".jpg", ".gif", ".webp"} {
		if _, err := os.Stat(imageSidecarPath(outDir, rawURL, ext)); err == nil {
			return imageSidecarPath(outDir, rawURL, ext), nil
		}
	}
	// YouTube takes a different shape — the watch-page HTML is JS-
	// rendered and full of player chrome, but the oembed endpoint
	// returns clean title/author/thumbnail JSON. Two modes:
	//   1. Default (--no audio): write a metadata-only .md sidecar
	//      using the oembed response.
	//   2. YouTubeFetchAudio=true: download the audio stream via
	//      kkdai/youtube (pure-Go yt-dlp replacement) and write a
	//      binary sidecar so transcribe picks it up like any other
	//      audio file. Falls back to the metadata path if the audio
	//      fetch fails — better to land *something* than nothing.
	if IsYouTubeURL(rawURL) {
		// Idempotent skip for the audio sidecar (extension depends on
		// the codec YouTube serves; probe both common shapes).
		for _, ext := range []string{".m4a", ".webm"} {
			if _, err := os.Stat(audioSidecarPath(outDir, rawURL, ext)); err == nil {
				return audioSidecarPath(outDir, rawURL, ext), nil
			}
		}
		if options.YouTubeFetchAudio && options.YouTubeAudioFetcher != nil {
			path, err := tryFetchYouTubeAudio(ctx, rawURL, outDir, options.YouTubeAudioFetcher)
			if err == nil {
				return path, nil
			}
			// Audio mode is best-effort. Geo-blocks, age gates, and
			// upload-in-progress videos all surface here; we log to
			// stderr and fall through to the metadata path so the URL
			// still lands in the corpus.
			fmt.Fprintf(os.Stderr, "ingest: youtube audio fetch failed for %s, falling back to metadata-only: %v\n", rawURL, err)
		}
		meta, err := fetchYouTubeMetadata(ctx, rawURL, opts)
		if err != nil {
			return "", fmt.Errorf("ingest: %w", err)
		}
		out := fmt.Sprintf("---\nsource_url: %q\nkind: youtube\n---\n\n%s\n", rawURL, meta)
		if err := fsutil.WriteFileAtomic(mdPath, []byte(out), 0644); err != nil {
			return "", fmt.Errorf("ingest: write %s: %w", mdPath, err)
		}
		return mdPath, nil
	}
	body, finalURL, err := safefetch.Fetch(ctx, fetchURL, opts)
	if err != nil {
		return "", fmt.Errorf("ingest: %w", err)
	}
	// finalURL reflects the post-redirect URL — a server that 302s to
	// a .pdf is just as much a PDF source as one that serves it
	// directly. fetchURL (pre-redirect) catches the case where a
	// .pdf URL serves with octet-stream and never redirects.
	if isPDF(body, fetchURL) || isPDF(nil, finalURL) {
		if err := fsutil.WriteFileAtomic(pdfPath, body, 0644); err != nil {
			return "", fmt.Errorf("ingest: write %s: %w", pdfPath, err)
		}
		return pdfPath, nil
	}
	// Images land as raw binary sidecars so downstream
	// semantic.ExtractImage (vision LLM path) sees the original bytes.
	// imageKind checks both pre- and post-redirect URLs since a server
	// can 302 a generic URL into a `.png`, or serve a `.png` URL with a
	// generic content type — either branch needs to match.
	if ext, ok := imageKind(body, fetchURL); ok {
		return writeImageSidecar(outDir, rawURL, ext, body)
	}
	if ext, ok := imageKind(nil, finalURL); ok {
		return writeImageSidecar(outDir, rawURL, ext, body)
	}
	md := htmlToMarkdown(body)
	// Pull OpenGraph / Twitter-Card metadata before-narrowing pruned
	// the <head> region — pages where the body is JS-rendered (notably
	// X/Twitter status pages) still carry title/description/image in
	// og:* meta tags, and that's often the only signal worth keeping.
	og := parseOGTags(body)
	if !og.Empty() {
		md = og.Format() + "\n" + md
	}
	kindLabel := ""
	switch {
	case IsTweetURL(rawURL):
		kindLabel = "kind: tweet\n"
	case IsGitHubURL(rawURL):
		kindLabel = "kind: github\n"
	}
	out := fmt.Sprintf("---\nsource_url: %q\n%s---\n\n%s\n", rawURL, kindLabel, md)
	if err := fsutil.WriteFileAtomic(mdPath, []byte(out), 0644); err != nil {
		return "", fmt.Errorf("ingest: write %s: %w", mdPath, err)
	}
	return mdPath, nil
}

// audioSidecarPath mirrors pdfSidecarPath but with a caller-supplied
// extension for YouTube audio sidecars (typical: .m4a for AAC, .webm
// for Opus — whichever YouTube serves).
func audioSidecarPath(outDir, rawURL, ext string) string {
	h := sha1.Sum([]byte(rawURL))
	hx := hex.EncodeToString(h[:8])
	stem := urlStem(rawURL)
	if stem == "" {
		stem = "url"
	}
	return filepath.Join(outDir, "ingested", stem+"-"+hx+ext)
}

// tryFetchYouTubeAudio downloads the audio stream and writes a
// binary sidecar. The MIME → extension mapping is small: YouTube
// reliably serves either audio/mp4 (m4a) or audio/webm. Anything
// else falls back to .m4a so the file still has SOMETHING that
// Whisper's content-type sniffer accepts.
func tryFetchYouTubeAudio(ctx context.Context, rawURL, outDir string, fetcher YouTubeAudioFetcher) (string, error) {
	audio, contentType, title, author, err := fetcher.Download(ctx, rawURL)
	if err != nil {
		return "", err
	}
	if len(audio) == 0 {
		return "", fmt.Errorf("empty audio stream")
	}
	ext := ".m4a"
	if strings.Contains(contentType, "webm") {
		ext = ".webm"
	}
	path := audioSidecarPath(outDir, rawURL, ext)
	if err := fsutil.WriteFileAtomic(path, audio, 0644); err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}
	// Also write a sibling .meta.md with the title/author so the
	// downstream transcribe pass can emit a frontmatter block that
	// keeps the source-URL connection. The transcribe pipeline writes
	// its own .md alongside; the metadata file is informational only.
	if title != "" || author != "" {
		metaPath := path + ".meta.md"
		meta := fmt.Sprintf("---\nsource_url: %q\nkind: youtube\ntitle: %q\nauthor: %q\n---\n", rawURL, title, author)
		_ = fsutil.WriteFileAtomic(metaPath, []byte(meta), 0644)
	}
	return path, nil
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
	scriptRe   = regexp.MustCompile(`(?is)<script\b[^>]*>.*?</\s*script\s*>`)
	styleRe    = regexp.MustCompile(`(?is)<style\b[^>]*>.*?</\s*style\s*>`)
	noscriptRe = regexp.MustCompile(`(?is)<noscript\b[^>]*>.*?</\s*noscript\s*>`)
	// Boilerplate regions — common HTML5 semantic tags that wrap site
	// chrome (navigation, footers, sidebars, page headers). Dropped
	// before the main pass so their text doesn't pollute the markdown
	// corpus. Real <article>/<main>-aware readability would be
	// stronger, but for typical blog/docs HTML this drops 80% of the
	// noise with a tiny amount of code.
	navRe    = regexp.MustCompile(`(?is)<nav\b[^>]*>.*?</\s*nav\s*>`)
	footerRe = regexp.MustCompile(`(?is)<footer\b[^>]*>.*?</\s*footer\s*>`)
	asideRe  = regexp.MustCompile(`(?is)<aside\b[^>]*>.*?</\s*aside\s*>`)
	// <header> at the top level is usually the site header (logo +
	// nav). It's tag-overloaded — articles also use <header> for a
	// title block — but the article-narrowing pass below extracts
	// <article>/<main> first, so by the time headerRe fires we're
	// looking at outer chrome whenever an article wrapper existed.
	headerRe = regexp.MustCompile(`(?is)<header\b[^>]*>.*?</\s*header\s*>`)
	// articleRe / mainRe capture the principal content region when
	// the page uses semantic HTML5. Narrowing to this region before
	// the rest of the conversion drops sidebars/comments/related-
	// links sections in one shot.
	articleRe    = regexp.MustCompile(`(?is)<article\b[^>]*>(.*?)</\s*article\s*>`)
	mainRe       = regexp.MustCompile(`(?is)<main\b[^>]*>(.*?)</\s*main\s*>`)
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
	s := narrowToArticle(string(html))
	s = scriptRe.ReplaceAllString(s, "")
	s = styleRe.ReplaceAllString(s, "")
	s = noscriptRe.ReplaceAllString(s, "")
	// Drop site chrome (nav/footer/aside/header) before paragraph &
	// heading conversion so their text doesn't get folded into the
	// markdown body. Order: scripts/styles first (they may contain
	// `</nav>` strings inside string literals that would confuse the
	// boilerplate regex), then boilerplate regions, then content.
	s = navRe.ReplaceAllString(s, "")
	s = footerRe.ReplaceAllString(s, "")
	s = asideRe.ReplaceAllString(s, "")
	s = headerRe.ReplaceAllString(s, "")
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

// narrowToArticle returns the inner content of the first <article>
// element if present, otherwise the first <main> element, otherwise
// the input unchanged. Lets the rest of the conversion ignore the
// sidebars/comments/related-content blocks that surround the article
// region on a typical blog/docs page.
//
// Picks the LARGEST matching block when there are several (some sites
// nest article tags for cards or related-posts widgets) so we don't
// accidentally narrow down to a thumbnail caption.
func narrowToArticle(s string) string {
	if region := largestMatch(articleRe, s); region != "" {
		return region
	}
	if region := largestMatch(mainRe, s); region != "" {
		return region
	}
	return s
}

// largestMatch returns the longest captured group across all matches
// of re in s. Empty result when the regex doesn't match at all.
func largestMatch(re *regexp.Regexp, s string) string {
	matches := re.FindAllStringSubmatch(s, -1)
	best := ""
	for _, m := range matches {
		if len(m) >= 2 && len(m[1]) > len(best) {
			best = m[1]
		}
	}
	return best
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

