package export

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/julianshen/gogfy/internal/schema"
)

func TestLoadJSONRoundTrip(t *testing.T) {
	g := GraphExport{
		Nodes: []schema.Node{{ID: "a", Label: "A", SourceFile: "a.go"}},
		Edges: []schema.Edge{{Source: "a", Target: "b", Relation: "calls"}},
	}
	b, err := ExportJSON(g)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "g.json")
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := LoadJSON(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Nodes) != 1 || got.Nodes[0].ID != "a" {
		t.Fatalf("round-trip Nodes mismatch: %+v", got.Nodes)
	}
	if len(got.Edges) != 1 || got.Edges[0].Relation != "calls" {
		t.Fatalf("round-trip Edges mismatch: %+v", got.Edges)
	}
}

func TestLoadJSONMissingFileReturnsErrNotExist(t *testing.T) {
	// Callers (globalgraph.Store.Load) match on errors.Is(err,
	// os.ErrNotExist) to fall back to an empty graph. Pin the
	// wrapping contract.
	_, err := LoadJSON(filepath.Join(t.TempDir(), "nope.json"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("error must wrap os.ErrNotExist for callers to detect missing files; got: %v", err)
	}
}

func TestLoadJSONMalformedJSONErrors(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(p, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadJSON(p)
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
	if !strings.Contains(err.Error(), "parse") {
		t.Fatalf("error should mention parse failure, got: %v", err)
	}
}

func TestExportJSON(t *testing.T) {
	g := GraphExport{
		Nodes: []schema.Node{{ID: "a", Label: "A", SourceFile: "a.go", Community: "1"}},
		Edges: []schema.Edge{{Source: "a", Target: "b", Relation: "calls", Confidence: schema.Extracted}},
	}
	data, err := ExportJSON(g)
	if err != nil {
		t.Fatal(err)
	}
	var decoded GraphExport
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(decoded.Nodes))
	}
	if decoded.Nodes[0].ID != "a" {
		t.Fatalf("expected node ID 'a', got %s", decoded.Nodes[0].ID)
	}
	if decoded.Nodes[0].Label != "A" {
		t.Fatalf("expected node Label 'A', got %s", decoded.Nodes[0].Label)
	}
	if len(decoded.Edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(decoded.Edges))
	}
	if decoded.Edges[0].Source != "a" || decoded.Edges[0].Target != "b" {
		t.Fatalf("expected edge a->b, got %s->%s", decoded.Edges[0].Source, decoded.Edges[0].Target)
	}
	if decoded.Edges[0].Confidence != schema.Extracted {
		t.Fatalf("expected confidence EXTRACTED, got %s", decoded.Edges[0].Confidence)
	}
}

func TestExportJSONEmpty(t *testing.T) {
	g := GraphExport{}
	data, err := ExportJSON(g)
	if err != nil {
		t.Fatal(err)
	}
	var decoded GraphExport
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Nodes) != 0 {
		t.Fatalf("expected 0 nodes, got %d", len(decoded.Nodes))
	}
	if len(decoded.Edges) != 0 {
		t.Fatalf("expected 0 edges, got %d", len(decoded.Edges))
	}
}

func TestExportHTMLEmbedsGraphPayload(t *testing.T) {
	g := GraphExport{
		Nodes: []schema.Node{{ID: "a", Label: "Alpha"}, {ID: "b", Label: "Beta"}},
		Edges: []schema.Edge{{Source: "a", Target: "b", Relation: "calls", Confidence: schema.Extracted}},
	}
	data, err := ExportHTML(g, HTMLOptions{})
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)
	if !strings.Contains(html, `"Alpha"`) || !strings.Contains(html, `"Beta"`) {
		t.Fatal("graph payload not embedded into the HTML template")
	}
	// Placeholder must have been substituted, otherwise viewer would crash on `null.nodes`.
	if strings.Contains(html, "/*__DATA__*/null") {
		t.Fatal("template placeholder was not substituted")
	}
}

