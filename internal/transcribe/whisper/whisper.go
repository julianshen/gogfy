// Package whisper implements the transcribe.Client interface against
// OpenAI's /v1/audio/transcriptions endpoint. The wire format is a
// multipart/form-data POST — unlike the chat completions JSON shape
// the LLM backends use — so the request builder lives here rather
// than sharing code with internal/llm/openai.
//
// The same endpoint shape is implemented by several self-hosted
// projects (whisper.cpp's server mode, Groq's whisper-large-v3
// service, etc.) so WithEndpoint lets users point at a compatible
// host without changing the auth shape.
package whisper

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"time"

	"github.com/julianshen/gogfy/internal/transcribe"
)

// DefaultModel is OpenAI's only generally available transcription
// model as of 2026. Override via WithModel if the endpoint is a
// compatible service hosting a different model name (e.g. Groq's
// `whisper-large-v3`).
const DefaultModel = "whisper-1"

// DefaultEndpoint is OpenAI's transcription URL.
const DefaultEndpoint = "https://api.openai.com/v1/audio/transcriptions"

// usdPerMinute is OpenAI's published Whisper price ($0.006/min as of
// 2026). Whisper is priced by audio duration, not tokens, so the cost
// is computed from the response's `duration` field.
const usdPerMinute = 0.006

// ErrMissingAPIKey is returned by New when OPENAI_API_KEY isn't set.
// The env var name matches the LLM openai backend on purpose —
// users typically have one OpenAI key, not two.
var ErrMissingAPIKey = errors.New("whisper: OPENAI_API_KEY environment variable not set")

// Client is the OpenAI Whisper implementation of transcribe.Client.
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

// WithEndpoint redirects to an alternate URL — for tests or
// users on a Whisper-API-compatible self-hosted endpoint.
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
		// Whisper requests can be much slower than chat completions
		// (audio uploads + server-side decoding); 5 minutes covers
		// realistic talk-length media without hanging indefinitely.
		http: &http.Client{Timeout: 5 * time.Minute},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Name returns the backend identifier for the cost report.
func (c *Client) Name() string { return "whisper-" + c.model }

// Transcribe POSTs the audio as multipart/form-data and parses the
// verbose-JSON response so we can capture the duration field for cost
// reporting.
func (c *Client) Transcribe(ctx context.Context, req transcribe.Request) (transcribe.Response, error) {
	if len(req.Audio) == 0 {
		return transcribe.Response{}, fmt.Errorf("whisper: empty audio")
	}
	body, contentType, err := buildMultipart(req, c.model)
	if err != nil {
		return transcribe.Response{}, fmt.Errorf("whisper: build request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, body)
	if err != nil {
		return transcribe.Response{}, fmt.Errorf("whisper: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", contentType)
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return transcribe.Response{}, fmt.Errorf("whisper: http: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return transcribe.Response{}, fmt.Errorf("whisper: %s: %s", resp.Status, snippet(respBody, 500))
	}
	var parsed transcriptionResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return transcribe.Response{}, fmt.Errorf("whisper: parse response: %w", err)
	}
	return transcribe.Response{
		Text:             parsed.Text,
		DurationSeconds:  parsed.Duration,
		EstimatedUSDCost: parsed.Duration / 60.0 * usdPerMinute,
	}, nil
}

// buildMultipart writes the multipart form OpenAI's API expects.
// Extracted so tests can introspect the wire format without standing
// up a real HTTP server.
func buildMultipart(req transcribe.Request, model string) (io.Reader, string, error) {
	buf := &bytes.Buffer{}
	w := multipart.NewWriter(buf)
	if err := w.WriteField("model", model); err != nil {
		return nil, "", err
	}
	// verbose_json includes the `duration` field needed for cost
	// estimation; plain `json` doesn't.
	if err := w.WriteField("response_format", "verbose_json"); err != nil {
		return nil, "", err
	}
	if req.Prompt != "" {
		if err := w.WriteField("prompt", req.Prompt); err != nil {
			return nil, "", err
		}
	}
	if req.Language != "" {
		if err := w.WriteField("language", req.Language); err != nil {
			return nil, "", err
		}
	}
	filename := req.Filename
	if filename == "" {
		// OpenAI rejects the upload if the filename has no extension —
		// fall back to a safe default that matches a supported MIME.
		filename = "audio.mp3"
	}
	part, err := w.CreateFormFile("file", filename)
	if err != nil {
		return nil, "", err
	}
	if _, err := part.Write(req.Audio); err != nil {
		return nil, "", err
	}
	if err := w.Close(); err != nil {
		return nil, "", err
	}
	return buf, w.FormDataContentType(), nil
}

func snippet(b []byte, n int) string {
	if len(b) > n {
		return string(b[:n]) + "…"
	}
	return string(b)
}

// transcriptionResponse is the verbose-json wire shape. Only the
// fields we surface are decoded; the rest (segments, words, language)
// are intentionally ignored for now.
type transcriptionResponse struct {
	Text     string  `json:"text"`
	Duration float64 `json:"duration"`
}
