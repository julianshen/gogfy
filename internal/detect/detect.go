package detect

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

func CollectFiles(root string, extensions []string) ([]string, error) {
	ignorePatterns, err := loadIgnorePatterns(root)
	if err != nil {
		return nil, err
	}

	extSet := make(map[string]bool, len(extensions))
	for _, e := range extensions {
		extSet[e] = true
	}

	var files []string
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		for _, pat := range ignorePatterns {
			matched, _ := filepath.Match(pat, rel)
			if matched || strings.HasPrefix(rel, pat) {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}
		if !info.IsDir() {
			ext := filepath.Ext(path)
			if extSet[ext] {
				files = append(files, path)
			}
		}
		return nil
	})
	return files, err
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
