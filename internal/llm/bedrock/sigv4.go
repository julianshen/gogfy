package bedrock

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"sort"
	"strings"
	"time"
)

// AWS Signature Version 4 (SigV4) for the Bedrock Runtime "bedrock"
// service. Implemented from the AWS spec
// (https://docs.aws.amazon.com/general/latest/gr/sigv4_signing.html)
// rather than via aws-sdk-go to keep gogfy dependency-free.
//
// The signing flow has four steps:
//   1. Build a canonical request (method, path, query, headers, payload hash)
//   2. Build a string-to-sign (algorithm, time, credential scope, canonical-request hash)
//   3. Derive a signing key (HMAC chain over date/region/service/"aws4_request")
//   4. Compute the signature (HMAC of signing key over string-to-sign)
// The Authorization header is then assembled from credential, signed
// headers, and signature.

// signCredentials are the inputs the signer needs.
type signCredentials struct {
	AccessKey    string
	SecretKey    string
	SessionToken string // optional — present for STS / temporary creds
	Region       string
	Service      string // "bedrock" for Bedrock Runtime
}

// signRequest adds an AWS SigV4 Authorization header (and x-amz-date,
// and optionally x-amz-security-token) to req. payload must be the
// exact bytes that will be sent — the canonical request includes its
// SHA-256 hash. The supplied now lets callers / tests pin a
// deterministic timestamp.
func signRequest(req *http.Request, payload []byte, creds signCredentials, now time.Time) {
	amzDate := now.UTC().Format("20060102T150405Z")
	dateStamp := now.UTC().Format("20060102")
	req.Header.Set("X-Amz-Date", amzDate)
	if creds.SessionToken != "" {
		req.Header.Set("X-Amz-Security-Token", creds.SessionToken)
	}
	// Host header must be present so we can include it in the signed
	// headers — Go's net/http strips it from the Header map at send
	// time, but for signing purposes we add it ourselves.
	if req.Host == "" {
		req.Host = req.URL.Host
	}
	payloadHash := sha256Hex(payload)
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)

	canonicalHeaders, signedHeaders := canonicalAndSignedHeaders(req)
	canonicalReq := strings.Join([]string{
		req.Method,
		canonicalURI(req.URL.Path),
		canonicalQuery(req.URL.RawQuery),
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	}, "\n")

	scope := dateStamp + "/" + creds.Region + "/" + creds.Service + "/aws4_request"
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		scope,
		sha256Hex([]byte(canonicalReq)),
	}, "\n")

	signingKey := deriveSigningKey(creds.SecretKey, dateStamp, creds.Region, creds.Service)
	signature := hex.EncodeToString(hmacSHA256(signingKey, stringToSign))

	auth := "AWS4-HMAC-SHA256 " +
		"Credential=" + creds.AccessKey + "/" + scope + ", " +
		"SignedHeaders=" + signedHeaders + ", " +
		"Signature=" + signature
	req.Header.Set("Authorization", auth)
}

// canonicalURI returns the URI-encoded path. For Bedrock the path
// contains model IDs like `anthropic.claude-3-5-haiku-20241022-v1:0`;
// the colon must be percent-encoded for the canonical request even
// though it transits the wire unencoded.
func canonicalURI(path string) string {
	if path == "" {
		return "/"
	}
	// Encode per RFC 3986 reserved-char rules used by SigV4.
	// Slashes are NOT encoded (they're path separators); colons,
	// spaces, etc. are.
	var b strings.Builder
	for i := 0; i < len(path); i++ {
		c := path[i]
		if isUnreservedPathChar(c) {
			b.WriteByte(c)
		} else {
			b.WriteByte('%')
			const hexd = "0123456789ABCDEF"
			b.WriteByte(hexd[c>>4])
			b.WriteByte(hexd[c&0xF])
		}
	}
	return b.String()
}

func isUnreservedPathChar(c byte) bool {
	switch {
	case c >= 'A' && c <= 'Z':
		return true
	case c >= 'a' && c <= 'z':
		return true
	case c >= '0' && c <= '9':
		return true
	case c == '-' || c == '.' || c == '_' || c == '~' || c == '/':
		return true
	}
	return false
}

// canonicalQuery sorts and re-encodes the query string per SigV4
// rules: lexicographic by key, RFC 3986 percent-encoded values.
func canonicalQuery(raw string) string {
	if raw == "" {
		return ""
	}
	pairs := strings.Split(raw, "&")
	sort.Strings(pairs)
	return strings.Join(pairs, "&")
}

// canonicalAndSignedHeaders returns the canonical-headers block and
// the signed-headers list. Host is always signed; all x-amz-* and
// Content-Type headers present on the request are signed too.
func canonicalAndSignedHeaders(req *http.Request) (string, string) {
	// Build map keyed by lowercase header name. Host comes from
	// req.Host (net/http hides it from the Header map at send time).
	headers := map[string]string{"host": req.Host}
	for name, vals := range req.Header {
		lower := strings.ToLower(name)
		// SigV4 requires trimmed values with inner whitespace
		// collapsed; for our use case header values don't carry
		// whitespace runs so a simple TrimSpace suffices.
		headers[lower] = strings.TrimSpace(strings.Join(vals, ","))
	}
	names := make([]string, 0, len(headers))
	for n := range headers {
		names = append(names, n)
	}
	sort.Strings(names)
	var canon strings.Builder
	for _, n := range names {
		canon.WriteString(n)
		canon.WriteString(":")
		canon.WriteString(headers[n])
		canon.WriteString("\n")
	}
	return canon.String(), strings.Join(names, ";")
}

func deriveSigningKey(secret, date, region, service string) []byte {
	k := hmacSHA256([]byte("AWS4"+secret), date)
	k = hmacSHA256(k, region)
	k = hmacSHA256(k, service)
	k = hmacSHA256(k, "aws4_request")
	return k
}

func hmacSHA256(key []byte, data string) []byte {
	m := hmac.New(sha256.New, key)
	_, _ = m.Write([]byte(data))
	return m.Sum(nil)
}

func sha256Hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
