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
