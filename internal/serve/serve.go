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
	ToolGodNodes     = "gogfy_god_nodes"
	ToolExplain      = "gogfy_explain"
	ToolQuery        = "gogfy_query"
	ToolPath         = "gogfy_path"
	ToolGetNeighbors = "gogfy_get_neighbors"
	ToolGraphStats   = "gogfy_graph_stats"
	ToolGetCommunity = "gogfy_get_community"
	ToolTraverse     = "gogfy_traverse"
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
	case ToolPath:
		return jsonRPCResult(req.ID, s.callPath(p.Arguments))
	case ToolGetNeighbors:
		return jsonRPCResult(req.ID, s.callGetNeighbors(p.Arguments))
	case ToolGraphStats:
		return jsonRPCResult(req.ID, s.callGraphStats(p.Arguments))
	case ToolGetCommunity:
		return jsonRPCResult(req.ID, s.callGetCommunity(p.Arguments))
	case ToolTraverse:
		return jsonRPCResult(req.ID, s.callTraverse(p.Arguments))
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

func (s *Server) callPath(args json.RawMessage) map[string]any {
	var p struct {
		Source string `json:"source"`
		Target string `json:"target"`
	}
	if err := json.Unmarshal(args, &p); err != nil || p.Source == "" || p.Target == "" {
		return toolResult("path requires `source` and `target` arguments (node IDs or labels)", true)
	}
	src, _, ok := s.findNode(p.Source)
	if !ok {
		return toolResult(fmt.Sprintf("source not found: %q", p.Source), true)
	}
	tgt, _, ok := s.findNode(p.Target)
	if !ok {
		return toolResult(fmt.Sprintf("target not found: %q", p.Target), true)
	}
	path := s.shortestPath(src.ID, tgt.ID)
	if len(path) == 0 {
		return toolResult(fmt.Sprintf("no path from %q to %q", src.Label, tgt.Label), false)
	}
	var b strings.Builder
	for i, id := range path {
		fmt.Fprintf(&b, "%d. %s (%s)\n", i+1, s.labelFor(id), id)
	}
	return toolResult(b.String(), false)
}

// callGetNeighbors returns a node's direct neighbors grouped by
// relation and direction. Cheaper than explain when the caller only
// wants the connection list (no community/source-file frontmatter).
func (s *Server) callGetNeighbors(args json.RawMessage) map[string]any {
	var p struct {
		ID       string `json:"id"`
		Relation string `json:"relation"`
	}
	if err := json.Unmarshal(args, &p); err != nil || p.ID == "" {
		return toolResult("get_neighbors requires an `id` argument (node ID or label)", true)
	}
	target, _, ok := s.findNode(p.ID)
	if !ok {
		return toolResult(fmt.Sprintf("no node matched %q", p.ID), true)
	}
	out := s.outEdges[target.ID]
	in := s.inEdges[target.ID]
	// Group by relation so the agent sees a clean per-relation list
	// instead of a flat blob. Sorted keys keep output byte-stable.
	type bucket struct {
		out []schema.Edge
		in  []schema.Edge
	}
	by := map[string]*bucket{}
	for _, e := range out {
		if p.Relation != "" && e.Relation != p.Relation {
			continue
		}
		if by[e.Relation] == nil {
			by[e.Relation] = &bucket{}
		}
		by[e.Relation].out = append(by[e.Relation].out, e)
	}
	for _, e := range in {
		if p.Relation != "" && e.Relation != p.Relation {
			continue
		}
		if by[e.Relation] == nil {
			by[e.Relation] = &bucket{}
		}
		by[e.Relation].in = append(by[e.Relation].in, e)
	}
	if len(by) == 0 {
		return toolResult(fmt.Sprintf("%s has no neighbors%s", target.Label, relationFilterSuffix(p.Relation)), false)
	}
	relations := make([]string, 0, len(by))
	for r := range by {
		relations = append(relations, r)
	}
	sort.Strings(relations)
	var b strings.Builder
	fmt.Fprintf(&b, "## %s neighbors\n", target.Label)
	for _, r := range relations {
		fmt.Fprintf(&b, "\n### %s\n", r)
		for _, e := range by[r].out {
			fmt.Fprintf(&b, "- → %s\n", s.labelFor(e.Target))
		}
		for _, e := range by[r].in {
			fmt.Fprintf(&b, "- ← %s\n", s.labelFor(e.Source))
		}
	}
	return toolResult(b.String(), false)
}

