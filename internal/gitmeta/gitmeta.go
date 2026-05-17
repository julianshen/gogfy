// Package gitmeta reads minimal git metadata directly from .git/ files
// so gogfy can stamp graph artifacts with the commit they were built
// from — without shelling out to a git binary (matches the rest of the
// codebase's no-external-binary policy).
package gitmeta

import (
	"os"
	"path/filepath"
	"strings"
)

// shortSHALen mirrors `git rev-parse --short=7`. Long enough to be
// unique in typical repos, short enough to read cleanly in a report.
const shortSHALen = 7

// HeadShortSHA returns the short SHA of HEAD for the repo containing
// dir (walks up looking for .git/). Returns ("", false) when:
//   - no .git ancestor found
//   - .git/HEAD or the referenced ref file is unreadable
//   - HEAD points to a non-existent ref (fresh repo before first commit)
//
// Callers should treat the missing-data case as non-fatal: stamping the
// commit is a nicety, not a correctness requirement.
func HeadShortSHA(dir string) (string, bool) {
	gitDir, ok := findGitDir(dir)
	if !ok {
		return "", false
	}
	head, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		return "", false
	}
	content := strings.TrimSpace(string(head))
	// Detached HEAD: HEAD contains the SHA directly.
	if !strings.HasPrefix(content, "ref:") {
		return shorten(content), len(content) >= shortSHALen
	}
	// Symbolic ref: HEAD says "ref: refs/heads/<branch>".
	refName := strings.TrimSpace(strings.TrimPrefix(content, "ref:"))
	refPath := filepath.Join(gitDir, refName)
	refData, err := os.ReadFile(refPath)
	if err != nil {
		// Packed refs fallback: a recently-pushed branch may live in
		// .git/packed-refs rather than as a loose file.
		if sha, ok := packedRefSHA(gitDir, refName); ok {
			return shorten(sha), true
		}
		return "", false
	}
	sha := strings.TrimSpace(string(refData))
	if len(sha) < shortSHALen {
		return "", false
	}
	return shorten(sha), true
}

// findGitDir walks up from dir looking for a directory containing a
// `.git` entry. Returns the path to that `.git` (file or dir). A
// `.git` *file* (git-worktree pointer) is followed to its `gitdir:`
// target so worktrees stamp the same commit as their main repo would.
func findGitDir(dir string) (string, bool) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", false
	}
	for {
		candidate := filepath.Join(abs, ".git")
		info, err := os.Stat(candidate)
		if err == nil {
			if info.IsDir() {
				return candidate, true
			}
			// `.git` is a file → read its `gitdir:` line.
			if real, ok := resolveGitFile(candidate); ok {
				return real, true
			}
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return "", false
		}
		abs = parent
	}
}

func resolveGitFile(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "gitdir:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "gitdir:")), true
		}
	}
	return "", false
}

func packedRefSHA(gitDir, refName string) (string, bool) {
	data, err := os.ReadFile(filepath.Join(gitDir, "packed-refs"))
	if err != nil {
		return "", false
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "^") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == refName {
			return fields[0], true
		}
	}
	return "", false
}

func shorten(sha string) string {
	if len(sha) > shortSHALen {
		return sha[:shortSHALen]
	}
	return sha
}
