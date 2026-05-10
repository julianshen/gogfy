package serve

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/julianshen/gogfy/internal/export"
	"github.com/julianshen/gogfy/internal/schema"
)

// jsonRPCRequest is a tiny helper to build a request line for the test harness.
func jsonRPCRequest(t *testing.T, id int, method string, params any) []byte {
	t.Helper()
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
	}
	if params != nil {
		req["params"] = params
	}
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return append(b, '\n')
}

// runOnce drives Serve with a single request and returns the parsed responses.
func runOnce(t *testing.T, srv *Server, requests ...[]byte) []map[string]any {
	t.Helper()
	in := bytes.NewBuffer(nil)
	for _, r := range requests {
		in.Write(r)
	}
	out := &bytes.Buffer{}
	if err := srv.Serve(context.Background(), in, out); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	var responses []map[string]any
	for _, line := range bytes.Split(out.Bytes(), []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var resp map[string]any
		if err := json.Unmarshal(line, &resp); err != nil {
			t.Fatalf("unmarshal response %q: %v", line, err)
		}
		responses = append(responses, resp)
	}
	return responses
}

func sampleServer() *Server {
	return New(export.GraphExport{
		Nodes: []schema.Node{
			{ID: "go:function:/m.go:foo", Label: "foo", SourceFile: "/m.go", Community: "core"},
			{ID: "go:function:/m.go:bar", Label: "bar", SourceFile: "/m.go", Community: "core"},
			{ID: "go:function:/m.go:hub", Label: "hub", SourceFile: "/m.go", Community: "core"},
		},
		Edges: []schema.Edge{
			{Source: "go:function:/m.go:foo", Target: "go:function:/m.go:hub", Relation: "calls", Confidence: schema.Inferred},
			{Source: "go:function:/m.go:bar", Target: "go:function:/m.go:hub", Relation: "calls", Confidence: schema.Inferred},
		},
	}, []byte("# Graph Report\n\n## God Nodes\n- hub\n"))
}

func TestInitializeReturnsServerInfo(t *testing.T) {
	resp := runOnce(t, sampleServer(), jsonRPCRequest(t, 1, "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
	}))
	if len(resp) != 1 {
		t.Fatalf("expected 1 response, got %d", len(resp))
	}
	result, ok := resp[0]["result"].(map[string]any)
	if !ok {
		t.Fatalf("missing result in initialize response: %v", resp[0])
	}
	info, ok := result["serverInfo"].(map[string]any)
	if !ok || info["name"] != "gogfy" {
		t.Fatalf("missing/wrong serverInfo: %v", result)
	}
	if _, ok := result["capabilities"].(map[string]any); !ok {
		t.Fatalf("missing capabilities: %v", result)
	}
}

func TestToolsListEnumeratesAllTools(t *testing.T) {
	resp := runOnce(t, sampleServer(), jsonRPCRequest(t, 1, "tools/list", nil))
	result := resp[0]["result"].(map[string]any)
	tools := result["tools"].([]any)
	names := map[string]bool{}
	for _, tool := range tools {
		names[tool.(map[string]any)["name"].(string)] = true
	}
	for _, want := range []string{"gogfy_god_nodes", "gogfy_explain", "gogfy_query"} {
		if !names[want] {
			t.Fatalf("missing tool %q in %v", want, names)
		}
	}
}

func TestToolCallGodNodes(t *testing.T) {
	resp := runOnce(t, sampleServer(), jsonRPCRequest(t, 1, "tools/call", map[string]any{
		"name":      "gogfy_god_nodes",
		"arguments": map[string]any{},
	}))
	result := resp[0]["result"].(map[string]any)
	content := result["content"].([]any)
	if len(content) == 0 {
		t.Fatal("empty god_nodes content")
	}
	text := content[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "hub") {
		t.Fatalf("expected hub in god_nodes output, got %q", text)
	}
}

func TestToolCallExplainByLabel(t *testing.T) {
	resp := runOnce(t, sampleServer(), jsonRPCRequest(t, 1, "tools/call", map[string]any{
		"name":      "gogfy_explain",
		"arguments": map[string]any{"id": "hub"},
	}))
	result := resp[0]["result"].(map[string]any)
	content := result["content"].([]any)
	text := content[0].(map[string]any)["text"].(string)
	// Hub has two incoming edges: foo and bar.
	if !strings.Contains(text, "foo") || !strings.Contains(text, "bar") {
		t.Fatalf("expected neighbors foo and bar in explain output, got %q", text)
	}
}

func TestToolCallExplainNotFound(t *testing.T) {
	resp := runOnce(t, sampleServer(), jsonRPCRequest(t, 1, "tools/call", map[string]any{
		"name":      "gogfy_explain",
		"arguments": map[string]any{"id": "no-such-thing"},
	}))
	result := resp[0]["result"].(map[string]any)
	if isErr, _ := result["isError"].(bool); !isErr {
		t.Fatalf("expected isError=true for not-found, got result=%v", result)
	}
}

