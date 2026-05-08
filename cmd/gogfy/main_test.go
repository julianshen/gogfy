package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestUpdateModeNoChangesPreservesOutputs(t *testing.T) {
	root := "../../testdata/e2e/mini-corpus"
	out := t.TempDir()

	// First run produces real outputs.
	if err := runPipeline(root, out, true, false); err != nil {
		t.Fatalf("first run failed: %v", err)
	}
	originals := map[string][]byte{}
	for _, f := range []string{"graph.json", "GRAPH_REPORT.md", "graph.html"} {
		b, err := os.ReadFile(filepath.Join(out, f))
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		if len(b) == 0 {
			t.Fatalf("%s empty after first run", f)
		}
		originals[f] = b
	}

	if err := runPipeline(root, out, true, false); err != nil {
		t.Fatalf("no-op update run failed: %v", err)
	}
	for f, want := range originals {
		got, err := os.ReadFile(filepath.Join(out, f))
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		if string(got) != string(want) {
			t.Fatalf("%s was overwritten by no-op --update run", f)
		}
	}
}

func TestDispatchRunSubcommand(t *testing.T) {
	out := t.TempDir()
	err := dispatch([]string{"run", "--out", out, "../../testdata/e2e/mini-corpus"}, os.Stderr)
	if err != nil {
		t.Fatalf("dispatch run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "graph.json")); err != nil {
		t.Fatalf("graph.json missing: %v", err)
	}
}

func TestDispatchRunUpdateFlagAfterRoot(t *testing.T) {
	// SPEC §8: `gogfy run <root> [--update] [--out dir]`. Flags after the
	// positional must work; on a fresh out dir, --update should produce
	// artifacts (no prior cache to compare against).
	out := t.TempDir()
	err := dispatch([]string{"run", "../../testdata/e2e/mini-corpus", "--update", "--out", out}, os.Stderr)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, ".gographify-cache")); err != nil {
		t.Fatalf("expected cache from --update run, got %v", err)
	}
}

func TestDispatchValidateSubcommand(t *testing.T) {
	out := t.TempDir()
	if err := runPipeline("../../testdata/e2e/mini-corpus", out, false, false); err != nil {
		t.Fatal(err)
	}
	if err := dispatch([]string{"validate", filepath.Join(out, "graph.json")}, os.Stderr); err != nil {
		t.Fatalf("dispatch validate: %v", err)
	}
}

func TestDispatchReportSubcommand(t *testing.T) {
	out := t.TempDir()
	if err := runPipeline("../../testdata/e2e/mini-corpus", out, false, false); err != nil {
		t.Fatal(err)
	}
	if err := dispatch([]string{"report", filepath.Join(out, "graph.json")}, os.Stderr); err != nil {
		t.Fatalf("dispatch report: %v", err)
	}
}

func TestDispatchUnknownSubcommand(t *testing.T) {
	if err := dispatch([]string{"bogus", "x"}, os.Stderr); err == nil {
		t.Fatal("expected error for unknown subcommand")
	}
}

func TestDispatchMissingArgs(t *testing.T) {
	if err := dispatch([]string{"run"}, os.Stderr); err == nil {
		t.Fatal("expected error for missing arg to run")
	}
	if err := dispatch([]string{}, os.Stderr); err == nil {
		t.Fatal("expected error for empty args")
	}
}

func TestAtomicWriteFailsOnUnwritablePath(t *testing.T) {
	// Parent directory does not exist; the staging WriteFile must fail.
	if err := atomicWrite("/nonexistent/dir/abc/graph.json", []byte("x")); err == nil {
		t.Fatal("expected error writing to missing directory")
	}
}

func TestAtomicWriteSucceeds(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")
	if err := atomicWrite(path, []byte("hello")); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Fatalf("got %q, want hello", got)
	}
	// .tmp sibling must not linger after a successful rename.
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("expected .tmp to be cleaned up, stat err=%v", err)
	}
}

