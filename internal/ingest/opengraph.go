package ingest

import (
	"regexp"
	"strings"
)

// ogMetaRe captures `<meta>` tags with an OpenGraph or Twitter Card
// property. Two attribute orderings exist in the wild (property+content
// and content+property) so we run both. Twitter-Card properties
// (`twitter:title`, etc.) are picked up as fallback when og:* is
// missing — common on X/Twitter pages whose body content is otherwise
// gated behind JS.
// `\s` before (?:property|name) is load-bearing — without it the
// pattern matches data-property="og:title", aria-name="og:x", and
// other compound attributes whose suffix happens to be "property" or
// "name". `<meta\b` (instead of `<meta\s+`) leaves the whitespace
// inside [^>]*?, so the required `\s` matches the space that
// separates `<meta` from the attribute even when property/name is
// the FIRST attribute on the tag.
var lineBreakRe = regexp.MustCompile(`[\r\n]+`)

var (
	ogMetaPropFirstRe    = regexp.MustCompile(`(?is)<meta\b[^>]*?\s(?:property|name)\s*=\s*["']((?:og|twitter):[^"']+)["'][^>]*?\scontent\s*=\s*["']([^"']*)["']`)
	ogMetaContentFirstRe = regexp.MustCompile(`(?is)<meta\b[^>]*?\scontent\s*=\s*["']([^"']*)["'][^>]*?\s(?:property|name)\s*=\s*["']((?:og|twitter):[^"']+)["']`)
)

// OGTags is the subset of OpenGraph / Twitter Card metadata Ingest
// surfaces in the sidecar. Empty fields mean the page didn't declare
// that property — Format skips them rather than printing blanks.
type OGTags struct {
	Title       string
	Description string
	ImageURL    string
	SiteName    string
}

// parseOGTags scans an HTML body for OpenGraph + Twitter Card meta
// tags. Both attribute orderings are accepted (property-first and
// content-first). og:X precedence over twitter:X is enforced by the
// `pick` priority list below, not by match order — a page that emits
// twitter:title before og:title still resolves to og:title.
func parseOGTags(html []byte) OGTags {
	props := map[string]string{}
	collect := func(name, content string) {
		// Strip line breaks: an embedded \n in a title would terminate
		// the markdown blockquote in Format() and let attacker-
		// controlled content (including frontmatter `---` markers)
		// leak out of the metadata block.
		content = lineBreakRe.ReplaceAllString(content, " ")
		props[name] = strings.TrimSpace(content)
	}
	for _, m := range ogMetaPropFirstRe.FindAllSubmatch(html, -1) {
		collect(string(m[1]), string(m[2]))
	}
	for _, m := range ogMetaContentFirstRe.FindAllSubmatch(html, -1) {
		// content is m[1], property is m[2] in this ordering.
		collect(string(m[2]), string(m[1]))
	}

	pick := func(keys ...string) string {
		for _, k := range keys {
			if v := props[k]; v != "" {
				return v
			}
		}
		return ""
	}
	return OGTags{
		Title:       pick("og:title", "twitter:title"),
		Description: pick("og:description", "twitter:description"),
		ImageURL:    pick("og:image", "twitter:image"),
		SiteName:    pick("og:site_name"),
	}
}

// Empty reports whether none of the surfaced fields were populated.
// Callers use this to decide whether to prepend a metadata block to
// the sidecar.
func (t OGTags) Empty() bool {
	return t.Title == "" && t.Description == "" && t.ImageURL == "" && t.SiteName == ""
}

// Format renders the tags as a fenced metadata block suitable for
// prepending to a sidecar markdown. The block is wrapped in `> ` quote
// lines so it visually separates from the article body and survives
// markdown linting tools that flag bare colons at column 1.
func (t OGTags) Format() string {
	var b strings.Builder
	b.WriteString("> **OpenGraph metadata**\n")
	if t.SiteName != "" {
		b.WriteString("> Site: " + t.SiteName + "\n")
	}
	if t.Title != "" {
		b.WriteString("> Title: " + t.Title + "\n")
	}
	if t.Description != "" {
		b.WriteString("> Description: " + t.Description + "\n")
	}
	if t.ImageURL != "" {
		b.WriteString("> Image: " + t.ImageURL + "\n")
	}
	return b.String()
}

// tweetHostRe matches Twitter / X status URLs. Detection is by host
// + `/status/` path segment — the actual tweet id is unused; we just
// want to know whether the URL is a tweet so the sidecar can be
// labeled accordingly even when OG metadata is also pulled.
var tweetHostRe = regexp.MustCompile(`(?i)^https?://(?:www\.|mobile\.)?(?:twitter|x)\.com/[^/]+/status/`)

// IsTweetURL reports whether rawURL points at a Twitter/X status.
// Exported so the caller (CLI / pipeline) can label or route tweets
// distinctly without re-implementing the host check.
func IsTweetURL(rawURL string) bool {
	return tweetHostRe.MatchString(rawURL)
}
