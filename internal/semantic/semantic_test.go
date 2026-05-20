package semantic

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/julianshen/gogfy/internal/llm"
	"github.com/julianshen/gogfy/internal/schema"
)

// mockClient is a stub Client for tests — captures the request and
// replays a canned response. Keeps the unit tests fully offline so CI
// doesn't depend on an API key or live network.
type mockClient struct {
	gotSystem string
	gotUser   string
	respText  string
	respErr   error
	tokensIn  int
	tokensOut int
}

func (m *mockClient) Name() string { return "mock" }
func (m *mockClient) Generate(_ context.Context, req llm.Request) (llm.Response, error) {
	m.gotSystem = req.System
	m.gotUser = req.User
	if m.respErr != nil {
		return llm.Response{}, m.respErr
	}
	return llm.Response{
		Text:             m.respText,
		InputTokens:      m.tokensIn,
		OutputTokens:     m.tokensOut,
		EstimatedUSDCost: 0.001,
	}, nil
}

func TestExtractEmitsModuleNodePlusEntities(t *testing.T) {
	mock := &mockClient{
		respText: `{"entities":[
			{"id":"auth-service","type":"Concept","label":"Auth Service"},
			{"id":"db","type":"Technology","label":"Postgres"}
		],"relations":[
			{"source":"auth-service","target":"db","type":"uses"}
		]}`,
		tokensIn:  100,
		tokensOut: 50,
	}
	res, err := Extract(context.Background(), mock, "/proj/architecture.md", []byte("Auth uses Postgres."))
	if err != nil {
		t.Fatal(err)
	}
	// Module + 2 entity nodes = 3 total.
	if len(res.Nodes) != 3 {
		t.Fatalf("expected 1 module + 2 entity nodes, got %d", len(res.Nodes))
	}
	if res.Nodes[0].ID != "doc:module:/proj/architecture.md" {
		t.Errorf("module node ID wrong: %q", res.Nodes[0].ID)
	}
	if res.Nodes[0].FileType != schema.FileTypeDocument {
		t.Errorf("module FileType: %q", res.Nodes[0].FileType)
	}
	// Edges: 2 module→entity mentions + 1 entity→entity uses = 3.
	if len(res.Edges) != 3 {
		t.Fatalf("expected 3 edges (2 mentions + 1 relation), got %d: %+v", len(res.Edges), res.Edges)
	}
	// LLM-extracted edges should be INFERRED, not EXTRACTED.
	for _, e := range res.Edges {
		if e.Confidence != schema.Inferred {
			t.Errorf("LLM edges should be INFERRED, got %v on %+v", e.Confidence, e)
		}
	}
	if res.InputTokens != 100 || res.OutputTokens != 50 {
		t.Errorf("token usage not propagated: %+v", res)
	}
}

func TestExtractEmptyInputNoOps(t *testing.T) {
	// Empty source bytes shouldn't even call the LLM — would waste
	// tokens on a guaranteed-empty extraction.
	mock := &mockClient{}
	res, err := Extract(context.Background(), mock, "/empty.md", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Nodes)+len(res.Edges) != 0 {
		t.Fatalf("empty input must not produce nodes/edges, got %+v", res)
	}
	if mock.gotUser != "" {
		t.Fatal("client should not be called on empty input")
	}
}

