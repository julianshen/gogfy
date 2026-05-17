// Package ollama implements the llm.Client interface against a
// local Ollama server (https://ollama.com). Unlike the cloud
// backends, no API key is required — Ollama runs on localhost and
// the connection is unauthenticated.
//
// Validates that the pluggable Client interface accommodates the
// shape of providers that:
//   - have no API key (env-var-required is cloud-only)
//   - run on a non-default host (default endpoint is loopback, not
//     a public DNS name)
//   - have no per-token pricing (it's running on the user's hardware)
package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/julianshen/gogfy/internal/llm"
)

// DefaultModel is a 7B-class model that fits comfortably on consumer
// hardware. Larger models (llama3:70b, qwen2:72b) work too — Ollama
// picks based on what the user has pulled — but the default needs to
// run somewhere.
const DefaultModel = "llama3.2"

// DefaultEndpoint is Ollama's local generate API. Override with
// WithEndpoint when running Ollama on a non-default port or host.
const DefaultEndpoint = "http://localhost:11434/api/generate"

// Client is the Ollama-server implementation of llm.Client. No API
// key; the model is picked from what the user has `ollama pull`ed.
type Client struct {
	model    string
	endpoint string
	http     *http.Client
}

// Option tweaks a Client at construction.
type Option func(*Client)

// WithModel overrides the default model name. Must match an
// `ollama pull`-ed tag.
func WithModel(m string) Option { return func(c *Client) { c.model = m } }

// WithEndpoint redirects requests — useful when Ollama listens on a
// non-default port or for tests with httptest.
func WithEndpoint(u string) Option { return func(c *Client) { c.endpoint = u } }

// WithHTTPClient lets tests inject a custom transport.
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

// New constructs a Client. Honors the OLLAMA_HOST environment
// variable if set (mirrors Ollama's own CLI convention), defaulting
// to localhost:11434 otherwise.
func New(opts ...Option) (*Client, error) {
	endpoint := DefaultEndpoint
	if host := os.Getenv("OLLAMA_HOST"); host != "" {
		// OLLAMA_HOST is conventionally just host:port (no scheme,
		// no path). Build the full URL ourselves so a user-set
		// `OLLAMA_HOST=192.168.1.5:11434` works without surprises.
		endpoint = "http://" + host + "/api/generate"
	}
	c := &Client{
		model:    DefaultModel,
		endpoint: endpoint,
		http:     &http.Client{Timeout: 5 * time.Minute}, // local models can be slow
	}
	for _, o := range opts {
		o(c)
	}
	return c, nil
}

// Name returns the backend identifier. Cost is always $0 here.
func (c *Client) Name() string { return "ollama-" + c.model }

// Generate posts a single-shot generation request. Ollama's API
// supports streaming, but the simpler non-streaming path is enough
// for entity extraction (which produces small JSON outputs anyway).
func (c *Client) Generate(ctx context.Context, req llm.Request) (llm.Response, error) {
	body, err := json.Marshal(generateRequest{
		Model:  c.model,
		Prompt: req.User,
		System: req.System,
		Stream: false,
		Options: map[string]any{
			"num_predict": defaultMaxTokens(req.MaxTokens),
		},
	})
	if err != nil {
		return llm.Response{}, fmt.Errorf("ollama: marshal request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return llm.Response{}, fmt.Errorf("ollama: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return llm.Response{}, fmt.Errorf("ollama: http (is `ollama serve` running?): %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return llm.Response{}, fmt.Errorf("ollama: %s: %s", resp.Status, snippet(respBody, 500))
	}
	var parsed generateResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return llm.Response{}, fmt.Errorf("ollama: parse response: %w", err)
	}
	return llm.Response{
		Text:         parsed.Response,
		InputTokens:  parsed.PromptEvalCount,
		OutputTokens: parsed.EvalCount,
		// No cost — local inference. EstimatedUSDCost stays 0.
	}, nil
}

func defaultMaxTokens(req int) int {
	if req > 0 {
		return req
	}
	return 4096
}

func snippet(b []byte, n int) string {
	if len(b) > n {
		return string(b[:n]) + "…"
	}
	return string(b)
}

// Wire-format types for Ollama's /api/generate endpoint.
type generateRequest struct {
	Model   string         `json:"model"`
	Prompt  string         `json:"prompt"`
	System  string         `json:"system,omitempty"`
	Stream  bool           `json:"stream"`
	Options map[string]any `json:"options,omitempty"`
}

type generateResponse struct {
	Response        string `json:"response"`
	PromptEvalCount int    `json:"prompt_eval_count"`
	EvalCount       int    `json:"eval_count"`
}
