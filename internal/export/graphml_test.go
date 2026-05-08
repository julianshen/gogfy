package export

import (
	"encoding/xml"
	"strings"
	"testing"

	"github.com/julianshen/gogfy/internal/schema"
)

func TestExportGraphMLValidXML(t *testing.T) {
	g := GraphExport{
		Nodes: []schema.Node{
			{ID: "a", Label: "A", SourceFile: "/a.go", Community: "1"},
			{ID: "b", Label: "B"},
		},
		Edges: []schema.Edge{
			{Source: "a", Target: "b", Relation: "calls", Confidence: schema.Inferred},
		},
	}
	data, err := ExportGraphML(g)
	if err != nil {
		t.Fatal(err)
	}
	// Must be well-formed XML the Gephi/yEd parsers can consume.
	dec := xml.NewDecoder(strings.NewReader(string(data)))
	for {
		_, err := dec.Token()
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			t.Fatalf("invalid XML: %v\n%s", err, data)
		}
	}
	s := string(data)
	for _, want := range []string{
		`edgedefault="directed"`,
		`<node id="a">`,
		`<node id="b">`,
		`<data key="label">A</data>`,
		`<data key="confidence">INFERRED</data>`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in output:\n%s", want, s)
		}
	}
}

func TestExportGraphMLEscapesXMLSpecials(t *testing.T) {
	g := GraphExport{
		Nodes: []schema.Node{{ID: "x", Label: `<script>alert("xss")&friends</script>`}},
	}
	data, err := ExportGraphML(g)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "<script>alert") {
		t.Fatalf("unescaped XML special chars in output:\n%s", data)
	}
	if !strings.Contains(string(data), "&lt;script&gt;") {
		t.Fatalf("expected escaped <script>; got:\n%s", data)
	}
}

func TestExportGraphMLEscapesIDsInAttributes(t *testing.T) {
	// Regression: previously used %q (Go-quoted), which doesn't produce
	// XML-attribute-safe escaping for `<`, `&`, etc. — a hostile ID would
	// produce a malformed document.
	g := GraphExport{
		Nodes: []schema.Node{{ID: `bad<id>&"`, Label: "ok"}},
	}
	data, err := ExportGraphML(g)
	if err != nil {
		t.Fatal(err)
	}
	dec := xml.NewDecoder(strings.NewReader(string(data)))
	for {
		_, err := dec.Token()
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			t.Fatalf("hostile ID broke XML well-formedness: %v\n%s", err, data)
		}
	}
}

func TestExportGraphMLEmpty(t *testing.T) {
	data, err := ExportGraphML(GraphExport{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `<graph id="G"`) {
		t.Fatal("empty graph should still emit a valid <graph> element")
	}
}
