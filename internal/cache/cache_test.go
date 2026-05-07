package cache

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCacheSkipsUnchangedFiles(t *testing.T) {
	root := t.TempDir()
	f := filepath.Join(root, "main.go")
	os.WriteFile(f, []byte("package main"), 0644)

	c := NewCache(filepath.Join(root, ".gographify-cache"))
	changed, err := c.ChangedFiles([]string{f})
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 1 {
		t.Fatalf("expected 1 changed, got %d", len(changed))
	}

	// Save and check again
	if err := c.Save([]string{f}); err != nil {
		t.Fatal(err)
	}
	changed, err = c.ChangedFiles([]string{f})
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 0 {
		t.Fatalf("expected 0 changed, got %d", len(changed))
	}
}

func TestCacheDetectsModifiedFiles(t *testing.T) {
	root := t.TempDir()
	f := filepath.Join(root, "main.go")
	os.WriteFile(f, []byte("package main"), 0644)

	c := NewCache(filepath.Join(root, ".gographify-cache"))
	if err := c.Save([]string{f}); err != nil {
		t.Fatal(err)
	}

	// Modify file
	os.WriteFile(f, []byte("package main\n\nfunc main() {}"), 0644)

	changed, err := c.ChangedFiles([]string{f})
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 1 {
		t.Fatalf("expected 1 changed after modification, got %d", len(changed))
	}
}

func TestCacheCorruptFile(t *testing.T) {
	root := t.TempDir()
	cachePath := filepath.Join(root, ".gographify-cache")
	os.WriteFile(cachePath, []byte("not json"), 0644)

	c := NewCache(cachePath)
	_, err := c.ChangedFiles([]string{"/some/file.go"})
	if err == nil {
		t.Fatal("expected error for corrupt cache file")
	}
}

func TestCacheEmptyFileList(t *testing.T) {
	root := t.TempDir()
	c := NewCache(filepath.Join(root, ".gographify-cache"))
	changed, err := c.ChangedFiles([]string{})
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 0 {
		t.Fatalf("expected 0 changed for empty list, got %d", len(changed))
	}
}

func TestCacheMultipleFiles(t *testing.T) {
	root := t.TempDir()
	f1 := filepath.Join(root, "a.go")
	f2 := filepath.Join(root, "b.go")
	os.WriteFile(f1, []byte("package a"), 0644)
	os.WriteFile(f2, []byte("package b"), 0644)

	c := NewCache(filepath.Join(root, ".gographify-cache"))
	if err := c.Save([]string{f1, f2}); err != nil {
		t.Fatal(err)
	}

	// Modify only f1
	os.WriteFile(f1, []byte("package a\n"), 0644)

	changed, err := c.ChangedFiles([]string{f1, f2})
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 1 {
		t.Fatalf("expected 1 changed, got %d", len(changed))
	}
	if changed[0] != f1 {
		t.Fatalf("expected f1 changed, got %s", changed[0])
	}
}

func TestCacheSaveMergesPriorHashes(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a.go")
	b := filepath.Join(root, "b.go")
	os.WriteFile(a, []byte("package a"), 0644)
	os.WriteFile(b, []byte("package b"), 0644)

	c := NewCache(filepath.Join(root, ".gographify-cache"))

	// Initial save with both files (full run).
	if err := c.Save([]string{a, b}); err != nil {
		t.Fatal(err)
	}

	// Subsequent --update run only re-saves the changed subset (just `a`).
	os.WriteFile(a, []byte("package a\n"), 0644)
	if err := c.Save([]string{a}); err != nil {
		t.Fatal(err)
	}

	// `b` was unchanged and should still be remembered: querying both files
	// must report only `a` as changed (after another modification to a).
	os.WriteFile(a, []byte("package a\nvar X = 1"), 0644)
	changed, err := c.ChangedFiles([]string{a, b})
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 1 || changed[0] != a {
		t.Fatalf("Save({a}) lost prior hash for b; ChangedFiles returned %v, want [a]", changed)
	}
}

func TestCacheHashFileError(t *testing.T) {
	_, err := hashFile("/nonexistent/path/to/file.go")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}
