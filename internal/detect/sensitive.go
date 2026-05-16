package detect

import (
	"path/filepath"
	"regexp"
	"strings"
)

// sensitivePatterns matches paths that likely contain secrets or
// credentials. Files matching any of these are dropped from the corpus
// silently — graphify's policy is that surfacing the existence of a
// `.env.production` file in a report is itself a leak.
//
// The set tracks graphify upstream verbatim so cross-tool consumers
// agree on what's skipped.
var sensitivePatterns = []*regexp.Regexp{
	// dotenv: .env, .envrc, .env.production, etc.
	regexp.MustCompile(`(^|[\\/])\.(env|envrc)(\..*)?$`),
	// keys & certs
	regexp.MustCompile(`(?i)\.(pem|key|p12|pfx|cert|crt|der|p8)$`),
	// credentials / secrets / tokens in the basename
	regexp.MustCompile(`(?i)\b(credential|secret|passwd|password|token|private_key)s?\b`),
	// SSH keypairs
	regexp.MustCompile(`(id_rsa|id_dsa|id_ecdsa|id_ed25519)(\.pub)?$`),
	// dotfiles that store auth
	regexp.MustCompile(`(?i)(\.netrc|\.pgpass|\.htpasswd)$`),
	// cloud creds
	regexp.MustCompile(`(?i)(aws_credentials|gcloud_credentials|service.account)`),
}

// IsSensitive reports whether path looks like a credential / secret store
// that should be skipped without surfacing its existence. Matching is
// case-insensitive on the basename for most patterns.
func IsSensitive(path string) bool {
	// Normalize to forward slashes so the regex patterns work on Windows
	// paths without per-platform branching.
	candidate := filepath.ToSlash(path)
	// Some patterns (dotenv) look at the slash-prefixed form; the rest
	// only need the basename. Cheapest to pass both shapes.
	base := candidate
	if i := strings.LastIndex(candidate, "/"); i >= 0 {
		base = candidate[i+1:]
	}
	for _, re := range sensitivePatterns {
		if re.MatchString(candidate) || re.MatchString(base) {
			return true
		}
	}
	return false
}
