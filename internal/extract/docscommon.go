package extract

import "regexp"

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
