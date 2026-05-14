package callflow

import (
	"strings"
	"testing"

	"github.com/julianshen/gogfy/internal/schema"
)

// fixture: three communities of mixed sizes with cross-community edges,
// matching the typical shape of a clustered graph.json.
func fixture() ([]schema.Node, []schema.Edge) {
	nodes := []schema.Node{
		// Community 0 (auth) — 3 nodes
		{ID: "a1", Label: "LoginHandler", SourceFile: "/r/auth/login.go", SourceLocation: "10:1", Community: "0"},
		{ID: "a2", Label: "TokenMint", SourceFile: "/r/auth/token.go", SourceLocation: "15:1", Community: "0"},
		{ID: "a3", Label: "UserStore", SourceFile: "/r/auth/users.go", SourceLocation: "5:1", Community: "0"},
		// Community 1 (api) — 4 nodes
		{ID: "b1", Label: "APIServer", SourceFile: "/r/api/server.go", SourceLocation: "1:1", Community: "1"},
		{ID: "b2", Label: "RouteUsers", SourceFile: "/r/api/routes.go", SourceLocation: "10:1", Community: "1"},
		{ID: "b3", Label: "RoutePosts", SourceFile: "/r/api/routes.go", SourceLocation: "20:1", Community: "1"},
		{ID: "b4", Label: "Middleware", SourceFile: "/r/api/middleware.go", SourceLocation: "5:1", Community: "1"},
		// Community 2 (db) — 2 nodes
		{ID: "c1", Label: "Repo", SourceFile: "/r/db/repo.go", SourceLocation: "1:1", Community: "2"},
		{ID: "c2", Label: "Migrate", SourceFile: "/r/db/migrate.go", SourceLocation: "1:1", Community: "2"},
	}
	edges := []schema.Edge{
		// intra-auth
		{Source: "a1", Target: "a2", Relation: "calls"},
		{Source: "a2", Target: "a3", Relation: "calls"},
		// intra-api
		{Source: "b1", Target: "b2", Relation: "calls"},
		{Source: "b1", Target: "b3", Relation: "calls"},
		{Source: "b4", Target: "b1", Relation: "wraps"},
		// cross: api -> auth (api uses auth)
		{Source: "b2", Target: "a1", Relation: "calls"},
		// cross: auth -> db
		{Source: "a3", Target: "c1", Relation: "reads"},
		// intra-db
		{Source: "c1", Target: "c2", Relation: "calls"},
	}
	return nodes, edges
}

func TestGenerateProducesSelfContainedHTMLWithMermaid(t *testing.T) {
	nodes, edges := fixture()
	out, err := Generate(nodes, edges, Options{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	for _, want := range []string{
		"<!DOCTYPE html>",
		"<style",                  // inline CSS
		"mermaid",                 // mermaid script reference (CDN or embedded)
		`class="mermaid"`,         // a mermaid block
		"flowchart LR",            // overview diagram orientation
		"Architecture Overview",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("HTML missing %q\n--- output head ---\n%s", want, head(out, 500))
		}
	}
}

func TestGenerateRendersSectionPerCommunity(t *testing.T) {
	// Each non-empty community should produce its own section with a
	// per-section flowchart. The fixture has 3 communities → 3 section
	// flowcharts (plus the overview).
	nodes, edges := fixture()
	out, err := Generate(nodes, edges, Options{})
	if err != nil {
		t.Fatal(err)
	}
	// At least one node label from each community must appear in the
	// rendered HTML — pins that per-section diagrams actually emit
	// the community's nodes.
	for _, want := range []string{"LoginHandler", "APIServer", "Repo"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected to see node label %q in output", want)
		}
	}
}

func TestGenerateOverviewIncludesCrossCommunityEdges(t *testing.T) {
	// The aggregated overview should reflect that community 1 (api)
	// has edges into community 0 (auth), and community 0 has edges
	// into community 2 (db). Look for the mermaid edge syntax
	// "-->|...|" between section node IDs at minimum.
	nodes, edges := fixture()
	out, err := Generate(nodes, edges, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "-->") {
		t.Fatalf("overview should contain at least one mermaid edge (-->) reflecting cross-community calls\n%s", head(out, 1000))
	}
}

