package export

import (
	"encoding/json"
	"testing"

	"github.com/julianshen/gogfy/internal/schema"
)

func TestExportJSON(t *testing.T) {
	g := GraphExport{
		Nodes: []schema.Node{{ID: "a", Label: "A"}},
		Edges: []schema.Edge{{Source: "a", Target: "b", Relation: "calls"}},
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
		t.Fatal("node count mismatch")
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
	expected := "Nodes: 1"
	if string(data) != "<html><body>"+expected+"</body></html>" {
		t.Fatalf("HTML output mismatch: got %s", string(data))
	}
}
