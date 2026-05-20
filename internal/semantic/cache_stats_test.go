package semantic

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/julianshen/gogfy/internal/schema"
)

func TestCacheStatsEmptyCache(t *testing.T) {
	// An un-populated cache dir (the common first-run case) must
	// return zero stats, not an error. Tooling that prints "your
	// cache is empty" depends on this.
	dir := t.TempDir()
	cache := NewCache(filepath.Join(dir, "never-written"))
	stats, err := cache.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.Entries != 0 {
		t.Errorf("expected 0 entries on empty cache, got %d", stats.Entries)
	}
	if stats.BytesOnDisk != 0 {
		t.Errorf("expected 0 bytes on empty cache, got %d", stats.BytesOnDisk)
	}
}

func TestCacheStatsReportsEntryCountAndSize(t *testing.T) {
	dir := t.TempDir()
	cache := NewCache(dir)
	// Populate via the real Store path so the test exercises the
	// same layout production uses.
	for i := 0; i < 3; i++ {
		if err := cache.Store("k"+string(rune('a'+i)), Result{
			Nodes: []schema.Node{{ID: "n", Label: "label"}},
		}); err != nil {
			t.Fatal(err)
		}
	}
	stats, err := cache.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.Entries != 3 {
		t.Errorf("expected 3 entries, got %d", stats.Entries)
	}
	if stats.BytesOnDisk == 0 {
		t.Errorf("expected nonzero bytes-on-disk, got 0")
	}
	if stats.OldestWrite.IsZero() || stats.NewestWrite.IsZero() {
		t.Errorf("oldest/newest write timestamps not set: %+v", stats)
	}
}

func TestCacheClearOlderThanRespectsCutoff(t *testing.T) {
	// Write three entries with controlled mtimes. Clear with a
	// cutoff between them; only the older ones should disappear.
	dir := t.TempDir()
	cache := NewCache(dir)
	for i := 0; i < 3; i++ {
		_ = cache.Store("k"+string(rune('a'+i)), Result{})
	}
	// Backdate two of the three entries so they fall on the
	// "remove" side of the cutoff.
	now := time.Now()
	old := now.Add(-2 * time.Hour)
	for _, name := range []string{"ka.json", "kb.json"} {
		if err := os.Chtimes(filepath.Join(dir, name), old, old); err != nil {
			t.Fatal(err)
		}
	}
	cutoff := now.Add(-1 * time.Hour)
	removed, err := cache.ClearOlderThan(cutoff)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 2 {
		t.Errorf("expected 2 removals, got %d", removed)
	}
	stats, _ := cache.Stats()
	if stats.Entries != 1 {
		t.Errorf("expected 1 entry remaining, got %d", stats.Entries)
	}
}

func TestCacheClearOlderThanZeroCutoffIsNoOp(t *testing.T) {
	// A caller that forgot to set --older-than shouldn't accidentally
	// wipe their cache. Zero cutoff returns 0 removed without
	// touching anything.
	dir := t.TempDir()
	cache := NewCache(dir)
	_ = cache.Store("x", Result{})
	removed, err := cache.ClearOlderThan(time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if removed != 0 {
		t.Errorf("zero cutoff should be a no-op, removed %d", removed)
	}
	stats, _ := cache.Stats()
	if stats.Entries != 1 {
		t.Errorf("entry was wiped despite zero cutoff: %+v", stats)
	}
}

func TestCacheClearAllRemovesEverything(t *testing.T) {
	dir := t.TempDir()
	cache := NewCache(dir)
	for i := 0; i < 4; i++ {
		_ = cache.Store("k"+string(rune('a'+i)), Result{})
	}
	removed, err := cache.ClearAll()
	if err != nil {
		t.Fatal(err)
	}
	if removed != 4 {
		t.Errorf("expected 4 removals, got %d", removed)
	}
	stats, _ := cache.Stats()
	if stats.Entries != 0 {
		t.Errorf("cache not empty after ClearAll: %+v", stats)
	}
}

func TestCacheStatsNilSafe(t *testing.T) {
	// nil *Cache is the disabled signal documented on Lookup/Store.
	// Stats / ClearOlderThan / ClearAll must follow the same contract.
	var c *Cache
	if _, err := c.Stats(); err != nil {
		t.Errorf("nil cache Stats: %v", err)
	}
	if _, err := c.ClearOlderThan(time.Now()); err != nil {
		t.Errorf("nil cache ClearOlderThan: %v", err)
	}
	if _, err := c.ClearAll(); err != nil {
		t.Errorf("nil cache ClearAll: %v", err)
	}
}
