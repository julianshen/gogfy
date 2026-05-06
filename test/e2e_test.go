package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestE2EPipeline(t *testing.T) {
	root := "testdata/e2e/mini-corpus"
	out := t.TempDir()

	cmd := exec.Command("go", "run", "./cmd/gogfy", "--out", out, "run", root)
	cmd.Dir = ".."
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI failed: %v\n%s", err, output)
	}

	for _, file := range []string{"graph.json", "GRAPH_REPORT.md", "graph.html"} {
		path := filepath.Join(out, file)
		info, err := os.Stat(path)
		if os.IsNotExist(err) {
			t.Fatalf("%s not created", file)
		}
		if info.Size() == 0 {
			t.Fatalf("%s is empty", file)
		}
	}
}

func TestE2EUpdateMode(t *testing.T) {
	root := "testdata/e2e/mini-corpus"
	out := t.TempDir()

	// First run
	cmd := exec.Command("go", "run", "./cmd/gogfy", "--out", out, "--update", "run", root)
	cmd.Dir = ".."
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("first run failed: %v\n%s", err, output)
	}

	// Second run with --update should skip unchanged files
	cmd = exec.Command("go", "run", "./cmd/gogfy", "--out", out, "--update", "run", root)
	cmd.Dir = ".."
	output, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("second run failed: %v\n%s", err, output)
	}

	// Cache file should exist
	if _, err := os.Stat(filepath.Join(out, ".gographify-cache")); os.IsNotExist(err) {
		t.Fatal("cache file not created")
	}
}