func TestExtractPassesSystemAndUserPrompts(t *testing.T) {
	mock := &mockClient{respText: `{"entities":[],"relations":[]}`}
	_, err := Extract(context.Background(), mock, "/x.md", []byte("hello world"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mock.gotSystem, "kebab-case") {
		t.Errorf("system prompt missing schema description: %q", mock.gotSystem)
	}
	if mock.gotUser != "hello world" {
		t.Errorf("user prompt mismatch: %q", mock.gotUser)
	}
}

func TestExtractStripsModelPreambleAndCodeFences(t *testing.T) {
	// Some models wrap JSON in prose or ```json fences. Extract should
	// find the JSON object anywhere in the response, not require the
	// model to strictly format.
	mock := &mockClient{
		respText: "Sure, here's the extraction:\n\n```json\n{\"entities\":[{\"id\":\"x\",\"type\":\"Concept\",\"label\":\"X\"}],\"relations\":[]}\n```\n",
	}
	res, err := Extract(context.Background(), mock, "/x.md", []byte("hi"))
	if err != nil {
		t.Fatalf("should parse JSON embedded in prose: %v", err)
	}
	if len(res.Nodes) != 2 { // module + 1 entity
		t.Fatalf("expected 2 nodes, got %d", len(res.Nodes))
	}
}

func TestExtractReturnsErrorOnMalformedJSON(t *testing.T) {
	mock := &mockClient{respText: "This response has no JSON at all."}
	_, err := Extract(context.Background(), mock, "/x.md", []byte("hi"))
	if err == nil {
		t.Fatal("expected error on missing JSON")
	}
	if !strings.Contains(err.Error(), "no JSON") {
		t.Errorf("error should mention missing JSON: %v", err)
	}
}

func TestExtractWrapsClientError(t *testing.T) {
	wantErr := errors.New("network down")
	mock := &mockClient{respErr: wantErr}
	_, err := Extract(context.Background(), mock, "/x.md", []byte("hi"))
	if err == nil {
		t.Fatal("expected error to propagate")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("client error should wrap with %%w: %v", err)
	}
}

func TestExtractSkipsEntitiesWithMissingIDOrLabel(t *testing.T) {
	// Defensive: the model occasionally returns half-empty entities.
	// Drop them silently rather than emitting blank-labeled nodes.
	mock := &mockClient{
		respText: `{"entities":[
			{"id":"keep","type":"Concept","label":"Keeper"},
			{"id":"","type":"Concept","label":"NoID"},
			{"id":"nolabel","type":"Concept","label":""}
		],"relations":[]}`,
	}
	res, _ := Extract(context.Background(), mock, "/x.md", []byte("hi"))
	// module + only "keep" survives = 2 nodes
	if len(res.Nodes) != 2 {
		t.Fatalf("expected 2 nodes (module + keeper), got %d: %+v", len(res.Nodes), res.Nodes)
	}
}

func TestExtractDefaultsRelationTypeWhenBlank(t *testing.T) {
	mock := &mockClient{
		respText: `{"entities":[
			{"id":"a","type":"Concept","label":"A"},
			{"id":"b","type":"Concept","label":"B"}
		],"relations":[
			{"source":"a","target":"b","type":""}
		]}`,
	}
	res, _ := Extract(context.Background(), mock, "/x.md", []byte("hi"))
	hit := false
	for _, e := range res.Edges {
		if e.Relation == "relates_to" {
			hit = true
		}
	}
	if !hit {
		t.Fatalf("blank relation type should default to relates_to: %+v", res.Edges)
	}
}

func TestExtractImagePassesImageToClient(t *testing.T) {
	mock := &visionMock{
		Response: `{"entities":[{"id":"box","type":"Artifact","label":"Box"}],"relations":[]}`,
	}
	res, err := ExtractImage(context.Background(), mock, "/diagrams/arch.png", []byte("png bytes"), "image/png")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Nodes) != 2 { // module + 1 entity
		t.Fatalf("expected 2 nodes, got %d", len(res.Nodes))
	}
	if len(mock.gotImages) != 1 {
		t.Fatalf("client should receive 1 image, got %d", len(mock.gotImages))
	}
	if mock.gotImages[0].MimeType != "image/png" {
		t.Errorf("mime type not passed: %q", mock.gotImages[0].MimeType)
	}
	if string(mock.gotImages[0].Data) != "png bytes" {
		t.Errorf("image bytes not passed verbatim: %q", mock.gotImages[0].Data)
	}
}

func TestExtractImageUsesVisionSystemPrompt(t *testing.T) {
	mock := &visionMock{Response: `{"entities":[],"relations":[]}`}
	_, err := ExtractImage(context.Background(), mock, "/x.png", []byte("img"), "image/png")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mock.gotSystem, "visible in an image") {
		t.Errorf("vision system prompt not used: %q", mock.gotSystem)
	}
}

func TestExtractImageEmptyBytesNoOps(t *testing.T) {
	mock := &visionMock{}
	res, err := ExtractImage(context.Background(), mock, "/x.png", nil, "image/png")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Nodes) != 0 || len(res.Edges) != 0 {
		t.Errorf("empty image must not produce nodes/edges: %+v", res)
	}
	if mock.called {
		t.Error("client must not be called for empty image bytes")
	}
}

