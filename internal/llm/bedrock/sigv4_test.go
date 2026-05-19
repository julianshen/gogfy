package bedrock

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestSigningKeyMatchesAWSReferenceVector pins our HMAC chain against
// the example values published by AWS in
// https://docs.aws.amazon.com/general/latest/gr/sigv4-signed-request-examples.html
// — same secret, date, region, service the docs use. If this drifts,
// every request we send to AWS will 403; an explicit test catches
// the regression before deploy rather than at runtime.
func TestSigningKeyMatchesAWSReferenceVector(t *testing.T) {
	// From the AWS example: deriving a signing key for an iam request
	// dated 20150830 in us-east-1.
	key := deriveSigningKey(
		"wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY",
		"20150830",
		"us-east-1",
		"iam",
	)
	// AWS-documented expected signing key (hex).
	want := "c4afb1cc5771d871763a393e44b703571b55cc28424d1a5e86da6ed3c154a4b9"
	got := hexBytes(key)
	if got != want {
		t.Errorf("signing key drift:\nwant %s\ngot  %s", want, got)
	}
}

// TestSignRequestProducesValidAuthorizationHeader checks the assembled
// Authorization header has the right shape and includes the
// non-negotiable parts (Credential scope, SignedHeaders covering
// host + x-amz-date + content-type, a 64-char hex signature).
func TestSignRequestProducesValidAuthorizationHeader(t *testing.T) {
	req, _ := http.NewRequest("POST", "https://bedrock-runtime.us-east-1.amazonaws.com/model/foo/invoke", nil)
	req.Header.Set("Content-Type", "application/json")
	creds := signCredentials{
		AccessKey: "AKIDEXAMPLE",
		SecretKey: "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY",
		Region:    "us-east-1",
		Service:   "bedrock",
	}
	fixed := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
	signRequest(req, []byte(`{"hello":"world"}`), creds, fixed)

	auth := req.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "AWS4-HMAC-SHA256 ") {
		t.Fatalf("auth header missing algorithm: %q", auth)
	}
	if !strings.Contains(auth, "Credential=AKIDEXAMPLE/20260519/us-east-1/bedrock/aws4_request") {
		t.Errorf("auth missing credential scope: %q", auth)
	}
	// SignedHeaders must include host (signing-required) plus the
	// content-type, x-amz-content-sha256, and x-amz-date we set.
	for _, h := range []string{"content-type", "host", "x-amz-content-sha256", "x-amz-date"} {
		if !strings.Contains(auth, h) {
			t.Errorf("auth signed-headers missing %q: %q", h, auth)
		}
	}
	if req.Header.Get("X-Amz-Date") != "20260519T120000Z" {
		t.Errorf("X-Amz-Date drifted: %q", req.Header.Get("X-Amz-Date"))
	}
	// Session token only appears when supplied.
	if got := req.Header.Get("X-Amz-Security-Token"); got != "" {
		t.Errorf("unexpected session-token header for non-STS creds: %q", got)
	}
}

func TestSignRequestIncludesSessionTokenForSTSCredentials(t *testing.T) {
	req, _ := http.NewRequest("POST", "https://bedrock-runtime.us-east-1.amazonaws.com/x", nil)
	signRequest(req, []byte("body"), signCredentials{
		AccessKey:    "AKIA",
		SecretKey:    "secret",
		SessionToken: "FQoGZ...short",
		Region:       "us-east-1",
		Service:      "bedrock",
	}, time.Now())
	if req.Header.Get("X-Amz-Security-Token") != "FQoGZ...short" {
		t.Errorf("STS session token not propagated to header")
	}
	// And the signed-headers list MUST include x-amz-security-token —
	// AWS rejects the request otherwise.
	if !strings.Contains(req.Header.Get("Authorization"), "x-amz-security-token") {
		t.Errorf("session token header not included in signed-headers list")
	}
}

// TestCanonicalURIEncodesColonInModelID — the Bedrock model id
// `anthropic.claude-3-5-haiku-20241022-v1:0` contains a colon. SigV4
// requires the canonical path to percent-encode reserved characters
// even when they transit the wire unencoded.
func TestCanonicalURIEncodesColonInModelID(t *testing.T) {
	got := canonicalURI("/model/anthropic.claude-3-5-haiku-20241022-v1:0/invoke")
	want := "/model/anthropic.claude-3-5-haiku-20241022-v1%3A0/invoke"
	if got != want {
		t.Errorf("canonical URI wrong:\nwant %s\ngot  %s", want, got)
	}
}

// hexBytes returns the lowercase hex encoding of b. Local helper —
// avoids pulling encoding/hex into the test file just for this.
func hexBytes(b []byte) string {
	const h = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, v := range b {
		out[i*2] = h[v>>4]
		out[i*2+1] = h[v&0xF]
	}
	return string(out)
}
