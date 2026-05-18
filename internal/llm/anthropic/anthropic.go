// Package anthropic implements the llm.Client interface against
// Anthropic's Messages API. Reads API key from ANTHROPIC_API_KEY env;
// returns a clear error if absent so callers can route the user to
// docs rather than seeing an opaque 401.
package anthropic

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

// DefaultModel is the cost/latency sweet spot for entity extraction —
// fast enough for batch corpus processing without paying Opus rates.
// Callers can override via WithModel.
const DefaultModel = "claude-3-5-haiku-20241022"

// DefaultEndpoint is Anthropic's Messages API. Exposed so tests can
// point a httptest.Server at a fixture without monkey-patching.
const DefaultEndpoint = "https://api.anthropic.com/v1/messages"

// Pricing (USD per million tokens) as of 2026. Update when Anthropic
// adjusts; the cost in Response is an estimate, not a billing source.
// Conservative: round in the user's favor for input, against for output.
var costPerMillion = map[string]struct {
	in, out float64
}{
	"claude-3-5-haiku-20241022":  {0.80, 4.00},
	"claude-3-5-sonnet-20241022": {3.00, 15.00},
	"claude-3-opus-20240229":     {15.00, 75.00},
}

// ErrMissingAPIKey is returned by New when ANTHROPIC_API_KEY isn't set.
// Callers should surface this with a hint to the user instead of
// propagating an opaque 401 from the API.
var ErrMissingAPIKey = errors.New("anthropic: ANTHROPIC_API_KEY environment variable not set")

// Client is the Anthropic Messages-API implementation of llm.Client.
type Client struct {
	apiKey   string
	model    string
	endpoint string
	http     *http.Client
}

// Option tweaks a Client at construction. Functional-options pattern
// keeps the New signature stable as we add knobs.
type Option func(*Client)

// WithModel overrides the default model (claude-3-5-haiku).
func WithModel(m string) Option { return func(c *Client) { c.model = m } }

// WithHTTPClient lets tests inject a custom transport without
// reaching for the global default.
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

// WithEndpoint redirects requests to an alternate URL — used by tests
// to hit a httptest.Server, and by users behind an OpenAI-compatible
// proxy.
func WithEndpoint(u string) Option { return func(c *Client) { c.endpoint = u } }

// New returns a Client reading the API key from ANTHROPIC_API_KEY.
// Tests should use NewWithKey to bypass the env lookup.
func New(opts ...Option) (*Client, error) {
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		return nil, ErrMissingAPIKey
	}
	return NewWithKey(key, opts...), nil
}

// NewWithKey constructs a Client with an explicit API key. Useful
// when the key comes from a non-env source (config file, secret
// manager) — keeps env-lookup policy out of the package.
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

// Name returns the backend identifier — used for log lines and the
// cost report.
func (c *Client) Name() string { return "anthropic-" + c.model }

// Generate posts a single-message request to /v1/messages and parses
// the response. Errors from the API (HTTP non-200) are wrapped with
// the response body so users can diagnose rate limits / auth issues.
func (c *Client) Generate(ctx context.Context, req llm.Request) (llm.Response, error) {
	body, err := json.Marshal(messagesRequest{
		Model:     c.model,
		MaxTokens: defaultMaxTokens(req.MaxTokens),
		System:    req.System,
		Messages:  []message{{Role: "user", Content: buildUserContent(req)}},
	})
	if err != nil {
		return llm.Response{}, fmt.Errorf("anthropic: marshal request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return llm.Response{}, fmt.Errorf("anthropic: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return llm.Response{}, fmt.Errorf("anthropic: http: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return llm.Response{}, fmt.Errorf("anthropic: %s: %s", resp.Status, snippet(respBody, 500))
	}
	var parsed messagesResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return llm.Response{}, fmt.Errorf("anthropic: parse response: %w", err)
	}
	text := ""
	for _, block := range parsed.Content {
		if block.Type == "text" {
			text += block.Text
		}
	}
	return llm.Response{
		Text:             text,
		InputTokens:      parsed.Usage.InputTokens,
		OutputTokens:     parsed.Usage.OutputTokens,
		EstimatedUSDCost: estimateCost(c.model, parsed.Usage.InputTokens, parsed.Usage.OutputTokens),
	}, nil
}

func defaultMaxTokens(req int) int {
	if req > 0 {
		return req
	}
	return 4096
}

// estimateCost returns a USD estimate. Unknown models cost $0 *and*
// emit a one-time stderr warning so a stale pricing table doesn't
// silently make "the run was free" appear in cost reports — that's
// the failure mode that actually matters when Anthropic adjusts
// prices or a user passes a custom model.
func estimateCost(model string, in, out int) float64 {
	p, ok := costPerMillion[model]
	if !ok {
		warnOnceUnknownModel(model)
		return 0
	}
	return (float64(in)*p.in + float64(out)*p.out) / 1_000_000
}

// warnOnceUnknownModel emits a single stderr line per unknown model
// so a multi-file run doesn't spam the user with the same warning N
// times. Synchronized so the parallel-extraction path is safe.
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
	fmt.Fprintf(os.Stderr, "anthropic: no pricing entry for model %q — cost estimate will be $0 (update internal/llm/anthropic pricing table)\n", model)
}

func snippet(b []byte, n int) string {
	if len(b) > n {
		return string(b[:n]) + "…"
	}
	return string(b)
}

// Wire-format types for the Anthropic Messages API. Lowercase JSON
// tags match the documented shape.
type messagesRequest struct {
	Model     string    `json:"model"`
	MaxTokens int       `json:"max_tokens"`
	System    string    `json:"system,omitempty"`
	Messages  []message `json:"messages"`
}

type message struct {
	Role string `json:"role"`
	// Content can be a string (text-only) or a []contentInput
	// (vision). Anthropic's API accepts both forms; we use the
	// string form when there are no images to keep the wire shape
	// minimal for text-only requests.
	Content any `json:"content"`
}

// contentInput is the array element for multi-modal messages. Type
// switches on "text" | "image" with the relevant field populated.
type contentInput struct {
	Type   string       `json:"type"`
	Text   string       `json:"text,omitempty"`
	Source *imageSource `json:"source,omitempty"`
}

type imageSource struct {
	Type      string `json:"type"`       // "base64"
	MediaType string `json:"media_type"` // "image/png", etc.
	Data      string `json:"data"`       // base64-encoded image bytes
}

// buildUserContent picks the simplest wire shape: bare string when
// there are no images, []contentInput when there are. Mirrors
// Anthropic's documented dual-shape input.
func buildUserContent(req llm.Request) any {
	if len(req.Images) == 0 {
		return req.User
	}
	parts := make([]contentInput, 0, len(req.Images)+1)
	for _, img := range req.Images {
		parts = append(parts, contentInput{
			Type: "image",
			Source: &imageSource{
				Type:      "base64",
				MediaType: img.MimeType,
				Data:      base64.StdEncoding.EncodeToString(img.Data),
			},
		})
	}
	if req.User != "" {
		parts = append(parts, contentInput{Type: "text", Text: req.User})
	}
	return parts
}

type messagesResponse struct {
	Content []contentBlock `json:"content"`
	Usage   usage          `json:"usage"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}
