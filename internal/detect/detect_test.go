package detect

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestCollectFilesFiltersExtensions(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "a.go"), []byte("package a"), 0644)
	os.WriteFile(filepath.Join(root, "b.py"), []byte("# b"), 0644)
	os.WriteFile(filepath.Join(root, "c.txt"), []byte("c"), 0644)

	files, err := CollectFiles(root, []string{".go", ".py"})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}
	want := []string{"a.go", "b.py"}
	got := []string{filepath.Base(files[0]), filepath.Base(files[1])}
	if !slices.Equal(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestCollectFilesRespectsGraphifyIgnore(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "keep.go"), []byte("package keep"), 0644)
	sub := filepath.Join(root, "vendor")
	os.MkdirAll(sub, 0755)
	os.WriteFile(filepath.Join(sub, "skip.go"), []byte("package skip"), 0644)
	os.WriteFile(filepath.Join(root, ".graphifyignore"), []byte("vendor/\n"), 0644)

	files, err := CollectFiles(root, []string{".go"})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if filepath.Base(files[0]) != "keep.go" {
		t.Fatalf("expected keep.go, got %s", files[0])
	}
}

func TestCollectFilesDeterministicOrder(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "z.go"), []byte("package z"), 0644)
	os.WriteFile(filepath.Join(root, "a.go"), []byte("package a"), 0644)
	os.WriteFile(filepath.Join(root, "m.go"), []byte("package m"), 0644)

	files1, err := CollectFiles(root, []string{".go"})
	if err != nil {
		t.Fatal(err)
	}
	files2, err := CollectFiles(root, []string{".go"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(files1, files2) {
		t.Fatalf("deterministic ordering failed: %v vs %v", files1, files2)
	}
	want := []string{"a.go", "m.go", "z.go"}
	got := make([]string, len(files1))
	for i, f := range files1 {
		got[i] = filepath.Base(f)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("expected sorted order %v, got %v", want, got)
	}
}

func TestCollectFilesIgnoreCommentsAndBlankLines(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "keep.go"), []byte("package keep"), 0644)
	sub := filepath.Join(root, "vendor")
	os.MkdirAll(sub, 0755)
	os.WriteFile(filepath.Join(sub, "skip.go"), []byte("package skip"), 0644)
	os.WriteFile(filepath.Join(root, ".graphifyignore"), []byte("# comment\n\nvendor/\n"), 0644)

	files, err := CollectFiles(root, []string{".go"})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
}

func TestCollectFilesEmptyExtensions(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "a.go"), []byte("package a"), 0644)

	files, err := CollectFiles(root, []string{})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("expected 0 files, got %d", len(files))
	}
}

func TestCollectFilesNonExistentRoot(t *testing.T) {
	_, err := CollectFiles("/nonexistent/path/12345", []string{".go"})
	if err == nil {
		t.Fatal("expected error for nonexistent root")
	}
}

func TestCollectFilesMalformedPatternIsTolerant(t *testing.T) {
	// gitignore semantics tolerate malformed patterns rather than erroring,
	// matching how `git` itself behaves. Verify CollectFiles still returns
	// the matching files instead of failing the whole run.
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "a.go"), []byte("package a"), 0644)
	os.WriteFile(filepath.Join(root, ".graphifyignore"), []byte("[\n"), 0644)

	files, err := CollectFiles(root, []string{".go"})
	if err != nil {
		t.Fatalf("malformed pattern should not abort traversal: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
}

func TestCollectFilesNestedIgnore(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "keep.go"), []byte("package keep"), 0644)
	sub := filepath.Join(root, "vendor", "nested")
	os.MkdirAll(sub, 0755)
	os.WriteFile(filepath.Join(sub, "skip.go"), []byte("package skip"), 0644)
	os.WriteFile(filepath.Join(root, ".graphifyignore"), []byte("vendor/\n"), 0644)

	files, err := CollectFiles(root, []string{".go"})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if filepath.Base(files[0]) != "keep.go" {
		t.Fatalf("expected keep.go, got %s", files[0])
	}
}

func TestCollectFilesGlobPatternMatch(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "keep.go"), []byte("package keep"), 0644)
	os.WriteFile(filepath.Join(root, "skip_test.go"), []byte("package skip"), 0644)
	os.WriteFile(filepath.Join(root, ".graphifyignore"), []byte("*_test.go\n"), 0644)

	files, err := CollectFiles(root, []string{".go"})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if filepath.Base(files[0]) != "keep.go" {
		t.Fatalf("expected keep.go, got %s", files[0])
	}
}

func TestCollectFilesNegationUnignores(t *testing.T) {
	// `!` re-includes a previously-ignored path. Required for gitignore parity.
	// Use `vendor/*` rather than `vendor/`; gitignore semantics forbid
	// re-including children once their parent directory is excluded.
	root := t.TempDir()
	dir := filepath.Join(root, "vendor")
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "skip.go"), []byte("package skip"), 0644)
	os.WriteFile(filepath.Join(dir, "keep.go"), []byte("package keep"), 0644)
	os.WriteFile(filepath.Join(root, ".graphifyignore"), []byte("vendor/*\n!vendor/keep.go\n"), 0644)

	files, err := CollectFiles(root, []string{".go"})
	if err != nil {
		t.Fatal(err)
	}
	bases := make([]string, len(files))
	for i, f := range files {
		bases[i] = filepath.Base(f)
	}
	if !slices.Contains(bases, "keep.go") {
		t.Fatalf("negation should re-include keep.go; got %v", bases)
	}
	if slices.Contains(bases, "skip.go") {
		t.Fatalf("skip.go should still be ignored; got %v", bases)
	}
}

