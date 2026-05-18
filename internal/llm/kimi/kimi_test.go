package kimi

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
	t.Setenv("MOONSHOT_API_KEY", "")
	t.Setenv("KIMI_API_KEY", "")
	_, err := New()
	if !errors.Is(err, ErrMissingAPIKey) {
		t.Fatalf("expected ErrMissingAPIKey, got %v", err)
	}
}

func TestNewAcceptsKimiAPIKeyFallback(t *testing.T) {
	t.Setenv("MOONSHOT_API_KEY", "")
	t.Setenv("KIMI_API_KEY", "k-fallback")
	c, err := New()
	if err != nil {
		t.Fatalf("fallback env should be accepted: %v", err)
	}
	if c.apiKey != "k-fallback" {
		t.Errorf("apiKey not picked from KIMI_API_KEY: %q", c.apiKey)
	}
}

func TestGenerateRoundTrip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			t.Errorf("missing Bearer auth: %v", r.Header)
		}
		var body chatRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if len(body.Messages) != 2 || body.Messages[0].Role != "system" || body.Messages[1].Role != "user" {
			t.Errorf("message shape wrong: %+v", body.Messages)
		}
		if body.Model != DefaultModel {
			t.Errorf("default model not used: %q", body.Model)
		}
		_ = json.NewEncoder(w).Encode(chatResponse{
			Choices: []choice{{Message: message{Role: "assistant", Content: `{"entities":[]}`}}},
			Usage:   usage{PromptTokens: 100, CompletionTokens: 50},
		})
	}))
	defer srv.Close()

	c := NewWithKey("sk-test", WithEndpoint(srv.URL))
	got, err := c.Generate(context.Background(), llm.Request{System: "sys", User: "usr"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != `{"entities":[]}` {
		t.Errorf("text not relayed: %q", got.Text)
	}
	if got.InputTokens != 100 || got.OutputTokens != 50 {
		t.Errorf("usage not relayed: %+v", got)
	}
	wantCost := (100*0.34 + 50*0.34) / 1_000_000
	if got.EstimatedUSDCost < wantCost-1e-12 || got.EstimatedUSDCost > wantCost+1e-12 {
		t.Errorf("cost wrong: got %v want %v", got.EstimatedUSDCost, wantCost)
	}
}

func TestGenerateSurfacesAPIErrorBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		_, _ = io.WriteString(w, `{"error":{"message":"rate limited","type":"rate_limit_reached_error"}}`)
	}))
	defer srv.Close()
	c := NewWithKey("sk-test", WithEndpoint(srv.URL))
	_, err := c.Generate(context.Background(), llm.Request{User: "x"})
	if err == nil || !strings.Contains(err.Error(), "rate") {
		t.Errorf("rate-limit body should surface: %v", err)
	}
}

func TestGenerateNoSystemPromptOmitsSystemMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body chatRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		if len(body.Messages) != 1 || body.Messages[0].Role != "user" {
			t.Errorf("expected single user message, got %+v", body.Messages)
		}
		_ = json.NewEncoder(w).Encode(chatResponse{Choices: []choice{{Message: message{Content: ""}}}})
	}))
	defer srv.Close()
	c := NewWithKey("sk-test", WithEndpoint(srv.URL))
	_, _ = c.Generate(context.Background(), llm.Request{User: "hi"})
}

func TestNameIncludesModelTag(t *testing.T) {
	c := NewWithKey("sk", WithModel("moonshot-v1-128k"))
	if c.Name() != "kimi-moonshot-v1-128k" {
		t.Errorf("Name() = %q", c.Name())
	}
}

func TestGenerateAttachesImageAsDataURI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body chatRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(body.Messages[len(body.Messages)-1].Content)
		var parts []map[string]any
		if err := json.Unmarshal(raw, &parts); err != nil {
			t.Fatalf("Content should be array with images: %s", raw)
		}
		hit := false
		for _, p := range parts {
			if p["type"] == "image_url" {
				if iu, ok := p["image_url"].(map[string]any); ok {
					if url, ok := iu["url"].(string); ok && strings.HasPrefix(url, "data:image/png;base64,") {
						hit = true
					}
				}
			}
		}
		if !hit {
			t.Errorf("expected image_url part with data URI: %v", parts)
		}
		_ = json.NewEncoder(w).Encode(chatResponse{
			Choices: []choice{{Message: message{Content: "ok"}}},
		})
	}))
	defer srv.Close()
	c := NewWithKey("k", WithEndpoint(srv.URL))
	_, err := c.Generate(context.Background(), llm.Request{
		User:   "describe",
		Images: []llm.ImageInput{{Data: []byte("png bytes"), MimeType: "image/png"}},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestEstimateCostUnknownModelReturnsZero(t *testing.T) {
	if got := estimateCost("moonshot-v999", 1000, 1000); got != 0 {
		t.Errorf("unknown model should cost 0, got %v", got)
	}
}