func TestExportHTMLContainsInteractiveFeatures(t *testing.T) {
	data, err := ExportHTML(GraphExport{}, HTMLOptions{})
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)
	for _, want := range []string{
		`id="search"`,        // search input
		`id="communities"`,   // community legend
		`id="panel"`,         // click-to-inspect panel
		`<svg`,               // canvas
		`stroke-dasharray`,   // confidence-tagged edges (inferred/ambiguous styling)
		`addEventListener`,   // interactivity
	} {
		if !strings.Contains(html, want) {
			t.Errorf("interactive HTML missing expected feature: %q", want)
		}
	}
}

func TestExportHTMLEmptyGraphDoesNotCrashViewer(t *testing.T) {
	// Empty GraphExport{} has nil Nodes/Edges. Default json.Marshal encodes
	// them as `null`, which would make the viewer's `DATA.nodes.map(...)`
	// throw `TypeError: Cannot read properties of null` and the page would
	// fail to render at all. Output must contain `[]`, not `null`.
	data, err := ExportHTML(GraphExport{}, HTMLOptions{})
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)
	if strings.Contains(html, `"nodes":null`) || strings.Contains(html, `"edges":null`) {
		t.Fatalf("empty graph emitted as null arrays — viewer would crash on .map() of null")
	}
	if !strings.Contains(html, `"nodes":[]`) || !strings.Contains(html, `"edges":[]`) {
		t.Fatal("expected empty-array payload `\"nodes\":[]` and `\"edges\":[]`")
	}
}

func TestExportHTMLDirectedFlagAddsArrowMarker(t *testing.T) {
	g := GraphExport{
		Nodes: []schema.Node{{ID: "a", Label: "A"}, {ID: "b", Label: "B"}},
		Edges: []schema.Edge{{Source: "a", Target: "b", Relation: "calls", Confidence: schema.Extracted}},
	}
	off, err := ExportHTML(g, HTMLOptions{Directed: false})
	if err != nil {
		t.Fatal(err)
	}
	on, err := ExportHTML(g, HTMLOptions{Directed: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(on), "DIRECTED = true") {
		t.Fatalf("expected DIRECTED = true in output when opts.Directed=true")
	}
	if !strings.Contains(string(off), "DIRECTED = false") {
		t.Fatalf("expected DIRECTED = false default")
	}
}

func TestExportHTMLEscapesPayloadSafely(t *testing.T) {
	// Adversarial labels should not be able to break out of the JS string
	// literal that the JSON payload is interpolated into.
	g := GraphExport{
		Nodes: []schema.Node{{ID: "x", Label: "</script><script>alert(1)</script>"}},
	}
	data, err := ExportHTML(g, HTMLOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "</script><script>alert(1)</script>") {
		t.Fatal("hostile label appeared verbatim — JSON encoder must escape `</script>` sequences")
	}
}

func TestExportHTMLAggregatesLargeGraphIntoCommunityMetaGraph(t *testing.T) {
	// Above the node-count limit, the viewer collapses each community
	// into a single meta-node so the visualization stays navigable on
	// big repos. Edge counts between communities aggregate as weights.
	g := GraphExport{}
	for i := 0; i < 10; i++ {
		g.Nodes = append(g.Nodes, schema.Node{
			ID: fmt.Sprintf("a%d", i), Label: fmt.Sprintf("A%d", i), Community: "A",
		})
		g.Nodes = append(g.Nodes, schema.Node{
			ID: fmt.Sprintf("b%d", i), Label: fmt.Sprintf("B%d", i), Community: "B",
		})
	}
	// Two cross-community edges between A and B.
	g.Edges = []schema.Edge{
		{Source: "a0", Target: "b0", Relation: "calls"},
		{Source: "a1", Target: "b1", Relation: "calls"},
	}
	data, err := ExportHTML(g, HTMLOptions{MaxNodes: 5})
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)
	// Aggregated meta-graph should have ~2 community nodes, not 20.
	// Pin via the embedded JSON: the payload should NOT contain
	// every "A%d"/"B%d" label, since they're collapsed.
	if strings.Contains(html, `"A5"`) {
		t.Fatalf("aggregation didn't kick in — original labels still present")
	}
	if !strings.Contains(html, "Community A") || !strings.Contains(html, "Community B") {
		t.Fatalf("aggregated meta-nodes should be labeled by community: %s", excerpt(html, 200))
	}
}

