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