func TestGenerateMaxSectionsTruncates(t *testing.T) {
	// 5 distinct communities, but MaxSections=2 should produce only
	// the 2 largest sections + group the rest into an "Other" bucket
	// (or drop them entirely — pin the truncation either way).
	var nodes []schema.Node
	for i := 0; i < 5; i++ {
		comm := byte('0' + i)
		nodes = append(nodes,
			schema.Node{ID: string([]byte{'n', comm, '1'}), Label: "A" + string([]byte{comm}), SourceFile: "/r/x.go", Community: string([]byte{comm})},
			schema.Node{ID: string([]byte{'n', comm, '2'}), Label: "B" + string([]byte{comm}), SourceFile: "/r/x.go", Community: string([]byte{comm})},
		)
	}
	// Bias community 0 + 1 to be largest so truncation order is
	// predictable: 4 nodes vs 2 elsewhere.
	nodes = append(nodes,
		schema.Node{ID: "n01a", Label: "A0a", SourceFile: "/r/x.go", Community: "0"},
		schema.Node{ID: "n01b", Label: "A0b", SourceFile: "/r/x.go", Community: "0"},
		schema.Node{ID: "n11a", Label: "A1a", SourceFile: "/r/x.go", Community: "1"},
		schema.Node{ID: "n11b", Label: "A1b", SourceFile: "/r/x.go", Community: "1"},
	)
	out, err := Generate(nodes, nil, Options{MaxSections: 2})
	if err != nil {
		t.Fatal(err)
	}
	// Sections 0 and 1 (largest) should produce labelled sections;
	// labels from community 4 (smallest) should not.
	if !strings.Contains(out, "A0a") || !strings.Contains(out, "A1a") {
		t.Fatalf("expected the two largest communities to appear; output head:\n%s", head(out, 800))
	}
}

func TestGenerateMaxNodesPerSectionCaps(t *testing.T) {
	// Build a single community with many nodes; MaxNodesPerSection=3
	// must keep the per-section diagram to at most 3 node lines.
	var nodes []schema.Node
	for i := 0; i < 20; i++ {
		nodes = append(nodes, schema.Node{
			ID:         "many-" + itoa(i),
			Label:      "Sym" + itoa(i),
			SourceFile: "/r/big.go",
			Community:  "0",
		})
	}
	out, err := Generate(nodes, nil, Options{MaxNodesPerSection: 3})
	if err != nil {
		t.Fatal(err)
	}
	// Heuristic: each rendered node produces one mermaid id line like
	// `nodeXYZ("Sym3")` in the section flowchart. With 20 sym labels
	// but cap=3, at most 3 sym labels appear inside the per-section
	// mermaid block.
	hits := 0
	for i := 0; i < 20; i++ {
		if strings.Contains(out, `"Sym`+itoa(i)+`"`) {
			hits++
		}
	}
	// Nav lists section names only — never node labels — so the count
	// of rendered sym labels should equal the cap exactly.
	if hits != 3 {
		t.Fatalf("MaxNodesPerSection=3 didn't cap the rendered nodes (saw %d sym labels)", hits)
	}
}

func TestGenerateMaxEdgesPerSectionCaps(t *testing.T) {
	// 5 fully-connected nodes in one community (10 intra-section edges).
	// MaxEdges=2 must produce at most 2 mermaid edge lines.
	nodes := []schema.Node{
		{ID: "n1", Label: "A", SourceFile: "/r/x.go", Community: "0"},
		{ID: "n2", Label: "B", SourceFile: "/r/x.go", Community: "0"},
		{ID: "n3", Label: "C", SourceFile: "/r/x.go", Community: "0"},
		{ID: "n4", Label: "D", SourceFile: "/r/x.go", Community: "0"},
		{ID: "n5", Label: "E", SourceFile: "/r/x.go", Community: "0"},
	}
	var edges []schema.Edge
	ids := []string{"n1", "n2", "n3", "n4", "n5"}
	for i := 0; i < len(ids); i++ {
		for j := i + 1; j < len(ids); j++ {
			edges = append(edges, schema.Edge{Source: ids[i], Target: ids[j], Relation: "calls"})
		}
	}
	out, err := Generate(nodes, edges, Options{MaxEdgesPerSection: 2})
	if err != nil {
		t.Fatal(err)
	}
	edgeLines := strings.Count(out, " -->|calls| ")
	// Section flowcharts only; the overview has no intra-community
	// cross edges in a single-community graph.
	if edgeLines > 2 {
		t.Fatalf("MaxEdgesPerSection=2 didn't cap; saw %d ' -->|calls| ' occurrences", edgeLines)
	}
}