func relationFilterSuffix(rel string) string {
	if rel == "" {
		return ""
	}
	return " for relation " + rel
}

// callGetCommunity lists a community's members and its cross-community
// edges. Accepts either a community ID or a member node's ID/label so
// agents don't have to maintain their own member-→-community map.
func (s *Server) callGetCommunity(args json.RawMessage) map[string]any {
	var p struct {
		ID    string `json:"id"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &p); err != nil || p.ID == "" {
		return toolResult("get_community requires an `id` argument (community ID or member node ID/label)", true)
	}
	if p.Limit <= 0 {
		p.Limit = 50
	}

	// Resolve `p.ID` to a community ID. Direct community match wins;
	// otherwise treat it as a node lookup and pull the node's community.
	communityID := ""
	for _, n := range s.graph.Nodes {
		if n.Community == p.ID {
			communityID = p.ID
			break
		}
	}
	if communityID == "" {
		if n, _, ok := s.findNode(p.ID); ok && n.Community != "" {
			communityID = n.Community
		}
	}
	if communityID == "" {
		return toolResult(fmt.Sprintf("no community matched %q (tried community ID, then member ID/label)", p.ID), true)
	}

	var members []schema.Node
	for _, n := range s.graph.Nodes {
		if n.Community == communityID {
			members = append(members, n)
		}
	}
	// Sort by label so the digest is reproducible run-to-run.
	sort.Slice(members, func(i, j int) bool {
		li := members[i].Label
		if li == "" {
			li = members[i].ID
		}
		lj := members[j].Label
		if lj == "" {
			lj = members[j].ID
		}
		return li < lj
	})

	// Cross-community link counts: by other-community ID, summed across
	// edges. Drops intra-community + dangling-target edges.
	cross := map[string]int{}
	memberSet := make(map[string]struct{}, len(members))
	for _, m := range members {
		memberSet[m.ID] = struct{}{}
	}
	for _, e := range s.graph.Edges {
		_, srcIn := memberSet[e.Source]
		_, dstIn := memberSet[e.Target]
		if srcIn == dstIn {
			// Both inside (intra) or both outside (no relevance).
			continue
		}
		otherID := e.Source
		if srcIn {
			otherID = e.Target
		}
		otherNode, ok := s.nodesByID[otherID]
		if !ok || otherNode.Community == "" || otherNode.Community == communityID {
			continue
		}
		cross[otherNode.Community]++
	}

	var b strings.Builder
	fmt.Fprintf(&b, "## Community %s (%d members)\n", communityID, len(members))
	for i, m := range members {
		if i >= p.Limit {
			fmt.Fprintf(&b, "- _… %d more_\n", len(members)-p.Limit)
			break
		}
		label := m.Label
		if label == "" {
			label = m.ID
		}
		fmt.Fprintf(&b, "- %s\n", label)
	}
	if len(cross) > 0 {
		// Highest edge count first; ties broken by other community ID
		// so output diffs cleanly.
		type pair struct {
			cid   string
			count int
		}
		ps := make([]pair, 0, len(cross))
		for cid, n := range cross {
			ps = append(ps, pair{cid, n})
		}
		sort.Slice(ps, func(i, j int) bool {
			if ps[i].count != ps[j].count {
				return ps[i].count > ps[j].count
			}
			return ps[i].cid < ps[j].cid
		})
		fmt.Fprintf(&b, "\n### Connections to other communities\n")
		for _, p := range ps {
			plural := "edges"
			if p.count == 1 {
				plural = "edge"
			}
			fmt.Fprintf(&b, "- Community %s: %d %s\n", p.cid, p.count, plural)
		}
	}
	return toolResult(b.String(), false)
}

// callTraverse runs a BFS from the named source up to `depth` hops or
// `limit` total nodes, whichever runs out first. Returns the visited
// subgraph grouped by hop distance so an agent can read "things 1 hop
// out, things 2 hops out, …" without separately querying each
// neighbor. Treats edges as undirected — direction is an extractor
// implementation detail and the user wants local context regardless.
func (s *Server) callTraverse(args json.RawMessage) map[string]any {
	var p struct {
		ID    string `json:"id"`
		Depth int    `json:"depth"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &p); err != nil || p.ID == "" {
		return toolResult("traverse requires an `id` argument (node ID or label)", true)
	}
	if p.Depth <= 0 {
		p.Depth = 2
	}
	if p.Limit <= 0 {
		p.Limit = 50
	}
	source, _, ok := s.findNode(p.ID)
	if !ok {
		return toolResult(fmt.Sprintf("no node matched %q", p.ID), true)
	}

	// Distance-keyed visit set. dist[source]=0; other entries assigned
	// on first visit. Capped at `limit` total so a query against a hub
	// node can't blow up the response.
	dist := map[string]int{source.ID: 0}
	queue := []string{source.ID}
	for len(queue) > 0 && len(dist) < p.Limit {
		cur := queue[0]
		queue = queue[1:]
		d := dist[cur]
		if d >= p.Depth {
			continue
		}
		// Undirected: walk both out- and in-edges.
		for _, e := range s.outEdges[cur] {
			if _, seen := dist[e.Target]; seen {
				continue
			}
			dist[e.Target] = d + 1
			queue = append(queue, e.Target)
			if len(dist) >= p.Limit {
				break
			}
		}
		for _, e := range s.inEdges[cur] {
			if _, seen := dist[e.Source]; seen {
				continue
			}
			dist[e.Source] = d + 1
			queue = append(queue, e.Source)
			if len(dist) >= p.Limit {
				break
			}
		}
	}

	// Group results by hop distance, sort within each layer by label.
	byHop := map[int][]string{}
	for id, d := range dist {
		byHop[d] = append(byHop[d], id)
	}
	for d := range byHop {
		layer := byHop[d]
		sort.Slice(layer, func(i, j int) bool {
			return s.labelFor(layer[i]) < s.labelFor(layer[j])
		})
		byHop[d] = layer
	}
	hops := make([]int, 0, len(byHop))
	for d := range byHop {
		hops = append(hops, d)
	}
	sort.Ints(hops)

	var b strings.Builder
	fmt.Fprintf(&b, "## Traverse from %s\n", source.Label)
	fmt.Fprintf(&b, "- %d nodes within %d hops (limit: %d)\n", len(dist), p.Depth, p.Limit)
	for _, d := range hops {
		layer := byHop[d]
		switch d {
		case 0:
			fmt.Fprintf(&b, "\n### Hop 0 (source)\n")
		default:
			fmt.Fprintf(&b, "\n### Hop %d (%d nodes)\n", d, len(layer))
		}
		for _, id := range layer {
			fmt.Fprintf(&b, "- %s\n", s.labelFor(id))
		}
	}
	return toolResult(b.String(), false)
}

