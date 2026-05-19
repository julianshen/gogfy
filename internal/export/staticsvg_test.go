package export

import (
	"strings"
	"testing"

	"github.com/julianshen/gogfy/internal/schema"
)

func TestStaticSVGProducesValidEnvelope(t *testing.T) {
	graph := GraphExport{
		Nodes: []schema.Node{
			{ID: "a", Label: "A", Community: "c1"},
			{ID: "b", Label: "B", Community: "c1"},
			{ID: "c", Label: "C", Community: "c2"},
		},
		Edges: []schema.Edge{
			{Source: "a", Target: "b", Relation: "calls"},
			{Source: "b", Target: "c", Relation: "calls"},
		},
	}
	out := string(StaticSVG(graph, StaticSVGOptions{}))
	for _, want := range []string{
		`<?xml version="1.0"`,
		`<svg xmlns="http://www.w3.org/2000/svg"`,
		`<circle`, // at least one node circle
		`<line`,   // at least one edge line
		`</svg>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in SVG output", want)
		}
	}
}

func TestStaticSVGDeterministicAcrossRuns(t *testing.T) {
	// Same graph in, byte-identical SVG out — the PRNG is seeded with
	// a constant and nodes are sorted by ID so layout is reproducible.
	// CI snapshots stay stable.
	graph := GraphExport{
		Nodes: []schema.Node{
			{ID: "a", Label: "A"},
			{ID: "b", Label: "B"},
			{ID: "c", Label: "C"},
		},
		Edges: []schema.Edge{{Source: "a", Target: "b"}, {Source: "b", Target: "c"}},
	}
	first := StaticSVG(graph, StaticSVGOptions{Iterations: 30})
	second := StaticSVG(graph, StaticSVGOptions{Iterations: 30})
	if string(first) != string(second) {
		t.Errorf("static SVG drifted across runs — layout is nondeterministic")
	}
}

func TestStaticSVGEscapesXMLInLabels(t *testing.T) {
	// A label with `<script>` or `&amp;` must NOT inject markup —
	// the SVG would render but a downstream embedder (a markdown
	// renderer that treats SVG as inline HTML) could be vulnerable.
	graph := GraphExport{Nodes: []schema.Node{{ID: "a", Label: `<script>x</script>`}}}
	out := string(StaticSVG(graph, StaticSVGOptions{ShowLabels: true}))
	if strings.Contains(out, "<script>") {
		t.Errorf("XML escaping failed — raw <script> tag in SVG output")
	}
	if !strings.Contains(out, "&lt;script&gt;") {
		t.Errorf("expected escaped form, got %q", out)
	}
}

func TestStaticSVGHandlesEmptyGraph(t *testing.T) {
	// An empty graph should still produce a valid SVG envelope —
	// useful for diff tooling that always emits a graph file.
	out := string(StaticSVG(GraphExport{}, StaticSVGOptions{}))
	if !strings.HasPrefix(out, `<?xml`) || !strings.Contains(out, `</svg>`) {
		t.Errorf("empty graph should still produce SVG envelope, got %q", out)
	}
}

func TestStaticSVGCommunityColorStable(t *testing.T) {
	// Same community ID → same color across calls. Otherwise
	// re-running the export shuffles colors and any human reader
	// loses the visual "this cluster is over there" association.
	a := communityColor("community-42")
	b := communityColor("community-42")
	if a != b {
		t.Errorf("communityColor is not stable: %q vs %q", a, b)
	}
}