func TestCollectFilesDoubleStarRecursive(t *testing.T) {
	// `**/foo` should match foo at any depth.
	root := t.TempDir()
	for _, p := range []string{"a/x.go", "a/b/x.go", "a/b/c/x.go", "y.go"} {
		full := filepath.Join(root, p)
		os.MkdirAll(filepath.Dir(full), 0755)
		os.WriteFile(full, []byte("package x"), 0644)
	}
	os.WriteFile(filepath.Join(root, ".graphifyignore"), []byte("**/x.go\n"), 0644)

	files, err := CollectFiles(root, []string{".go"})
	if err != nil {
		t.Fatal(err)
	}
	bases := make([]string, len(files))
	for i, f := range files {
		bases[i] = filepath.Base(f)
	}
	for _, b := range bases {
		if b == "x.go" {
			t.Fatalf("**/x.go should ignore every x.go; got %v", bases)
		}
	}
	if !slices.Contains(bases, "y.go") {
		t.Fatalf("y.go should remain; got %v", bases)
	}
}

func TestCollectFilesLeadingSlashAnchorsToRoot(t *testing.T) {
	// `/foo.go` should only match at root, not in subdirs.
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "foo.go"), []byte("package x"), 0644)
	os.MkdirAll(filepath.Join(root, "sub"), 0755)
	os.WriteFile(filepath.Join(root, "sub", "foo.go"), []byte("package x"), 0644)
	os.WriteFile(filepath.Join(root, ".graphifyignore"), []byte("/foo.go\n"), 0644)

	files, err := CollectFiles(root, []string{".go"})
	if err != nil {
		t.Fatal(err)
	}
	bases := make([]string, len(files))
	for i, f := range files {
		bases[i] = filepath.ToSlash(f[len(root)+1:])
	}
	for _, b := range bases {
		if b == "foo.go" {
			t.Fatalf("anchored /foo.go should ignore root foo.go; got %v", bases)
		}
	}
	if !slices.Contains(bases, "sub/foo.go") {
		t.Fatalf("sub/foo.go should not be ignored by anchored /foo.go; got %v", bases)
	}
}

func TestCollectFilesSkipsIgnoredDirSubtree(t *testing.T) {
	// Regression: `vendor/` ignore must SkipDir so the walker never reads
	// files inside an ignored subtree. Prior to the fix the walker descended
	// because `MatchesPath("vendor")` (bare form) returns false for pattern
	// `vendor/` even though `MatchesPath("vendor/")` is true.
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "keep.go"), []byte("package keep"), 0644)
	dir := filepath.Join(root, "vendor")
	os.MkdirAll(dir, 0755)
	// Create an unreadable file inside vendor — if the walker descends into
	// the ignored dir it would hit this and fail.
	bad := filepath.Join(dir, "skip.go")
	os.WriteFile(bad, []byte("package skip"), 0644)
	os.Chmod(bad, 0000)
	defer os.Chmod(bad, 0644)
	os.WriteFile(filepath.Join(root, ".graphifyignore"), []byte("vendor/\n"), 0644)

	files, err := CollectFiles(root, []string{".go"})
	if err != nil {
		t.Fatalf("CollectFiles must SkipDir over `vendor/`, not descend: %v", err)
	}
	if len(files) != 1 || filepath.Base(files[0]) != "keep.go" {
		t.Fatalf("expected only keep.go; got %v", files)
	}
}

func TestCollectFilesCharClassPattern(t *testing.T) {
	// Pin gitignore character-class behavior so a future matcher swap can't
	// silently regress.
	root := t.TempDir()
	for _, p := range []string{"a.tmp", "b.tmp", "c.tmp", "x.go"} {
		os.WriteFile(filepath.Join(root, p), []byte("x"), 0644)
	}
	os.WriteFile(filepath.Join(root, ".graphifyignore"), []byte("[ab].tmp\n"), 0644)

	files, err := CollectFiles(root, []string{".tmp", ".go"})
	if err != nil {
		t.Fatal(err)
	}
	bases := make([]string, len(files))
	for i, f := range files {
		bases[i] = filepath.Base(f)
	}
	for _, b := range []string{"a.tmp", "b.tmp"} {
		if slices.Contains(bases, b) {
			t.Fatalf("[ab].tmp should ignore %q; got %v", b, bases)
		}
	}
	for _, b := range []string{"c.tmp", "x.go"} {
		if !slices.Contains(bases, b) {
			t.Fatalf("expected %q present; got %v", b, bases)
		}
	}
}

func TestCollectFilesLoadIgnorePermissionError(t *testing.T) {
	root := t.TempDir()
	ignorePath := filepath.Join(root, ".graphifyignore")
	os.WriteFile(ignorePath, []byte("vendor/\n"), 0644)
	os.Chmod(ignorePath, 0000)
	defer os.Chmod(ignorePath, 0644)

	_, err := CollectFiles(root, []string{".go"})
	if err == nil {
		t.Fatal("expected error for unreadable ignore file")
	}
}
