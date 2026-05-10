// Package fsutil collects small filesystem helpers shared across the
// installer and githook packages: atomic writes and tolerant reads.
package fsutil

import (
	"os"
	"path/filepath"
)

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
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