// callGraphStats surfaces orientation-level numbers an agent can use
// to decide where to drill in: node/edge/community counts plus
// file-type and confidence breakdowns.
func (s *Server) callGraphStats(_ json.RawMessage) map[string]any {
	communities := map[string]struct{}{}
	fileTypes := map[schema.FileType]int{}
	confidence := map[schema.Confidence]int{}
	for _, n := range s.graph.Nodes {
		if n.Community != "" {
			communities[n.Community] = struct{}{}
		}
		if n.FileType != "" {
			fileTypes[n.FileType]++
		}
	}
	for _, e := range s.graph.Edges {
		confidence[e.Confidence]++
	}
	var b strings.Builder
	fmt.Fprintf(&b, "## Graph stats\n")
	fmt.Fprintf(&b, "- %d nodes\n", len(s.graph.Nodes))
	fmt.Fprintf(&b, "- %d edges\n", len(s.graph.Edges))
	fmt.Fprintf(&b, "- %d communities\n", len(communities))
	if len(fileTypes) > 0 {
		kinds := make([]schema.FileType, 0, len(fileTypes))
		for k := range fileTypes {
			kinds = append(kinds, k)
		}
		sort.Slice(kinds, func(i, j int) bool { return string(kinds[i]) < string(kinds[j]) })
		fmt.Fprintf(&b, "- File types:")
		for _, k := range kinds {
			fmt.Fprintf(&b, " %d %s,", fileTypes[k], k)
		}
		// Trim trailing comma.
		out := b.String()
		out = strings.TrimSuffix(out, ",")
		b.Reset()
		b.WriteString(out)
		b.WriteByte('\n')
	}
	if len(confidence) > 0 {
		fmt.Fprintf(&b, "- Confidence:")
		for _, c := range []schema.Confidence{schema.Extracted, schema.Inferred, schema.Ambiguous} {
			if n := confidence[c]; n > 0 {
				fmt.Fprintf(&b, " %d %s,", n, c)
			}
		}
		out := b.String()
		out = strings.TrimSuffix(out, ",")
		b.Reset()
		b.WriteString(out)
		b.WriteByte('\n')
	}
	return toolResult(b.String(), false)
}

