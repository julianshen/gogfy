// Package semantic extracts entities and relationships from prose
// (markdown, txt, rst) using an LLM. The output shape mirrors the
// AST-extractor result so semantic nodes/edges flow through the rest
// of the pipeline without special-casing.
//
// Confidence on emitted edges defaults to INFERRED — LLM judgements
// are weaker than AST extraction. Users who want stricter routing can
// post-filter the graph by confidence.
package semantic

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/julianshen/gogfy/internal/llm"
	"github.com/julianshen/gogfy/internal/schema"
)

// systemPrompt instructs the model to emit a fixed JSON shape with no
// commentary. Kept short on purpose — the model already knows the
// task; verbose prompts cost tokens without lifting accuracy on
// well-defined extraction.
const systemPrompt = `You extract entities and relationships from prose.

Return a JSON object exactly matching this shape, with no commentary:

{
  "entities": [
    {"id": "kebab-case-slug", "type": "Concept|Person|Organization|Technology|Artifact", "label": "Human-readable name"}
  ],
  "relations": [
    {"source": "entity-id", "target": "entity-id", "type": "describes|uses|implements|references|mentions"}
  ]
}

Rules:
- Only emit entities and relations explicitly supported by the text.
- IDs must be lowercase kebab-case and unique within the response.
- Skip entities that appear only once and are not central.
- If the text contains no extractable entities, return {"entities":[],"relations":[]}.`

// visionSystemPrompt mirrors systemPrompt but pivots the extraction
// surface from prose to visual content. Same JSON output shape so
// downstream consumers don't need a separate parse path.
const visionSystemPrompt = `You extract entities and relationships visible in an image.

Return a JSON object exactly matching this shape, with no commentary:

{
  "entities": [
    {"id": "kebab-case-slug", "type": "Concept|Person|Organization|Technology|Artifact", "label": "Human-readable name"}
  ],
  "relations": [
    {"source": "entity-id", "target": "entity-id", "type": "describes|uses|contains|part-of"}
  ]
}

Rules:
- Only emit entities and relations clearly visible in the image.
- IDs must be lowercase kebab-case and unique within the response.
- For diagrams (architecture/flowcharts), each box/node becomes an entity and each arrow becomes a relation.
- For photographs, identify the principal subjects only.
- If the image contains nothing extractable (blank, noise), return {"entities":[],"relations":[]}.`

// Result is what Extract returns: the nodes/edges in the same shape
// the AST extractors emit, plus token-usage telemetry so callers can
// build a running cost estimate across many files.
type Result struct {
	Nodes            []schema.Node
	Edges            []schema.Edge
	InputTokens      int
	OutputTokens     int
	EstimatedUSDCost float64
}

// Extract calls the client with the file's text and converts the
// JSON response into schema.Node/Edge values. Adds a module node for
// the source file and `mentions` edges from it to each extracted
// entity so the LLM output is anchored to where it came from.
func Extract(ctx context.Context, client llm.Client, path string, src []byte) (Result, error) {
	if len(src) == 0 {
		return Result{}, nil
	}
	resp, err := client.Generate(ctx, llm.Request{
		System: systemPrompt,
		User:   string(src),
		// Markdown corpora are typically small enough that 4K output
		// is plenty; oversize budgets just inflate cost on the few
		// requests where the model goes long.
		MaxTokens: 4096,
	})
	return parseResponse(resp, path, err)
}

// ExtractImage is the vision counterpart to Extract. Pass raw image
// bytes (PNG, JPEG, WebP) plus the MIME type; the model sees the
// image and emits entities + relations the same JSON shape Extract
// uses, so downstream merge/dedup treat the output uniformly.
//
// Caller decides the MIME type — usually from the file extension via
// http.DetectContentType on the first 512 bytes.
func ExtractImage(ctx context.Context, client llm.Client, path string, image []byte, mimeType string) (Result, error) {
	if len(image) == 0 {
		return Result{}, nil
	}
	resp, err := client.Generate(ctx, llm.Request{
		System: visionSystemPrompt,
		User:   "Extract entities and relationships from this image.",
		Images: []llm.ImageInput{{Data: image, MimeType: mimeType}},
		// Visual content can require longer outputs (multiple
		// entities per scene) but the JSON shape is still bounded.
		MaxTokens: 4096,
	})
	return parseResponse(resp, path, err)
}

