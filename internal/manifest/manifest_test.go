package manifest

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestBuildCapturesAllFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.go"), "package a\n")
	writeFile(t, filepath.Join(dir, "b.py"), "x = 1\n")

	m, err := Build([]string{filepath.Join(dir, "a.go"), filepath.Join(dir, "b.py")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(m.Entries))
	}
	// Sorted by path: a.go before b.py.
	if m.Entries[0].Ext != ".go" || m.Entries[1].Ext != ".py" {
		t.Errorf("entries not sorted or ext drift: %+v", m.Entries)
	}
	if m.Entries[0].SHA == "" || m.Entries[1].SHA == "" {
		t.Errorf("SHA missing: %+v", m.Entries)
	}
}

func TestBuildReusesSHAWhenMTimeUnchanged(t *testing.T) {
	// Fast path: if a prior entry has the same mtime + size, the SHA
	// is reused without re-hashing. Otherwise a 10k-file re-run pays
	// 10k SHA-256 reads for no information gain.
	dir := t.TempDir()
	p := filepath.Join(dir, "a.go")
	writeFile(t, p, "package a\n")
	first, err := Build([]string{p}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Plant a wrong SHA in the prior; if reuse is working, the
	// resulting manifest will surface that wrong SHA (not the real
	// one).
	first.Entries[0].SHA = "stub-not-real"
	second, err := Build([]string{p}, first)
	if err != nil {
		t.Fatal(err)
	}
	if second.Entries[0].SHA != "stub-not-real" {
		t.Errorf("SHA reuse failed — manifest re-hashed despite mtime match")
	}
}

func TestCompareDetectsAddedRemovedModified(t *testing.T) {
	prior := &Manifest{Entries: []Entry{
		{Path: "/x/a.go", SHA: "h1"},
		{Path: "/x/b.go", SHA: "h2"},
	}}
	current := &Manifest{Entries: []Entry{
		{Path: "/x/a.go", SHA: "h1-new"}, // modified
		{Path: "/x/c.go", SHA: "h3"},     // added
		// b.go removed
	}}
	d := Compare(prior, current)
	if len(d.Added) != 1 || d.Added[0] != "/x/c.go" {
		t.Errorf("added wrong: %v", d.Added)
	}
	if len(d.Removed) != 1 || d.Removed[0] != "/x/b.go" {
		t.Errorf("removed wrong: %v", d.Removed)
	}
	if len(d.Modified) != 1 || d.Modified[0] != "/x/a.go" {
		t.Errorf("modified wrong: %v", d.Modified)
	}
}

func TestCompareMTimeAloneDoesNotMarkModified(t *testing.T) {
	// `touch` / `git checkout` / rsync touch mtime without changing
	// content. The diff is content-based (SHA), so an mtime-only
	// change must NOT appear in Modified.
	prior := &Manifest{Entries: []Entry{
		{Path: "/x/a.go", MTime: 100, SHA: "h"},
	}}
	current := &Manifest{Entries: []Entry{
		{Path: "/x/a.go", MTime: 200, SHA: "h"},
	}}
	if d := Compare(prior, current); len(d.Modified) != 0 {
		t.Errorf("mtime-only change must not modify: %+v", d)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "manifest.json")
	want := &Manifest{Entries: []Entry{{Path: "/a.go", MTime: 100, Size: 42, SHA: "abc", Ext: ".go"}}}
	if err := Save(p, want); err != nil {
		t.Fatal(err)
	}
	got, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.Entries[0] != want.Entries[0] {
		t.Errorf("round-trip drift: %+v vs %+v", got.Entries[0], want.Entries[0])
	}
}

func TestLoadMissingReturnsNilNoError(t *testing.T) {
	got, err := Load(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Errorf("missing manifest must not be an error: %v", err)
	}
	if got != nil {
		t.Errorf("missing manifest must return nil, got %+v", got)
	}
}

func TestBuildDetectsContentChangeViaMTimeBump(t *testing.T) {
	// Modify a file with a real time gap so mtime differs, confirm
	// SHA refreshes.
	dir := t.TempDir()
	p := filepath.Join(dir, "a.go")
	writeFile(t, p, "package a\n")
	first, err := Build([]string{p}, nil)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond) // ensure mtime changes
	writeFile(t, p, "package b\n")    // different content
	second, err := Build([]string{p}, first)
	if err != nil {
		t.Fatal(err)
	}
	if first.Entries[0].SHA == second.Entries[0].SHA {
		t.Errorf("SHA didn't update after content change")
	}
}
