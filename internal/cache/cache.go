// Package cache provides incremental caching with an mtime fast-path:
// unchanged mtime skips the hash entirely, so a 10K-file corpus pays
// O(N) stat() calls rather than O(N) SHA-256 reads on subsequent runs.
// mtime-bumped files fall through to the hash check so sync tools that
// touch mtime (rsync, git checkout, IDE save-without-change) don't
// cause spurious re-extraction.
package cache

import (
	"encoding/json"
	"os"

	"github.com/julianshen/gogfy/internal/fsutil"
)

// Entry is the per-file cache record. mtime is unix nanoseconds —
// matches os.FileInfo.ModTime().UnixNano() so a stored value compares
// equal to a freshly-statted one without rounding.
type Entry struct {
	MTime int64  `json:"mtime"`
	Hash  string `json:"hash"`
}

// Cache tracks file content hashes + mtimes to support incremental builds.
type Cache struct {
	path string
}

// NewCache creates a Cache that persists to the given path.
func NewCache(path string) *Cache {
	return &Cache{path: path}
}

// ChangedFiles returns the subset of files whose content has changed
// since the last save. Decision tree per file:
//   - mtime matches stored mtime → unchanged (no hash)
//   - mtime differs but hash matches → unchanged (sync touched mtime)
//   - hash differs (or no entry) → changed
func (c *Cache) ChangedFiles(files []string) ([]string, error) {
	prev, err := c.load()
	if err != nil {
		if os.IsNotExist(err) {
			prev = nil
		} else {
			return nil, err
		}
	}
	var changed []string
	for _, f := range files {
		entry, hasEntry := prev[f]
		if hasEntry {
			info, statErr := os.Stat(f)
			// The `MTime != 0` guard rejects legacy-upgraded entries from
			// the fast path. A real file with mtime == epoch (Unix 0)
			// will pay the hash cost on every run; vanishingly rare in
			// practice, and ambiguous with "no trustworthy mtime."
			if statErr == nil && info.ModTime().UnixNano() == entry.MTime && entry.MTime != 0 {
				continue
			}
		}
		// Slow path: hash and compare. A missing-entry case still
		// hashes so a brand-new file is correctly flagged changed.
		h, err := hashFile(f)
		if err != nil {
			return nil, err
		}
		if hasEntry && h == entry.Hash {
			continue
		}
		changed = append(changed, f)
	}
	return changed, nil
}

// Save captures mtime + content hash per file, merging with any prior
// entries so an incremental run that passes only the changed-file
// subset doesn't drop unchanged entries.
//
// Save uses the same mtime fast-path as ChangedFiles: if a file's mtime
// matches the prior entry's, the stored hash is reused without
// re-hashing. Without this, callers passing the full file list to Save
// would pay N SHA-256 reads on every steady-state run, defeating the
// win from ChangedFiles.
func (c *Cache) Save(files []string) error {
	prev, err := c.load()
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		prev = map[string]Entry{}
	}
	for _, f := range files {
		info, err := os.Stat(f)
		if err != nil {
			return err
		}
		mtime := info.ModTime().UnixNano()
		if old, ok := prev[f]; ok && old.MTime == mtime && old.MTime != 0 && old.Hash != "" {
			continue
		}
		h, err := hashFile(f)
		if err != nil {
			return err
		}
		prev[f] = Entry{MTime: mtime, Hash: h}
	}
	data, err := json.Marshal(prev)
	if err != nil {
		return err
	}
	// Atomic write: a partial write previously corrupted the cache,
	// forcing a full re-extract on the next run.
	return fsutil.WriteFileAtomic(c.path, data, 0644)
}

// load reads the cache file. Backwards compatible: legacy files stored
// `{path: "hashstring"}` (no mtime); those entries decode with mtime=0,
// which forces the slow-path hash check on first access — the entry is
// then upgraded to {mtime, hash} on the next Save.
func (c *Cache) load() (map[string]Entry, error) {
	data, err := os.ReadFile(c.path)
	if err != nil {
		return nil, err
	}
	// Try the modern {mtime, hash} schema first.
	var modern map[string]Entry
	if err := json.Unmarshal(data, &modern); err == nil && validateEntries(modern) {
		return modern, nil
	}
	// Fall back to the legacy hash-only schema.
	var legacy map[string]string
	if err := json.Unmarshal(data, &legacy); err != nil {
		return nil, err
	}
	out := make(map[string]Entry, len(legacy))
	for k, v := range legacy {
		out[k] = Entry{Hash: v}
	}
	return out, nil
}

// validateEntries rejects modern-schema decodes that came from a legacy
// file (every entry has empty Hash and zero MTime — json.Unmarshal of
// `{"a":"hex"}` into map[string]Entry "succeeds" with zero values).
func validateEntries(m map[string]Entry) bool {
	for _, e := range m {
		if e.Hash != "" || e.MTime != 0 {
			return true
		}
	}
	return len(m) == 0
}

func hashFile(path string) (string, error) {
	return fsutil.SHA256File(path)
}
