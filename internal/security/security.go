// Package security provides path-traversal and file-size guards used by the
// detect/extract pipeline to keep the corpus walk inside the user-specified
// root and to skip pathologically large files.
package security

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DefaultMaxFileSize is the default per-file size cap (10 MiB). Files larger
// than this are skipped during detection: tree-sitter parsing of multi-megabyte
// minified bundles or generated code typically yields graphs with little
// structural value relative to the parse cost.
const DefaultMaxFileSize int64 = 10 << 20

// ErrPathOutsideRoot is returned by SafeJoin/RootGuard.Check when the
// resolved (post-symlink) path would escape the user-specified corpus root.
var ErrPathOutsideRoot = errors.New("path resolves outside root")

// ErrFileTooLarge is returned by the size-check helpers when a file exceeds
// the cap.
var ErrFileTooLarge = errors.New("file exceeds size cap")

// IsSkippable reports whether err is a non-fatal security violation that
// callers should treat as "drop this file from the corpus" rather than "abort
// the whole walk". Centralizing the predicate alongside the sentinels means
// adding a future skip-class error updates one place, not every caller.
func IsSkippable(err error) bool {
	return errors.Is(err, ErrPathOutsideRoot) || errors.Is(err, ErrFileTooLarge)
}

// RootGuard caches the resolved (symlink-followed) form of the corpus root
// so SafeJoin-style checks don't re-stat the root for every candidate path.
type RootGuard struct {
	resolved string
}

// NewRootGuard resolves root once. Returns an error if root can't be made
// absolute or its symlinks can't be followed — callers should treat that
// as "refuse to walk" rather than silently drop every file.
func NewRootGuard(root string) (*RootGuard, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve root: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, fmt.Errorf("resolve root: %w", err)
	}
	return &RootGuard{resolved: resolved}, nil
}

// Check resolves path through filepath.EvalSymlinks (after first making it
// absolute, so callers like `gogfy run .` don't get the cwd-relative form
// compared against an absolute root) and verifies that the resolved form is
// the guard's root or a descendant of it. Returns the resolved path on
// success so callers can read through that (sidestepping any symlink swap
// between check and open).
func (g *RootGuard) Check(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	if !withinRoot(g.resolved, resolved) {
		return "", fmt.Errorf("%w: %q -> %q", ErrPathOutsideRoot, path, resolved)
	}
	return resolved, nil
}

// SafeJoin is a one-shot convenience over NewRootGuard + Check. Prefer the
// guard form when checking many paths against the same root.
func SafeJoin(root, path string) (string, error) {
	g, err := NewRootGuard(root)
	if err != nil {
		return "", err
	}
	return g.Check(path)
}

// withinRoot reports whether resolved is root or a descendant of root.
// Uses filepath.Rel rather than a HasPrefix check so it handles edge cases
// the latter mishandles — notably root == "/" (where "/"+"/" produces "//"
// and rejects every concrete descendant) and case-insensitive filesystems
// where the prefix's casing might not match the descendant's.
func withinRoot(root, resolved string) bool {
	rel, err := filepath.Rel(root, resolved)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// CheckFileSize stats path and reports ErrFileTooLarge if the file exceeds
// max bytes. Prefer CheckFileInfoSize when an os.FileInfo is already in hand
// (e.g., from a filepath.Walk callback) to avoid the extra syscall.
func CheckFileSize(path string, max int64) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	return CheckFileInfoSize(path, info, max)
}

// CheckFileInfoSize is the FileInfo variant of CheckFileSize: no syscall,
// just a comparison against the existing stat result.
func CheckFileInfoSize(path string, info os.FileInfo, max int64) error {
	if info.Size() > max {
		return fmt.Errorf("%w: %s is %d bytes (cap %d)", ErrFileTooLarge, path, info.Size(), max)
	}
	return nil
}
