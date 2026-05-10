// Package serve implements an MCP (Model Context Protocol) server over stdio.
//
// MCP speaks JSON-RPC 2.0 with newline-delimited messages. We hand-roll the
// minimum surface graphs need: initialize, tools/list, tools/call,
// resources/list, resources/read. Notifications produce no response.
//
// The server is read-only: it serves an in-memory snapshot of GraphExport plus
// the rendered GRAPH_REPORT.md bytes. Rebuilding the graph is out of scope —
// callers run `gogfy run` separately and re-launch the server (or watch mode
// keeps artifacts fresh).
package serve

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/julianshen/gogfy/internal/analyze"
	"github.com/julianshen/gogfy/internal/export"
	"github.com/julianshen/gogfy/internal/schema"
)

// Server holds the in-memory graph + report bytes the MCP tools read from.
type Server struct {
	graph  export.GraphExport
	report []byte
}

// New constructs a Server seeded with a graph snapshot and the rendered
// GRAPH_REPORT.md. Both are read-only for the lifetime of the server.
func New(graph export.GraphExport, report []byte) *Server {
	return &Server{graph: graph, report: report}
}

// Serve runs the JSON-RPC loop until in returns EOF or ctx is cancelled.
// Each newline-terminated line on in is one request or notification.
// Responses are written one-per-line on out.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	// MCP messages can be sizable (graph payloads); raise the line cap from
	// bufio's 64KiB default to 4MiB.
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		line := scanner.Bytes()
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		resp, ok := s.handle(line)
		if !ok {
			// Notification — no response per JSON-RPC 2.0.
			continue
		}
		if _, err := out.Write(append(resp, '\n')); err != nil {
			return err
		}
	}
	return scanner.Err()
}

// rpcRequest is the JSON-RPC 2.0 envelope. Notifications omit "id".
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// handle dispatches one request line. Returns (response-bytes, true) for a
// request and (nil, false) for a notification (which must not be answered).
func (s *Server) handle(line []byte) ([]byte, bool) {
	var req rpcRequest
	if err := json.Unmarshal(line, &req); err != nil {
		return jsonRPCError(nil, -32700, "parse error: "+err.Error()), true
	}
	if len(req.ID) == 0 {
		return nil, false
	}
	switch req.Method {
	case "initialize":
		return jsonRPCResult(req.ID, s.initializeResult()), true
	case "tools/list":
		return jsonRPCResult(req.ID, map[string]any{"tools": toolDescriptors()}), true
	case "tools/call":
		return s.toolsCall(req), true
	case "resources/list":
		return jsonRPCResult(req.ID, map[string]any{"resources": s.resourceDescriptors()}), true
	case "resources/read":
		return s.resourcesRead(req), true
	default:
		return jsonRPCError(req.ID, -32601, "method not found: "+req.Method), true
	}
}

func (s *Server) initializeResult() map[string]any {
	return map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities": map[string]any{
			"tools":     map[string]any{},
			"resources": map[string]any{},
		},
		"serverInfo": map[string]any{
			"name":    "gogfy",
			"version": "0.1.0",
		},
	}
}

// jsonRPCResult / jsonRPCError encode a response envelope. Marshal cannot fail
// for the values we feed it (plain maps + strings), so the error is dropped.
func jsonRPCResult(id json.RawMessage, result any) []byte {
	b, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	})
	return b
}

func jsonRPCError(id json.RawMessage, code int, message string) []byte {
	b, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error":   map[string]any{"code": code, "message": message},
	})
	return b
}

// toolResult wraps text content per the MCP tools/call response shape.
// isError=true signals a tool-side failure (vs. an RPC-level error which
// would use jsonRPCError instead).
func toolResult(text string, isError bool) map[string]any {
	return map[string]any{
		"content": []any{
			map[string]any{"type": "text", "text": text},
		},
		"isError": isError,
	}
}

// toolsCall routes a tools/call by tool name.
func (s *Server) toolsCall(req rpcRequest) []byte {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return jsonRPCError(req.ID, -32602, "invalid params: "+err.Error())
	}
	switch p.Name {
	case "gogfy_god_nodes":
		return jsonRPCResult(req.ID, s.callGodNodes(p.Arguments))
	case "gogfy_explain":
		return jsonRPCResult(req.ID, s.callExplain(p.Arguments))
	case "gogfy_query":
		return jsonRPCResult(req.ID, s.callQuery(p.Arguments))
	default:
		return jsonRPCError(req.ID, -32602, "unknown tool: "+p.Name)
	}
}

func (s *Server) callGodNodes(args json.RawMessage) map[string]any {
	var p struct {
		Limit int `json:"limit"`
	}
	_ = json.Unmarshal(args, &p)
	r := analyze.NewAnalyzer().Analyze(s.graph.Nodes, s.graph.Edges)
	gods := r.GodNodes
	if p.Limit > 0 && p.Limit < len(gods) {
		gods = gods[:p.Limit]
	}
	var b strings.Builder
	for _, n := range gods {
		fmt.Fprintf(&b, "- %s (%s)\n", n.Label, n.ID)
	}
	if b.Len() == 0 {
		b.WriteString("(no god nodes)\n")
	}
	return toolResult(b.String(), false)
}

