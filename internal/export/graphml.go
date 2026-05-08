package export

import (
	"encoding/xml"
	"fmt"
	"sort"
	"strings"

	"github.com/julianshen/gogfy/internal/schema"
)

// ExportGraphML returns a GraphML XML representation of the graph, suitable
// for opening in Gephi, yEd, or any GraphML-aware tool. Edges are directed
// (GraphML default for `<graph edgedefault="directed">`); custom data keys
// surface the SourceFile, Community, Relation, and Confidence fields.
func ExportGraphML(g GraphExport) ([]byte, error) {
	var b strings.Builder
	b.WriteString(xml.Header)
	b.WriteString(`<graphml xmlns="http://graphml.graphdrawing.org/xmlns">` + "\n")
	// Schema for the data keys we attach to nodes/edges.
	b.WriteString(`  <key id="label" for="node" attr.name="label" attr.type="string"/>` + "\n")
	b.WriteString(`  <key id="source_file" for="node" attr.name="source_file" attr.type="string"/>` + "\n")
	b.WriteString(`  <key id="community" for="node" attr.name="community" attr.type="string"/>` + "\n")
	b.WriteString(`  <key id="relation" for="edge" attr.name="relation" attr.type="string"/>` + "\n")
	b.WriteString(`  <key id="confidence" for="edge" attr.name="confidence" attr.type="string"/>` + "\n")
	b.WriteString(`  <graph id="G" edgedefault="directed">` + "\n")

	// Sort for deterministic output.
	nodes := append([]schema.Node(nil), g.Nodes...)
	schema.SortNodesByID(nodes)
	for _, n := range nodes {
		fmt.Fprintf(&b, `    <node id=%q>`+"\n", n.ID)
		writeData(&b, "label", n.Label)
		if n.SourceFile != "" {
			writeData(&b, "source_file", n.SourceFile)
		}
		if n.Community != "" {
			writeData(&b, "community", n.Community)
		}
		b.WriteString("    </node>\n")
	}

	edges := append([]schema.Edge(nil), g.Edges...)
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].Source != edges[j].Source {
			return edges[i].Source < edges[j].Source
		}
		if edges[i].Target != edges[j].Target {
			return edges[i].Target < edges[j].Target
		}
		return edges[i].Relation < edges[j].Relation
	})
	for i, e := range edges {
		fmt.Fprintf(&b, `    <edge id="e%d" source=%q target=%q>`+"\n", i, e.Source, e.Target)
		writeData(&b, "relation", e.Relation)
		writeData(&b, "confidence", e.Confidence.String())
		b.WriteString("    </edge>\n")
	}
	b.WriteString("  </graph>\n</graphml>\n")
	return []byte(b.String()), nil
}

// writeData appends a single GraphML <data key="...">value</data> line, with
// XML special chars escaped via xml.EscapeText so labels containing `<` or `&`
// don't break the document.
func writeData(b *strings.Builder, key, value string) {
	fmt.Fprintf(b, `      <data key=%q>`, key)
	_ = xml.EscapeText(stringWriter{b}, []byte(value))
	b.WriteString("</data>\n")
}

// stringWriter adapts strings.Builder for io.Writer so xml.EscapeText can
// stream into it directly.
type stringWriter struct{ b *strings.Builder }

func (w stringWriter) Write(p []byte) (int, error) { return w.b.Write(p) }