func TestGenerateEscapesPipeAndNewlineInLabels(t *testing.T) {
	// Mermaid's `|` edge-label delimiter and per-statement-newline
	// lexer both silently corrupt diagrams if user content contains
	// the character. Regression test for both at once.
	nodes := []schema.Node{
		{ID: "n1", Label: "Map|String", SourceFile: "/r/x.go", Community: "0"},
		{ID: "n2", Label: "Multi\nLine", SourceFile: "/r/x.go", Community: "0"},
	}
	out, err := Generate(nodes, []schema.Edge{
		{Source: "n1", Target: "n2", Relation: "calls"},
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	// The raw `|` and newline must not survive into a rendered label.
	if strings.Contains(out, `"Map|String"`) {
		t.Fatal("raw | inside mermaid label would break edge syntax")
	}
	if strings.Contains(out, "Multi\nLine") {
		t.Fatal("raw newline inside mermaid label would break the lexer")
	}
	if !strings.Contains(out, "Map&#124;String") {
		t.Fatal(`expected &#124; entity escape for |`)
	}
	if !strings.Contains(out, "Multi Line") {
		t.Fatal("newline should collapse to a space inside the label")
	}
}

func TestGenerateSectionNameFallback(t *testing.T) {
	// Source files at filesystem root have no parent directory, so
	// topDir returns "" and sectionName must fall back to
	// "Community <id>" — not "" / "Uncategorized".
	nodes := []schema.Node{
		{ID: "n1", Label: "A", SourceFile: "rootfile.go", Community: "7"},
		{ID: "n2", Label: "B", SourceFile: "another.go", Community: "7"},
	}
	out, err := Generate(nodes, nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Community 7") {
		t.Fatalf("expected 'Community 7' fallback heading in output; head:\n%s", head(out, 800))
	}
}

func TestGenerateSurfacesTruncationNotice(t *testing.T) {
	// Many communities but MaxSections=2 should produce an HTML notice
	// telling the user some sections were dropped. Currently the bot
	// reviewers flagged silent-truncation as data loss; this pins the
	// notice contract.
	var nodes []schema.Node
	for i := 0; i < 10; i++ {
		nodes = append(nodes, schema.Node{
			ID:         "n" + itoa(i),
			Label:      "L" + itoa(i),
			SourceFile: "/r/x.go",
			Community:  itoa(i),
		})
	}
	out, err := Generate(nodes, nil, Options{MaxSections: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "smaller section(s) were truncated") {
		t.Fatalf("expected truncation notice in output; head:\n%s", head(out, 1500))
	}
}

func TestGenerateEmptyGraphErrors(t *testing.T) {
	if _, err := Generate(nil, nil, Options{}); err == nil {
		t.Fatal("expected error on empty graph (nothing to chart)")
	}
}

func TestGenerateEscapesUserContentInMermaid(t *testing.T) {
	// A label containing quotes / mermaid syntax must not break the
	// surrounding flowchart block.
	nodes := []schema.Node{
		{ID: "n1", Label: `evil"; alert(1) /*`, SourceFile: "/r/x.go", Community: "0"},
		{ID: "n2", Label: "Regular", SourceFile: "/r/y.go", Community: "0"},
	}
	edges := []schema.Edge{{Source: "n1", Target: "n2", Relation: "calls"}}
	out, err := Generate(nodes, edges, Options{})
	if err != nil {
		t.Fatal(err)
	}
	// Raw closing-script attack vector should not appear unescaped.
	if strings.Contains(out, `</script><script>`) {
		t.Fatalf("script-tag breakout vector found unescaped")
	}
	// The Regular label must still appear cleanly.
	if !strings.Contains(out, "Regular") {
		t.Fatal("legitimate labels must still render")
	}
}

func TestGenerateEscapesAmpersandInLabels(t *testing.T) {
	// A label containing & must be HTML-entity-escaped or downstream
	// (mermaid renders labels via HTML) would misparse an entity-like
	// substring such as "&amp" inside the user's identifier.
	nodes := []schema.Node{
		{ID: "n1", Label: "Foo&Bar", SourceFile: "/r/x.go", Community: "0"},
	}
	out, err := Generate(nodes, nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Foo&amp;Bar") {
		t.Fatalf("`&` in label not entity-escaped:\n%s", head(out, 800))
	}
}

func TestGenerateNavLinksAllSections(t *testing.T) {
	// Each rendered section needs an in-page anchor so the nav bar
	// can jump to it. Pin one anchor per fixture community.
	nodes, edges := fixture()
	out, err := Generate(nodes, edges, Options{})
	if err != nil {
		t.Fatal(err)
	}
	// At least one href="#section-..." per community.
	anchors := strings.Count(out, `href="#section-`)
	if anchors < 3 {
		t.Fatalf("expected at least 3 section anchors (one per community), got %d", anchors)
	}
}

func TestGenerateDeterministic(t *testing.T) {
	// Same input → byte-identical output. The section order, per-section
	// node ordering, and edge ordering inside mermaid blocks all use
	// maps internally; without sorted iteration they'd drift.
	nodes, edges := fixture()
	a, err := Generate(nodes, edges, Options{})
	if err != nil {
		t.Fatal(err)
	}
	b, err := Generate(nodes, edges, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatal("Generate is non-deterministic on identical input")
	}
}

func TestGenerateProjectNameUsedInTitle(t *testing.T) {
	nodes, edges := fixture()
	out, err := Generate(nodes, edges, Options{ProjectName: "myproj"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "myproj") {
		t.Fatalf("ProjectName should appear in title; head:\n%s", head(out, 400))
	}
}

func head(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func itoa(i int) string {
	const digits = "0123456789"
	if i == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = digits[i%10]
		i /= 10
	}
	return string(buf[pos:])
}
