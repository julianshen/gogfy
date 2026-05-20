// Package bedrock implements the llm.Client interface against AWS
// Bedrock Runtime's InvokeModel endpoint. Currently supports Claude
// 3 / 3.5 / 3.7 family models — their wire format is the Anthropic
// Messages API minus the `model` field (which lives in the URL path).
//
// Credentials come from the standard AWS env chain:
//
//	AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY [, AWS_SESSION_TOKEN]
//
// Region from AWS_REGION (falls back to us-east-1). This is the same
// chain `aws-cli` uses, so any developer already authenticated to AWS
// inherits the auth here without extra wiring. The AWS SDK is
// intentionally NOT a dependency — gogfy keeps a small dependency
// footprint and SigV4 is the only AWS protocol piece needed here.
package bedrock

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

// DefaultModel — the Bedrock-form of the same Claude 3.5 Haiku that
// the direct-Anthropic client uses by default. Suffixes are Bedrock-
// specific: `-v1:0` is AWS's model-version tag.
const DefaultModel = "anthropic.claude-3-5-haiku-20241022-v1:0"

// DefaultRegion is used when AWS_REGION isn't set. us-east-1 carries
// the full Claude lineup; not all regions do, so users on us-west-2
// etc. must set AWS_REGION explicitly.
const DefaultRegion = "us-east-1"

// AnthropicVersion is the value Bedrock requires in the body to
// identify which Anthropic API surface the request targets. Bedrock
// pins to a specific date string regardless of the model.
const AnthropicVersion = "bedrock-2023-05-31"

// costPerMillion is the same shape as the Anthropic-direct table.
// Bedrock prices are usually within a few % of Anthropic-direct; the
// numbers here are us-east-1 on-demand as of 2026 and should be kept
// in sync when AWS adjusts.
var costPerMillion = map[string]struct {
	in, out float64
}{
	"anthropic.claude-3-5-haiku-20241022-v1:0":  {0.80, 4.00},
	"anthropic.claude-3-5-sonnet-20241022-v2:0": {3.00, 15.00},
	"anthropic.claude-3-opus-20240229-v1:0":     {15.00, 75.00},
}

// ErrMissingCredentials fires when neither AWS_ACCESS_KEY_ID nor
// AWS_SECRET_ACCESS_KEY is set. We don't try to read ~/.aws/credentials
// or query EC2/ECS metadata — gogfy's contract is "env or explicit",
// matching the other LLM clients.
var ErrMissingCredentials = errors.New("bedrock: AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY environment variables not set")

// Client implements llm.Client against Bedrock Runtime.
type Client struct {
	creds    signCredentials
	model    string
	endpoint string // override for tests; empty means derive from region
	http     *http.Client
	// nowFn lets tests pin the SigV4 timestamp. Production uses time.Now.
	nowFn func() time.Time
}

// Option tweaks a Client at construction.
type Option func(*Client)

// WithModel overrides the default model (claude-3-5-haiku).
func WithModel(m string) Option { return func(c *Client) { c.model = m } }

// WithRegion overrides the AWS_REGION env value.
func WithRegion(r string) Option { return func(c *Client) { c.creds.Region = r } }

// WithEndpoint redirects requests to an alternate URL — used by tests
// to hit a httptest.Server. The full URL replaces the derived one
// including path, so callers must include `/model/{id}/invoke`.
func WithEndpoint(u string) Option { return func(c *Client) { c.endpoint = u } }

// WithHTTPClient swaps the HTTP transport — useful for tests and for
// users behind a proxy.
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

// withNow lets tests pin the SigV4 timestamp so the signature is
// deterministic. Unexported because nothing in production needs it.
func withNow(fn func() time.Time) Option { return func(c *Client) { c.nowFn = fn } }

// New returns a Client reading credentials from the AWS env chain.
// Returns ErrMissingCredentials if the required vars are absent so
// callers can show a clear hint rather than surfacing an opaque 403
// from AWS.
func New(opts ...Option) (*Client, error) {
	ak := os.Getenv("AWS_ACCESS_KEY_ID")
	sk := os.Getenv("AWS_SECRET_ACCESS_KEY")
	if ak == "" || sk == "" {
		return nil, ErrMissingCredentials
	}
	return NewWithCredentials(ak, sk, os.Getenv("AWS_SESSION_TOKEN"), opts...), nil
}

