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
	"bytes"
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

// protocolVersion is the MCP revision we advertise on `initialize`. The
// 2024-11-05 revision is the broadly-deployed baseline; bumping is a
// one-line change here when client compatibility allows.
const protocolVersion = "2024-11-05"

// Tool names exposed via tools/list. Exported so other packages (e.g.
// internal/installer's snippet writer) can reference them by const rather
// than risking drift if a tool is renamed.
const (
	ToolGodNodes = "gogfy_god_nodes"
	ToolExplain  = "gogfy_explain"
	ToolQuery    = "gogfy_query"
)

// Server holds the in-memory graph + report bytes the MCP tools read from.
//
// Indices (nodesByID, labelMatches, outEdges, inEdges) and the cached
// analyze report are built once in New and never mutated, so all per-request
// lookups stay O(1) / O(deg).
type Server struct {
	graph        export.GraphExport
	report       []byte
	nodesByID    map[string]schema.Node
	labelMatches map[string][]string // label → [nodeIDs]; multiple ids = collision
	outEdges     map[string][]schema.Edge
	inEdges      map[string][]schema.Edge
	analyzed     analyze.Report
}

// New constructs a Server seeded with a graph snapshot and the rendered
// GRAPH_REPORT.md. Both are read-only for the lifetime of the server.
func New(graph export.GraphExport, report []byte) *Server {
	s := &Server{
		graph:        graph,
		report:       report,
		nodesByID:    make(map[string]schema.Node, len(graph.Nodes)),
		labelMatches: make(map[string][]string),
		outEdges:     make(map[string][]schema.Edge),
		inEdges:      make(map[string][]schema.Edge),
	}
	for _, n := range graph.Nodes {
		s.nodesByID[n.ID] = n
		s.labelMatches[n.Label] = append(s.labelMatches[n.Label], n.ID)
	}
	for _, e := range graph.Edges {
		s.outEdges[e.Source] = append(s.outEdges[e.Source], e)
		s.inEdges[e.Target] = append(s.inEdges[e.Target], e)
	}
	s.analyzed = analyze.NewAnalyzer().Analyze(graph.Nodes, graph.Edges)
	return s
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
		if len(bytes.TrimSpace(line)) == 0 {
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
		return jsonRPCResult(req.ID, map[string]any{"tools": toolDescriptors}), true
	case "tools/call":
		return s.toolsCall(req), true
	case "resources/list":
		return jsonRPCResult(req.ID, map[string]any{"resources": resourceDescriptors}), true
	case "resources/read":
		return s.resourcesRead(req), true
	default:
		return jsonRPCError(req.ID, -32601, "method not found: "+req.Method), true
	}
}

func (s *Server) initializeResult() map[string]any {
	return map[string]any{
		"protocolVersion": protocolVersion,
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
	case ToolGodNodes:
		return jsonRPCResult(req.ID, s.callGodNodes(p.Arguments))
	case ToolExplain:
		return jsonRPCResult(req.ID, s.callExplain(p.Arguments))
	case ToolQuery:
		return jsonRPCResult(req.ID, s.callQuery(p.Arguments))
	default:
		return jsonRPCError(req.ID, -32602, "unknown tool: "+p.Name)
	}
}

func (s *Server) callGodNodes(args json.RawMessage) map[string]any {
	var p struct {
		Limit int `json:"limit"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &p); err != nil {
			return toolResult("invalid arguments: "+err.Error(), true)
		}
	}
	gods := s.analyzed.GodNodes
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
	target, candidates, ok := s.findNode(p.ID)
	if !ok {
		return toolResult(fmt.Sprintf("no node matched %q (tried ID and label)", p.ID), true)
	}
	if len(candidates) > 1 {
		// Label collision: multiple nodes share this label. Surface the
		// alternatives so the agent can disambiguate by ID on a follow-up.
		var b strings.Builder
		fmt.Fprintf(&b, "label %q matches %d nodes; showing the first. Disambiguate by passing the full ID:\n", p.ID, len(candidates))
		for _, id := range candidates {
			fmt.Fprintf(&b, "- %s\n", id)
		}
		b.WriteString("\n")
		head := b.String()
		body := s.explainBody(target)
		return toolResult(head+body, false)
	}
	return toolResult(s.explainBody(target), false)
}

func (s *Server) explainBody(target schema.Node) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## %s\n", target.Label)
	fmt.Fprintf(&b, "- ID: %s\n", target.ID)
	if target.SourceFile != "" {
		fmt.Fprintf(&b, "- File: %s\n", target.SourceFile)
	}
	if target.Community != "" {
		fmt.Fprintf(&b, "- Community: %s\n", target.Community)
	}
	outgoing := s.outEdges[target.ID]
	incoming := s.inEdges[target.ID]
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
	return b.String()
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
		// Search across label, ID, and source file: agents reasonably expect
		// to find a function by its name, its fully-qualified ID, or the
		// file it lives in. All three are cheap on the same loop.
		if strings.Contains(strings.ToLower(n.Label), needle) ||
			strings.Contains(strings.ToLower(n.ID), needle) ||
			strings.Contains(strings.ToLower(n.SourceFile), needle) {
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

// findNode resolves a query string to a node, accepting either an exact ID
// or a label. Returns the picked node, the full list of label-collision
// candidates (for caller-side disambiguation), and whether anything matched.
// ID match always wins and returns a one-element candidates slice.
func (s *Server) findNode(q string) (schema.Node, []string, bool) {
	if n, ok := s.nodesByID[q]; ok {
		return n, []string{q}, true
	}
	if ids, ok := s.labelMatches[q]; ok && len(ids) > 0 {
		return s.nodesByID[ids[0]], ids, true
	}
	return schema.Node{}, nil, false
}

func (s *Server) labelFor(id string) string {
	if n, ok := s.nodesByID[id]; ok {
		return n.Label
	}
	return id
}

// resourceDescriptors is the static resources/list payload.
var resourceDescriptors = []map[string]any{
	{
		"uri":         "gogfy://report",
		"name":        "GRAPH_REPORT.md",
		"description": "Markdown report: god nodes, surprising connections, confidence summary, exploration questions.",
		"mimeType":    "text/markdown",
	},
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
var toolDescriptors = []map[string]any{
	{
		"name":        ToolGodNodes,
		"description": "List the most-connected nodes in the graph (the project's hubs).",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"limit": map[string]any{"type": "integer", "description": "Max nodes to return (default: all)."},
			},
		},
	},
	{
		"name":        ToolExplain,
		"description": "Show a node's metadata plus its incoming and outgoing edges. If the label collides across multiple nodes, the candidates are listed for ID-based disambiguation.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id": map[string]any{"type": "string", "description": "Node ID or label."},
			},
			"required": []any{"id"},
		},
	},
	{
		"name":        ToolQuery,
		"description": "Find nodes whose label, ID, or source file contains the given substring (case-insensitive).",
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