func TestToolCallQuerySubstringMatch(t *testing.T) {
	resp := runOnce(t, sampleServer(), jsonRPCRequest(t, 1, "tools/call", map[string]any{
		"name":      "gogfy_query",
		"arguments": map[string]any{"text": "FO"},
	}))
	result := resp[0]["result"].(map[string]any)
	content := result["content"].([]any)
	text := content[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "foo") {
		t.Fatalf("case-insensitive query for FO should match foo, got %q", text)
	}
	if strings.Contains(text, "hub") {
		t.Fatalf("query for FO should not match hub, got %q", text)
	}
}

func TestResourcesListIncludesReport(t *testing.T) {
	resp := runOnce(t, sampleServer(), jsonRPCRequest(t, 1, "resources/list", nil))
	result := resp[0]["result"].(map[string]any)
	resources := result["resources"].([]any)
	found := false
	for _, r := range resources {
		if r.(map[string]any)["uri"] == "gogfy://report" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing gogfy://report in resources list: %v", resources)
	}
}

func TestResourcesReadReportReturnsBytes(t *testing.T) {
	resp := runOnce(t, sampleServer(), jsonRPCRequest(t, 1, "resources/read", map[string]any{
		"uri": "gogfy://report",
	}))
	result := resp[0]["result"].(map[string]any)
	contents := result["contents"].([]any)
	text := contents[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "Graph Report") {
		t.Fatalf("expected report contents, got %q", text)
	}
}

func TestUnknownMethodReturnsMethodNotFoundError(t *testing.T) {
	resp := runOnce(t, sampleServer(), jsonRPCRequest(t, 1, "no/such/method", nil))
	errObj, ok := resp[0]["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error object, got %v", resp[0])
	}
	if code, _ := errObj["code"].(float64); int(code) != -32601 {
		t.Fatalf("expected JSON-RPC method-not-found code -32601, got %v", errObj["code"])
	}
}

func TestParseErrorOnMalformedJSON(t *testing.T) {
	in := bytes.NewBufferString("not json\n")
	out := &bytes.Buffer{}
	if err := sampleServer().Serve(context.Background(), in, out); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	var resp map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	errObj := resp["error"].(map[string]any)
	if int(errObj["code"].(float64)) != -32700 {
		t.Fatalf("expected -32700 parse error, got %v", errObj["code"])
	}
}

func TestUnknownToolReturnsInvalidParamsError(t *testing.T) {
	resp := runOnce(t, sampleServer(), jsonRPCRequest(t, 1, "tools/call", map[string]any{
		"name":      "gogfy_no_such_tool",
		"arguments": map[string]any{},
	}))
	errObj, ok := resp[0]["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error, got %v", resp[0])
	}
	if int(errObj["code"].(float64)) != -32602 {
		t.Fatalf("expected -32602, got %v", errObj["code"])
	}
}

func TestQueryEmptyTextReturnsError(t *testing.T) {
	resp := runOnce(t, sampleServer(), jsonRPCRequest(t, 1, "tools/call", map[string]any{
		"name":      "gogfy_query",
		"arguments": map[string]any{"text": ""},
	}))
	result := resp[0]["result"].(map[string]any)
	if isErr, _ := result["isError"].(bool); !isErr {
		t.Fatalf("expected isError for empty text")
	}
}

func TestQueryWithLimitAndNoMatches(t *testing.T) {
	resp := runOnce(t, sampleServer(), jsonRPCRequest(t, 1, "tools/call", map[string]any{
		"name":      "gogfy_query",
		"arguments": map[string]any{"text": "definitely-not-present", "limit": 5},
	}))
	result := resp[0]["result"].(map[string]any)
	text := result["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "no labels matched") {
		t.Fatalf("expected no-match message, got %q", text)
	}
}

func TestQueryRespectsLimit(t *testing.T) {
	srv := New(export.GraphExport{
		Nodes: []schema.Node{
			{ID: "a", Label: "match1"}, {ID: "b", Label: "match2"}, {ID: "c", Label: "match3"},
		},
	}, nil)
	resp := runOnce(t, srv, jsonRPCRequest(t, 1, "tools/call", map[string]any{
		"name":      "gogfy_query",
		"arguments": map[string]any{"text": "match", "limit": 2},
	}))
	result := resp[0]["result"].(map[string]any)
	text := result["content"].([]any)[0].(map[string]any)["text"].(string)
	if strings.Count(text, "- ") != 2 {
		t.Fatalf("expected 2 results with limit=2, got %q", text)
	}
}

func TestExplainMissingIdArg(t *testing.T) {
	resp := runOnce(t, sampleServer(), jsonRPCRequest(t, 1, "tools/call", map[string]any{
		"name":      "gogfy_explain",
		"arguments": map[string]any{},
	}))
	result := resp[0]["result"].(map[string]any)
	if isErr, _ := result["isError"].(bool); !isErr {
		t.Fatalf("expected isError for missing id")
	}
}

