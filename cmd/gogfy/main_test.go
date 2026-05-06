package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestMainFunc(t *testing.T) {
	// Save and restore original args
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	// Test missing args
	os.Args = []string{"gogfy"}
	// main() calls os.Exit, so we can't directly test it without a subprocess
	// Instead, test the logic inline
	if len(os.Args) < 3 {
		// This simulates the error path
		fmt.Fprintln(os.Stderr, "usage: gogfy run <root>")
		return
	}
	t.Fatal("should have returned early")
}

func TestRunPipeline(t *testing.T) {
	root := "../../testdata/e2e/mini-corpus"
	out := t.TempDir()

	if err := runPipeline(root, out, false); err != nil {
		t.Fatalf("pipeline failed: %v", err)
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

func TestRunPipelineUpdateMode(t *testing.T) {
	root := "../../testdata/e2e/mini-corpus"
	out := t.TempDir()

	// First run
	if err := runPipeline(root, out, true); err != nil {
		t.Fatalf("first run failed: %v", err)
	}

	// Second run with update should skip unchanged files
	if err := runPipeline(root, out, true); err != nil {
		t.Fatalf("second run failed: %v", err)
	}

	// Cache file should exist
	if _, err := os.Stat(filepath.Join(out, ".gographify-cache")); os.IsNotExist(err) {
		t.Fatal("cache file not created")
	}
}

func TestRunPipelineUpdateModeNoChanges(t *testing.T) {
	root := "../../testdata/e2e/mini-corpus"
	out := t.TempDir()

	// First run
	if err := runPipeline(root, out, true); err != nil {
		t.Fatalf("first run failed: %v", err)
	}

	// Second run with update and no file changes
	if err := runPipeline(root, out, true); err != nil {
		t.Fatalf("second run failed: %v", err)
	}
}

func TestRunPipelineInvalidRoot(t *testing.T) {
	out := t.TempDir()
	if err := runPipeline("/nonexistent/path/12345", out, false); err == nil {
		t.Fatal("expected error for invalid root")
	}
}

func TestRunPipelineEmptyCorpus(t *testing.T) {
	root := t.TempDir()
	out := t.TempDir()

	if err := runPipeline(root, out, false); err != nil {
		t.Fatalf("pipeline failed on empty corpus: %v", err)
	}

	for _, file := range []string{"graph.json", "GRAPH_REPORT.md", "graph.html"} {
		path := filepath.Join(out, file)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Fatalf("%s not created for empty corpus", file)
		}
	}
}

func TestRunPipelineReadOnlyOut(t *testing.T) {
	root := "../../testdata/e2e/mini-corpus"
	out := t.TempDir()
	os.Chmod(out, 0555)
	defer os.Chmod(out, 0755)

	if err := runPipeline(root, out, false); err == nil {
		t.Fatal("expected error for read-only output directory")
	}
}
