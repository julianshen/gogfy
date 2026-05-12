package wiki

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/julianshen/gogfy/internal/schema"
)

func TestGenerateProducesIndexAndCommunityArticles(t *testing.T) {
	nodes := []schema.Node{
		{ID: "n1", Label: "Alpha", Community: "0", SourceFile: "a.go"},
		{ID: "n2", Label: "Bravo", Community: "0", SourceFile: "a.go"},
		{ID: "n3", Label: "Charlie", Community: "1", SourceFile: "b.go"},
		{ID: "n4", Label: "Delta", Community: "1", SourceFile: "b.go"},
	}
	edges := []schema.Edge{
		{Source: "n1", Target: "n2", Relation: "calls", Confidence: schema.Extracted},
		{Source: "n3", Target: "n4", Relation: "calls", Confidence: schema.Extracted},
		{Source: "n2", Target: "n3", Relation: "imports", Confidence: schema.Inferred},
	}
	dir := t.TempDir()
	count, err := Generate(nodes, edges, dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("expected 2 community articles, got %d", count)
	}
	// index.md exists and lists both communities.
	idx := readFile(t, filepath.Join(dir, "index.md"))
	for _, want := range []string{"# Knowledge Graph Index", "Community 0", "Community 1", "4 nodes", "3 edges", "2 communities"} {
		if !strings.Contains(idx, want) {
			t.Fatalf("index missing %q in:\n%s", want, idx)
		}
	}
}

func TestGenerateCrossCommunityLinksCounted(t *testing.T) {
	// Two communities with one bridging edge: each article should list
	// the other as a cross-community connection with count 1.
	nodes := []schema.Node{
		{ID: "n1", Label: "A1", Community: "0"},
		{ID: "n2", Label: "A2", Community: "0"},
		{ID: "n3", Label: "B1", Community: "1"},
	}
	edges := []schema.Edge{
		{Source: "n1", Target: "n2", Relation: "calls", Confidence: schema.Extracted},
		{Source: "n2", Target: "n3", Relation: "imports", Confidence: schema.Extracted},
	}
	dir := t.TempDir()
	if _, err := Generate(nodes, edges, dir, Options{}); err != nil {
		t.Fatal(err)
	}
	a := readFile(t, filepath.Join(dir, "Community_0.md"))
	b := readFile(t, filepath.Join(dir, "Community_1.md"))
	if !strings.Contains(a, "[Community 1](Community_1.md)") {
		t.Fatalf("community 0 article missing cross link to 1:\n%s", a)
	}
	if !strings.Contains(b, "[Community 0](Community_0.md)") {
		t.Fatalf("community 1 article missing cross link to 0:\n%s", b)
	}
}

func TestGenerateCustomLabelsAndGodNodes(t *testing.T) {
	nodes := []schema.Node{
		{ID: "n1", Label: "Engine", Community: "0", SourceFile: "engine.go"},
		{ID: "n2", Label: "Worker", Community: "0"},
	}
	edges := []schema.Edge{
		{Source: "n1", Target: "n2", Relation: "calls", Confidence: schema.Extracted},
	}
	dir := t.TempDir()
	opts := Options{
		CommunityLabels: map[string]string{"0": "Runtime Core"},
		GodNodes:        []schema.Node{nodes[0]},
	}
	count, err := Generate(nodes, edges, dir, opts)
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("expected 1 community + 1 god-node article (count=2), got %d", count)
	}
	idx := readFile(t, filepath.Join(dir, "index.md"))
	if !strings.Contains(idx, "[Runtime Core](Runtime_Core.md)") {
		t.Fatalf("index missing custom label: %s", idx)
	}
	if !strings.Contains(idx, "## God Nodes") || !strings.Contains(idx, "[Engine](Engine.md)") {
		t.Fatalf("index missing god-nodes section: %s", idx)
	}
	// The community article should use the custom label, not "Community 0".
	a := readFile(t, filepath.Join(dir, "Runtime_Core.md"))
	if !strings.Contains(a, "# Runtime Core") {
		t.Fatalf("community article missing custom title: %s", a)
	}
}

