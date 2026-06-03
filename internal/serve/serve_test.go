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
	for _, want := range []string{"gogfy_god_nodes", "gogfy_explain", "gogfy_query", "gogfy_get_neighbors", "gogfy_graph_stats", "gogfy_get_community"} {
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

func TestResourcesListIncludesNewSlices(t *testing.T) {
	// Every analyze-report slice the server caches must surface as
	// its own resource — otherwise clients have to ask for the whole
	// markdown report and parse it.
	want := []string{
		"gogfy://stats",
		"gogfy://god-nodes",
		"gogfy://surprising-links",
		"gogfy://questions",
		"gogfy://audit",
	}
	resp := runOnce(t, sampleServer(), jsonRPCRequest(t, 1, "resources/list", nil))
	result := resp[0]["result"].(map[string]any)
	resources := result["resources"].([]any)
	got := map[string]bool{}
	for _, r := range resources {
		got[r.(map[string]any)["uri"].(string)] = true
	}
	for _, uri := range want {
		if !got[uri] {
			t.Errorf("missing resource %s in resources/list", uri)
		}
	}
}

func TestResourcesReadStatsReturnsJSON(t *testing.T) {
	resp := runOnce(t, sampleServer(), jsonRPCRequest(t, 1, "resources/read", map[string]any{
		"uri": "gogfy://stats",
	}))
	result := resp[0]["result"].(map[string]any)
	contents := result["contents"].([]any)
	body := contents[0].(map[string]any)
	if body["mimeType"] != "application/json" {
		t.Errorf("stats mime: got %v want application/json", body["mimeType"])
	}
	text := body["text"].(string)
	for _, key := range []string{`"nodes"`, `"edges"`, `"god_nodes"`} {
		if !strings.Contains(text, key) {
			t.Errorf("stats body missing %s: %q", key, text)
		}
	}
}

func TestResourcesReadGodNodesReturnsList(t *testing.T) {
	resp := runOnce(t, sampleServer(), jsonRPCRequest(t, 1, "resources/read", map[string]any{
		"uri": "gogfy://god-nodes",
	}))
	result := resp[0]["result"].(map[string]any)
	contents := result["contents"].([]any)
	text := contents[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, `"god_nodes"`) {
		t.Errorf("expected god_nodes key, got %q", text)
	}
}

func TestResourcesReadUnknownStillErrors(t *testing.T) {
	// Adding new resources mustn't break the unknown-URI error path.
	resp := runOnce(t, sampleServer(), jsonRPCRequest(t, 1, "resources/read", map[string]any{
		"uri": "gogfy://does-not-exist",
	}))
	if _, ok := resp[0]["error"].(map[string]any); !ok {
		t.Fatalf("expected error for unknown URI, got %v", resp[0])
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

func TestToolCallPathFindsShortestRoute(t *testing.T) {
	// foo -> hub <- bar (sample graph). Path foo→bar should be foo, hub, bar.
	resp := runOnce(t, sampleServer(), jsonRPCRequest(t, 1, "tools/call", map[string]any{
		"name":      "gogfy_path",
		"arguments": map[string]any{"source": "foo", "target": "bar"},
	}))
	result := resp[0]["result"].(map[string]any)
	text := result["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "foo") || !strings.Contains(text, "hub") || !strings.Contains(text, "bar") {
		t.Fatalf("expected foo, hub, bar in path, got %q", text)
	}
}

func TestToolCallPathSourceEqualsTarget(t *testing.T) {
	resp := runOnce(t, sampleServer(), jsonRPCRequest(t, 1, "tools/call", map[string]any{
		"name":      "gogfy_path",
		"arguments": map[string]any{"source": "hub", "target": "hub"},
	}))
	result := resp[0]["result"].(map[string]any)
	text := result["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "hub") {
		t.Fatalf("trivial self-path should include the node, got %q", text)
	}
}

func TestToolCallPathNoConnection(t *testing.T) {
	srv := New(export.GraphExport{
		Nodes: []schema.Node{
			{ID: "a", Label: "a"}, {ID: "b", Label: "b"},
		},
	}, nil)
	resp := runOnce(t, srv, jsonRPCRequest(t, 1, "tools/call", map[string]any{
		"name":      "gogfy_path",
		"arguments": map[string]any{"source": "a", "target": "b"},
	}))
	result := resp[0]["result"].(map[string]any)
	text := result["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "no path") {
		t.Fatalf("expected 'no path' message, got %q", text)
	}
}

func TestToolCallPathRejectsMissingArgs(t *testing.T) {
	resp := runOnce(t, sampleServer(), jsonRPCRequest(t, 1, "tools/call", map[string]any{
		"name":      "gogfy_path",
		"arguments": map[string]any{"source": "foo"},
	}))
	result := resp[0]["result"].(map[string]any)
	if isErr, _ := result["isError"].(bool); !isErr {
		t.Fatalf("expected isError when target missing")
	}
}

func TestToolCallPathSourceNotFound(t *testing.T) {
	resp := runOnce(t, sampleServer(), jsonRPCRequest(t, 1, "tools/call", map[string]any{
		"name":      "gogfy_path",
		"arguments": map[string]any{"source": "nope", "target": "hub"},
	}))
	result := resp[0]["result"].(map[string]any)
	if isErr, _ := result["isError"].(bool); !isErr {
		t.Fatalf("expected isError on unknown source")
	}
}

func TestToolsListIncludesPath(t *testing.T) {
	resp := runOnce(t, sampleServer(), jsonRPCRequest(t, 1, "tools/list", nil))
	result := resp[0]["result"].(map[string]any)
	tools := result["tools"].([]any)
	for _, tool := range tools {
		if tool.(map[string]any)["name"].(string) == "gogfy_path" {
			return
		}
	}
	t.Fatalf("gogfy_path missing from tools/list: %v", tools)
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

func TestToolCallGetNeighbors(t *testing.T) {
	resp := runOnce(t, sampleServer(), jsonRPCRequest(t, 1, "tools/call", map[string]any{
		"name":      "gogfy_get_neighbors",
		"arguments": map[string]any{"id": "hub"},
	}))
	result := resp[0]["result"].(map[string]any)
	text := result["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "neighbors") {
		t.Fatalf("expected neighbors output, got %q", text)
	}
}

func TestToolCallGetNeighborsRelationFilter(t *testing.T) {
	// Filter to a relation that exists vs one that doesn't.
	respCalls := runOnce(t, sampleServer(), jsonRPCRequest(t, 1, "tools/call", map[string]any{
		"name":      "gogfy_get_neighbors",
		"arguments": map[string]any{"id": "hub", "relation": "calls"},
	}))
	textCalls := respCalls[0]["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)

	respNonsense := runOnce(t, sampleServer(), jsonRPCRequest(t, 1, "tools/call", map[string]any{
		"name":      "gogfy_get_neighbors",
		"arguments": map[string]any{"id": "hub", "relation": "doesNotExist"},
	}))
	textNonsense := respNonsense[0]["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(textNonsense, "no neighbors") {
		t.Fatalf("expected 'no neighbors' for nonexistent relation, got %q", textNonsense)
	}
	if textCalls == textNonsense {
		t.Fatal("relation filter must change output")
	}
}

func TestToolCallGraphStats(t *testing.T) {
	resp := runOnce(t, sampleServer(), jsonRPCRequest(t, 1, "tools/call", map[string]any{
		"name":      "gogfy_graph_stats",
		"arguments": map[string]any{},
	}))
	result := resp[0]["result"].(map[string]any)
	text := result["content"].([]any)[0].(map[string]any)["text"].(string)
	for _, want := range []string{"nodes", "edges", "communities"} {
		if !strings.Contains(text, want) {
			t.Errorf("graph_stats missing %q: %s", want, text)
		}
	}
}

func TestToolCallGetNeighborsMissingID(t *testing.T) {
	resp := runOnce(t, sampleServer(), jsonRPCRequest(t, 1, "tools/call", map[string]any{
		"name":      "gogfy_get_neighbors",
		"arguments": map[string]any{},
	}))
	result := resp[0]["result"].(map[string]any)
	if !result["isError"].(bool) {
		t.Fatal("missing id should be an error")
	}
}

func TestToolCallGetCommunityByID(t *testing.T) {
	resp := runOnce(t, sampleServer(), jsonRPCRequest(t, 1, "tools/call", map[string]any{
		"name":      "gogfy_get_community",
		"arguments": map[string]any{"id": "core"},
	}))
	result := resp[0]["result"].(map[string]any)
	text := result["content"].([]any)[0].(map[string]any)["text"].(string)
	for _, want := range []string{"Community core", "3 members", "foo", "bar", "hub"} {
		if !strings.Contains(text, want) {
			t.Errorf("expected %q in get_community output, got %q", want, text)
		}
	}
}

func TestToolCallGetCommunityByMemberLabel(t *testing.T) {
	// Passing a node label/ID should also resolve to its community.
	resp := runOnce(t, sampleServer(), jsonRPCRequest(t, 1, "tools/call", map[string]any{
		"name":      "gogfy_get_community",
		"arguments": map[string]any{"id": "foo"},
	}))
	result := resp[0]["result"].(map[string]any)
	text := result["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "Community core") {
		t.Fatalf("member→community resolution failed: %s", text)
	}
}

func TestToolCallGetCommunityNotFound(t *testing.T) {
	resp := runOnce(t, sampleServer(), jsonRPCRequest(t, 1, "tools/call", map[string]any{
		"name":      "gogfy_get_community",
		"arguments": map[string]any{"id": "no-such-thing"},
	}))
	result := resp[0]["result"].(map[string]any)
	if !result["isError"].(bool) {
		t.Fatal("unknown community should return an error")
	}
}

func TestToolCallTraverseBFSReturnsHops(t *testing.T) {
	resp := runOnce(t, sampleServer(), jsonRPCRequest(t, 1, "tools/call", map[string]any{
		"name":      "gogfy_traverse",
		"arguments": map[string]any{"id": "foo", "depth": 2},
	}))
	result := resp[0]["result"].(map[string]any)
	text := result["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "Hop 0") {
		t.Errorf("missing hop-0 source label: %s", text)
	}
	if !strings.Contains(text, "Hop 1") {
		t.Errorf("expected hop-1 layer (foo → hub): %s", text)
	}
	// foo → hub → bar should make bar reachable at hop 2.
	if !strings.Contains(text, "bar") {
		t.Errorf("expected 'bar' at hop 2 via hub: %s", text)
	}
}

func TestToolCallTraverseRespectsLimit(t *testing.T) {
	resp := runOnce(t, sampleServer(), jsonRPCRequest(t, 1, "tools/call", map[string]any{
		"name":      "gogfy_traverse",
		"arguments": map[string]any{"id": "foo", "depth": 5, "limit": 2},
	}))
	result := resp[0]["result"].(map[string]any)
	text := result["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "2 nodes within") {
		t.Fatalf("limit not honored, expected 2-node cap in summary: %s", text)
	}
}

func TestToolCallTraverseMissingID(t *testing.T) {
	resp := runOnce(t, sampleServer(), jsonRPCRequest(t, 1, "tools/call", map[string]any{
		"name":      "gogfy_traverse",
		"arguments": map[string]any{},
	}))
	result := resp[0]["result"].(map[string]any)
	if !result["isError"].(bool) {
		t.Fatal("missing id should error")
	}
}

func TestToolCallQueryRanksExactLabelMatchFirst(t *testing.T) {
	// Three nodes whose labels all contain "foo" in some form. Exact
	// match must rank above prefix; prefix above contains.
	srv := New(export.GraphExport{
		Nodes: []schema.Node{
			{ID: "n1", Label: "foobar"},   // prefix match
			{ID: "n2", Label: "foo"},      // exact match — should win
			{ID: "n3", Label: "myfoo"},    // contains-only match
		},
	}, nil)
	resp := runOnce(t, srv, jsonRPCRequest(t, 1, "tools/call", map[string]any{
		"name":      "gogfy_query",
		"arguments": map[string]any{"text": "foo"},
	}))
	result := resp[0]["result"].(map[string]any)
	text := result["content"].([]any)[0].(map[string]any)["text"].(string)
	// Exact match "foo" must appear before "foobar" must appear before "myfoo".
	exactIdx := strings.Index(text, "foo (n2)")
	prefixIdx := strings.Index(text, "foobar")
	containsIdx := strings.Index(text, "myfoo")
	if exactIdx < 0 || prefixIdx < 0 || containsIdx < 0 {
		t.Fatalf("missing one of the matches: %s", text)
	}
	if !(exactIdx < prefixIdx && prefixIdx < containsIdx) {
		t.Fatalf("ranking broken: exact=%d prefix=%d contains=%d\n%s", exactIdx, prefixIdx, containsIdx, text)
	}
}

func TestToolCallQueryDegreeTiebreaker(t *testing.T) {
	// Two nodes with identical exact-label match — the more-connected
	// one should rank first.
	srv := New(export.GraphExport{
		Nodes: []schema.Node{
			{ID: "popular", Label: "auth"},
			{ID: "obscure", Label: "auth"},
			{ID: "x1", Label: "x1"}, {ID: "x2", Label: "x2"}, {ID: "x3", Label: "x3"},
		},
		Edges: []schema.Edge{
			{Source: "popular", Target: "x1"},
			{Source: "popular", Target: "x2"},
			{Source: "popular", Target: "x3"},
		},
	}, nil)
	resp := runOnce(t, srv, jsonRPCRequest(t, 1, "tools/call", map[string]any{
		"name":      "gogfy_query",
		"arguments": map[string]any{"text": "auth"},
	}))
	text := resp[0]["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	popularIdx := strings.Index(text, "(popular)")
	obscureIdx := strings.Index(text, "(obscure)")
	if popularIdx < 0 || obscureIdx < 0 {
		t.Fatalf("missing matches: %s", text)
	}
	if popularIdx > obscureIdx {
		t.Fatalf("degree tiebreak broken — popular (3 edges) should rank above obscure (0 edges): %s", text)
	}
}

func TestToolCallQuerySourceFileMatchRanksLowest(t *testing.T) {
	srv := New(export.GraphExport{
		Nodes: []schema.Node{
			{ID: "exact", Label: "auth"},
			{ID: "src", Label: "Unrelated", SourceFile: "auth/handler.go"},
		},
	}, nil)
	resp := runOnce(t, srv, jsonRPCRequest(t, 1, "tools/call", map[string]any{
		"name":      "gogfy_query",
		"arguments": map[string]any{"text": "auth"},
	}))
	text := resp[0]["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	exactIdx := strings.Index(text, "(exact)")
	srcIdx := strings.Index(text, "(src)")
	if !(exactIdx < srcIdx) {
		t.Fatalf("source-file-only match should rank last: exact=%d src=%d\n%s", exactIdx, srcIdx, text)
	}
}

// impactServer builds a graph with a transitive dependency chain plus a
// non-dependency `contains` edge, for exercising gogfy_impact.
//
//	a --calls--> b --calls--> c        (c's transitive dependents: b hop1, a hop2)
//	file --contains--> c               (structural; must NOT count as a dependent)
//	x --imports--> c                   (c's direct dependent via imports)
func impactServer() *Server {
	return New(export.GraphExport{
		Nodes: []schema.Node{
			{ID: "a", Label: "a"},
			{ID: "b", Label: "b"},
			{ID: "c", Label: "c"},
			{ID: "x", Label: "x"},
			{ID: "file", Label: "file"},
		},
		Edges: []schema.Edge{
			{Source: "a", Target: "b", Relation: "calls", Confidence: schema.Inferred},
			{Source: "b", Target: "c", Relation: "calls", Confidence: schema.Inferred},
			{Source: "x", Target: "c", Relation: "imports", Confidence: schema.Inferred},
			{Source: "file", Target: "c", Relation: "contains", Confidence: schema.Extracted},
		},
	}, nil)
}

func impactText(t *testing.T, srv *Server, args map[string]any) string {
	t.Helper()
	resp := runOnce(t, srv, jsonRPCRequest(t, 1, "tools/call", map[string]any{
		"name":      "gogfy_impact",
		"arguments": args,
	}))
	return resp[0]["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
}

func TestToolCallImpactTransitiveDependents(t *testing.T) {
	text := impactText(t, impactServer(), map[string]any{"id": "c"})
	// b (hop1, via calls) and x (hop1, via imports) are direct dependents;
	// a is a hop-2 dependent via b. file (contains) must be excluded.
	for _, want := range []string{"b", "x", "a"} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected dependent %q in impact output, got:\n%s", want, text)
		}
	}
	if strings.Contains(text, "file") {
		t.Fatalf("structural `contains` edge must not count as a dependent, got:\n%s", text)
	}
	if !strings.Contains(text, "Direct dependents") || !strings.Contains(text, "Hop 2") {
		t.Fatalf("expected hop grouping in output, got:\n%s", text)
	}
}

func TestToolCallImpactDepthLimitsHops(t *testing.T) {
	// depth=1 keeps only direct dependents (b, x) — a (hop 2) is excluded.
	text := impactText(t, impactServer(), map[string]any{"id": "c", "depth": 1})
	if !strings.Contains(text, "b") || !strings.Contains(text, "x") {
		t.Fatalf("expected direct dependents b and x, got:\n%s", text)
	}
	if strings.Contains(text, "Hop 2") {
		t.Fatalf("depth=1 must not include hop 2, got:\n%s", text)
	}
}

func TestToolCallImpactRelationFilter(t *testing.T) {
	// Filtering to imports surfaces only x; the calls-chain (b, a) is excluded.
	text := impactText(t, impactServer(), map[string]any{"id": "c", "relation": "imports"})
	if !strings.Contains(text, "x") {
		t.Fatalf("expected imports dependent x, got:\n%s", text)
	}
	if strings.Contains(text, "via calls") {
		t.Fatalf("relation=imports must not follow calls edges, got:\n%s", text)
	}
}

func TestToolCallImpactNothingDepends(t *testing.T) {
	// `a` is the top of the chain — nothing depends on it.
	text := impactText(t, impactServer(), map[string]any{"id": "a"})
	if !strings.Contains(text, "Nothing depends") {
		t.Fatalf("expected 'Nothing depends' for leaf-of-reverse-graph, got:\n%s", text)
	}
}

func TestToolCallImpactMissingID(t *testing.T) {
	text := impactText(t, impactServer(), map[string]any{})
	if !strings.Contains(text, "requires an `id`") {
		t.Fatalf("expected missing-id error, got: %q", text)
	}
}

func TestToolsListIncludesImpact(t *testing.T) {
	resp := runOnce(t, sampleServer(), jsonRPCRequest(t, 1, "tools/list", nil))
	tools := resp[0]["result"].(map[string]any)["tools"].([]any)
	found := false
	for _, tool := range tools {
		if tool.(map[string]any)["name"].(string) == "gogfy_impact" {
			found = true
		}
	}
	if !found {
		t.Fatal("gogfy_impact missing from tools/list")
	}
}
