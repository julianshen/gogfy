package export

import (
	"encoding/json"
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

func TestExportHTML(t *testing.T) {
	g := GraphExport{
		Nodes: []schema.Node{{ID: "a", Label: "A"}},
		Edges: []schema.Edge{{Source: "a", Target: "b", Relation: "calls"}},
	}
	data, err := ExportHTML(g)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("HTML output is empty")
	}
	want := "<html><body>Nodes: 1, Edges: 1</body></html>"
	if string(data) != want {
		t.Fatalf("HTML output mismatch: got %s, want %s", string(data), want)
	}
}
