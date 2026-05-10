package extract

import (
	"regexp"

	"github.com/julianshen/gogfy/internal/schema"
)

// urlPatternInProse matches plausible HTTP(S) URLs embedded in free-form
// prose: scheme + host + path, stopping at common closing punctuation.
// Trailing punctuation (".", ",", ")", etc.) is trimmed by the caller
// since reasonable URLs in prose end at sentence punctuation, not at
// the URL's own structure.
var urlPatternInProse = regexp.MustCompile(`https?://[^\s<>"\[\]]+`)

// extractURLs returns every URL-like substring found in s, with trailing
// sentence punctuation removed. Used by document extractors that don't
// have a structural AST (plain text) or where the AST loses inline
// target details (RST without docutils).
func extractURLs(s string) []string {
	matches := urlPatternInProse.FindAllString(s, -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, trimURLTrailing(m))
	}
	return out
}

// urlEdges builds one references-relation edge per URL found in body,
// each sourced from `source` and targeting `<lang>:link:<url>`. Used by
// the structurally-thin extractors (text, pdf) where the document has
// no internal sections to attribute URLs to and every link sources from
// the module node.
func urlEdges(lang, source, body string) []schema.Edge {
	urls := extractURLs(body)
	out := make([]schema.Edge, 0, len(urls))
	for _, u := range urls {
		out = append(out, schema.Edge{
			Source:     source,
			Target:     schema.LangID(lang, "link", u),
			Relation:   "references",
			Confidence: schema.Extracted,
		})
	}
	return out
}

// trimURLTrailing strips trailing sentence punctuation that's almost
// certainly not part of the URL itself: `.`, `,`, `)`, `]`, `;`, `:`.
// Keeps `/` and `?` since they're part of valid URL paths.
func trimURLTrailing(u string) string {
	for len(u) > 0 {
		last := u[len(u)-1]
		if last == '.' || last == ',' || last == ')' || last == ']' ||
			last == ';' || last == ':' || last == '!' || last == '?' {
			u = u[:len(u)-1]
			continue
		}
		break
	}
	return u
}
