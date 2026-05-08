package export

import (
	"strings"
	"testing"

	"github.com/julianshen/gogfy/internal/schema"
)

func TestExportCypherShape(t *testing.T) {
	g := GraphExport{
		Nodes: []schema.Node{
			{ID: "fn:/m.go:main.foo", Label: "foo", SourceFile: "/m.go", Community: "1"},
			{ID: "fn:/m.go:main.bar", Label: "bar"},
		},
		Edges: []schema.Edge{
			{Source: "fn:/m.go:main.bar", Target: "fn:/m.go:main.foo", Relation: "calls", Confidence: schema.Inferred},
		},
	}
	out := string(must(ExportCypher(g)))
	for _, want := range []string{
		`MERGE (n:Node {id: "fn:/m.go:main.foo"}) SET n.label = "foo"`,
		`MATCH (a:Node {id: "fn:/m.go:main.bar"}), (b:Node {id: "fn:/m.go:main.foo"}) MERGE (a)-[r:CALLS]->(b)`,
		`SET r.confidence = "INFERRED"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output:\n%s", want, out)
		}
	}
}

func TestExportCypherEscapesQuotesAndBackslashes(t *testing.T) {
	g := GraphExport{
		Nodes: []schema.Node{{ID: `weird"id\path`, Label: `lab"el`}},
	}
	out := string(must(ExportCypher(g)))
	// Must be parseable Cypher: every `"` and `\` in IDs/labels must be
	// escaped so the literal closes correctly.
	if !strings.Contains(out, `"weird\"id\\path"`) {
		t.Fatalf("backslash/quote not escaped in id: %s", out)
	}
	if !strings.Contains(out, `"lab\"el"`) {
		t.Fatalf("quote not escaped in label: %s", out)
	}
}

func TestExportCypherRelTypeNormalization(t *testing.T) {
	g := GraphExport{
		Nodes: []schema.Node{{ID: "a", Label: "a"}, {ID: "b", Label: "b"}},
		Edges: []schema.Edge{
			{Source: "a", Target: "b", Relation: "method-call!", Confidence: schema.Extracted},
		},
	}
	out := string(must(ExportCypher(g)))
	if !strings.Contains(out, "[r:METHOD_CALL_]") {
		t.Fatalf("non-identifier chars in relation should be normalized: %s", out)
	}
}

func TestQuoteCypherEscapesControlChars(t *testing.T) {
	// Control bytes (< 0x20) need \u#### escapes; leaving them literal in
	// a "..."-quoted Cypher string would make cypher-shell -f choke.
	got := quoteCypher("a\x00b\x01c")
	if !strings.Contains(got, `\u0000`) || !strings.Contains(got, `\u0001`) {
		t.Fatalf("control chars not \\u-escaped: %q", got)
	}
}

func TestExportCypherEmptyRelTypeFallsBack(t *testing.T) {
	if got := cypherRelType(""); got != "RELATED" {
		t.Fatalf("empty rel type should fall back to RELATED, got %q", got)
	}
	if got := cypherRelType("123"); !strings.HasPrefix(got, "_") {
		t.Fatalf("rel type starting with digit should be prefixed with _, got %q", got)
	}
}

func must(b []byte, err error) []byte {
	if err != nil {
		panic(err)
	}
	return b
}
