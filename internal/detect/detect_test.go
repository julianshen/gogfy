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

func TestCollectFilesInvalidGlobPattern(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "a.go"), []byte("package a"), 0644)
	os.WriteFile(filepath.Join(root, ".graphifyignore"), []byte("[\n"), 0644)

	_, err := CollectFiles(root, []string{".go"})
	if err == nil {
		t.Fatal("expected error for invalid glob pattern")
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