// parseResponse is the shared post-LLM-call path: strip JSON from
// the response, map entities → schema.Node, relations → schema.Edge,
// anchor everything to a module node for the source path. Splitting
// it out keeps Extract and ExtractImage to one prompt-policy line
// each.
func parseResponse(resp llm.Response, path string, callErr error) (Result, error) {
	if callErr != nil {
		return Result{}, fmt.Errorf("semantic: %w", callErr)
	}
	payload, ok := extractJSON(resp.Text)
	if !ok {
		return Result{}, fmt.Errorf("semantic: response had no JSON object: %s", snippet(resp.Text, 200))
	}
	var parsed extracted
	if err := json.Unmarshal([]byte(payload), &parsed); err != nil {
		return Result{}, fmt.Errorf("semantic: parse response JSON: %w: %s", err, snippet(payload, 200))
	}

	// Build the result. Module node anchors the LLM output to its
	// source file so the graph remains connected after merge with
	// AST-extracted code nodes.
	moduleID := schema.LangID("doc", "module", path)
	out := Result{
		Nodes: []schema.Node{{
			ID:         moduleID,
			Label:      path,
			SourceFile: path,
			FileType:   schema.FileTypeDocument,
		}},
		InputTokens:      resp.InputTokens,
		OutputTokens:     resp.OutputTokens,
		EstimatedUSDCost: resp.EstimatedUSDCost,
	}

	for _, e := range parsed.Entities {
		if e.ID == "" || e.Label == "" {
			continue
		}
		nodeID := schema.LangID("doc", "entity", path+":"+e.ID)
		out.Nodes = append(out.Nodes, schema.Node{
			ID:         nodeID,
			Label:      e.Label,
			SourceFile: path,
			FileType:   schema.FileTypeDocument,
		})
		out.Edges = append(out.Edges, schema.Edge{
			Source:     moduleID,
			Target:     nodeID,
			Relation:   "mentions",
			Confidence: schema.Inferred,
		})
	}
	for _, r := range parsed.Relations {
		if r.Source == "" || r.Target == "" {
			continue
		}
		relType := r.Type
		if relType == "" {
			relType = "relates_to"
		}
		out.Edges = append(out.Edges, schema.Edge{
			Source:     schema.LangID("doc", "entity", path+":"+r.Source),
			Target:     schema.LangID("doc", "entity", path+":"+r.Target),
			Relation:   relType,
			Confidence: schema.Inferred,
		})
	}
	return out, nil
}

// extractJSON finds the first JSON object in s. Some models wrap
// their answer in markdown fences or add prose before/after — strip
// to the first {...} block so the unmarshal succeeds in those cases
// instead of erroring on a trivially-fixable shape mismatch.
var jsonObjRe = regexp.MustCompile(`(?s)\{.*\}`)

func extractJSON(s string) (string, bool) {
	match := jsonObjRe.FindString(s)
	if match == "" {
		return "", false
	}
	return strings.TrimSpace(match), true
}

func snippet(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

// extracted is the LLM-output wire shape. Loose strings on every
// field so a model that emits unexpected `type` values doesn't fail
// the whole call.
type extracted struct {
	Entities []struct {
		ID    string `json:"id"`
		Type  string `json:"type"`
		Label string `json:"label"`
	} `json:"entities"`
	Relations []struct {
		Source string `json:"source"`
		Target string `json:"target"`
		Type   string `json:"type"`
	} `json:"relations"`
}
