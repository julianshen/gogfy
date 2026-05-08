package export

import (
	"encoding/xml"
	"fmt"
	"strings"

	"github.com/julianshen/gogfy/internal/schema"
)

// ExportGraphML returns a GraphML XML representation of the graph, suitable
// for opening in Gephi, yEd, or any GraphML-aware tool. Edges are directed
// (GraphML default for `<graph edgedefault="directed">`); custom data keys
// surface the SourceFile, Community, Relation, and Confidence fields.
//
// Node and edge IDs are XML-attribute-escaped so labels/IDs containing `<`,
// `&`, or other special chars don't break the document.
func ExportGraphML(g GraphExport) ([]byte, error) {
	var b strings.Builder
	b.WriteString(xml.Header)
	// xsi + schemaLocation lets stricter validators (some yEd builds, raw
	// xmllint --schema) accept the document without warnings.
	b.WriteString(`<graphml xmlns="http://graphml.graphdrawing.org/xmlns"` +
		` xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"` +
		` xsi:schemaLocation="http://graphml.graphdrawing.org/xmlns http://graphml.graphdrawing.org/xmlns/1.0/graphml.xsd">` + "\n")
	b.WriteString(`  <key id="label" for="node" attr.name="label" attr.type="string"/>` + "\n")
	b.WriteString(`  <key id="source_file" for="node" attr.name="source_file" attr.type="string"/>` + "\n")
	b.WriteString(`  <key id="community" for="node" attr.name="community" attr.type="string"/>` + "\n")
	b.WriteString(`  <key id="relation" for="edge" attr.name="relation" attr.type="string"/>` + "\n")
	b.WriteString(`  <key id="confidence" for="edge" attr.name="confidence" attr.type="string"/>` + "\n")
	b.WriteString(`  <graph id="G" edgedefault="directed">` + "\n")

	nodes := append([]schema.Node(nil), g.Nodes...)
	schema.SortNodesByID(nodes)
	for _, n := range nodes {
		fmt.Fprintf(&b, `    <node id="%s">`+"\n", xmlAttr(n.ID))
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
	sortEdges(edges)
	for i, e := range edges {
		fmt.Fprintf(&b, `    <edge id="e%d" source="%s" target="%s">`+"\n",
			i, xmlAttr(e.Source), xmlAttr(e.Target))
		writeData(&b, "relation", e.Relation)
		writeData(&b, "confidence", e.Confidence.String())
		b.WriteString("    </edge>\n")
	}
	b.WriteString("  </graph>\n</graphml>\n")
	return []byte(b.String()), nil
}

// writeData appends a single GraphML <data key="...">value</data> line, with
// XML special chars in the value escaped via xml.EscapeText so labels
// containing `<` or `&` don't break the document. *strings.Builder already
// satisfies io.Writer; no adapter needed.
func writeData(b *strings.Builder, key, value string) {
	fmt.Fprintf(b, `      <data key="%s">`, key)
	_ = xml.EscapeText(b, []byte(value))
	b.WriteString("</data>\n")
}

// xmlAttr escapes a string for safe inclusion as an XML attribute value
// (which uses `&quot;` rather than `\"`). Reusing xml.EscapeText routes
// every special char through the standard encoder.
func xmlAttr(s string) string {
	var sb strings.Builder
	_ = xml.EscapeText(&sb, []byte(s))
	return sb.String()
}
