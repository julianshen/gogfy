package export

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/julianshen/gogfy/internal/schema"
)

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
	data, err := ExportHTML(g)
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
	data, err := ExportHTML(GraphExport{})
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
	data, err := ExportHTML(GraphExport{})
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

func TestExportHTMLEscapesPayloadSafely(t *testing.T) {
	// Adversarial labels should not be able to break out of the JS string
	// literal that the JSON payload is interpolated into.
	g := GraphExport{
		Nodes: []schema.Node{{ID: "x", Label: "</script><script>alert(1)</script>"}},
	}
	data, err := ExportHTML(g)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "</script><script>alert(1)</script>") {
		t.Fatal("hostile label appeared verbatim — JSON encoder must escape `</script>` sequences")
	}
}