func (s *Server) callExplain(args json.RawMessage) map[string]any {
	var p struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(args, &p); err != nil || p.ID == "" {
		return toolResult("explain requires an `id` argument (node ID or label)", true)
	}
	target, ok := s.findNode(p.ID)
	if !ok {
		return toolResult(fmt.Sprintf("no node matched %q (tried ID and label)", p.ID), true)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "## %s\n", target.Label)
	fmt.Fprintf(&b, "- ID: %s\n", target.ID)
	if target.SourceFile != "" {
		fmt.Fprintf(&b, "- File: %s\n", target.SourceFile)
	}
	if target.Community != "" {
		fmt.Fprintf(&b, "- Community: %s\n", target.Community)
	}
	outgoing, incoming := s.neighbors(target.ID)
	if len(outgoing) > 0 {
		fmt.Fprintf(&b, "\n### Outgoing\n")
		for _, e := range outgoing {
			fmt.Fprintf(&b, "- %s -> %s\n", e.Relation, s.labelFor(e.Target))
		}
	}
	if len(incoming) > 0 {
		fmt.Fprintf(&b, "\n### Incoming\n")
		for _, e := range incoming {
			fmt.Fprintf(&b, "- %s <- %s\n", e.Relation, s.labelFor(e.Source))
		}
	}
	return toolResult(b.String(), false)
}

func (s *Server) callQuery(args json.RawMessage) map[string]any {
	var p struct {
		Text  string `json:"text"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &p); err != nil || p.Text == "" {
		return toolResult("query requires a `text` argument", true)
	}
	needle := strings.ToLower(p.Text)
	limit := p.Limit
	if limit <= 0 {
		limit = 25
	}
	matches := []schema.Node{}
	for _, n := range s.graph.Nodes {
		if strings.Contains(strings.ToLower(n.Label), needle) {
			matches = append(matches, n)
			if len(matches) >= limit {
				break
			}
		}
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].ID < matches[j].ID })
	var b strings.Builder
	for _, n := range matches {
		fmt.Fprintf(&b, "- %s (%s)\n", n.Label, n.ID)
	}
	if b.Len() == 0 {
		fmt.Fprintf(&b, "(no labels matched %q)\n", p.Text)
	}
	return toolResult(b.String(), false)
}

// findNode resolves a query string to one node, accepting either an exact ID
// or a label. ID match wins; falls back to first label match.
func (s *Server) findNode(q string) (schema.Node, bool) {
	for _, n := range s.graph.Nodes {
		if n.ID == q {
			return n, true
		}
	}
	for _, n := range s.graph.Nodes {
		if n.Label == q {
			return n, true
		}
	}
	return schema.Node{}, false
}

func (s *Server) neighbors(id string) (out, in []schema.Edge) {
	for _, e := range s.graph.Edges {
		if e.Source == id {
			out = append(out, e)
		}
		if e.Target == id {
			in = append(in, e)
		}
	}
	return out, in
}

func (s *Server) labelFor(id string) string {
	for _, n := range s.graph.Nodes {
		if n.ID == id {
			return n.Label
		}
	}
	return id
}

func (s *Server) resourceDescriptors() []map[string]any {
	return []map[string]any{
		{
			"uri":         "gogfy://report",
			"name":        "GRAPH_REPORT.md",
			"description": "Markdown report: god nodes, surprising connections, confidence summary, exploration questions.",
			"mimeType":    "text/markdown",
		},
	}
}

func (s *Server) resourcesRead(req rpcRequest) []byte {
	var p struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return jsonRPCError(req.ID, -32602, "invalid params: "+err.Error())
	}
	if p.URI != "gogfy://report" {
		return jsonRPCError(req.ID, -32602, "unknown resource: "+p.URI)
	}
	return jsonRPCResult(req.ID, map[string]any{
		"contents": []any{
			map[string]any{
				"uri":      p.URI,
				"mimeType": "text/markdown",
				"text":     string(s.report),
			},
		},
	})
}

// toolDescriptors is the static tools/list payload. JSON Schemas are kept
// minimal — agents only need them to know argument names and required-ness.
func toolDescriptors() []map[string]any {
	return []map[string]any{
		{
			"name":        "gogfy_god_nodes",
			"description": "List the most-connected nodes in the graph (the project's hubs).",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"limit": map[string]any{"type": "integer", "description": "Max nodes to return (default: all)."},
				},
			},
		},
		{
			"name":        "gogfy_explain",
			"description": "Show a node's metadata plus its incoming and outgoing edges.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{"type": "string", "description": "Node ID or label."},
				},
				"required": []any{"id"},
			},
		},
		{
			"name":        "gogfy_query",
			"description": "Find nodes whose label contains the given substring (case-insensitive).",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"text":  map[string]any{"type": "string"},
					"limit": map[string]any{"type": "integer", "description": "Max matches to return (default: 25)."},
				},
				"required": []any{"text"},
			},
		},
	}
}
