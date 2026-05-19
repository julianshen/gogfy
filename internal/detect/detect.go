// Package detect discovers source files in a directory tree, respecting ignore patterns.
package detect

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/julianshen/gogfy/internal/security"
	gitignore "github.com/sabhiram/go-gitignore"
)

// SkipLogger receives stderr-bound warnings about files dropped from the
// corpus by security guards (symlink escape, size cap). Tests override it
// to assert; production leaves the default (os.Stderr).
var SkipLogger io.Writer = os.Stderr

// CollectFiles recursively collects files under root matching the given extensions,
// skipping entries matched by .graphifyignore patterns.
//
// .graphifyignore follows gitignore semantics: last-match-wins, `!` negation
// re-includes previously-ignored paths, leading `/` anchors to the root,
// trailing `/` matches directories, and `**` recurses across path segments.
//
// Only the root .graphifyignore is read; per-subdirectory ignore files
// (a feature of git itself) are not yet layered. If the file contains any
// `!` negation, ignored directories are still descended so that re-included
// children can be reached, at the cost of walking large vendored trees.
func CollectFiles(root string, extensions []string) ([]string, error) {
	matcher, hasNegations, err := loadIgnoreMatcher(root)
	if err != nil {
		return nil, err
	}
	includeMatcher, err := loadIncludeMatcher(root)
	if err != nil {
		return nil, err
	}
	guard, err := security.NewRootGuard(root)
	if err != nil {
		return nil, fmt.Errorf("security: %w", err)
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
		// Hidden entries (basename starts with '.') are skipped by
		// default — they're usually config, not source. `.graphifyinclude`
		// is the escape hatch: any pattern matched there re-includes
		// the entry. Mirrors upstream graphify's allowlist behavior.
		// The base check uses filepath.Base so `subdir/.git` is caught
		// even though `rel` is the full path.
		if strings.HasPrefix(filepath.Base(rel), ".") {
			if includeMatcher == nil || !includeMatcher.MatchesPath(matchPath) {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}
		if matcher != nil && matcher.MatchesPath(matchPath) {
			if !info.IsDir() {
				return nil
			}
			// Skip the whole subtree iff there are no `!` negations that
			// could re-include any descendant. The bare-form check the
			// previous version did was wrong: pattern `vendor/` matches
			// `vendor/` but not `vendor`, which suppressed SkipDir for
			// the canonical "ignore a directory" rule.
			if !hasNegations {
				return filepath.SkipDir
			}
			return nil
		}
		if !info.IsDir() {
			ext := filepath.Ext(path)
			if _, ok := extSet[ext]; !ok {
				return nil
			}
			// Silent skip for credential / secret-bearing files.
			// graphify's policy: surfacing the existence of `.env.prod`
			// in a public report is itself a leak — drop without
			// logging the filename to stderr.
			if IsSensitive(path) {
				return nil
			}
			// Resolve through the guard so a hostile symlink can't redirect
			// the eventual extractor read; on success, append the resolved
			// path so the read goes directly to the real file (closing a
			// TOCTOU between this check and the eventual os.ReadFile).
			resolved, err := guard.Check(path)
			if err != nil {
				if security.IsSkippable(err) {
					fmt.Fprintf(SkipLogger, "gogfy: skipping %s: %v\n", path, err)
					return nil
				}
				return err
			}
			// Size cap must be enforced against the resolved target, not
			// the walk-time FileInfo: filepath.Walk uses Lstat, so for
			// symlinks `info.Size()` is the link size (~tens of bytes),
			// trivially evading the cap. Re-stat the resolved path.
			if err := security.CheckFileSize(resolved, security.DefaultMaxFileSize); err != nil {
				if security.IsSkippable(err) {
					fmt.Fprintf(SkipLogger, "gogfy: skipping %s: %v\n", path, err)
					return nil
				}
				return err
			}
			files = append(files, resolved)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

// loadIgnoreMatcher parses .graphifyignore with gitignore semantics,
// layering files from the VCS root (.git/.hg/.svn ancestor) down to
// the scan root. Patterns nearer to scan root come last so gitignore's
// last-match-wins gives them precedence — matches git's own behavior
// where a child .gitignore overrides an ancestor's rule.
//
// Returns the matcher, whether any `!` negation patterns are present
// (so the walker can decide whether SkipDir is safe), and any I/O
// error. All three returns are zero if no files are found.
func loadIgnoreMatcher(root string) (*gitignore.GitIgnore, bool, error) {
	chain := ignoreDirChain(root)
	var lines []string
	hasNegations := false
	for _, dir := range chain {
		f, err := os.Open(filepath.Join(dir, ".graphifyignore"))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, false, err
		}
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
		scanErr := scanner.Err()
		f.Close()
		if scanErr != nil {
			return nil, false, scanErr
		}
	}
	if len(lines) == 0 {
		return nil, false, nil
	}
	return gitignore.CompileIgnoreLines(lines...), hasNegations, nil
}

// loadIncludeMatcher reads .graphifyinclude from the scan root (and
// the VCS-ancestor chain, same layering as .graphifyignore). The
// resulting matcher is the dotfile-skip escape hatch: paths that
// match are re-included even though their basename starts with `.`.
//
// Pattern syntax is identical to gitignore (last-match-wins, `!`
// negation, etc.) since users already know the dialect from .gitignore
// and .graphifyignore.
//
// Returns (nil, nil) when no .graphifyinclude exists — the dotfile
// default-skip then applies to everything.
func loadIncludeMatcher(root string) (*gitignore.GitIgnore, error) {
	chain := ignoreDirChain(root)
	var lines []string
	for _, dir := range chain {
		f, err := os.Open(filepath.Join(dir, ".graphifyinclude"))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			lines = append(lines, line)
		}
		scanErr := scanner.Err()
		f.Close()
		if scanErr != nil {
			return nil, scanErr
		}
	}
	if len(lines) == 0 {
		return nil, nil
	}
	return gitignore.CompileIgnoreLines(lines...), nil
}

// ignoreDirChain returns the ordered list of directories whose
// .graphifyignore files contribute to the matcher: VCS root first,
// scanRoot last. The scan root's patterns win on conflict because
// gitignore is last-match-wins and we append in walk order.
//
// VCS marker recognition mirrors git/hg/svn so monorepos with nested
// `.git/` worktrees still terminate at the right boundary.
func ignoreDirChain(scanRoot string) []string {
	abs, err := filepath.Abs(scanRoot)
	if err != nil {
		return []string{scanRoot}
	}
	dirs := []string{abs}
	cur := abs
	for {
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		dirs = append(dirs, parent)
		if isVCSRoot(parent) {
			break
		}
		cur = parent
	}
	// Reverse so closest-to-VCS comes first; scanRoot is last so its
	// patterns win on conflict via gitignore's last-match-wins rule.
	for i, j := 0, len(dirs)-1; i < j; i, j = i+1, j-1 {
		dirs[i], dirs[j] = dirs[j], dirs[i]
	}
	return dirs
}

func isVCSRoot(dir string) bool {
	for _, m := range []string{".git", ".hg", ".svn"} {
		if info, err := os.Stat(filepath.Join(dir, m)); err == nil && info.IsDir() {
			return true
		}
	}
	return false
}
