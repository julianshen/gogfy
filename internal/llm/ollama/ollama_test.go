package ollama

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/julianshen/gogfy/internal/llm"
)

func TestNewNoAPIKeyRequired(t *testing.T) {
	// Cloud-API-key requirement doesn't apply to local Ollama. New
	// should succeed without any env var set.
	t.Setenv("OLLAMA_HOST", "")
	c, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if c == nil {
		t.Fatal("nil client")
	}
}

func TestNewHonorsOLLAMA_HOST(t *testing.T) {
	t.Setenv("OLLAMA_HOST", "192.168.1.5:11434")
	c, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c.endpoint, "192.168.1.5") {
		t.Errorf("OLLAMA_HOST not threaded into endpoint: %q", c.endpoint)
	}
}

func TestGenerateRoundTrip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body generateRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.System != "sys" || body.Prompt != "usr" {
			t.Errorf("request shape wrong: %+v", body)
		}
		if body.Stream {
			t.Errorf("non-streaming mode required; got stream=true")
		}
		_ = json.NewEncoder(w).Encode(generateResponse{
			Response:        `{"entities":[]}`,
			PromptEvalCount: 60,
			EvalCount:       15,
		})
	}))
	defer srv.Close()
	c, _ := New(WithEndpoint(srv.URL))
	got, err := c.Generate(context.Background(), llm.Request{System: "sys", User: "usr"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != `{"entities":[]}` {
		t.Errorf("text not relayed: %q", got.Text)
	}
	if got.InputTokens != 60 || got.OutputTokens != 15 {
		t.Errorf("token usage not relayed: %+v", got)
	}
	// Local inference has no cost — must stay zero.
	if got.EstimatedUSDCost != 0 {
		t.Errorf("local inference cost must be 0, got %v", got.EstimatedUSDCost)
	}
}

func TestGenerateSurfacesServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		_, _ = io.WriteString(w, `model not found`)
	}))
	defer srv.Close()
	c, _ := New(WithEndpoint(srv.URL))
	_, err := c.Generate(context.Background(), llm.Request{User: "x"})
	if err == nil || !strings.Contains(err.Error(), "model not found") {
		t.Errorf("server error should surface: %v", err)
	}
}

func TestGenerateConnectionRefusedMentionsOllamaServe(t *testing.T) {
	// The single most common Ollama user error: forgetting to run
	// `ollama serve`. Surface the hint in the error so users don't
	// have to grep gogfy issues to learn this.
	// Use a port that's almost certainly unused.
	c, _ := New(WithEndpoint("http://127.0.0.1:1/api/generate"))
	_, err := c.Generate(context.Background(), llm.Request{User: "x"})
	if err == nil {
		t.Fatal("expected connection error")
	}
	if !strings.Contains(err.Error(), "ollama serve") {
		t.Errorf("error should hint at `ollama serve`: %v", err)
	}
}

func TestNameIncludesModelTag(t *testing.T) {
	c, _ := New(WithModel("mistral"))
	if c.Name() != "ollama-mistral" {
		t.Errorf("Name() = %q", c.Name())
	}
}
