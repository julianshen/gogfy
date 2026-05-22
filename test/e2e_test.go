package e2e

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestE2EPipeline(t *testing.T) {
	root := "testdata/e2e/mini-corpus"
	out := t.TempDir()

	cmd := exec.Command("go", "run", "./cmd/gogfy", "run", root, "--out", out)
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

func TestE2EDeterministicGraphJSON(t *testing.T) {
	// graph.json must be byte-identical across runs on the same corpus.
	// Two distinct sources of nondeterminism were fixed (deterministic
	// Louvain default + sorted edge serialization in dedup); this guards
	// against either silently regressing — reproducibility is what the
	// --update / graphdiff flows and snapshot stability rely on.
	root := "testdata/e2e/mini-corpus"

	run := func() []byte {
		out := t.TempDir()
		cmd := exec.Command("go", "run", "./cmd/gogfy", "run", root, "--out", out, "--no-viz")
		cmd.Dir = ".."
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("CLI failed: %v\n%s", err, output)
		}
		data, err := os.ReadFile(filepath.Join(out, "graph.json"))
		if err != nil {
			t.Fatalf("read graph.json: %v", err)
		}
		return data
	}

	first := run()
	second := run()
	if !bytes.Equal(first, second) {
		t.Fatalf("graph.json differs across runs (%d vs %d bytes) — clustering or "+
			"serialization regressed determinism", len(first), len(second))
	}
}

func TestE2EUpdateMode(t *testing.T) {
	root := "testdata/e2e/mini-corpus"
	out := t.TempDir()

	// First run
	cmd := exec.Command("go", "run", "./cmd/gogfy", "run", root, "--out", out, "--update")
	cmd.Dir = ".."
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("first run failed: %v\n%s", err, output)
	}

	// Second run with --update should skip unchanged files
	cmd = exec.Command("go", "run", "./cmd/gogfy", "run", root, "--out", out, "--update")
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