func TestGodNodesEmptyGraph(t *testing.T) {
	srv := New(export.GraphExport{}, nil)
	resp := runOnce(t, srv, jsonRPCRequest(t, 1, "tools/call", map[string]any{
		"name":      "gogfy_god_nodes",
		"arguments": map[string]any{},
	}))
	result := resp[0]["result"].(map[string]any)
	text := result["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "no god nodes") {
		t.Fatalf("expected empty-graph message, got %q", text)
	}
}

func TestGodNodesRespectsLimit(t *testing.T) {
	// Build a graph with several god nodes and verify limit truncates.
	nodes := []schema.Node{}
	edges := []schema.Edge{}
	for i := 0; i < 6; i++ {
		hubID := fmt.Sprintf("hub%d", i)
		nodes = append(nodes, schema.Node{ID: hubID, Label: hubID, Community: "core"})
		for j := 0; j < 4; j++ {
			leafID := fmt.Sprintf("leaf%d_%d", i, j)
			nodes = append(nodes, schema.Node{ID: leafID, Label: leafID, Community: "core"})
			edges = append(edges, schema.Edge{Source: hubID, Target: leafID, Relation: "calls", Confidence: schema.Extracted})
		}
	}
	srv := New(export.GraphExport{Nodes: nodes, Edges: edges}, nil)
	resp := runOnce(t, srv, jsonRPCRequest(t, 1, "tools/call", map[string]any{
		"name":      "gogfy_god_nodes",
		"arguments": map[string]any{"limit": 2},
	}))
	result := resp[0]["result"].(map[string]any)
	text := result["content"].([]any)[0].(map[string]any)["text"].(string)
	if got := strings.Count(text, "- "); got != 2 {
		t.Fatalf("expected 2 entries with limit=2, got %d in %q", got, text)
	}
}

func TestExplainResolvesByExactID(t *testing.T) {
	resp := runOnce(t, sampleServer(), jsonRPCRequest(t, 1, "tools/call", map[string]any{
		"name":      "gogfy_explain",
		"arguments": map[string]any{"id": "go:function:/m.go:hub"},
	}))
	result := resp[0]["result"].(map[string]any)
	text := result["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "## hub") {
		t.Fatalf("expected hub heading, got %q", text)
	}
}

func TestUnknownResourceURIReturnsError(t *testing.T) {
	resp := runOnce(t, sampleServer(), jsonRPCRequest(t, 1, "resources/read", map[string]any{
		"uri": "gogfy://does-not-exist",
	}))
	errObj, ok := resp[0]["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error, got %v", resp[0])
	}
	if int(errObj["code"].(float64)) != -32602 {
		t.Fatalf("expected -32602, got %v", errObj["code"])
	}
}

func TestExplainSurfacesLabelCollisions(t *testing.T) {
	srv := New(export.GraphExport{
		Nodes: []schema.Node{
			{ID: "go:method:/a.go:Run", Label: "Run"},
			{ID: "go:method:/b.go:Run", Label: "Run"},
		},
	}, nil)
	resp := runOnce(t, srv, jsonRPCRequest(t, 1, "tools/call", map[string]any{
		"name":      "gogfy_explain",
		"arguments": map[string]any{"id": "Run"},
	}))
	result := resp[0]["result"].(map[string]any)
	text := result["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "matches 2 nodes") {
		t.Fatalf("expected collision warning, got %q", text)
	}
	if !strings.Contains(text, "go:method:/a.go:Run") || !strings.Contains(text, "go:method:/b.go:Run") {
		t.Fatalf("expected both candidate IDs listed, got %q", text)
	}
}

func TestQueryMatchesIDAndSourceFile(t *testing.T) {
	srv := New(export.GraphExport{
		Nodes: []schema.Node{
			{ID: "go:function:/special_path.go:foo", Label: "foo", SourceFile: "/special_path.go"},
			{ID: "go:function:/other.go:bar", Label: "bar", SourceFile: "/other.go"},
		},
	}, nil)
	// Match by source file substring — no node has "special" in its label.
	resp := runOnce(t, srv, jsonRPCRequest(t, 1, "tools/call", map[string]any{
		"name":      "gogfy_query",
		"arguments": map[string]any{"text": "special"},
	}))
	text := resp[0]["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "foo") {
		t.Fatalf("expected source-file match to surface foo, got %q", text)
	}
	if strings.Contains(text, "bar") {
		t.Fatalf("query should not match bar, got %q", text)
	}
}

func TestGodNodesRejectsMalformedArgs(t *testing.T) {
	resp := runOnce(t, sampleServer(), []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"gogfy_god_nodes","arguments":{"limit":"not-a-number"}}}`+"\n"))
	result := resp[0]["result"].(map[string]any)
	if isErr, _ := result["isError"].(bool); !isErr {
		t.Fatalf("expected isError on malformed limit, got %v", result)
	}
}

func TestNotificationInitializedProducesNoResponse(t *testing.T) {
	// Notifications (no "id" field) MUST NOT produce a response per JSON-RPC 2.0.
	in := bytes.NewBufferString(`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n")
	out := &bytes.Buffer{}
	if err := sampleServer().Serve(context.Background(), in, out); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("notification produced output: %q", out.String())
	}
}
