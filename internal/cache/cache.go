// Package cache provides incremental caching based on file content hashes.
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
)

// Cache tracks file content hashes to support incremental builds.
type Cache struct {
	path string
}

// NewCache creates a Cache that persists to the given path.
func NewCache(path string) *Cache {
	return &Cache{path: path}
}

// ChangedFiles returns the subset of files whose content has changed since the last save.
func (c *Cache) ChangedFiles(files []string) ([]string, error) {
	oldHashes, err := c.load()
	if err != nil {
		if os.IsNotExist(err) {
			oldHashes = nil
		} else {
			return nil, err
		}
	}
	var changed []string
	for _, f := range files {
		h, err := hashFile(f)
		if err != nil {
			return nil, err
		}
		if oldHashes[f] != h {
			changed = append(changed, f)
		}
	}
	return changed, nil
}

// Save stores the current content hashes for the given files.
func (c *Cache) Save(files []string) error {
	hashes := make(map[string]string, len(files))
	for _, f := range files {
		h, err := hashFile(f)
		if err != nil {
			return err
		}
		hashes[f] = h
	}
	data, err := json.Marshal(hashes)
	if err != nil {
		return err
	}
	return os.WriteFile(c.path, data, 0644)
}

func (c *Cache) load() (map[string]string, error) {
	data, err := os.ReadFile(c.path)
	if err != nil {
		return nil, err
	}
	var hashes map[string]string
	if err := json.Unmarshal(data, &hashes); err != nil {
		return nil, err
	}
	return hashes, nil
}

func hashFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
