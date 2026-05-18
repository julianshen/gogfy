// Package gemini implements the llm.Client interface against
// Google's generative-language (Gemini) API. Validates the
// llm.Client interface against a third cloud shape — Gemini's auth
// uses an API-key query parameter instead of an Authorization
// header, and its wire format wraps text in "parts" arrays rather
// than flat strings.
package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/julianshen/gogfy/internal/llm"
)

// DefaultModel is Google's cheapest current-generation model — same
// role as Haiku and gpt-4o-mini in their respective backends.
const DefaultModel = "gemini-2.0-flash"

// endpointTemplate is the model-scoped generateContent endpoint.
// Per Google's docs, the model name lives in the path, not a JSON
// field. We build the full URL in New so callers can WithEndpoint
// to point at a fixture server.
const endpointTemplate = "https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent"

// Pricing (USD per million tokens) as of 2026. The Gemini Flash
// price is per-1M-tokens (input + output charged separately) just
// like the others, even though Google's own pricing page sometimes
// quotes per-1K — keep the table on the same scale as the other
// backends so the cost-aggregation math stays uniform.
var costPerMillion = map[string]struct {
	in, out float64
}{
	"gemini-2.0-flash":      {0.10, 0.40},
	"gemini-2.0-flash-lite": {0.075, 0.30},
	"gemini-1.5-pro":        {3.50, 10.50},
	"gemini-1.5-flash":      {0.075, 0.30},
}

// ErrMissingAPIKey is returned by New when GEMINI_API_KEY isn't set.
// (Google calls it API_KEY in the docs but many users have their
// key under GEMINI_API_KEY or GOOGLE_API_KEY — we check both.)
var ErrMissingAPIKey = errors.New("gemini: GEMINI_API_KEY (or GOOGLE_API_KEY) environment variable not set")

// Client is the Gemini generative-language implementation of llm.Client.
type Client struct {
	apiKey   string
	model    string
	endpoint string // optional override; empty uses endpointTemplate
	http     *http.Client
}

// Option tweaks a Client at construction.
type Option func(*Client)

// WithModel overrides the default model.
func WithModel(m string) Option { return func(c *Client) { c.model = m } }

// WithHTTPClient lets tests inject a custom transport.
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

// WithEndpoint redirects requests to an alternate URL — for tests.
// The key is still appended as a `?key=` query parameter on top.
func WithEndpoint(u string) Option { return func(c *Client) { c.endpoint = u } }

// New reads the API key from GEMINI_API_KEY, falling back to
// GOOGLE_API_KEY. Surfaces ErrMissingAPIKey when both are empty.
func New(opts ...Option) (*Client, error) {
	key := os.Getenv("GEMINI_API_KEY")
	if key == "" {
		key = os.Getenv("GOOGLE_API_KEY")
	}
	if key == "" {
		return nil, ErrMissingAPIKey
	}
	return NewWithKey(key, opts...), nil
}

// NewWithKey constructs a Client with an explicit API key.
func NewWithKey(key string, opts ...Option) *Client {
	c := &Client{
		apiKey: key,
		model:  DefaultModel,
		http:   &http.Client{Timeout: 60 * time.Second},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Name returns the backend identifier for the cost report.
func (c *Client) Name() string { return "gemini-" + c.model }

// Generate sends a generateContent request and parses the response.
// Gemini's wire shape splits "system" and "contents" — system goes
// into a sibling top-level field, not a message with role=system.
func (c *Client) Generate(ctx context.Context, req llm.Request) (llm.Response, error) {
	body := generateRequest{
		Contents: []content{{
			Role:  "user",
			Parts: []part{{Text: req.User}},
		}},
		GenerationConfig: &generationConfig{MaxOutputTokens: defaultMaxTokens(req.MaxTokens)},
	}
	if req.System != "" {
		body.SystemInstruction = &content{Parts: []part{{Text: req.System}}}
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return llm.Response{}, fmt.Errorf("gemini: marshal request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.urlForRequest(), bytes.NewReader(payload))
	if err != nil {
		return llm.Response{}, fmt.Errorf("gemini: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return llm.Response{}, fmt.Errorf("gemini: http: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return llm.Response{}, fmt.Errorf("gemini: %s: %s", resp.Status, snippet(respBody, 500))
	}
	var parsed generateResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return llm.Response{}, fmt.Errorf("gemini: parse response: %w", err)
	}
	text := ""
	if len(parsed.Candidates) > 0 {
		for _, p := range parsed.Candidates[0].Content.Parts {
			text += p.Text
		}
	}
	return llm.Response{
		Text:             text,
		InputTokens:      parsed.UsageMetadata.PromptTokenCount,
		OutputTokens:     parsed.UsageMetadata.CandidatesTokenCount,
		EstimatedUSDCost: estimateCost(c.model, parsed.UsageMetadata.PromptTokenCount, parsed.UsageMetadata.CandidatesTokenCount),
	}, nil
}

// urlForRequest assembles the per-call URL — model in the path,
// key in the query string. Honors WithEndpoint when set so tests
// can swap in an httptest server (and httptest doesn't need to
// reproduce the model-in-path scheme).
func (c *Client) urlForRequest() string {
	base := c.endpoint
	if base == "" {
		base = fmt.Sprintf(endpointTemplate, url.PathEscape(c.model))
	}
	sep := "?"
	if u, err := url.Parse(base); err == nil && u.RawQuery != "" {
		sep = "&"
	}
	return base + sep + "key=" + url.QueryEscape(c.apiKey)
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
	fmt.Fprintf(os.Stderr, "gemini: no pricing entry for model %q — cost estimate will be $0 (update internal/llm/gemini pricing table)\n", model)
}

func snippet(b []byte, n int) string {
	if len(b) > n {
		return string(b[:n]) + "…"
	}
	return string(b)
}

// Wire-format types for Gemini's generateContent endpoint. Field
// names follow the documented JSON shape.
type generateRequest struct {
	SystemInstruction *content          `json:"system_instruction,omitempty"`
	Contents          []content         `json:"contents"`
	GenerationConfig  *generationConfig `json:"generationConfig,omitempty"`
}

type content struct {
	Role  string `json:"role,omitempty"`
	Parts []part `json:"parts"`
}

type part struct {
	Text string `json:"text"`
}

type generationConfig struct {
	MaxOutputTokens int `json:"maxOutputTokens,omitempty"`
}

type generateResponse struct {
	Candidates    []candidate   `json:"candidates"`
	UsageMetadata usageMetadata `json:"usageMetadata"`
}

type candidate struct {
	Content content `json:"content"`
}

type usageMetadata struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
}