func TestDispatchFlagsAfterSubcommand(t *testing.T) {
	// SPEC §8 documents `gogfy run <root> [--update] [--out dir]` — flags AFTER
	// the subcommand. Per-subcommand FlagSets are required to honor that.
	out := t.TempDir()
	err := dispatch([]string{"run", "../../testdata/e2e/mini-corpus", "--out", out}, os.Stderr)
	if err != nil {
		t.Fatalf("dispatch run with trailing --out: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "graph.json")); err != nil {
		t.Fatalf("graph.json missing in --out dir: %v", err)
	}
}

func TestUpdateModeFirstRunOnEmptyCorpusStillWritesArtifacts(t *testing.T) {
	root := t.TempDir() // empty corpus
	out := t.TempDir()
	if err := runPipeline(root, out, true, false); err != nil {
		t.Fatalf("pipeline: %v", err)
	}
	for _, f := range []string{"graph.json", "GRAPH_REPORT.md", "graph.html"} {
		if _, err := os.Stat(filepath.Join(out, f)); err != nil {
			t.Fatalf("%s missing on first --update run with empty corpus: %v", f, err)
		}
	}
}

func TestDispatchBadFlag(t *testing.T) {
	if err := dispatch([]string{"--nope"}, os.Stderr); err == nil {
		t.Fatal("expected flag parse error")
	}
}

func TestRunPipeline(t *testing.T) {
	root := "../../testdata/e2e/mini-corpus"
	out := t.TempDir()

	if err := runPipeline(root, out, false, false); err != nil {
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
	if err := runPipeline(root, out, true, false); err != nil {
		t.Fatalf("first run failed: %v", err)
	}

	// Second run with update should skip unchanged files
	if err := runPipeline(root, out, true, false); err != nil {
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
	if err := runPipeline(root, out, true, false); err != nil {
		t.Fatalf("first run failed: %v", err)
	}

	// Second run with update and no file changes
	if err := runPipeline(root, out, true, false); err != nil {
		t.Fatalf("second run failed: %v", err)
	}
}

func TestRunPipelineInvalidRoot(t *testing.T) {
	out := t.TempDir()
	if err := runPipeline("/nonexistent/path/12345", out, false, false); err == nil {
		t.Fatal("expected error for invalid root")
	}
}

func TestRunPipelineEmptyCorpus(t *testing.T) {
	root := t.TempDir()
	out := t.TempDir()

	if err := runPipeline(root, out, false, false); err != nil {
		t.Fatalf("pipeline failed on empty corpus: %v", err)
	}

	for _, file := range []string{"graph.json", "GRAPH_REPORT.md", "graph.html"} {
		path := filepath.Join(out, file)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Fatalf("%s not created for empty corpus", file)
		}
	}
}

func TestValidateCommandAcceptsValidGraph(t *testing.T) {
	root := "../../testdata/e2e/mini-corpus"
	out := t.TempDir()
	if err := runPipeline(root, out, false, false); err != nil {
		t.Fatalf("pipeline: %v", err)
	}
	if err := validateCommand(filepath.Join(out, "graph.json")); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

func TestValidateCommandRejectsDanglingEdge(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "graph.json")
	const body = `{"nodes":[{"ID":"a","Label":"A"}],"edges":[{"Source":"a","Target":"missing","Relation":"calls","Confidence":0}]}`
	if err := os.WriteFile(bad, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	if err := validateCommand(bad); err == nil {
		t.Fatal("expected error for dangling edge target")
	}
}

func TestValidateCommandRejectsMissingFile(t *testing.T) {
	if err := validateCommand("/nonexistent/path/graph.json"); err == nil {
		t.Fatal("expected error for missing graph file")
	}
}

func TestReportCommandRendersReport(t *testing.T) {
	root := "../../testdata/e2e/mini-corpus"
	out := t.TempDir()
	if err := runPipeline(root, out, false, false); err != nil {
		t.Fatalf("pipeline: %v", err)
	}
	if err := reportCommand(filepath.Join(out, "graph.json"), io.Discard); err != nil {
		t.Fatalf("report: %v", err)
	}
}

func TestReportCommandWritesToProvidedWriter(t *testing.T) {
	root := "../../testdata/e2e/mini-corpus"
	out := t.TempDir()
	if err := runPipeline(root, out, false, false); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := reportCommand(filepath.Join(out, "graph.json"), &buf); err != nil {
		t.Fatalf("report: %v", err)
	}
	if !bytes.Contains(buf.Bytes(), []byte("# Graph Report")) {
		t.Fatalf("expected rendered report header, got %q", buf.String())
	}
}

func TestReportCommandRejectsBadJSON(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "graph.json")
	if err := os.WriteFile(bad, []byte("not json"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := reportCommand(bad, io.Discard); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestRunPipelineReadOnlyOut(t *testing.T) {
	root := "../../testdata/e2e/mini-corpus"
	out := t.TempDir()
	os.Chmod(out, 0555)
	defer os.Chmod(out, 0755)

	if err := runPipeline(root, out, false, false); err == nil {
		t.Fatal("expected error for read-only output directory")
	}
}
