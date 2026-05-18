package openai

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
	t.Setenv("OPENAI_API_KEY", "")
	_, err := New()
	if !errors.Is(err, ErrMissingAPIKey) {
		t.Fatalf("expected ErrMissingAPIKey, got %v", err)
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
		// System + user → 2 messages in order.
		if len(body.Messages) != 2 || body.Messages[0].Role != "system" || body.Messages[1].Role != "user" {
			t.Errorf("message shape wrong: %+v", body.Messages)
		}
		_ = json.NewEncoder(w).Encode(chatResponse{
			Choices: []choice{{Message: message{Role: "assistant", Content: `{"entities":[]}`}}},
			Usage:   usage{PromptTokens: 80, CompletionTokens: 20},
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
	if got.InputTokens != 80 || got.OutputTokens != 20 {
		t.Errorf("usage not relayed: %+v", got)
	}
	wantCost := (80*0.15 + 20*0.60) / 1_000_000
	if got.EstimatedUSDCost < wantCost-1e-12 || got.EstimatedUSDCost > wantCost+1e-12 {
		t.Errorf("cost wrong: got %v want %v", got.EstimatedUSDCost, wantCost)
	}
}

func TestGenerateSurfacesAPIErrorBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		_, _ = io.WriteString(w, `{"error":{"message":"rate limited","type":"rate_limit"}}`)
	}))
	defer srv.Close()
	c := NewWithKey("sk-test", WithEndpoint(srv.URL))
	_, err := c.Generate(context.Background(), llm.Request{User: "x"})
	if err == nil || !strings.Contains(err.Error(), "rate") {
		t.Errorf("rate-limit body should surface: %v", err)
	}
}

func TestGenerateNoSystemPromptOmitsSystemMessage(t *testing.T) {
	// When only User is set, the request should have a single message
	// (user) — not an empty system message.
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
	c := NewWithKey("sk", WithModel("gpt-X"))
	if c.Name() != "openai-gpt-X" {
		t.Errorf("Name() = %q", c.Name())
	}
}

func TestGenerateAttachesImageAsDataURI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body chatRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		// User message Content must be an array when images attached.
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
