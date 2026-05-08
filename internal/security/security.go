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

// ErrPathOutsideRoot is returned by SafeJoin when the resolved (post-symlink)
// path would escape the user-specified corpus root.
var ErrPathOutsideRoot = errors.New("path resolves outside root")

// ErrFileTooLarge is returned by CheckFileSize when a file exceeds the cap.
var ErrFileTooLarge = errors.New("file exceeds size cap")

// SafeJoin returns the absolute, fully symlink-resolved form of path and
// errors if the resolved path would escape root. Use this at the boundary
// between filesystem walk results and "we're about to read this file" so a
// hostile symlink inside the corpus can't redirect a read to /etc/passwd.
func SafeJoin(root, path string) (string, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve root: %w", err)
	}
	rootResolved, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		// If root itself doesn't resolve cleanly, fall back to the abs form;
		// callers surface non-existent-root errors at a higher layer.
		rootResolved = rootAbs
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	if !withinRoot(rootResolved, resolved) {
		return "", fmt.Errorf("%w: %q -> %q", ErrPathOutsideRoot, path, resolved)
	}
	return resolved, nil
}

// withinRoot reports whether resolved is root or a descendant of root.
// Both arguments must be cleaned absolute paths.
func withinRoot(root, resolved string) bool {
	if root == resolved {
		return true
	}
	rootSep := root + string(filepath.Separator)
	return strings.HasPrefix(resolved, rootSep)
}

// CheckFileSize returns nil if the file at path is at or under max bytes,
// ErrFileTooLarge if it exceeds the cap, or the underlying I/O error if the
// file can't be stat'd.
func CheckFileSize(path string, max int64) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Size() > max {
		return fmt.Errorf("%w: %s is %d bytes (cap %d)", ErrFileTooLarge, path, info.Size(), max)
	}
	return nil
}
