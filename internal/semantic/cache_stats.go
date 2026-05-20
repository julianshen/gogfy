package semantic

import (
	"os"
	"path/filepath"
	"time"
)

// CacheStats summarizes the on-disk semantic cache for tooling
// (gogfy cache stats). Walks the cache directory directly rather than
// loading entries — stats need size + age, not body content.
type CacheStats struct {
	Dir         string    // absolute path to the cache directory
	Entries     int       // number of *.json entries
	BytesOnDisk int64     // sum of file sizes
	OldestWrite time.Time // mtime of the oldest entry (zero if empty)
	NewestWrite time.Time // mtime of the newest entry
}

// Stats walks the cache directory and returns a CacheStats summary.
// Missing directory is NOT an error — returns a zero-value stats
// struct with Dir filled in so callers can report "no cache yet."
//
// The walk is shallow (top-level *.json only) because that's the
// only layout Store writes. A future hierarchical layout would
// require a recursive walk; the test pinning current behavior
// would surface that change.
func (c *Cache) Stats() (CacheStats, error) {
	if c == nil || c.dir == "" {
		return CacheStats{}, nil
	}
	stats := CacheStats{Dir: c.dir}
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return stats, nil
		}
		return stats, err
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue // best-effort; one unreadable entry shouldn't kill the report
		}
		stats.Entries++
		stats.BytesOnDisk += info.Size()
		mt := info.ModTime()
		if stats.OldestWrite.IsZero() || mt.Before(stats.OldestWrite) {
			stats.OldestWrite = mt
		}
		if mt.After(stats.NewestWrite) {
			stats.NewestWrite = mt
		}
	}
	return stats, nil
}

// ClearOlderThan removes cache entries whose mtime is older than the
// cutoff. Returns the count of entries removed. Useful for cron-style
// "trim cache older than 7 days" workflows.
//
// A cutoff in the future (cutoff > now) is allowed — removes every
// entry. A zero cutoff is treated as "remove nothing" so a caller
// that forgot to set it doesn't accidentally wipe the cache.
func (c *Cache) ClearOlderThan(cutoff time.Time) (int, error) {
	if c == nil || c.dir == "" {
		return 0, nil
	}
	if cutoff.IsZero() {
		return 0, nil
	}
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	removed := 0
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(cutoff) {
			continue
		}
		if err := os.Remove(filepath.Join(c.dir, e.Name())); err != nil {
			// Don't error on a single removal failure — other entries
			// can still be cleaned.
			continue
		}
		removed++
	}
	return removed, nil
}

// ClearAll removes every entry in the cache. Returns the count of
// entries removed. Equivalent to `ClearOlderThan(<far future>)` but
// avoids the time math.
func (c *Cache) ClearAll() (int, error) {
	if c == nil || c.dir == "" {
		return 0, nil
	}
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	removed := 0
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		if err := os.Remove(filepath.Join(c.dir, e.Name())); err != nil {
			continue
		}
		removed++
	}
	return removed, nil
}
