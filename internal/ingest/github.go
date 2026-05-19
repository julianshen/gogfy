package ingest

import "regexp"

// githubRepoRe captures `<owner>/<repo>` from a bare repo URL — no
// /blob/, /tree/, /pulls, /issues paths. Used to route to the repo's
// default-branch README rather than the HTML landing page.
var githubRepoRe = regexp.MustCompile(`^https?://github\.com/([^/]+)/([^/?#]+)/?$`)

// githubBlobRe captures `<owner>/<repo>/blob/<ref>/<path>` so we can
// rewrite to the raw.githubusercontent.com equivalent. <ref> is the
// branch / tag / SHA segment; <path> is everything after it.
var githubBlobRe = regexp.MustCompile(`^https?://github\.com/([^/]+)/([^/]+)/blob/([^/]+)/(.+)$`)

// IsGitHubURL reports whether rawURL is a github.com URL gogfy can
// rewrite to a raw-content endpoint. Exported so callers can label
// the sidecar (e.g. frontmatter `kind: github`) before fetch.
func IsGitHubURL(rawURL string) bool {
	return githubRepoRe.MatchString(rawURL) || githubBlobRe.MatchString(rawURL)
}

// rewriteGitHubURL converts a github.com URL into a
// raw.githubusercontent.com URL whose body is text rather than HTML
// chrome:
//   - github.com/o/r        → raw.githubusercontent.com/o/r/HEAD/README.md
//   - github.com/o/r/blob/b/p → raw.githubusercontent.com/o/r/b/p
//
// `HEAD` lets GitHub resolve the default branch without a separate
// API call. Other URL shapes (issues, pulls, releases) are left
// unchanged — the existing HTML→markdown path handles them; rewriting
// would just point at a 404.
//
// Returns ("", false) when no rewrite applies so callers can decide
// whether to fetch the original URL or signal a non-rewritable
// GitHub shape.
func rewriteGitHubURL(rawURL string) (string, bool) {
	if m := githubBlobRe.FindStringSubmatch(rawURL); m != nil {
		return "https://raw.githubusercontent.com/" + m[1] + "/" + m[2] + "/" + m[3] + "/" + m[4], true
	}
	if m := githubRepoRe.FindStringSubmatch(rawURL); m != nil {
		return "https://raw.githubusercontent.com/" + m[1] + "/" + m[2] + "/HEAD/README.md", true
	}
	return "", false
}
