// Package fsutil collects small filesystem helpers shared across the
// installer and githook packages: atomic writes and tolerant reads.
package fsutil

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
)

// SHA256File streams the file at path through SHA-256 and returns the
// hex digest. Used by `cache` for source-file fingerprinting and by
// `globalgraph` for source-graph idempotency. Streaming (vs ReadFile +
// Sum256) keeps peak memory bounded for large graph.json inputs.
func SHA256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// ReadFileOrEmpty reads path, returning nil bytes for ENOENT (caller
// treats as "no file yet"). Any other error propagates.
func ReadFileOrEmpty(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return data, nil
}

// WriteFileAtomic creates the parent directory then writes data via a
// sibling .tmp file followed by rename, so a partial write cannot replace
// a previously good file with a truncated one. perm is applied via the
// initial WriteFile and survives the rename.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		// A partial write may still have created the file; clean it up
		// symmetrically with the rename-failure path so we never leave
		// a stale .tmp behind.
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
