package detect

import (
	"os"
	"path/filepath"
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
