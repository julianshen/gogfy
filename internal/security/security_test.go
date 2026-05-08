package security

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSafeJoinAcceptsPathsInsideRoot(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	os.MkdirAll(sub, 0755)
	os.WriteFile(filepath.Join(sub, "a.go"), []byte("package a"), 0644)

	got, err := SafeJoin(root, filepath.Join(sub, "a.go"))
	if err != nil {
		t.Fatalf("inside-root path rejected: %v", err)
	}
	// EvalSymlinks resolves macOS `/var → /private/var`, so compare against
	// the symlink-resolved root rather than the raw test-tempdir.
	rootResolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, rootResolved) {
		t.Fatalf("expected resolved path to start with %q, got %q", rootResolved, got)
	}
}

func TestSafeJoinRejectsRelativeEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir() // sibling, not under root

	_, err := SafeJoin(root, outside)
	if err == nil {
		t.Fatal("expected error for path outside root")
	}
	if !errors.Is(err, ErrPathOutsideRoot) {
		t.Fatalf("expected ErrPathOutsideRoot, got %v", err)
	}
}

func TestSafeJoinRejectsSymlinkEscape(t *testing.T) {
	// Symlink inside root pointing at a path outside root must be rejected.
	root := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "secret")
	os.WriteFile(target, []byte("secret"), 0644)

	link := filepath.Join(root, "evil.go")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	_, err := SafeJoin(root, link)
	if err == nil {
		t.Fatal("expected error for symlink escaping root")
	}
	if !errors.Is(err, ErrPathOutsideRoot) {
		t.Fatalf("expected ErrPathOutsideRoot, got %v", err)
	}
}

func TestSafeJoinAllowsInternalSymlink(t *testing.T) {
	// Symlinks within the same root are fine.
	root := t.TempDir()
	target := filepath.Join(root, "real.go")
	os.WriteFile(target, []byte("package r"), 0644)
	link := filepath.Join(root, "link.go")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	if _, err := SafeJoin(root, link); err != nil {
		t.Fatalf("internal symlink rejected: %v", err)
	}
}

func TestCheckFileSizeUnderLimit(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "small.go")
	os.WriteFile(path, []byte("package small"), 0644)
	if err := CheckFileSize(path, 1024); err != nil {
		t.Fatalf("under-limit file rejected: %v", err)
	}
}

func TestCheckFileSizeOverLimit(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "big.go")
	os.WriteFile(path, make([]byte, 2048), 0644)
	err := CheckFileSize(path, 1024)
	if err == nil {
		t.Fatal("over-limit file accepted")
	}
	if !errors.Is(err, ErrFileTooLarge) {
		t.Fatalf("expected ErrFileTooLarge, got %v", err)
	}
}

func TestCheckFileSizeMissingFile(t *testing.T) {
	if err := CheckFileSize("/nonexistent/path/abc", 1024); err == nil {
		t.Fatal("missing file accepted")
	}
}

func TestSafeJoinErrorsOnMissingPath(t *testing.T) {
	root := t.TempDir()
	_, err := SafeJoin(root, filepath.Join(root, "does-not-exist"))
	if err == nil {
		t.Fatal("expected error for missing path")
	}
	if errors.Is(err, ErrPathOutsideRoot) {
		t.Fatalf("missing-path error should not be ErrPathOutsideRoot: %v", err)
	}
}

func TestSafeJoinFallsBackWhenRootUnresolvable(t *testing.T) {
	// Non-existent root: EvalSymlinks fails on the root itself; SafeJoin
	// falls back to the abs form so an under-it path can still be checked.
	missingRoot := filepath.Join(t.TempDir(), "does-not-exist")
	// Path under that missing root also doesn't exist → resolve-path errors.
	// We just want to exercise the root fallback branch without crashing.
	_, err := SafeJoin(missingRoot, filepath.Join(missingRoot, "x"))
	if err == nil {
		t.Fatal("expected an error for unresolvable path")
	}
}
