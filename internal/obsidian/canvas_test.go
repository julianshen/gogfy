package obsidian

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/julianshen/gogfy/internal/schema"
)

func TestCanvasEmitsGroupNodesAndFileCards(t *testing.T) {
	nodes := []schema.Node{
		{ID: "a", Label: "Auth", Community: "1"},
		{ID: "b", Label: "Billing", Community: "1"},
		{ID: "c", Label: "Cache", Community: "2"},
	}
	data, err := Canvas(nodes, nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	var f canvasFile
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	groups, files := 0, 0
	for _, n := range f.Nodes {
		switch n.Type {
		case "group":
			groups++
		case "file":
			files++
		}
	}
	if groups != 2 {
		t.Errorf("expected 2 group nodes (one per community), got %d", groups)
	}
	if files != 3 {
		t.Errorf("expected 3 file cards (one per node), got %d", files)
	}
}

func TestCanvasFileCardsReferenceVaultFilenames(t *testing.T) {
	// Each file card's `file` must match the .md filename buildFilenames
	// would assign, so the canvas links to actual vault notes.
	nodes := []schema.Node{
		{ID: "a", Label: "Foo", Community: "1"},
		{ID: "b", Label: "Foo", Community: "1"}, // collision
	}
	data, _ := Canvas(nodes, nil, Options{})
	var f canvasFile
	json.Unmarshal(data, &f)
	hasFoo := false
	hasFoo1 := false
	for _, n := range f.Nodes {
		if n.File == "Foo.md" {
			hasFoo = true
		}
		if n.File == "Foo_1.md" {
			hasFoo1 = true
		}
	}
	if !hasFoo || !hasFoo1 {
		t.Fatalf("expected both Foo.md and Foo_1.md cards, got Foo=%v Foo_1=%v", hasFoo, hasFoo1)
	}
}

func TestCanvasEdgesOnlyBetweenCanvasNodes(t *testing.T) {
	// An edge whose endpoint isn't on the canvas (unassigned community)
	// must not produce a dangling canvasEdge.
	nodes := []schema.Node{
		{ID: "a", Label: "A", Community: "1"},
		{ID: "b", Label: "B", Community: "1"},
		{ID: "stray", Label: "Stray"}, // no community → not on canvas
	}
	edges := []schema.Edge{
		{Source: "a", Target: "b"},     // both on canvas — should appear
		{Source: "a", Target: "stray"}, // stray not on canvas — must drop
	}
	data, _ := Canvas(nodes, edges, Options{})
	var f canvasFile
	json.Unmarshal(data, &f)
	if len(f.Edges) != 1 {
		t.Fatalf("expected 1 edge (a→b), got %d: %+v", len(f.Edges), f.Edges)
	}
}

func TestCanvasEdgeLabelEmbedsRelationAndConfidence(t *testing.T) {
	nodes := []schema.Node{
		{ID: "a", Label: "A", Community: "1"},
		{ID: "b", Label: "B", Community: "1"},
	}
	edges := []schema.Edge{
		{Source: "a", Target: "b", Relation: "calls", Confidence: schema.Inferred},
	}
	data, _ := Canvas(nodes, edges, Options{})
	if !strings.Contains(string(data), `"label": "calls [INFERRED]"`) {
		t.Fatalf("edge label should embed relation + confidence: %s", excerpt(data, 400))
	}
}

func TestCanvasCommunityLabelsHonored(t *testing.T) {
	nodes := []schema.Node{
		{ID: "a", Label: "A", Community: "1"},
	}
	data, _ := Canvas(nodes, nil, Options{
		CommunityLabels: map[string]string{"1": "Auth Layer"},
	})
	if !strings.Contains(string(data), `"Auth Layer"`) {
		t.Fatalf("custom community label missing: %s", excerpt(data, 400))
	}
	if strings.Contains(string(data), `"Community 1"`) {
		t.Fatalf("default name leaked despite override: %s", excerpt(data, 400))
	}
}

func TestCanvasEdgeCapPreventsWireball(t *testing.T) {
	// 250 cross-edges between 2 communities — must cap at 200.
	nodes := []schema.Node{}
	for i := 0; i < 250; i++ {
		nodes = append(nodes,
			schema.Node{ID: fmt.Sprintf("a%d", i), Label: fmt.Sprintf("A%d", i), Community: "1"},
			schema.Node{ID: fmt.Sprintf("b%d", i), Label: fmt.Sprintf("B%d", i), Community: "2"},
		)
	}
	edges := []schema.Edge{}
	for i := 0; i < 250; i++ {
		edges = append(edges, schema.Edge{Source: fmt.Sprintf("a%d", i), Target: fmt.Sprintf("b%d", i)})
	}
	data, _ := Canvas(nodes, edges, Options{})
	var f canvasFile
	json.Unmarshal(data, &f)
	if len(f.Edges) != 200 {
		t.Fatalf("edge count should be capped at 200, got %d", len(f.Edges))
	}
}

func TestCanvasEmptyEmitsValidJSON(t *testing.T) {
	data, err := Canvas(nil, nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	// Obsidian rejects missing nodes/edges arrays — must explicitly
	// emit empty arrays, not omit the keys.
	s := string(data)
	if !strings.Contains(s, `"nodes": []`) || !strings.Contains(s, `"edges": []`) {
		t.Fatalf("empty canvas should have explicit empty arrays: %s", s)
	}
}

func excerpt(data []byte, n int) string {
	if len(data) > n {
		return string(data[:n]) + "…"
	}
	return string(data)
}

func TestCanvasMultiEdgeBetweenSamePairKeepsBothWithDistinctIDs(t *testing.T) {
	// Two edges between the same pair (different relations) must NOT
	// collide on canvas edge ID — Obsidian would silently drop one.
	nodes := []schema.Node{
		{ID: "a", Label: "A", Community: "1"},
		{ID: "b", Label: "B", Community: "1"},
	}
	edges := []schema.Edge{
		{Source: "a", Target: "b", Relation: "calls", Confidence: schema.Extracted},
		{Source: "a", Target: "b", Relation: "imports", Confidence: schema.Extracted},
	}
	data, _ := Canvas(nodes, edges, Options{})
	var f canvasFile
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, e := range f.Edges {
		if ids[e.ID] {
			t.Fatalf("edge ID collision on multi-edge: %s", e.ID)
		}
		ids[e.ID] = true
	}
	if len(f.Edges) != 2 {
		t.Fatalf("both multi-edges should survive, got %d", len(f.Edges))
	}
}
