// Package openai implements the llm.Client interface against the
// OpenAI Chat Completions API. Same shape as the Anthropic backend
// (env-based key, pricing table, httptest-friendly endpoint override)
// — kept separate so cost models and wire formats don't tangle.
package openai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/julianshen/gogfy/internal/llm"
)

// DefaultModel is the cost/latency sweet spot for entity extraction.
// gpt-4o-mini is competitive with Haiku on accuracy and the cheapest
// chat-completions-quality option as of 2026. Override via WithModel.
const DefaultModel = "gpt-4o-mini"

// DefaultEndpoint is OpenAI's chat completions URL.
const DefaultEndpoint = "https://api.openai.com/v1/chat/completions"

// Pricing (USD per million tokens) as of 2026. The anthropic
// backend's table doesn't translate — OpenAI prices input + output
// at different ratios — so each backend keeps its own.
var costPerMillion = map[string]struct {
	in, out float64
}{
	"gpt-4o-mini":   {0.15, 0.60},
	"gpt-4o":        {2.50, 10.00},
	"gpt-4-turbo":   {10.00, 30.00},
	"gpt-3.5-turbo": {0.50, 1.50},
}

// ErrMissingAPIKey is returned by New when OPENAI_API_KEY isn't set.
// Callers should surface this with a hint rather than propagating an
// opaque 401.
var ErrMissingAPIKey = errors.New("openai: OPENAI_API_KEY environment variable not set")

// Client is the OpenAI Chat Completions implementation of llm.Client.
type Client struct {
	apiKey   string
	model    string
	endpoint string
	http     *http.Client
}

// Option tweaks a Client at construction.
type Option func(*Client)

// WithModel overrides the default model.
func WithModel(m string) Option { return func(c *Client) { c.model = m } }

// WithHTTPClient lets tests inject a custom transport.
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

// WithEndpoint redirects requests to an alternate URL — for tests or
// users behind an OpenAI-compatible proxy.
func WithEndpoint(u string) Option { return func(c *Client) { c.endpoint = u } }

// New reads the API key from OPENAI_API_KEY.
func New(opts ...Option) (*Client, error) {
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		return nil, ErrMissingAPIKey
	}
	return NewWithKey(key, opts...), nil
}

// NewWithKey constructs a Client with an explicit API key.
func NewWithKey(key string, opts ...Option) *Client {
	c := &Client{
		apiKey:   key,
		model:    DefaultModel,
		endpoint: DefaultEndpoint,
		http:     &http.Client{Timeout: 60 * time.Second},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Name returns the backend identifier for the cost report.
func (c *Client) Name() string { return "openai-" + c.model }

// Generate posts a chat-completions request and extracts the first
// choice's message content.
func (c *Client) Generate(ctx context.Context, req llm.Request) (llm.Response, error) {
	msgs := []message{}
	if req.System != "" {
		msgs = append(msgs, message{Role: "system", Content: req.System})
	}
	msgs = append(msgs, message{Role: "user", Content: buildUserContent(req)})

	body, err := json.Marshal(chatRequest{
		Model:     c.model,
		Messages:  msgs,
		MaxTokens: defaultMaxTokens(req.MaxTokens),
	})
	if err != nil {
		return llm.Response{}, fmt.Errorf("openai: marshal request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return llm.Response{}, fmt.Errorf("openai: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return llm.Response{}, fmt.Errorf("openai: http: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return llm.Response{}, fmt.Errorf("openai: %s: %s", resp.Status, snippet(respBody, 500))
	}
	var parsed chatResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return llm.Response{}, fmt.Errorf("openai: parse response: %w", err)
	}
	text := ""
	if len(parsed.Choices) > 0 {
		// Responses are always text (even when input had images),
		// but Content is typed as `any` to share the message struct
		// with requests where it can be an array. Coerce to string.
		if s, ok := parsed.Choices[0].Message.Content.(string); ok {
			text = s
		}
	}
	return llm.Response{
		Text:             text,
		InputTokens:      parsed.Usage.PromptTokens,
		OutputTokens:     parsed.Usage.CompletionTokens,
		EstimatedUSDCost: estimateCost(c.model, parsed.Usage.PromptTokens, parsed.Usage.CompletionTokens),
	}, nil
}

func defaultMaxTokens(req int) int {
	if req > 0 {
		return req
	}
	return 4096
}

func estimateCost(model string, in, out int) float64 {
	p, ok := costPerMillion[model]
	if !ok {
		warnOnceUnknownModel(model)
		return 0
	}
	return (float64(in)*p.in + float64(out)*p.out) / 1_000_000
}

var (
	warnedModelsMu sync.Mutex
	warnedModels   = map[string]struct{}{}
)

func warnOnceUnknownModel(model string) {
	warnedModelsMu.Lock()
	defer warnedModelsMu.Unlock()
	if _, dup := warnedModels[model]; dup {
		return
	}
	warnedModels[model] = struct{}{}
	fmt.Fprintf(os.Stderr, "openai: no pricing entry for model %q — cost estimate will be $0 (update internal/llm/openai pricing table)\n", model)
}

func snippet(b []byte, n int) string {
	if len(b) > n {
		return string(b[:n]) + "…"
	}
	return string(b)
}

// Wire-format types for the Chat Completions API. Field names follow
// OpenAI's documented JSON shape.
type chatRequest struct {
	Model     string    `json:"model"`
	Messages  []message `json:"messages"`
	MaxTokens int       `json:"max_tokens,omitempty"`
}

type message struct {
	Role string `json:"role"`
	// Content can be a string (text-only) or a []contentPart (vision).
	// OpenAI's Chat Completions API accepts both shapes — we use the
	// minimal string form when no images are attached.
	Content any `json:"content"`
}

// contentPart is the array element for multi-modal messages.
type contentPart struct {
	Type     string    `json:"type"`               // "text" | "image_url"
	Text     string    `json:"text,omitempty"`
	ImageURL *imageURL `json:"image_url,omitempty"`
}

type imageURL struct {
	URL string `json:"url"` // data URI: data:image/png;base64,...
}

// buildUserContent picks bare string when no images, multi-part
// array otherwise. Image bytes encode as data URIs to keep the
// request self-contained (no follow-up signed-URL fetch).
func buildUserContent(req llm.Request) any {
	if len(req.Images) == 0 {
		return req.User
	}
	parts := []contentPart{}
	if req.User != "" {
		parts = append(parts, contentPart{Type: "text", Text: req.User})
	}
	for _, img := range req.Images {
		dataURI := "data:" + img.MimeType + ";base64," + base64.StdEncoding.EncodeToString(img.Data)
		parts = append(parts, contentPart{Type: "image_url", ImageURL: &imageURL{URL: dataURI}})
	}
	return parts
}

type chatResponse struct {
	Choices []choice `json:"choices"`
	Usage   usage    `json:"usage"`
}

type choice struct {
	Message message `json:"message"`
}

type usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}
