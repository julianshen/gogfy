// Package detect discovers source files in a directory tree, respecting ignore patterns.
package detect

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strings"

	gitignore "github.com/sabhiram/go-gitignore"
)

// CollectFiles recursively collects files under root matching the given extensions,
// skipping entries matched by .graphifyignore patterns.
//
// .graphifyignore follows gitignore semantics: last-match-wins, `!` negation
// re-includes previously-ignored paths, leading `/` anchors to the root,
// trailing `/` matches directories, and `**` recurses across path segments.
func CollectFiles(root string, extensions []string) ([]string, error) {
	matcher, hasNegations, err := loadIgnoreMatcher(root)
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
		if rel == "." {
			return nil
		}
		// gitignore matches dirs with a trailing slash; without it, patterns
		// like `vendor/*` over-match the parent directory itself in
		// go-gitignore. Use the trailing-slash form for the ignore check, and
		// the bare form to decide whether to SkipDir (so a children-only
		// pattern doesn't truncate the walk).
		slash := filepath.ToSlash(rel)
		matchPath := slash
		if info.IsDir() {
			matchPath += "/"
		}
		if matcher != nil && matcher.MatchesPath(matchPath) {
			if !info.IsDir() {
				return nil
			}
			// Skip the whole subtree only if there are no `!` negations
			// that could re-include children, AND the bare directory name
			// matches (so children-only patterns like `vendor/*` don't
			// prune the walk before we see the children).
			if !hasNegations && matcher.MatchesPath(slash) {
				return filepath.SkipDir
			}
			return nil
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

// loadIgnoreMatcher parses .graphifyignore at root with full gitignore
// semantics. Returns the matcher, whether any `!` negation patterns are
// present (so the walker can decide whether SkipDir is safe), and any
// I/O error. Both first returns are nil if the file is absent.
func loadIgnoreMatcher(root string) (*gitignore.GitIgnore, bool, error) {
	f, err := os.Open(filepath.Join(root, ".graphifyignore"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	defer f.Close()

	var lines []string
	hasNegations := false
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "!") {
			hasNegations = true
		}
		lines = append(lines, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, false, err
	}
	return gitignore.CompileIgnoreLines(lines...), hasNegations, nil
}
