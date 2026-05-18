package gemini

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/julianshen/gogfy/internal/llm"
)

func TestNewRequiresAPIKey(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "")
	_, err := New()
	if !errors.Is(err, ErrMissingAPIKey) {
		t.Fatalf("expected ErrMissingAPIKey, got %v", err)
	}
}

func TestNewFallsBackToGOOGLE_API_KEY(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "google-key-value")
	c, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if c.apiKey != "google-key-value" {
		t.Errorf("apiKey not picked up from GOOGLE_API_KEY: %q", c.apiKey)
	}
}

func TestGenerateRoundTripWithSystemInstruction(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Key arrives as a query param, not a header — Gemini's
		// quirk. Verify it's threaded through correctly.
		if r.URL.Query().Get("key") != "test-key" {
			t.Errorf("missing/wrong key query param: %q", r.URL.Query().Get("key"))
		}
		var body generateRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.SystemInstruction == nil || body.SystemInstruction.Parts[0].Text != "sys" {
			t.Errorf("system instruction wrong: %+v", body.SystemInstruction)
		}
		if len(body.Contents) != 1 || body.Contents[0].Parts[0].Text != "usr" {
			t.Errorf("user content wrong: %+v", body.Contents)
		}
		_ = json.NewEncoder(w).Encode(generateResponse{
			Candidates: []candidate{{Content: content{Parts: []part{{Text: `{"entities":[]}`}}}}},
			UsageMetadata: usageMetadata{
				PromptTokenCount:     70,
				CandidatesTokenCount: 18,
				TotalTokenCount:      88,
			},
		})
	}))
	defer srv.Close()

	c := NewWithKey("test-key", WithEndpoint(srv.URL))
	got, err := c.Generate(context.Background(), llm.Request{System: "sys", User: "usr"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != `{"entities":[]}` {
		t.Errorf("text not relayed: %q", got.Text)
	}
	if got.InputTokens != 70 || got.OutputTokens != 18 {
		t.Errorf("token usage not relayed: %+v", got)
	}
	wantCost := (70*0.10 + 18*0.40) / 1_000_000
	if got.EstimatedUSDCost < wantCost-1e-12 || got.EstimatedUSDCost > wantCost+1e-12 {
		t.Errorf("cost wrong: got %v want %v", got.EstimatedUSDCost, wantCost)
	}
}

func TestGenerateOmitsSystemInstructionWhenEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body generateRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.SystemInstruction != nil {
			t.Errorf("expected no system_instruction when Request.System empty: %+v", body.SystemInstruction)
		}
		_ = json.NewEncoder(w).Encode(generateResponse{
			Candidates: []candidate{{Content: content{Parts: []part{{Text: "ok"}}}}},
		})
	}))
	defer srv.Close()
	c := NewWithKey("k", WithEndpoint(srv.URL))
	_, _ = c.Generate(context.Background(), llm.Request{User: "hi"})
}

func TestGenerateSurfacesAPIErrorBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		_, _ = io.WriteString(w, `{"error":{"code":400,"message":"API key not valid"}}`)
	}))
	defer srv.Close()
	c := NewWithKey("bad-key", WithEndpoint(srv.URL))
	_, err := c.Generate(context.Background(), llm.Request{User: "x"})
	if err == nil || !strings.Contains(err.Error(), "API key not valid") {
		t.Errorf("API error body should surface: %v", err)
	}
}

func TestGenerateConcatenatesMultiplePartTexts(t *testing.T) {
	// Gemini can return multiple text parts per candidate. Caller
	// wants them joined.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(generateResponse{
			Candidates: []candidate{{
				Content: content{Parts: []part{
					{Text: "Part A. "},
					{Text: "Part B."},
				}},
			}},
		})
	}))
	defer srv.Close()
	c := NewWithKey("k", WithEndpoint(srv.URL))
	got, _ := c.Generate(context.Background(), llm.Request{User: "x"})
	if got.Text != "Part A. Part B." {
		t.Fatalf("parts not concatenated: %q", got.Text)
	}
}

func TestNameIncludesModelTag(t *testing.T) {
	c := NewWithKey("k", WithModel("gemini-2.5-pro"))
	if c.Name() != "gemini-gemini-2.5-pro" {
		t.Errorf("Name() = %q", c.Name())
	}
}

func TestURLForRequestPreservesExistingQuery(t *testing.T) {
	// If the override endpoint already has a query string, the key
	// must be appended with `&`, not `?`. Avoids a malformed URL
	// when a user points at an OpenAI-compatible proxy that uses
	// query params for routing.
	c := NewWithKey("k", WithEndpoint("https://example.com/path?route=gemini"))
	got := c.urlForRequest()
	if !strings.Contains(got, "route=gemini") || !strings.Contains(got, "key=k") {
		t.Errorf("URL composition wrong: %q", got)
	}
	if strings.Contains(got, "?key=") && strings.Contains(got, "?route=") {
		t.Errorf("double `?` in URL: %q", got)
	}
}
