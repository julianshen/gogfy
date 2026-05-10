package extract

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// writeZipFixture builds an in-memory zip containing each `parts` entry
// and writes it to `dir/name`. Used by all OOXML extractor tests (.docx,
// .xlsx, future .pptx) to construct deterministic fixtures without
// committing binary files to the repo.
func writeZipFixture(t *testing.T, dir, name string, parts map[string]string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for n, body := range parts {
		w, err := zw.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}
