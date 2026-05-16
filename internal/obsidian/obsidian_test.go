package obsidian

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/julianshen/gogfy/internal/schema"
)

func TestGenerateWritesPerNodeMarkdownAndCommunityNotes(t *testing.T) {
	dir := t.TempDir()
	nodes := []schema.Node{
		{ID: "a", Label: "Auth", Community: "1", FileType: schema.FileTypeCode, SourceFile: "src/auth.go"},
		{ID: "b", Label: "Billing", Community: "1", FileType: schema.FileTypeCode, SourceFile: "src/billing.go"},
		{ID: "c", Label: "Cache", Community: "2", FileType: schema.FileTypeCode, SourceFile: "src/cache.go"},
	}
	edges := []schema.Edge{
		{Source: "a", Target: "b", Relation: "calls", Confidence: schema.Extracted},
		{Source: "a", Target: "c", Relation: "uses", Confidence: schema.Inferred},
	}
	count, err := Generate(nodes, edges, Options{OutDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if count < 3 {
		t.Fatalf("expected at least 3 written notes (one per node), got %d", count)
	}

	authPath := filepath.Join(dir, "Auth.md")
	authBytes, err := os.ReadFile(authPath)
	if err != nil {
		t.Fatalf("expected per-node Auth.md: %v", err)
	}
	auth := string(authBytes)
	// Frontmatter
	for _, want := range []string{`---`, `source_file: "src/auth.go"`, `type: "code"`} {
		if !strings.Contains(auth, want) {
			t.Errorf("Auth.md missing frontmatter %q: %s", want, auth)
		}
	}
	// Connections — wikilinks to neighbors
	if !strings.Contains(auth, "[[Billing]]") || !strings.Contains(auth, "[[Cache]]") {
		t.Errorf("Auth.md missing wikilinks to neighbors: %s", auth)
	}
	// Relation + confidence rendered on the connection line
	if !strings.Contains(auth, "`calls`") || !strings.Contains(auth, "EXTRACTED") {
		t.Errorf("Auth.md missing relation/confidence: %s", auth)
	}
	// Community tag
	if !strings.Contains(auth, "community/") {
		t.Errorf("Auth.md missing community tag: %s", auth)
	}
}

func TestGenerateCommunityOverviewNotes(t *testing.T) {
	dir := t.TempDir()
	nodes := []schema.Node{
		{ID: "a", Label: "Auth", Community: "1"},
		{ID: "b", Label: "Billing", Community: "1"},
		{ID: "c", Label: "Cache", Community: "2"},
		{ID: "d", Label: "Disk", Community: "2"},
	}
	edges := []schema.Edge{
		{Source: "a", Target: "c"},
	}
	if _, err := Generate(nodes, edges, Options{OutDir: dir}); err != nil {
		t.Fatal(err)
	}

	// _COMMUNITY_<name>.md files sort to top of vault listings via underscore prefix.
	files, err := filepath.Glob(filepath.Join(dir, "_COMMUNITY_*.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 community overview notes, got %d: %v", len(files), files)
	}
	// First community overview must list its members + cross-community edges.
	for _, p := range files {
		body, _ := os.ReadFile(p)
		bs := string(body)
		if !strings.Contains(bs, "## Members") {
			t.Errorf("%s missing Members section: %s", p, bs)
		}
	}
	// At least one note must mention the cross-community link.
	hit := false
	for _, p := range files {
		body, _ := os.ReadFile(p)
		if strings.Contains(string(body), "Connections to other communities") {
			hit = true
			break
		}
	}
	if !hit {
		t.Fatal("no community note surfaced cross-community connections")
	}
}

func TestGenerateLabelCollisionDedup(t *testing.T) {
	// Two nodes with the same label must get unique filenames so wikilinks
	// don't collide and the writer doesn't silently clobber one file.
	dir := t.TempDir()
	nodes := []schema.Node{
		{ID: "a", Label: "Helper", Community: "1"},
		{ID: "b", Label: "Helper", Community: "1"},
	}
	if _, err := Generate(nodes, nil, Options{OutDir: dir}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "Helper.md")); err != nil {
		t.Fatalf("Helper.md missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "Helper_1.md")); err != nil {
		t.Fatalf("Helper_1.md missing (dedup suffix not applied): %v", err)
	}
}

func TestGenerateSanitizesUnsafeFilenameChars(t *testing.T) {
	// Slashes, colons, asterisks, etc. are illegal in many OS file systems
	// and must be stripped. Trailing .md must also strip so a label like
	// "README.md" doesn't produce "README.md.md".
	dir := t.TempDir()
	nodes := []schema.Node{
		{ID: "a", Label: "foo/bar:baz", Community: "1"},
		{ID: "b", Label: "README.md", Community: "1"},
	}
	if _, err := Generate(nodes, nil, Options{OutDir: dir}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "README.md")); err != nil {
		t.Fatalf("README.md should be a single .md, not README.md.md: %v", err)
	}
	// No file should have a `/` or `:` in its name.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.ContainsAny(e.Name(), `/\:*?"<>|`) {
			t.Errorf("unsafe char in filename: %q", e.Name())
		}
	}
}