func TestExportHTMLBelowLimitRendersFullGraph(t *testing.T) {
	g := GraphExport{
		Nodes: []schema.Node{{ID: "a", Label: "AliceLabel", Community: "1"}},
	}
	data, _ := ExportHTML(g, HTMLOptions{MaxNodes: 100})
	if !strings.Contains(string(data), "AliceLabel") {
		t.Fatal("small graph must render with original labels (no aggregation)")
	}
}

func TestExportHTMLDefaultMaxNodesDoesNotAggregateModerateGraphs(t *testing.T) {
	// Zero MaxNodes → default. A graph of 50 nodes is small enough
	// that no aggregation should fire.
	g := GraphExport{}
	for i := 0; i < 50; i++ {
		g.Nodes = append(g.Nodes, schema.Node{
			ID: fmt.Sprintf("n%d", i), Label: fmt.Sprintf("N%d", i), Community: "X",
		})
	}
	data, _ := ExportHTML(g, HTMLOptions{})
	if !strings.Contains(string(data), `"N25"`) {
		t.Fatal("default limit should accommodate 50-node graphs without aggregation")
	}
}

func excerpt(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

func TestExportHTMLAggregatedMetaEdgeWeightsCorrect(t *testing.T) {
	// 5 cross-community A↔B edges should produce one meta-edge with
	// "5 cross-community edges". Pins the weight aggregation against
	// a refactor that drops to 1 or sums every edge.
	g := GraphExport{}
	for i := 0; i < 6; i++ {
		g.Nodes = append(g.Nodes, schema.Node{ID: fmt.Sprintf("a%d", i), Community: "A"})
		g.Nodes = append(g.Nodes, schema.Node{ID: fmt.Sprintf("b%d", i), Community: "B"})
	}
	for i := 0; i < 5; i++ {
		g.Edges = append(g.Edges, schema.Edge{
			Source: fmt.Sprintf("a%d", i), Target: fmt.Sprintf("b%d", i), Relation: "calls",
		})
	}
	data, _ := ExportHTML(g, HTMLOptions{MaxNodes: 5})
	if !strings.Contains(string(data), "5 cross-community edges") {
		t.Fatalf("expected aggregated weight of 5: %s", excerpt(string(data), 400))
	}
}

func TestExportHTMLAggregatedSymmetricDedupAtoB_BtoA(t *testing.T) {
	// A→B and B→A must collapse into a single weighted meta-edge, not
	// double-counted. Pins the cu/cv swap.
	g := GraphExport{
		Nodes: []schema.Node{
			{ID: "a1", Community: "A"}, {ID: "a2", Community: "A"},
			{ID: "b1", Community: "B"}, {ID: "b2", Community: "B"},
			{ID: "c1", Community: "C"}, {ID: "c2", Community: "C"},
		},
		Edges: []schema.Edge{
			{Source: "a1", Target: "b1", Relation: "calls"},
			{Source: "b2", Target: "a2", Relation: "calls"}, // reverse direction
		},
	}
	data, _ := ExportHTML(g, HTMLOptions{MaxNodes: 3})
	html := string(data)
	if !strings.Contains(html, "2 cross-community edges") {
		t.Fatalf("symmetric edges should aggregate to weight 2: %s", excerpt(html, 400))
	}
}

func TestExportHTMLAggregatedDropsSelfLoopsAndIntra(t *testing.T) {
	g := GraphExport{
		Nodes: []schema.Node{
			{ID: "a1", Community: "A"}, {ID: "a2", Community: "A"},
			{ID: "b1", Community: "B"}, {ID: "b2", Community: "B"},
			{ID: "c1", Community: "C"}, {ID: "c2", Community: "C"},
		},
		Edges: []schema.Edge{
			{Source: "a1", Target: "a2"},           // intra
			{Source: "a1", Target: "a1"},           // self-loop
			{Source: "a1", Target: "b1"},           // cross
		},
	}
	data, _ := ExportHTML(g, HTMLOptions{MaxNodes: 3})
	html := string(data)
	if !strings.Contains(html, "1 cross-community edges") {
		t.Fatalf("expected exactly 1 meta-edge (intra + self-loop dropped): %s", excerpt(html, 400))
	}
}

func TestExportHTMLAggregatedUnassignedBucketIsolatedFromUserCommunityNamed_(t *testing.T) {
	// Regression for sentinel collision: a user-defined community named
	// "_" must NOT merge with nodes that have empty Community.
	g := GraphExport{
		Nodes: []schema.Node{
			{ID: "user1", Community: "_"}, {ID: "user2", Community: "_"},
			{ID: "free1", Community: ""},  {ID: "free2", Community: ""},
			{ID: "a1", Community: "A"}, {ID: "a2", Community: "A"},
		},
		Edges: []schema.Edge{
			{Source: "user1", Target: "free1"}, // user-named "_" ↔ unassigned must be cross
		},
	}
	data, _ := ExportHTML(g, HTMLOptions{MaxNodes: 3})
	html := string(data)
	if !strings.Contains(html, `"community:_"`) {
		t.Fatalf("user community named _ must keep its literal id: %s", excerpt(html, 600))
	}
	if !strings.Contains(html, `"community:__unassigned__"`) {
		t.Fatalf("unassigned bucket must have a separate id: %s", excerpt(html, 600))
	}
}

func TestExportHTMLNegativeMaxNodesForcesFullRender(t *testing.T) {
	// Callers who explicitly want full fidelity on huge graphs pass -1.
	g := GraphExport{}
	for i := 0; i < 50; i++ {
		g.Nodes = append(g.Nodes, schema.Node{
			ID: fmt.Sprintf("n%d", i), Label: fmt.Sprintf("KeepLabel%d", i), Community: "X",
		})
	}
	data, _ := ExportHTML(g, HTMLOptions{MaxNodes: -1})
	if !strings.Contains(string(data), "KeepLabel25") {
		t.Fatal("MaxNodes=-1 should preserve original nodes regardless of count")
	}
}

func TestExportHTMLBoundaryAtExactlyMaxNodes(t *testing.T) {
	// At MaxNodes exactly: no aggregation. At MaxNodes+1: aggregation.
	mk := func(n int) GraphExport {
		g := GraphExport{}
		for i := 0; i < n; i++ {
			g.Nodes = append(g.Nodes, schema.Node{
				ID: fmt.Sprintf("n%d", i), Label: fmt.Sprintf("Sentinel%d", i), Community: "X",
			})
		}
		return g
	}
	dataAt, _ := ExportHTML(mk(5), HTMLOptions{MaxNodes: 5})
	if !strings.Contains(string(dataAt), "Sentinel3") {
		t.Fatal("at exactly MaxNodes the full graph should render")
	}
	dataAbove, _ := ExportHTML(mk(6), HTMLOptions{MaxNodes: 5})
	if strings.Contains(string(dataAbove), "Sentinel3") {
		t.Fatal("at MaxNodes+1 aggregation must fire")
	}
}

func TestExportHTMLContainsPhysicsToggle(t *testing.T) {
	// The viewer exposes a physics on/off control so users can pin
	// nodes by hand without the simulation snapping them back.
	data, _ := ExportHTML(GraphExport{}, HTMLOptions{})
	html := string(data)
	if !strings.Contains(html, `id="physics"`) {
		t.Fatal("physics toggle checkbox missing")
	}
	if !strings.Contains(html, `physicsOn`) {
		t.Fatal("physics state variable missing from script")
	}
}