// FindNode resolves a query (node ID or label) to a node, returning the
// resolved node, all label-collision candidates, and whether anything
// matched. Exported so the CLI `gogfy path` subcommand can reuse the
// indexed lookup.
func (s *Server) FindNode(q string) (schema.Node, []string, bool) {
	return s.findNode(q)
}

// ShortestPath returns the IDs along the shortest undirected path from
// source to target (inclusive). Exported wrapper around shortestPath.
func (s *Server) ShortestPath(source, target string) []string {
	return s.shortestPath(source, target)
}

// LabelFor returns the label of the node with the given ID, or the ID
// itself when not found. Exported wrapper around labelFor.
func (s *Server) LabelFor(id string) string {
	return s.labelFor(id)
}

// shortestPath returns the IDs of nodes along the shortest undirected path
// from source to target (inclusive). Empty slice means unreachable.
//
// Treats edges as undirected because graph connectivity is what callers
// actually want — `imports` and `calls` directions are an implementation
// detail of the extractor pass, not of "is A related to B".
func (s *Server) shortestPath(source, target string) []string {
	if source == target {
		return []string{source}
	}
	prev := map[string]string{source: ""}
	queue := []string{source}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, neighbor := range s.neighborIDs(cur) {
			if _, seen := prev[neighbor]; seen {
				continue
			}
			prev[neighbor] = cur
			if neighbor == target {
				// Reconstruct path back to source.
				var path []string
				for n := target; n != ""; n = prev[n] {
					path = append([]string{n}, path...)
				}
				return path
			}
			queue = append(queue, neighbor)
		}
	}
	return nil
}

// neighborIDs returns the unique neighbor node IDs of id (both directions).
func (s *Server) neighborIDs(id string) []string {
	seen := map[string]bool{}
	for _, e := range s.outEdges[id] {
		seen[e.Target] = true
	}
	for _, e := range s.inEdges[id] {
		seen[e.Source] = true
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out) // deterministic BFS order
	return out
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
	// Score matches by relevance so the right node ranks first instead
	// of relying on graph-iteration order. Buckets are intentionally
	// spaced so a higher tier always outranks a lower one even when the
	// lower tier accumulates a high degree tie-break bonus.
	type scored struct {
		node  schema.Node
		score int
	}
	var matches []scored
	degree := s.degreeMap()
	for _, n := range s.graph.Nodes {
		score := matchScore(needle, n)
		if score == 0 {
			continue
		}
		// Tie-breaker: popular nodes outrank obscure ones with the same
		// match quality. Capped so degree can't flip tiers.
		bonus := degree[n.ID]
		if bonus > 9 {
			bonus = 9
		}
		matches = append(matches, scored{node: n, score: score*10 + bonus})
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].score != matches[j].score {
			return matches[i].score > matches[j].score
		}
		return matches[i].node.ID < matches[j].node.ID
	})
	if len(matches) > limit {
		matches = matches[:limit]
	}
	var b strings.Builder
	for _, m := range matches {
		fmt.Fprintf(&b, "- %s (%s)\n", m.node.Label, m.node.ID)
	}
	if b.Len() == 0 {
		fmt.Fprintf(&b, "(no labels matched %q)\n", p.Text)
	}
	return toolResult(b.String(), false)
}