func TestGenerateUsesCommunityLabels(t *testing.T) {
	dir := t.TempDir()
	nodes := []schema.Node{
		{ID: "a", Label: "A", Community: "1"},
		{ID: "b", Label: "B", Community: "1"},
	}
	if _, err := Generate(nodes, nil, Options{
		OutDir:          dir,
		CommunityLabels: map[string]string{"1": "Auth Layer"},
	}); err != nil {
		t.Fatal(err)
	}
	// The community overview note should use the custom name.
	matches, _ := filepath.Glob(filepath.Join(dir, "_COMMUNITY_Auth Layer.md"))
	if len(matches) != 1 {
		t.Fatalf("expected _COMMUNITY_Auth Layer.md (custom label), got %v", matches)
	}
}

func TestGenerateEmptyInputNoOps(t *testing.T) {
	dir := t.TempDir()
	count, err := Generate(nil, nil, Options{OutDir: dir})
	if err != nil {
		t.Fatalf("empty input should not error: %v", err)
	}
	if count != 0 {
		t.Fatalf("empty input should write 0 notes, got %d", count)
	}
}

func TestGenerateCaseInsensitiveCollision(t *testing.T) {
	// Foo / foo must produce distinct files even on case-insensitive
	// filesystems (macOS APFS, Windows NTFS) — otherwise WriteFileAtomic
	// silently clobbers one and leaves dangling wikilinks.
	dir := t.TempDir()
	nodes := []schema.Node{
		{ID: "a", Label: "Foo", Community: "1"},
		{ID: "b", Label: "foo", Community: "1"},
	}
	if _, err := Generate(nodes, nil, Options{OutDir: dir}); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	got := 0
	for _, e := range entries {
		if strings.EqualFold(e.Name(), "foo.md") || strings.HasPrefix(strings.ToLower(e.Name()), "foo_") {
			got++
		}
	}
	if got < 2 {
		t.Fatalf("expected 2 distinct files for case-only collision, got %d (entries=%v)", got, entries)
	}
}

func TestGenerateWindowsReservedBasename(t *testing.T) {
	// Labels matching Windows reserved basenames (CON, AUX, NUL, …) must
	// be suffixed so the atomic write can succeed cross-platform.
	dir := t.TempDir()
	nodes := []schema.Node{
		{ID: "a", Label: "CON", Community: "1"},
	}
	if _, err := Generate(nodes, nil, Options{OutDir: dir}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "CON_.md")); err != nil {
		t.Fatalf("reserved basename should be suffixed with _: %v", err)
	}
}

func TestGenerateThreeWayCollisionDedup(t *testing.T) {
	dir := t.TempDir()
	nodes := []schema.Node{
		{ID: "a", Label: "Helper", Community: "1"},
		{ID: "b", Label: "Helper", Community: "1"},
		{ID: "c", Label: "Helper", Community: "1"},
	}
	if _, err := Generate(nodes, nil, Options{OutDir: dir}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Helper.md", "Helper_1.md", "Helper_2.md"} {
		if _, err := os.Stat(filepath.Join(dir, want)); err != nil {
			t.Errorf("expected %s: %v", want, err)
		}
	}
}

