// Package manifest persists corpus state (per-file mtime + SHA-256 +
// size + extension) so callers can answer "what changed since the
// last manifest?" without re-walking and re-hashing every file.
//
// This is the detection-layer counterpart to internal/cache (which
// gates extraction on the same metadata). The two could share storage
// in theory, but the manifest is intended to be readable by external
// tooling (CI, status reporters) so its file lives at a stable
// location with a documented schema rather than an internal cache
// path.
package manifest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/julianshen/gogfy/internal/fsutil"
)

// Entry is the per-file manifest record.
type Entry struct {
	Path  string `json:"path"`
	MTime int64  `json:"mtime"` // unix nanoseconds, matches os.FileInfo.ModTime().UnixNano()
	Size  int64  `json:"size"`
	SHA   string `json:"sha256"`
	Ext   string `json:"ext"` // lowercase extension (with dot), or "" for extensionless files
}

// Manifest is a sorted list of entries. Sorted by path so two
// manifests over the same corpus produce byte-identical JSON, which
// makes the file diffable with regular text tools.
type Manifest struct {
	Entries []Entry `json:"entries"`
}

// Build walks `files` (a caller-supplied path list — typically the
// output of detect.CollectFiles) and computes a fresh manifest.
// Reuses prior entries' SHA when mtime matches, mirroring
// internal/cache's fast path so re-running on an unchanged corpus
// pays only stat() per file, not hash.
func Build(files []string, prior *Manifest) (*Manifest, error) {
	priorByPath := map[string]Entry{}
	if prior != nil {
		for _, e := range prior.Entries {
			priorByPath[e.Path] = e
		}
	}
	out := &Manifest{Entries: make([]Entry, 0, len(files))}
	for _, f := range files {
		info, err := os.Stat(f)
		if err != nil {
			return nil, fmt.Errorf("manifest: stat %s: %w", f, err)
		}
		mtime := info.ModTime().UnixNano()
		entry := Entry{
			Path:  f,
			MTime: mtime,
			Size:  info.Size(),
			Ext:   filepath.Ext(f),
		}
		if old, ok := priorByPath[f]; ok && old.MTime == mtime && old.MTime != 0 && old.Size == info.Size() {
			entry.SHA = old.SHA
		} else {
			h, err := fsutil.SHA256File(f)
			if err != nil {
				return nil, fmt.Errorf("manifest: hash %s: %w", f, err)
			}
			entry.SHA = h
		}
		out.Entries = append(out.Entries, entry)
	}
	sort.Slice(out.Entries, func(i, j int) bool { return out.Entries[i].Path < out.Entries[j].Path })
	return out, nil
}

// Diff is the set of changes between two manifests, oriented as
// "what happened to get from prior to current".
type Diff struct {
	Added    []string `json:"added"`
	Removed  []string `json:"removed"`
	Modified []string `json:"modified"`
}

// Compare returns the diff between two manifests. A file is
// "modified" when its path is present in both but the SHA differs;
// "added" when present only in current; "removed" when present only
// in prior. mtime alone does not flag modification — sync tools
// (rsync, git checkout) frequently touch mtime without changing
// content.
func Compare(prior, current *Manifest) Diff {
	priorByPath := map[string]Entry{}
	if prior != nil {
		for _, e := range prior.Entries {
			priorByPath[e.Path] = e
		}
	}
	curByPath := map[string]Entry{}
	if current != nil {
		for _, e := range current.Entries {
			curByPath[e.Path] = e
		}
	}
	var d Diff
	for p, ce := range curByPath {
		pe, was := priorByPath[p]
		switch {
		case !was:
			d.Added = append(d.Added, p)
		case pe.SHA != ce.SHA:
			d.Modified = append(d.Modified, p)
		}
	}
	for p := range priorByPath {
		if _, ok := curByPath[p]; !ok {
			d.Removed = append(d.Removed, p)
		}
	}
	sort.Strings(d.Added)
	sort.Strings(d.Removed)
	sort.Strings(d.Modified)
	return d
}

// Save writes m to path. Atomic via fsutil so a crashed run doesn't
// leave a half-flushed manifest.
func Save(path string, m *Manifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(path, data, 0644)
}

// Load reads a manifest from path. Returns (nil, nil) when path
// doesn't exist — callers should treat a missing manifest as "first
// run" rather than an error.
func Load(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("manifest: parse %s: %w", path, err)
	}
	return &m, nil
}
