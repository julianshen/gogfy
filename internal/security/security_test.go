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
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	if err := CheckFileSize(missing, 1024); err == nil {
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

func TestIsSkippable(t *testing.T) {
	if !IsSkippable(ErrPathOutsideRoot) {
		t.Fatal("ErrPathOutsideRoot should be skippable")
	}
	if !IsSkippable(ErrFileTooLarge) {
		t.Fatal("ErrFileTooLarge should be skippable")
	}
	if IsSkippable(errors.New("random error")) {
		t.Fatal("unrelated error should not be skippable")
	}
	if IsSkippable(nil) {
		t.Fatal("nil should not be skippable")
	}
}

func TestRootGuardAcceptsRootItself(t *testing.T) {
	// withinRoot's exact-equal special case: passing root as the path under
	// check resolves to root itself, which must be accepted (not rejected
	// by the prefix-with-separator check, which would reject "/foo" against
	// prefix "/foo/").
	root := t.TempDir()
	g, err := NewRootGuard(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.Check(root); err != nil {
		t.Fatalf("root path should be accepted by its own guard: %v", err)
	}
}

func TestRootGuardAcceptsFilesystemRoot(t *testing.T) {
	// Regression: when root is "/", the prior implementation built a "//"
	// prefix that no descendant ever matches, silently rejecting every
	// file. NewRootGuard("/") must accept any concrete path under it.
	g, err := NewRootGuard(string(filepath.Separator))
	if err != nil {
		t.Fatal(err)
	}
	// /tmp exists on every supported platform's fs root.
	if _, err := g.Check("/tmp"); err != nil {
		t.Fatalf("/tmp under filesystem root rejected: %v", err)
	}
}

func TestRootGuardAcceptsRelativePathInput(t *testing.T) {
	// Regression: callers like `gogfy run .` make filepath.Walk yield
	// relative paths. The guard must absolutize them before resolving,
	// otherwise it compares an abs root against a rel resolved path and
	// rejects every file.
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "a.go"), []byte("package a"), 0644)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(cwd)
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}

	g, err := NewRootGuard(".")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.Check("a.go"); err != nil {
		t.Fatalf("relative path under relative root rejected: %v", err)
	}
}

func TestSafeJoinErrorsOnUnresolvableRoot(t *testing.T) {
	// Non-existent root: don't silently fall back to the unresolved form.
	// Doing so would make every path under a path-resolved location (e.g.,
	// macOS /var → /private/var) appear to escape the root, producing
	// wholesale false rejects. Both NewRootGuard and SafeJoin should
	// surface the error.
	missingRoot := filepath.Join(t.TempDir(), "does-not-exist")
	if _, err := NewRootGuard(missingRoot); err == nil {
		t.Fatal("expected error from NewRootGuard on missing root")
	}
	if _, err := SafeJoin(missingRoot, missingRoot); err == nil {
		t.Fatal("expected SafeJoin to propagate NewRootGuard error")
	}
}