func TestGenerateDominantConfidenceTieBreak(t *testing.T) {
	// Auth has 2 Inferred + 2 Extracted edges. Tie-break is alphabetic
	// on the string ("AMBIGUOUS" < "EXTRACTED" < "INFERRED"), so the
	// dominant confidence is "EXTRACTED" — visible as graphify/EXTRACTED.
	dir := t.TempDir()
	nodes := []schema.Node{
		{ID: "a", Label: "Auth", Community: "1"},
		{ID: "b", Label: "B", Community: "1"},
		{ID: "c", Label: "C", Community: "1"},
		{ID: "d", Label: "D", Community: "1"},
		{ID: "e", Label: "E", Community: "1"},
	}
	edges := []schema.Edge{
		{Source: "a", Target: "b", Confidence: schema.Extracted},
		{Source: "a", Target: "c", Confidence: schema.Extracted},
		{Source: "a", Target: "d", Confidence: schema.Inferred},
		{Source: "a", Target: "e", Confidence: schema.Inferred},
	}
	if _, err := Generate(nodes, edges, Options{OutDir: dir}); err != nil {
		t.Fatal(err)
	}
	auth, _ := os.ReadFile(filepath.Join(dir, "Auth.md"))
	if !strings.Contains(string(auth), "graphify/EXTRACTED") {
		t.Fatalf("tie-break should yield alphabetically smallest confidence (EXTRACTED): %s", auth)
	}
}

func TestGenerateObsidianTagFoldsSpacesInLabels(t *testing.T) {
	// Community names with spaces must be foldable to Obsidian tag
	// syntax (no spaces). Both the inline tag and the Dataview block
	// must use the underscore form.
	dir := t.TempDir()
	nodes := []schema.Node{
		{ID: "a", Label: "A", Community: "1"},
		{ID: "b", Label: "B", Community: "1"},
	}
	if _, err := Generate(nodes, nil, Options{
		OutDir:          dir,
		CommunityLabels: map[string]string{"1": "Auth Layer"},
	}); err != nil {
		t.Fatal(err)
	}
	a, _ := os.ReadFile(filepath.Join(dir, "A.md"))
	if !strings.Contains(string(a), "#community/Auth_Layer") {
		t.Fatalf("space in community name must fold to underscore in inline tag: %s", a)
	}
	overview, _ := os.ReadFile(filepath.Join(dir, "_COMMUNITY_Auth Layer.md"))
	if !strings.Contains(string(overview), "#community/Auth_Layer") {
		t.Fatalf("Dataview FROM clause must use underscore-form tag: %s", overview)
	}
}

func TestGenerateCrossCommunityEdgeCountOrdering(t *testing.T) {
	// Community 1 has more edges to community 3 than to community 2.
	// Output must order Connections-to-other by count desc.
	dir := t.TempDir()
	nodes := []schema.Node{
		{ID: "a1", Label: "A1", Community: "1"},
		{ID: "a2", Label: "A2", Community: "1"},
		{ID: "b1", Label: "B1", Community: "2"},
		{ID: "b2", Label: "B2", Community: "2"},
		{ID: "c1", Label: "C1", Community: "3"},
		{ID: "c2", Label: "C2", Community: "3"},
	}
	edges := []schema.Edge{
		{Source: "a1", Target: "b1"},                                  // 1→2: 1 edge
		{Source: "a1", Target: "c1"}, {Source: "a2", Target: "c2"},    // 1→3: 2 edges
	}
	if _, err := Generate(nodes, edges, Options{OutDir: dir}); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(filepath.Join(dir, "_COMMUNITY_Community 1.md"))
	s := string(body)
	c3Idx := strings.Index(s, "[[_COMMUNITY_Community 3]]")
	c2Idx := strings.Index(s, "[[_COMMUNITY_Community 2]]")
	if c3Idx < 0 || c2Idx < 0 {
		t.Fatalf("cross-community links missing: %s", s)
	}
	if c3Idx > c2Idx {
		t.Fatalf("community 3 (2 edges) must appear before community 2 (1 edge) in count-desc order: %s", s)
	}
}
