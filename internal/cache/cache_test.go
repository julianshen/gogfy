package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"
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

func TestCacheMtimeFastPathSkipsHashing(t *testing.T) {
	// Verify the mtime fast path: an unchanged file should NOT be
	// re-hashed (hashing is the dominant cost on large corpora).
	// We sniff this by making the file *unreadable* between Save and
	// ChangedFiles — if ChangedFiles tries to hash, it'll error; if it
	// uses the mtime fast path it'll succeed.
	root := t.TempDir()
	f := filepath.Join(root, "main.go")
	if err := os.WriteFile(f, []byte("package main"), 0644); err != nil {
		t.Fatal(err)
	}
	c := NewCache(filepath.Join(root, ".cache"))
	if err := c.Save([]string{f}); err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(f, 0); err != nil {
		t.Skipf("chmod 0 not honored: %v", err)
	}
	defer os.Chmod(f, 0644)
	if data, err := os.ReadFile(f); err == nil {
		t.Skipf("env allows reading chmod-0 file (data=%v); fast-path can't be verified", data)
	}

	changed, err := c.ChangedFiles([]string{f})
	if err != nil {
		t.Fatalf("mtime fast path should skip hashing (would otherwise fail to read): %v", err)
	}
	if len(changed) != 0 {
		t.Fatalf("expected unchanged file via mtime, got %d changed", len(changed))
	}
}

func TestCacheMtimeBumpedSameHashTreatedUnchanged(t *testing.T) {
	// Sync tools (git checkout, rsync, IDE save-without-change) bump
	// mtime without modifying content. Without the slow-path hash check
	// we'd re-extract the file pointlessly.
	root := t.TempDir()
	f := filepath.Join(root, "main.go")
	if err := os.WriteFile(f, []byte("package main"), 0644); err != nil {
		t.Fatal(err)
	}
	c := NewCache(filepath.Join(root, ".cache"))
	if err := c.Save([]string{f}); err != nil {
		t.Fatal(err)
	}

	// Bump mtime without modifying content (touch).
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(f, future, future); err != nil {
		t.Skipf("chtimes failed: %v", err)
	}

	changed, err := c.ChangedFiles([]string{f})
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 0 {
		t.Fatalf("touched-but-identical file should not be flagged changed: %d", len(changed))
	}
}

func TestCacheLegacyHashOnlyEntriesUpgradeCleanly(t *testing.T) {
	// Existing manifests on disk store {path: hashString}. After
	// upgrading the cache binary, those entries should still be
	// recognized — we hash once to verify and then upgrade the entry
	// to {mtime,hash} on the next Save.
	root := t.TempDir()
	f := filepath.Join(root, "main.go")
	if err := os.WriteFile(f, []byte("package main"), 0644); err != nil {
		t.Fatal(err)
	}
	cachePath := filepath.Join(root, ".cache")
	// Pre-populate legacy-format cache.
	legacy := `{"` + f + `":"` + sha256OfFile(t, f) + `"}`
	if err := os.WriteFile(cachePath, []byte(legacy), 0644); err != nil {
		t.Fatal(err)
	}
	c := NewCache(cachePath)
	changed, err := c.ChangedFiles([]string{f})
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 0 {
		t.Fatalf("legacy-format hash-only entry should be honored: %d changed", len(changed))
	}
}

func sha256OfFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestCacheSaveFastPathSkipsRehashing(t *testing.T) {
	// In steady state (no file changes), a second Save should not
	// re-hash files — otherwise callers passing the full file list
	// to Save would pay N SHA-256 reads per run and defeat the
	// ChangedFiles fast-path. Sniff this by making files unreadable
	// between Save invocations: if Save re-hashes, it fails to read.
	root := t.TempDir()
	f := filepath.Join(root, "main.go")
	if err := os.WriteFile(f, []byte("package main"), 0644); err != nil {
		t.Fatal(err)
	}
	c := NewCache(filepath.Join(root, ".cache"))
	if err := c.Save([]string{f}); err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(f, 0); err != nil {
		t.Skipf("chmod 0 not honored: %v", err)
	}
	defer os.Chmod(f, 0644)
	if _, err := os.ReadFile(f); err == nil {
		t.Skip("env allows reading chmod-0 file; can't verify fast-path")
	}

	if err := c.Save([]string{f}); err != nil {
		t.Fatalf("Save fast-path should reuse hash when mtime unchanged: %v", err)
	}
}