// matchScore returns 0 for non-matches; higher is more relevant.
// Exact label match (case-insensitive) gets the top tier so renaming a
// function "auth" surfaces it ahead of nodes whose label merely contains
// "auth" as a substring. ID and source-file matches rank below label
// matches since agents typically look by name first.
func matchScore(needle string, n schema.Node) int {
	label := strings.ToLower(n.Label)
	switch {
	case label == needle:
		return 100
	case strings.HasPrefix(label, needle):
		return 50
	case strings.Contains(label, needle):
		return 25
	}
	if strings.Contains(strings.ToLower(n.ID), needle) {
		return 15
	}
	if strings.Contains(strings.ToLower(n.SourceFile), needle) {
		return 10
	}
	return 0
}

// degreeMap counts incident edges per node for the tie-breaker bonus.
// Built lazily here (rather than indexed once in New) because query is
// expected to be infrequent relative to explain/get_neighbors — keeping
// the Server zero-cost on init still wins more than caching saves.
func (s *Server) degreeMap() map[string]int {
	d := make(map[string]int, len(s.graph.Nodes))
	for _, e := range s.graph.Edges {
		d[e.Source]++
		d[e.Target]++
	}
	return d
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
		"name":        ToolPath,
		"description": "Find the shortest connectivity path between two nodes (treats edges as undirected — direction is an extractor implementation detail).",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"source": map[string]any{"type": "string", "description": "Start node ID or label."},
				"target": map[string]any{"type": "string", "description": "End node ID or label."},
			},
			"required": []any{"source", "target"},
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
	{
		"name":        ToolGetNeighbors,
		"description": "List a node's neighbors grouped by relation and direction. Cheaper than 'explain' when only the connection list is needed.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":       map[string]any{"type": "string", "description": "Node ID or label."},
				"relation": map[string]any{"type": "string", "description": "Optional: filter to a specific relation (e.g. 'calls', 'imports')."},
			},
			"required": []any{"id"},
		},
	},
	{
		"name":        ToolGraphStats,
		"description": "Return high-level graph statistics: node count, edge count, community count, file-type breakdown, confidence breakdown. Useful as a first-look orientation before drilling in.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{},
		},
	},
	{
		"name":        ToolGetCommunity,
		"description": "List members of a community plus its connections to other communities. Pass the community ID or a member's node ID/label — both resolve to the same community.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":    map[string]any{"type": "string", "description": "Community ID, or a node ID/label whose community to inspect."},
				"limit": map[string]any{"type": "integer", "description": "Max members to list (default: 50). Cross-community link list isn't capped."},
			},
			"required": []any{"id"},
		},
	},
	{
		"name":        ToolTraverse,
		"description": "BFS from a starting node up to N hops, returning the visited subgraph grouped by hop distance. Use when you need a node's local context (callers + callees, n levels deep) without pulling in the whole graph. The node-budget caps the result so you can constrain context size.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":     map[string]any{"type": "string", "description": "Starting node ID or label."},
				"depth":  map[string]any{"type": "integer", "description": "Max BFS hops from the source (default: 2). Each hop adds a layer of neighbors."},
				"limit":  map[string]any{"type": "integer", "description": "Max nodes total in the result (default: 50). BFS stops when reached so you stay within a context budget."},
			},
			"required": []any{"id"},
		},
	},
}