// visionMock captures image attachments so tests can assert on
// shape without sending real network traffic.
type visionMock struct {
	gotSystem string
	gotImages []llm.ImageInput
	Response  string
	called    bool
}

func (m *visionMock) Name() string { return "vision-mock" }
func (m *visionMock) Generate(_ context.Context, req llm.Request) (llm.Response, error) {
	m.called = true
	m.gotSystem = req.System
	m.gotImages = req.Images
	return llm.Response{Text: m.Response}, nil
}

func TestExtractParsesHyperedgesFromResponse(t *testing.T) {
	// A 3-member hyperedge in the response should land in Result.Hyperedges
	// with all member IDs translated through the doc:entity:<path>:<id>
	// scheme so they connect to the entities the LLM emitted alongside.
	mock := &mockClient{
		respText: `{
			"entities": [
				{"id": "alice", "type": "Person", "label": "Alice"},
				{"id": "bob", "type": "Person", "label": "Bob"},
				{"id": "carol", "type": "Person", "label": "Carol"},
				{"id": "paper", "type": "Artifact", "label": "Joint paper"}
			],
			"relations": [],
			"hyperedges": [
				{"members": ["alice", "bob", "carol", "paper"], "type": "co_authored"}
			]
		}`,
	}
	res, err := Extract(context.Background(), mock, "/proj/notes.md", []byte("body"))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hyperedges) != 1 {
		t.Fatalf("expected 1 hyperedge, got %d: %+v", len(res.Hyperedges), res.Hyperedges)
	}
	h := res.Hyperedges[0]
	if h.Relation != "co_authored" {
		t.Errorf("relation drift: %q", h.Relation)
	}
	if len(h.Members) != 4 {
		t.Errorf("member count: got %d, want 4", len(h.Members))
	}
	// Members should be translated to doc:entity:<path>:<id>.
	for _, m := range h.Members {
		if !contains(m, "doc:entity:/proj/notes.md:") {
			t.Errorf("hyperedge member not translated through doc:entity scheme: %q", m)
		}
	}
}

func TestExtractDropsPairwiseHyperedges(t *testing.T) {
	// 2-member "hyperedges" are degenerate — they should be expressed
	// as Relations, not Hyperedges. Silently drop to avoid duplicating
	// what relations already cover.
	mock := &mockClient{
		respText: `{
			"entities": [
				{"id": "a", "type": "Concept", "label": "A"},
				{"id": "b", "type": "Concept", "label": "B"}
			],
			"hyperedges": [
				{"members": ["a", "b"], "type": "pair"}
			]
		}`,
	}
	res, _ := Extract(context.Background(), mock, "/p.md", []byte("body"))
	if len(res.Hyperedges) != 0 {
		t.Errorf("pairwise hyperedge should be dropped, got %+v", res.Hyperedges)
	}
}

func TestExtractHyperedgeRelationDefaultsToCoOccurs(t *testing.T) {
	mock := &mockClient{
		respText: `{
			"entities": [{"id":"a","label":"A"},{"id":"b","label":"B"},{"id":"c","label":"C"}],
			"hyperedges": [{"members": ["a","b","c"]}]
		}`,
	}
	res, _ := Extract(context.Background(), mock, "/p.md", []byte("body"))
	if len(res.Hyperedges) != 1 || res.Hyperedges[0].Relation != "co_occurs" {
		t.Errorf("expected default co_occurs relation, got %+v", res.Hyperedges)
	}
}

func TestExtractMissingHyperedgesFieldIsOK(t *testing.T) {
	// Old / minimal responses without a hyperedges field must still
	// parse cleanly — backwards compatibility.
	mock := &mockClient{
		respText: `{"entities":[{"id":"a","label":"A"}],"relations":[]}`,
	}
	res, err := Extract(context.Background(), mock, "/p.md", []byte("body"))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hyperedges) != 0 {
		t.Errorf("missing field should yield empty hyperedges, got %+v", res.Hyperedges)
	}
}

// Local helper — semantic package already has `contains` via cache_test.
func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