// NewWithCredentials constructs a Client with explicit AWS credentials.
// Useful when keys come from a secret manager rather than the env.
func NewWithCredentials(accessKey, secretKey, sessionToken string, opts ...Option) *Client {
	region := os.Getenv("AWS_REGION")
	if region == "" {
		region = DefaultRegion
	}
	c := &Client{
		creds: signCredentials{
			AccessKey:    accessKey,
			SecretKey:    secretKey,
			SessionToken: sessionToken,
			Region:       region,
			Service:      "bedrock",
		},
		model: DefaultModel,
		// 5-minute ceiling matches the transcribe client and is enough
		// headroom for long-context Claude calls on Bedrock; users
		// override via WithHTTPClient if they need more.
		http: &http.Client{Timeout: 5 * time.Minute},
		nowFn: time.Now,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Name returns "bedrock-{model}" for cost reports and log lines.
func (c *Client) Name() string { return "bedrock-" + c.model }

// EstimateCost implements llm.CostEstimator for pre-flight pricing.
// Bedrock per-token rates are usually within a few % of direct-
// Anthropic; delegate to the local cost table.
func (c *Client) EstimateCost(inputTokens, outputTokens int) float64 {
	return estimateCost(c.model, inputTokens, outputTokens)
}

// Generate posts a single-turn request to Bedrock's InvokeModel
// endpoint. The body is the Anthropic Messages-API shape (minus the
// model field, which lives in the path); the response is parsed
// identically to the direct Anthropic client.
func (c *Client) Generate(ctx context.Context, req llm.Request) (llm.Response, error) {
	body, err := json.Marshal(invokeRequest{
		AnthropicVersion: AnthropicVersion,
		MaxTokens:        defaultMaxTokens(req.MaxTokens),
		System:           req.System,
		Messages:         []message{{Role: "user", Content: buildUserContent(req)}},
	})
	if err != nil {
		return llm.Response{}, fmt.Errorf("bedrock: marshal request: %w", err)
	}
	url := c.endpointURL()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return llm.Response{}, fmt.Errorf("bedrock: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	signRequest(httpReq, body, c.creds, c.nowFn())

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return llm.Response{}, fmt.Errorf("bedrock: http: %w", err)
	}
	defer resp.Body.Close()
	respBody, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return llm.Response{}, fmt.Errorf("bedrock: read response (%s): %w", resp.Status, readErr)
	}
	if resp.StatusCode != http.StatusOK {
		return llm.Response{}, fmt.Errorf("bedrock: %s: %s", resp.Status, snippet(respBody, 500))
	}
	var parsed invokeResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return llm.Response{}, fmt.Errorf("bedrock: parse response: %w", err)
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

// endpointURL builds the Bedrock Runtime invoke URL. Test injection
// via WithEndpoint short-circuits this.
func (c *Client) endpointURL() string {
	if c.endpoint != "" {
		return c.endpoint
	}
	return "https://bedrock-runtime." + c.creds.Region + ".amazonaws.com/model/" + c.model + "/invoke"
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
	fmt.Fprintf(os.Stderr, "bedrock: no pricing entry for model %q — cost estimate will be $0 (update internal/llm/bedrock pricing table)\n", model)
}

func snippet(b []byte, n int) string {
	if len(b) > n {
		return string(b[:n]) + "…"
	}
	return string(b)
}

// Wire-format types for Bedrock's InvokeModel endpoint when calling
// an Anthropic Claude model. The body shape mirrors Anthropic's
// Messages API except `anthropic_version` replaces the top-level
// `model` field.
type invokeRequest struct {
	AnthropicVersion string    `json:"anthropic_version"`
	MaxTokens        int       `json:"max_tokens"`
	System           string    `json:"system,omitempty"`
	Messages         []message `json:"messages"`
}

type message struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type contentInput struct {
	Type   string       `json:"type"`
	Text   string       `json:"text,omitempty"`
	Source *imageSource `json:"source,omitempty"`
}

type imageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

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

type invokeResponse struct {
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
