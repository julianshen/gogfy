// Package llm defines a provider-agnostic interface for LLM calls so
// gogfy can swap backends (Anthropic Claude, OpenAI, Gemini, Ollama,
// local models) without touching the rest of the pipeline. The
// concrete implementations live in sub-packages.
package llm

import "context"

// Client is the LLM-agnostic call surface gogfy needs. Kept minimal —
// a single "generate JSON from system+user prompt" verb is enough for
// the semantic-extraction use case. Backends that want streaming or
// tool-use can expose those as additional methods; gogfy's pipeline
// only consumes Generate.
type Client interface {
	// Generate sends system + user prompts and returns the model's
	// completion text. The model is expected to respond with the JSON
	// shape the caller asked for; parsing happens in the consumer
	// (internal/semantic, etc.).
	//
	// Token counts are returned even on success so callers can build
	// a running cost estimate.
	Generate(ctx context.Context, req Request) (Response, error)

	// Name returns a short identifier for the backend (e.g.
	// "anthropic-claude-3-5-haiku") for logging and cost reports.
	Name() string
}

// Request is the call shape the Client interface accepts.
type Request struct {
	System    string
	User      string
	MaxTokens int
	// Images, when non-empty, attaches each image as a vision input
	// alongside the User text. Each backend converts to its own wire
	// format (Anthropic source-object, OpenAI image_url data URI,
	// Gemini inlineData, Ollama base64 images array). Backends whose
	// active model doesn't support vision will either silently drop
	// images or return a clear API error — callers should default-on
	// images only for files that classify as image.
	Images []ImageInput
}

// ImageInput is a single vision attachment. Data is raw bytes (the
// backend handles base64 encoding); MimeType is required so the API
// can decode (e.g. "image/png", "image/jpeg", "image/webp").
type ImageInput struct {
	Data     []byte
	MimeType string
}

// Response carries the model output plus token accounting.
type Response struct {
	Text             string
	InputTokens      int
	OutputTokens     int
	EstimatedUSDCost float64
}

// CostEstimator is an optional interface backends implement to
// support pre-flight cost estimation. Pure local computation — no
// API call.
//
// Callers detect via type assertion: `if est, ok := client.(llm.CostEstimator); ok { … }`.
// Backends without a pricing table (notably local Ollama, where
// cost is structurally $0) implement this trivially; backends like
// Anthropic / OpenAI / Gemini / Kimi / Bedrock delegate to their
// existing internal cost-table.
//
// Contract: best-effort estimate for `inputTokens` input + `outputTokens`
// output. Returns USD as a float. Negative or nonsensical inputs
// should return 0 rather than erroring — pre-flight is for ballpark
// sizing, not billing.
type CostEstimator interface {
	EstimateCost(inputTokens, outputTokens int) float64
}

// ApproxInputTokens converts a source-byte count to an approximate
// token count using the English-text heuristic of ~3.5 characters
// per token. Code tends to be denser (more punctuation, shorter
// identifiers) so this slightly OVER-estimates code corpora, which
// is the safer side for budget pre-flight ("expect at most X").
//
// Centralized here rather than at each call site so a future
// per-language calibration table can land in one place.
func ApproxInputTokens(byteCount int) int {
	if byteCount <= 0 {
		return 0
	}
	return (byteCount + 2) / 3 // ~3.5 chars/token, rounded conservatively
}

// ApproxOutputTokens estimates the model's response size as a
// fraction of input. Semantic extraction is information-extraction-
// shaped — model output is typically a small JSON document much
// shorter than the prose it summarizes. Capped at maxTokens since
// that's the request-level ceiling. 4096 is the gogfy default; the
// caller can pass a different ceiling when known.
func ApproxOutputTokens(inputTokens, maxTokens int) int {
	if maxTokens <= 0 {
		maxTokens = 4096
	}
	// 1/8 of input is roughly what `semantic.Extract` produces on
	// typical README/docstring corpora — closer to 1/4 for code
	// (more entity references per byte). 1/8 keeps the estimate
	// conservative without massive over-prediction.
	est := inputTokens / 8
	if est > maxTokens {
		est = maxTokens
	}
	if est < 64 {
		est = 64 // floor — even tiny inputs trigger boilerplate JSON
	}
	return est
}
