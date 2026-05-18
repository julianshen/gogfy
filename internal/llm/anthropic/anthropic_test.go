package anthropic

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
	t.Setenv("ANTHROPIC_API_KEY", "")
	_, err := New()
	if !errors.Is(err, ErrMissingAPIKey) {
		t.Fatalf("expected ErrMissingAPIKey, got %v", err)
	}
}

func TestNewReadsAPIKeyFromEnv(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-test-key")
	c, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if c.apiKey != "sk-test-key" {
		t.Errorf("apiKey not set from env: got %q", c.apiKey)
	}
}

func TestGenerateRoundTrip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "sk-test" {
			t.Errorf("missing api key header: %v", r.Header)
		}
		if r.Header.Get("anthropic-version") == "" {
			t.Error("missing anthropic-version header")
		}
		var body messagesRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.System != "system prompt" || len(body.Messages) != 1 {
			t.Errorf("request shape wrong: %+v", body)
		}
		resp := messagesResponse{
			Content: []contentBlock{{Type: "text", Text: `{"entities":[]}`}},
			Usage:   usage{InputTokens: 50, OutputTokens: 12},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := NewWithKey("sk-test", WithEndpoint(srv.URL))
	got, err := c.Generate(context.Background(), llm.Request{System: "system prompt", User: "user prompt"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != `{"entities":[]}` {
		t.Errorf("text not relayed: %q", got.Text)
	}
	if got.InputTokens != 50 || got.OutputTokens != 12 {
		t.Errorf("usage not relayed: %+v", got)
	}
	// Cost: 50*0.80 + 12*4.00 = 40+48 = 88 per million → 0.000088
	wantCost := (50*0.80 + 12*4.00) / 1_000_000
	if got.EstimatedUSDCost < wantCost-1e-12 || got.EstimatedUSDCost > wantCost+1e-12 {
		t.Errorf("cost estimate wrong: got %v want %v", got.EstimatedUSDCost, wantCost)
	}
}

func TestGenerateSurfacesAPIErrorBody(t *testing.T) {
	// On non-200 responses the body is included in the error so users
	// can see "overloaded" vs "invalid api key" without enabling debug.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		_, _ = io.WriteString(w, `{"error":{"type":"rate_limit","message":"slow down"}}`)
	}))
	defer srv.Close()

	c := NewWithKey("sk-test", WithEndpoint(srv.URL))
	_, err := c.Generate(context.Background(), llm.Request{User: "x"})
	if err == nil {
		t.Fatal("expected error for non-200")
	}
	if !strings.Contains(err.Error(), "rate_limit") {
		t.Errorf("body should surface in error: %v", err)
	}
}

func TestGenerateConcatenatesContentBlocks(t *testing.T) {
	// Claude can return multiple text blocks (especially when using
	// tool-use). Caller wants them joined into one string.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(messagesResponse{
			Content: []contentBlock{
				{Type: "text", Text: "Part 1. "},
				{Type: "text", Text: "Part 2."},
			},
		})
	}))
	defer srv.Close()
	c := NewWithKey("sk-test", WithEndpoint(srv.URL))
	got, _ := c.Generate(context.Background(), llm.Request{User: "x"})
	if got.Text != "Part 1. Part 2." {
		t.Fatalf("content blocks not concatenated: %q", got.Text)
	}
}

func TestNameIncludesModelTag(t *testing.T) {
	c := NewWithKey("sk", WithModel("custom-model-x"))
	if c.Name() != "anthropic-custom-model-x" {
		t.Errorf("Name() = %q", c.Name())
	}
}

func TestGenerateAttachesImageInUserContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body messagesRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		// With images attached, Content must be an array (not a bare
		// string). Decode it loosely to confirm the shape.
		raw, _ := json.Marshal(body.Messages[0].Content)
		var parts []map[string]any
		if err := json.Unmarshal(raw, &parts); err != nil {
			t.Fatalf("Content should be an array when images present: %s", raw)
		}
		hasImage, hasText := false, false
		for _, p := range parts {
			switch p["type"] {
			case "image":
				hasImage = true
				if src, ok := p["source"].(map[string]any); ok {
					if src["media_type"] != "image/png" {
						t.Errorf("media_type wrong: %v", src["media_type"])
					}
				}
			case "text":
				hasText = true
			}
		}
		if !hasImage || !hasText {
			t.Errorf("expected both image + text parts: %v", parts)
		}
		_ = json.NewEncoder(w).Encode(messagesResponse{
			Content: []contentBlock{{Type: "text", Text: "ok"}},
		})
	}))
	defer srv.Close()
	c := NewWithKey("k", WithEndpoint(srv.URL))
	_, err := c.Generate(context.Background(), llm.Request{
		User:   "what is in this image",
		Images: []llm.ImageInput{{Data: []byte("fake png bytes"), MimeType: "image/png"}},
	})
	if err != nil {
		t.Fatal(err)
	}
}
