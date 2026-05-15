package labels

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/julianshen/gogfy/internal/schema"
)

func TestGeneratePicksHighestDegreeNodeLabel(t *testing.T) {
	// Community "1": node "b" has degree 2 (connected to a and c), beats a/c each at 1.
	nodes := []schema.Node{
		{ID: "a", Label: "Alpha", Community: "1"},
		{ID: "b", Label: "Beta", Community: "1"},
		{ID: "c", Label: "Gamma", Community: "1"},
		{ID: "d", Label: "Delta", Community: "2"},
	}
	edges := []schema.Edge{
		{Source: "a", Target: "b", Relation: "calls"},
		{Source: "b", Target: "c", Relation: "calls"},
	}
	got := Generate(nodes, edges)
	if got["1"] != "Beta" {
		t.Fatalf("community 1: got %q want %q", got["1"], "Beta")
	}
	if got["2"] != "Delta" {
		t.Fatalf("community 2: got %q want %q", got["2"], "Delta")
	}
}

func TestGenerateBreaksDegreeTiesByLabel(t *testing.T) {
	// All nodes degree 0 — must be deterministic across runs. Tie-break
	// alphabetically on label so output is reproducible.
	nodes := []schema.Node{
		{ID: "z", Label: "Zeta", Community: "1"},
		{ID: "a", Label: "Alpha", Community: "1"},
	}
	got := Generate(nodes, nil)
	if got["1"] != "Alpha" {
		t.Fatalf("expected alphabetical tie-break: got %q", got["1"])
	}
}

func TestGenerateSkipsEmptyCommunity(t *testing.T) {
	// Nodes without Community shouldn't synthesize an entry for "".
	nodes := []schema.Node{{ID: "a", Label: "Alpha"}}
	got := Generate(nodes, nil)
	if _, ok := got[""]; ok {
		t.Fatalf("must not emit entry for empty community")
	}
}

func TestGenerateSanitizesControlChars(t *testing.T) {
	nodes := []schema.Node{{ID: "a", Label: "Bad\x00Label\nx", Community: "1"}}
	got := Generate(nodes, nil)
	if strings.ContainsAny(got["1"], "\x00\n\r\t") {
		t.Fatalf("control chars must be stripped: %q", got["1"])
	}
}

func TestGenerateCapsAt256Chars(t *testing.T) {
	long := strings.Repeat("x", 500)
	nodes := []schema.Node{{ID: "a", Label: long, Community: "1"}}
	got := Generate(nodes, nil)
	if len([]rune(got["1"])) > 256 {
		t.Fatalf("label not capped at 256 runes, got %d", len([]rune(got["1"])))
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".graphify_labels.json")
	in := map[string]string{"1": "Auth", "2": "Storage"}
	if err := Save(path, in); err != nil {
		t.Fatal(err)
	}
	out, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 || out["1"] != "Auth" || out["2"] != "Storage" {
		t.Fatalf("round-trip mismatch: %+v", out)
	}
}

func TestLoadMissingReturnsErrNotExist(t *testing.T) {
	// Callers (wiki CLI) detect missing via errors.Is(os.ErrNotExist) to
	// fall back to default "Community <N>" naming silently.
	_, err := Load(filepath.Join(t.TempDir(), "missing.json"))
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("must wrap os.ErrNotExist: %v", err)
	}
}

func TestSaveProducesStableJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "l.json")
	in := map[string]string{"3": "C", "1": "A", "2": "B"}
	if err := Save(path, in); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Keys must appear in sorted order for byte-stable diffs.
	s := string(data)
	if strings.Index(s, `"1"`) > strings.Index(s, `"2"`) || strings.Index(s, `"2"`) > strings.Index(s, `"3"`) {
		t.Fatalf("keys not sorted: %s", s)
	}
}
