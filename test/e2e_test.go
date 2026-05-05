package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestE2EPipeline(t *testing.T) {
	root := "testdata/e2e/mini-corpus"
	out := t.TempDir()

	cmd := exec.Command("go", "run", "./cmd/gographify", "--out", out, "run", root)
	cmd.Dir = ".."
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI failed: %v\n%s", err, output)
	}

	if _, err := os.Stat(filepath.Join(out, "graph.json")); os.IsNotExist(err) {
		t.Fatal("graph.json not created")
	}
	if _, err := os.Stat(filepath.Join(out, "GRAPH_REPORT.md")); os.IsNotExist(err) {
		t.Fatal("GRAPH_REPORT.md not created")
	}
	if _, err := os.Stat(filepath.Join(out, "graph.html")); os.IsNotExist(err) {
		t.Fatal("graph.html not created")
	}
}
