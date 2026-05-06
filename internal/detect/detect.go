// Package detect discovers source files in a directory tree, respecting ignore patterns.
package detect

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// CollectFiles recursively collects files under root matching the given extensions,
// skipping entries matched by .graphifyignore patterns.
func CollectFiles(root string, extensions []string) ([]string, error) {
	ignorePatterns, err := loadIgnorePatterns(root)
	if err != nil {
		return nil, err
	}

	extSet := make(map[string]struct{}, len(extensions))
	for _, e := range extensions {
		extSet[e] = struct{}{}
	}

	var files []string
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		for _, pat := range ignorePatterns {
			matched, err := filepath.Match(pat, rel)
			if err != nil {
				return err
			}
			if matched {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			// Handle directory prefix patterns like "vendor/"
			if strings.HasSuffix(pat, "/") {
				dirPat := strings.TrimSuffix(pat, "/")
				if rel == dirPat || strings.HasPrefix(rel, dirPat+string(filepath.Separator)) {
					if info.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
			}
		}
		if !info.IsDir() {
			ext := filepath.Ext(path)
			if _, ok := extSet[ext]; ok {
				files = append(files, path)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

func loadIgnorePatterns(root string) ([]string, error) {
	f, err := os.Open(filepath.Join(root, ".graphifyignore"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var patterns []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			patterns = append(patterns, line)
		}
	}
	return patterns, scanner.Err()
}
