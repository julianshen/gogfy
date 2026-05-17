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
}

// Response carries the model output plus token accounting.
type Response struct {
	Text             string
	InputTokens      int
	OutputTokens     int
	EstimatedUSDCost float64
}
