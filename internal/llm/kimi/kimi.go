// Package kimi implements the llm.Client interface against Moonshot
// AI's Kimi API. The wire format is OpenAI-Chat-Completions-compatible
// (Bearer auth, same request/response shape), so the implementation
// mirrors the openai backend — kept separate so the model/pricing
// table and the endpoint don't tangle with vendor-specific tweaks.
package kimi

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

// DefaultModel is Moonshot's mid-tier context window. 32K is enough
// for typical document semantic-extraction without paying for the
// 128K tier on every call.
const DefaultModel = "moonshot-v1-32k"

// DefaultEndpoint is Moonshot's chat-completions URL. The
// international `api.moonshot.ai` host serves the same API; users
// behind that endpoint can override with WithEndpoint.
const DefaultEndpoint = "https://api.moonshot.cn/v1/chat/completions"

// Pricing (USD per million tokens) as of 2026. Moonshot's pricing is
// published in CNY; values here approximate the USD equivalent and
// will go stale — the warn-once path in estimateCost flags unknown
// models so the table can be refreshed without silently zeroing cost.
var costPerMillion = map[string]struct {
	in, out float64
}{
	"moonshot-v1-8k":   {0.17, 0.17},
	"moonshot-v1-32k":  {0.34, 0.34},
	"moonshot-v1-128k": {0.85, 0.85},
}

// ErrMissingAPIKey is returned by New when neither MOONSHOT_API_KEY
// nor KIMI_API_KEY is set. Two names because the vendor uses Moonshot
// in API docs but the consumer-facing brand is Kimi — users reach for
// either.
var ErrMissingAPIKey = errors.New("kimi: MOONSHOT_API_KEY (or KIMI_API_KEY) environment variable not set")

// Client is the Kimi (Moonshot) implementation of llm.Client.
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
// users on the international Moonshot host.
func WithEndpoint(u string) Option { return func(c *Client) { c.endpoint = u } }

// New reads the API key from MOONSHOT_API_KEY, falling back to
// KIMI_API_KEY.
func New(opts ...Option) (*Client, error) {
	key := os.Getenv("MOONSHOT_API_KEY")
	if key == "" {
		key = os.Getenv("KIMI_API_KEY")
	}
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
func (c *Client) Name() string { return "kimi-" + c.model }

// EstimateCost implements llm.CostEstimator for pre-flight pricing.
func (c *Client) EstimateCost(inputTokens, outputTokens int) float64 {
	return estimateCost(c.model, inputTokens, outputTokens)
}

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
		return llm.Response{}, fmt.Errorf("kimi: marshal request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return llm.Response{}, fmt.Errorf("kimi: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return llm.Response{}, fmt.Errorf("kimi: http: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return llm.Response{}, fmt.Errorf("kimi: %s: %s", resp.Status, snippet(respBody, 500))
	}
	var parsed chatResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return llm.Response{}, fmt.Errorf("kimi: parse response: %w", err)
	}
	text := ""
	if len(parsed.Choices) > 0 {
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
	fmt.Fprintf(os.Stderr, "kimi: no pricing entry for model %q — cost estimate will be $0 (update internal/llm/kimi pricing table)\n", model)
}

func snippet(b []byte, n int) string {
	if len(b) > n {
		return string(b[:n]) + "…"
	}
	return string(b)
}

// Wire-format types — identical to the OpenAI Chat Completions shape
// Moonshot mirrors. Duplicated rather than imported so a future
// vendor-specific divergence (extra field, renamed key) doesn't
// require an awkward cross-package change.
type chatRequest struct {
	Model     string    `json:"model"`
	Messages  []message `json:"messages"`
	MaxTokens int       `json:"max_tokens,omitempty"`
}

type message struct {
	Role string `json:"role"`
	// Content is string for text-only, []contentPart for vision.
	Content any `json:"content"`
}

type contentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *imageURL `json:"image_url,omitempty"`
}

type imageURL struct {
	URL string `json:"url"`
}

// buildUserContent picks bare string when no images, multi-part array
// otherwise. Moonshot's vision-capable models (moonshot-v1-*-vision)
// accept the same data-URI image_url shape OpenAI does.
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