func TestGenerateClearStaleArticles(t *testing.T) {
	// Simulate a prior gogfy run: marker file + stale article. The new
	// run should remove the stale article AND keep working (marker
	// proves the dir is gogfy-owned).
	dir := t.TempDir()
	stalePath := filepath.Join(dir, "Old_Stale_Label.md")
	if err := os.WriteFile(stalePath, []byte("# OldStale\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, wikiMarkerFile), []byte("# marker\n"), 0644); err != nil {
		t.Fatal(err)
	}
	nodes := []schema.Node{
		{ID: "n1", Label: "X", Community: "0"},
		{ID: "n2", Label: "Y", Community: "0"},
	}
	edges := []schema.Edge{
		{Source: "n1", Target: "n2", Relation: "calls", Confidence: schema.Extracted},
	}
	if _, err := Generate(nodes, edges, dir, Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stalePath); !os.IsNotExist(err) {
		t.Fatalf("stale article should be removed, but Stat returned err=%v", err)
	}
	// Marker should still be present after regeneration.
	if _, err := os.Stat(filepath.Join(dir, wikiMarkerFile)); err != nil {
		t.Fatalf("marker file should survive regeneration, got err=%v", err)
	}
}

func TestGenerateRefusesToClobberUnmarkedMarkdown(t *testing.T) {
	// If the user passes --out to a directory that already contains
	// markdown but no .gogfy_wiki marker (e.g., a project's docs/
	// folder with README.md and CONTRIBUTING.md), Generate must NOT
	// delete those files. Bot-flagged data-loss path.
	dir := t.TempDir()
	user := filepath.Join(dir, "README.md")
	if err := os.WriteFile(user, []byte("# Important user content\n"), 0644); err != nil {
		t.Fatal(err)
	}
	nodes := []schema.Node{
		{ID: "n1", Label: "X", Community: "0"},
		{ID: "n2", Label: "Y", Community: "0"},
	}
	edges := []schema.Edge{
		{Source: "n1", Target: "n2", Relation: "calls", Confidence: schema.Extracted},
	}
	_, err := Generate(nodes, edges, dir, Options{})
	if err == nil {
		t.Fatalf("expected error when outDir contains unmarked .md files")
	}
	// User's README must still exist.
	if _, err := os.Stat(user); err != nil {
		t.Fatalf("user's README.md was deleted: %v", err)
	}
}

func TestGenerateFreshDirWritesMarker(t *testing.T) {
	// First-run case: empty dir. Generate proceeds normally and writes
	// the marker file so subsequent runs know the dir is gogfy-owned.
	dir := t.TempDir()
	nodes := []schema.Node{
		{ID: "n1", Label: "X", Community: "0"},
		{ID: "n2", Label: "Y", Community: "0"},
	}
	edges := []schema.Edge{
		{Source: "n1", Target: "n2", Relation: "calls", Confidence: schema.Extracted},
	}
	if _, err := Generate(nodes, edges, dir, Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, wikiMarkerFile)); err != nil {
		t.Fatalf("marker file missing after Generate on fresh dir: %v", err)
	}
}

func TestGenerateErrorsOnEmptyCommunities(t *testing.T) {
	// Nodes without a Community field are ignored by groupByCommunity;
	// if NONE of them have a community, Generate must error rather than
	// clear an existing wiki dir based on an empty input set.
	nodes := []schema.Node{{ID: "n1", Label: "Orphan"}}
	dir := t.TempDir()
	if _, err := Generate(nodes, nil, dir, Options{}); err == nil {
		t.Fatalf("expected error for empty communities, got nil")
	}
}

func TestGenerateLinksResolveOnLabelCollision(t *testing.T) {
	// Two communities with the same label MUST produce two distinct
	// files AND cross-community links that resolve to the right file.
	// Previously the `[[Label]]` syntax would silently point both at
	// the first community.
	nodes := []schema.Node{
		{ID: "a1", Label: "A1", Community: "0"},
		{ID: "a2", Label: "A2", Community: "0"},
		{ID: "b1", Label: "B1", Community: "1"},
		{ID: "b2", Label: "B2", Community: "1"},
	}
	edges := []schema.Edge{
		{Source: "a1", Target: "a2", Relation: "calls", Confidence: schema.Extracted},
		{Source: "a1", Target: "b1", Relation: "imports", Confidence: schema.Extracted}, // cross-community
		{Source: "b1", Target: "b2", Relation: "calls", Confidence: schema.Extracted},
	}
	dir := t.TempDir()
	opts := Options{
		CommunityLabels: map[string]string{"0": "Examples", "1": "Examples"},
	}
	if _, err := Generate(nodes, edges, dir, opts); err != nil {
		t.Fatal(err)
	}
	// Both files exist (uniqueSlug appended _2).
	if _, err := os.Stat(filepath.Join(dir, "Examples.md")); err != nil {
		t.Fatalf("Examples.md missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "Examples_2.md")); err != nil {
		t.Fatalf("Examples_2.md missing (uniqueSlug should have appended _2): %v", err)
	}
	// Cross-link from one community article points at the OTHER file,
	// not back at itself. The first community (sorted by cid) gets the
	// bare slug; the second gets _2. A link from the first to the
	// second must therefore target Examples_2.md.
	first := readFile(t, filepath.Join(dir, "Examples.md"))
	if !strings.Contains(first, "[Examples](Examples_2.md)") {
		t.Fatalf("first community must cross-link to Examples_2.md, got:\n%s", first)
	}
}

func TestSafeFilenameUTF8Truncation(t *testing.T) {
	// A label long enough to exceed the 200-char cap, using a
	// multi-byte rune. Byte-slicing at 200 would split a 3-byte rune
	// and produce invalid UTF-8; rune-aware truncation keeps the cut
	// at a valid boundary.
	long := strings.Repeat("界", 300) // 3 bytes each × 300 = 900 bytes
	got := safeFilename(long)
	if !utf8.ValidString(got) {
		t.Fatalf("safeFilename produced invalid UTF-8: %q", got)
	}
	if len([]rune(got)) != 200 {
		t.Fatalf("expected 200 runes after truncation, got %d", len([]rune(got)))
	}
}

func TestSafeFilenameStrips(t *testing.T) {
	cases := map[string]string{
		"Plain":          "Plain",
		"Has Space":      "Has_Space",
		"path/with/seps": "path-with-seps",
		"bad<>chars":     "bad__chars",
		"":               "unnamed",
		"trailing.":      "trailing",
	}
	for in, want := range cases {
		if got := safeFilename(in); got != want {
			t.Errorf("safeFilename(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUniqueSlugCollision(t *testing.T) {
	used := map[string]bool{}
	if got := uniqueSlug(used, "x"); got != "x" {
		t.Fatalf("first call: got %q want %q", got, "x")
	}
	if got := uniqueSlug(used, "x"); got != "x_2" {
		t.Fatalf("second call: got %q want %q", got, "x_2")
	}
	if got := uniqueSlug(used, "x"); got != "x_3" {
		t.Fatalf("third call: got %q want %q", got, "x_3")
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
