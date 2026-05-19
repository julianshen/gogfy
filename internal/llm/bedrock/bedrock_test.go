package bedrock

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/julianshen/gogfy/internal/llm"
)

// TestGenerateRoundTripsViaHTTPTest exercises the full client against
// a fake Bedrock endpoint: request body shape, auth header presence,
// response parsing, cost accounting.
func TestGenerateRoundTripsViaHTTPTest(t *testing.T) {
	var (
		gotAuth        string
		gotBodyJSON    map[string]any
		gotAmzDate     string
		gotContentType string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAmzDate = r.Header.Get("X-Amz-Date")
		gotContentType = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBodyJSON)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"content": [{"type":"text","text":"hello from bedrock"}],
			"usage": {"input_tokens": 42, "output_tokens": 7}
		}`)
	}))
	defer srv.Close()

	c := NewWithCredentials("AKIA", "secret", "", WithEndpoint(srv.URL), WithModel("anthropic.claude-3-5-haiku-20241022-v1:0"))
	resp, err := c.Generate(context.Background(), llm.Request{
		System: "you are concise",
		User:   "hi",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "hello from bedrock" {
		t.Errorf("response text drift: %q", resp.Text)
	}
	if resp.InputTokens != 42 || resp.OutputTokens != 7 {
		t.Errorf("usage drift: in=%d out=%d", resp.InputTokens, resp.OutputTokens)
	}
	// 42 * 0.80 + 7 * 4.00 = 33.6 + 28 = 61.6 µUSD = 0.0000616
	if resp.EstimatedUSDCost <= 0 {
		t.Errorf("expected nonzero cost estimate, got %v", resp.EstimatedUSDCost)
	}
	if !strings.HasPrefix(gotAuth, "AWS4-HMAC-SHA256 ") {
		t.Errorf("missing or malformed Authorization header: %q", gotAuth)
	}
	if gotAmzDate == "" {
		t.Errorf("missing X-Amz-Date header")
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type drift: %q", gotContentType)
	}
	// Body shape: anthropic_version + max_tokens + system + messages.
	if gotBodyJSON["anthropic_version"] != "bedrock-2023-05-31" {
		t.Errorf("anthropic_version drift: %v", gotBodyJSON["anthropic_version"])
	}
	if gotBodyJSON["system"] != "you are concise" {
		t.Errorf("system not propagated: %v", gotBodyJSON["system"])
	}
	if _, ok := gotBodyJSON["model"]; ok {
		t.Errorf("model field should NOT appear in body (it's in the URL): %v", gotBodyJSON["model"])
	}
}

func TestGenerateSurfacesAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"message":"not authorized for this model"}`)
	}))
	defer srv.Close()

	c := NewWithCredentials("AKIA", "secret", "", WithEndpoint(srv.URL))
	_, err := c.Generate(context.Background(), llm.Request{User: "hi"})
	if err == nil {
		t.Fatal("expected error on 403")
	}
	if !strings.Contains(err.Error(), "403") || !strings.Contains(err.Error(), "not authorized") {
		t.Errorf("error should carry status + body: %v", err)
	}
}

func TestNewWithoutCredentialsFails(t *testing.T) {
	// Snapshot then clear env; restore via t.Cleanup so a parallel
	// test elsewhere doesn't see a hole.
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	_, err := New()
	if err == nil {
		t.Fatal("expected ErrMissingCredentials when env unset")
	}
	if err != ErrMissingCredentials {
		t.Errorf("expected ErrMissingCredentials sentinel, got %v", err)
	}
}

func TestEstimateCostUnknownModelReturnsZero(t *testing.T) {
	// Unknown model id -> $0 estimate + one-shot stderr warning. This
	// is the same policy as the Anthropic-direct client; pinning it
	// so a future "default to some price" change is a conscious choice.
	if got := estimateCost("bogus-model", 1000, 1000); got != 0 {
		t.Errorf("unknown model should estimate $0, got %v", got)
	}
}

func TestClientNameIncludesModel(t *testing.T) {
	c := NewWithCredentials("AKIA", "secret", "", WithModel("anthropic.claude-3-opus-20240229-v1:0"))
	if c.Name() != "bedrock-anthropic.claude-3-opus-20240229-v1:0" {
		t.Errorf("Name() drift: %q", c.Name())
	}
}

func TestWithRegionOverridesEnv(t *testing.T) {
	t.Setenv("AWS_REGION", "us-east-1")
	c := NewWithCredentials("AKIA", "secret", "", WithRegion("eu-west-2"))
	if c.creds.Region != "eu-west-2" {
		t.Errorf("WithRegion didn't override env: %q", c.creds.Region)
	}
}

// TestSignatureIsDeterministicAcrossRunsAtFixedTime keeps the same
// server (so the Host header — port-bound — doesn't drift) and pins
// the timestamp, then verifies two runs produce byte-identical
// Authorization headers. This catches nondeterminism in canonical
// headers / query encoding while ackowledging that Host legitimately
// participates in the signature.
func TestSignatureIsDeterministicAcrossRunsAtFixedTime(t *testing.T) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"content":[],"usage":{"input_tokens":0,"output_tokens":0}}`)
	}))
	defer srv.Close()

	fixed := func() time.Time { return time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC) }
	c := NewWithCredentials("AKIA", "secret", "", WithEndpoint(srv.URL), withNow(fixed))
	for i := 0; i < 2; i++ {
		if _, err := c.Generate(context.Background(), llm.Request{User: "x"}); err != nil {
			t.Fatal(err)
		}
	}
	if seen[0] != seen[1] {
		t.Errorf("signature non-deterministic at fixed timestamp:\nfirst:  %s\nsecond: %s", seen[0], seen[1])
	}
}
