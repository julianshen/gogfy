package export

import (
	"fmt"
	"strings"

	"github.com/julianshen/gogfy/internal/schema"
)

// ExportCypher returns a Neo4j Cypher script that recreates the graph via
// MERGE statements, idempotent on the (id) key. Suitable for piping into
// `cypher-shell -f`. Direct push to a running Bolt endpoint is left to a
// follow-up PR (would need the neo4j driver dep).
//
// Each node becomes:
//
//	MERGE (n:Node {id: "<id>"}) SET n.label = "...", n.source_file = "..."
//
// Each edge becomes:
//
//	MATCH (a:Node {id: "<src>"}), (b:Node {id: "<dst>"})
//	MERGE (a)-[r:CALLS]->(b) SET r.confidence = "..."
//
// The relation type is upper-cased per Cypher convention; non-identifier
// characters in the original relation name are replaced with `_`.
func ExportCypher(g GraphExport) ([]byte, error) {
	var b strings.Builder
	b.WriteString("// gogfy graph export — Cypher script (idempotent via MERGE)\n")

	nodes := append([]schema.Node(nil), g.Nodes...)
	schema.SortNodesByID(nodes)
	for _, n := range nodes {
		fmt.Fprintf(&b, "MERGE (n:Node {id: %s})", quoteCypher(n.ID))
		var sets []string
		if n.Label != "" {
			sets = append(sets, fmt.Sprintf("n.label = %s", quoteCypher(n.Label)))
		}
		if n.SourceFile != "" {
			sets = append(sets, fmt.Sprintf("n.source_file = %s", quoteCypher(n.SourceFile)))
		}
		if n.Community != "" {
			sets = append(sets, fmt.Sprintf("n.community = %s", quoteCypher(n.Community)))
		}
		if len(sets) > 0 {
			b.WriteString(" SET ")
			b.WriteString(strings.Join(sets, ", "))
		}
		b.WriteString(";\n")
	}

	edges := append([]schema.Edge(nil), g.Edges...)
	sortEdges(edges)
	for _, e := range edges {
		fmt.Fprintf(&b,
			"MATCH (a:Node {id: %s}), (b:Node {id: %s}) MERGE (a)-[r:%s]->(b) SET r.confidence = %s;\n",
			quoteCypher(e.Source), quoteCypher(e.Target),
			cypherRelType(e.Relation), quoteCypher(e.Confidence.String()))
	}
	return []byte(b.String()), nil
}

// quoteCypher returns a Cypher string literal with `\` and `"` escaped. We
// intentionally use double quotes so labels containing `'` (common in
// natural-language text) don't need extra escaping. Control characters
// below 0x20 (other than the named ones below) get the openCypher \u####
// escape so a stray NUL/SOH/etc. doesn't make cypher-shell choke.
func quoteCypher(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// cypherRelType uppercases a relation name and replaces non-[A-Z0-9_] chars
// with `_` so it's a valid Cypher relationship type. Empty input yields
// "RELATED" as a safe default.
func cypherRelType(rel string) string {
	if rel == "" {
		return "RELATED"
	}
	var b strings.Builder
	for _, r := range strings.ToUpper(rel) {
		switch {
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := b.String()
	if out == "" || (out[0] >= '0' && out[0] <= '9') {
		out = "_" + out
	}
	return out
}
