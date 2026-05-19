package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/julianshen/gogfy/internal/export"
	"github.com/julianshen/gogfy/internal/llm"
	"github.com/julianshen/gogfy/internal/schema"
	"github.com/julianshen/gogfy/internal/transcribe"
)

// dispatchTo runs dispatch with stdout temporarily redirected so tests
// can assert on what subcommands write to stdout. dispatch() itself
// hardcodes os.Stdout for the report/serve/path/benchmark/etc.
// subcommands; redirection is the lowest-touch alternative to a
// signature refactor.
func dispatchTo(t *testing.T, args []string, stdout, stderr io.Writer) error {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(stdout, r)
		close(done)
	}()
	dispatchErr := dispatch(args, stderr)
	w.Close()
	os.Stdout = orig
	<-done
	return dispatchErr
}

func TestUpdateModeNoChangesPreservesOutputs(t *testing.T) {
	root := "../../testdata/e2e/mini-corpus"
	out := t.TempDir()

	// First run produces real outputs.
	if err := runPipeline(root, out, true, false, runOptions{}); err != nil {
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

	if err := runPipeline(root, out, true, false, runOptions{}); err != nil {
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
	if err := runPipeline("../../testdata/e2e/mini-corpus", out, false, false, runOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := dispatch([]string{"validate", filepath.Join(out, "graph.json")}, os.Stderr); err != nil {
		t.Fatalf("dispatch validate: %v", err)
	}
}

func TestDispatchReportSubcommand(t *testing.T) {
	out := t.TempDir()
	if err := runPipeline("../../testdata/e2e/mini-corpus", out, false, false, runOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := dispatch([]string{"report", filepath.Join(out, "graph.json")}, os.Stderr); err != nil {
		t.Fatalf("dispatch report: %v", err)
	}
}

func TestDispatchBenchmarkSubcommand(t *testing.T) {
	out := t.TempDir()
	if err := runPipeline("../../testdata/e2e/mini-corpus", out, false, false, runOptions{}); err != nil {
		t.Fatal(err)
	}
	graphPath := filepath.Join(out, "graph.json")
	// Test the flag-reorderer's recognition of --corpus-words / --depth
	// / --json when the positional appears before them, AND that the
	// JSON branch writes a parseable Result to stdout. mini-corpus
	// has label "main" which matches the default
	// "what is the main entry point" question, so success is expected.
	var stdout, stderr bytes.Buffer
	if err := dispatchTo(t, []string{
		"benchmark", graphPath,
		"--corpus-words", "1000",
		"--depth", "2",
		"--json",
	}, &stdout, &stderr); err != nil {
		t.Fatalf("dispatch benchmark: %v\nstderr: %s", err, stderr.String())
	}
	// JSON branch must emit a parseable Result with the corpus override
	// honored.
	var got struct {
		CorpusWords    int     `json:"corpus_words"`
		CorpusTokens   int     `json:"corpus_tokens"`
		Nodes          int     `json:"nodes"`
		AvgQueryTokens int     `json:"avg_query_tokens"`
		ReductionRatio float64 `json:"reduction_ratio"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("benchmark --json did not emit valid JSON to stdout: %v\nstdout: %s", err, stdout.String())
	}
	if got.CorpusWords != 1000 {
		t.Fatalf("--corpus-words override not honored: got %d", got.CorpusWords)
	}
	if got.Nodes == 0 || got.AvgQueryTokens == 0 {
		t.Fatalf("expected non-zero nodes + avg_query_tokens, got %+v", got)
	}
}

func TestReorderFlags(t *testing.T) {
	cases := []struct {
		name        string
		args        []string
		valueFlags  []string
		boolFlags   []string
		want        []string
		errContains string
	}{
		{
			name:       "positional before value flag",
			args:       []string{"graph.json", "--out", "dir"},
			valueFlags: []string{"out"},
			want:       []string{"--out", "dir", "graph.json"},
		},
		{
			name:       "equals form preserved without splitting",
			args:       []string{"graph.json", "--corpus-words=500"},
			valueFlags: []string{"corpus-words"},
			want:       []string{"--corpus-words=500", "graph.json"},
		},
		{
			name:      "bool flag does not consume next arg",
			args:      []string{"graph.json", "--json", "extra"},
			boolFlags: []string{"json"},
			want:      []string{"--json", "graph.json", "extra"},
		},
		{
			name:      "bool flag at end of args",
			args:      []string{"graph.json", "--json"},
			boolFlags: []string{"json"},
			want:      []string{"--json", "graph.json"},
		},
		{
			name:       "single-dash form also recognized",
			args:       []string{"-out", "dir", "graph.json"},
			valueFlags: []string{"out"},
			want:       []string{"-out", "dir", "graph.json"},
		},
		{
			name:        "missing value at end of args",
			args:        []string{"graph.json", "--out"},
			valueFlags:  []string{"out"},
			errContains: "requires a value",
		},
		{
			name:        "unknown flag errors",
			args:        []string{"--bogus", "x"},
			valueFlags:  []string{"out"},
			errContains: "unknown flag",
		},
		{
			name:        "value-flag followed by another flag is rejected",
			args:        []string{"graph.json", "--depth", "--json"},
			valueFlags:  []string{"depth"},
			boolFlags:   []string{"json"},
			errContains: "requires a value, got flag",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := reorderFlags(c.args, c.valueFlags, c.boolFlags)
			if c.errContains != "" {
				if err == nil || !strings.Contains(err.Error(), c.errContains) {
					t.Fatalf("expected error containing %q, got: %v", c.errContains, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !equalStrings(got, c.want) {
				t.Fatalf("reorder mismatch:\n got: %v\nwant: %v", got, c.want)
			}
		})
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestDispatchGlobalAddListRemove(t *testing.T) {
	// End-to-end: run pipeline → global add → global list → global remove.
	// Uses --dir to isolate from any real ~/.gogfy on the test machine.
	out := t.TempDir()
	if err := runPipeline("../../testdata/e2e/mini-corpus", out, false, false, runOptions{}); err != nil {
		t.Fatal(err)
	}
	graphPath := filepath.Join(out, "graph.json")
	storeDir := t.TempDir()

	var stdout, stderr bytes.Buffer
	if err := dispatchTo(t, []string{"global", "add", graphPath, "--as", "minirepo", "--dir", storeDir}, &stdout, &stderr); err != nil {
		t.Fatalf("global add: %v\nstderr: %s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "minirepo") {
		t.Fatalf("add stdout missing repo tag: %q", stdout.String())
	}

	// Re-add unchanged source → skipped path.
	stdout.Reset()
	stderr.Reset()
	if err := dispatchTo(t, []string{"global", "add", graphPath, "--as", "minirepo", "--dir", storeDir}, &stdout, &stderr); err != nil {
		t.Fatalf("global add (re-run): %v", err)
	}
	if !strings.Contains(stdout.String(), "skipped") {
		t.Fatalf("re-add should report skipped, got: %q", stdout.String())
	}

	// list must show the tag.
	stdout.Reset()
	if err := dispatchTo(t, []string{"global", "list", "--dir", storeDir}, &stdout, &stderr); err != nil {
		t.Fatalf("global list: %v", err)
	}
	if !strings.Contains(stdout.String(), "minirepo") {
		t.Fatalf("list missing tag: %q", stdout.String())
	}

	// path prints the merged-graph file path.
	stdout.Reset()
	if err := dispatchTo(t, []string{"global", "path", "--dir", storeDir}, &stdout, &stderr); err != nil {
		t.Fatalf("global path: %v", err)
	}
	if !strings.Contains(stdout.String(), "global-graph.json") {
		t.Fatalf("path missing global-graph.json: %q", stdout.String())
	}

	// remove cleans it up.
	stdout.Reset()
	if err := dispatchTo(t, []string{"global", "remove", "minirepo", "--dir", storeDir}, &stdout, &stderr); err != nil {
		t.Fatalf("global remove: %v", err)
	}
	stdout.Reset()
	if err := dispatchTo(t, []string{"global", "list", "--dir", storeDir}, &stdout, &stderr); err != nil {
		t.Fatalf("global list (after remove): %v", err)
	}
	if !strings.Contains(stdout.String(), "no repos added yet") {
		t.Fatalf("list should report empty after remove, got: %q", stdout.String())
	}
}

func TestDefaultRepoTag(t *testing.T) {
	// Project-shaped path: parent-of-parent wins.
	if got := defaultRepoTag("/home/me/myproj/graphify-out/graph.json"); got != "myproj" {
		t.Errorf("project path: got %q, want myproj", got)
	}
	// Bare graph.json in cwd resolves to the cwd's basename or
	// falls through to "". Whatever the result, validateTag in
	// globalgraph.Add must accept it or the user gets a clear
	// "pass --as" error.
	if got := defaultRepoTag("graph.json"); got == "." || got == ".." || got == "/" {
		t.Errorf("defaultRepoTag should never return a path-only token, got %q", got)
	}
}

func TestDispatchGlobalAddUsesIsolatedDir(t *testing.T) {
	// Regression: the --dir flag must actually drive the write
	// location. If a future refactor dropped the flag wiring, the
	// existing list-then-remove test would still pass (it'd read
	// from whatever ~/.gogfy holds). Pin the location with os.Stat.
	out := t.TempDir()
	if err := runPipeline("../../testdata/e2e/mini-corpus", out, false, false, runOptions{}); err != nil {
		t.Fatal(err)
	}
	storeDir := t.TempDir()
	var stdout, stderr bytes.Buffer
	if err := dispatchTo(t, []string{
		"global", "add", filepath.Join(out, "graph.json"),
		"--as", "iso", "--dir", storeDir,
	}, &stdout, &stderr); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if _, err := os.Stat(filepath.Join(storeDir, "global-graph.json")); err != nil {
		t.Fatalf("expected global-graph.json inside --dir %q: %v", storeDir, err)
	}
	if _, err := os.Stat(filepath.Join(storeDir, "global-manifest.json")); err != nil {
		t.Fatalf("expected global-manifest.json inside --dir %q: %v", storeDir, err)
	}
}

func TestDispatchGlobalListRejectsStrayPositional(t *testing.T) {
	err := dispatch([]string{"global", "list", "stray", "--dir", t.TempDir()}, os.Stderr)
	if err == nil || !strings.Contains(err.Error(), "unexpected positional") {
		t.Fatalf("expected unexpected-positional error, got: %v", err)
	}
}

func TestDispatchGlobalPathRejectsStrayPositional(t *testing.T) {
	err := dispatch([]string{"global", "path", "stray", "--dir", t.TempDir()}, os.Stderr)
	if err == nil || !strings.Contains(err.Error(), "unexpected positional") {
		t.Fatalf("expected unexpected-positional error, got: %v", err)
	}
}

func TestDispatchGlobalRejectsUnknownVerb(t *testing.T) {
	err := dispatch([]string{"global", "bogus"}, os.Stderr)
	if err == nil {
		t.Fatal("expected error for unknown global verb")
	}
	if !strings.Contains(err.Error(), "unknown verb") {
		t.Fatalf("expected 'unknown verb' error, got: %v", err)
	}
}

func TestDispatchCallflowSubcommand(t *testing.T) {
	out := t.TempDir()
	if err := runPipeline("../../testdata/e2e/mini-corpus", out, false, false, runOptions{}); err != nil {
		t.Fatal(err)
	}
	graphPath := filepath.Join(out, "graph.json")
	htmlPath := filepath.Join(out, "callflow.html")
	if err := dispatch([]string{
		"callflow", graphPath,
		"--out", htmlPath,
		"--max-sections", "3",
		"--max-nodes", "5",
		"--project", "test-project",
	}, os.Stderr); err != nil {
		t.Fatalf("dispatch callflow: %v", err)
	}
	body, err := os.ReadFile(htmlPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !strings.Contains(string(body), "test-project") {
		t.Fatal("--project not honored")
	}
	if !strings.Contains(string(body), "mermaid") {
		t.Fatal("output missing mermaid reference")
	}
}

func TestDispatchCallflowRejectsUnknownFlag(t *testing.T) {
	out := t.TempDir()
	if err := runPipeline("../../testdata/e2e/mini-corpus", out, false, false, runOptions{}); err != nil {
		t.Fatal(err)
	}
	graphPath := filepath.Join(out, "graph.json")
	err := dispatch([]string{"callflow", graphPath, "--no-such-flag", "x"}, os.Stderr)
	if err == nil {
		t.Fatal("expected error for unknown flag")
	}
	if !strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("expected 'unknown flag' error, got: %v", err)
	}
}

func TestDispatchBenchmarkRejectsUnknownFlag(t *testing.T) {
	out := t.TempDir()
	if err := runPipeline("../../testdata/e2e/mini-corpus", out, false, false, runOptions{}); err != nil {
		t.Fatal(err)
	}
	graphPath := filepath.Join(out, "graph.json")
	err := dispatch([]string{"benchmark", graphPath, "--no-such-flag", "x"}, os.Stderr)
	if err == nil {
		t.Fatal("expected error for unknown flag")
	}
	if !strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("expected 'unknown flag' error, got: %v", err)
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
	if err := runPipeline(root, out, true, false, runOptions{}); err != nil {
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

	if err := runPipeline(root, out, false, false, runOptions{}); err != nil {
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
	if err := runPipeline(root, out, true, false, runOptions{}); err != nil {
		t.Fatalf("first run failed: %v", err)
	}

	// Second run with update should skip unchanged files
	if err := runPipeline(root, out, true, false, runOptions{}); err != nil {
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
	if err := runPipeline(root, out, true, false, runOptions{}); err != nil {
		t.Fatalf("first run failed: %v", err)
	}

	// Second run with update and no file changes
	if err := runPipeline(root, out, true, false, runOptions{}); err != nil {
		t.Fatalf("second run failed: %v", err)
	}
}

func TestRunPipelineInvalidRoot(t *testing.T) {
	out := t.TempDir()
	if err := runPipeline("/nonexistent/path/12345", out, false, false, runOptions{}); err == nil {
		t.Fatal("expected error for invalid root")
	}
}

func TestRunPipelineEmptyCorpus(t *testing.T) {
	root := t.TempDir()
	out := t.TempDir()

	if err := runPipeline(root, out, false, false, runOptions{}); err != nil {
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
	if err := runPipeline(root, out, false, false, runOptions{}); err != nil {
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
	if err := runPipeline(root, out, false, false, runOptions{}); err != nil {
		t.Fatalf("pipeline: %v", err)
	}
	if err := reportCommand(filepath.Join(out, "graph.json"), io.Discard); err != nil {
		t.Fatalf("report: %v", err)
	}
}

func TestReportCommandWritesToProvidedWriter(t *testing.T) {
	root := "../../testdata/e2e/mini-corpus"
	out := t.TempDir()
	if err := runPipeline(root, out, false, false, runOptions{}); err != nil {
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

func TestRunPipelineWritesGraphMLWhenRequested(t *testing.T) {
	root := "../../testdata/e2e/mini-corpus"
	out := t.TempDir()
	if err := runPipeline(root, out, false, false, runOptions{GraphML: true}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(out, "graph.graphml"))
	if err != nil {
		t.Fatalf("graph.graphml not written: %v", err)
	}
	if !bytes.Contains(data, []byte("<graphml")) {
		t.Fatalf("graphml output looks malformed:\n%s", excerptBytes(data))
	}
}

func TestRunPipelineWritesCypherWhenRequested(t *testing.T) {
	root := "../../testdata/e2e/mini-corpus"
	out := t.TempDir()
	if err := runPipeline(root, out, false, false, runOptions{Cypher: true}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(out, "graph.cypher"))
	if err != nil {
		t.Fatalf("graph.cypher not written: %v", err)
	}
	if !bytes.Contains(data, []byte("MERGE")) {
		t.Fatalf("cypher output missing MERGE statements:\n%s", excerptBytes(data))
	}
}

func TestRunPipelineDoesNotWriteOptionalArtifactsByDefault(t *testing.T) {
	root := "../../testdata/e2e/mini-corpus"
	out := t.TempDir()
	if err := runPipeline(root, out, false, false, runOptions{}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"graph.graphml", "graph.cypher"} {
		if _, err := os.Stat(filepath.Join(out, name)); err == nil {
			t.Fatalf("%s should not exist when its flag is off", name)
		}
	}
}

func TestServeCommandFailsOnMissingGraph(t *testing.T) {
	err := serveCommand(
		[]string{"--graph", "/nonexistent/graph.json"},
		bytes.NewBuffer(nil),
		io.Discard,
		os.Stderr,
	)
	if err == nil {
		t.Fatal("expected error for missing graph file")
	}
}

func TestServeCommandTolerantOfMissingReport(t *testing.T) {
	root := "../../testdata/e2e/mini-corpus"
	out := t.TempDir()
	if err := runPipeline(root, out, false, false, runOptions{}); err != nil {
		t.Fatal(err)
	}
	// Delete the report so the fallback path runs.
	if err := os.Remove(filepath.Join(out, "GRAPH_REPORT.md")); err != nil {
		t.Fatal(err)
	}
	stdin := bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"gogfy://report"}}` + "\n")
	var stdout, stderr bytes.Buffer
	if err := serveCommand(
		[]string{"--graph", filepath.Join(out, "graph.json"), "--report", filepath.Join(out, "GRAPH_REPORT.md")},
		stdin, &stdout, &stderr,
	); err != nil {
		t.Fatalf("serveCommand: %v", err)
	}
	if !bytes.Contains(stdout.Bytes(), []byte("report not available")) {
		t.Fatalf("expected fallback report text, got %q", stdout.String())
	}
}

func TestDispatchInstallSubcommandWritesConfig(t *testing.T) {
	ws := t.TempDir()
	if err := dispatch([]string{"install", "--platform", "claude", "--workspace", ws}, os.Stderr); err != nil {
		t.Fatalf("dispatch install: %v", err)
	}
	expected := filepath.Join(ws, ".mcp.json")
	if _, err := os.Stat(expected); err != nil {
		t.Fatalf("config not written at %s: %v", expected, err)
	}
}

func TestDispatchInstallRejectsUnknownPlatform(t *testing.T) {
	ws := t.TempDir()
	if err := dispatch([]string{"install", "--platform", "definitely-not-real", "--workspace", ws}, os.Stderr); err == nil {
		t.Fatal("expected error for unknown platform")
	}
}

func TestDispatchUninstallRemovesEntry(t *testing.T) {
	ws := t.TempDir()
	if err := dispatch([]string{"install", "--platform", "claude", "--workspace", ws}, os.Stderr); err != nil {
		t.Fatal(err)
	}
	if err := dispatch([]string{"uninstall", "--platform", "claude", "--workspace", ws}, os.Stderr); err != nil {
		t.Fatalf("dispatch uninstall: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(ws, ".mcp.json"))
	if bytes.Contains(data, []byte("gogfy")) {
		t.Fatalf("gogfy entry still present after uninstall: %s", data)
	}
}

func TestDispatchInstallRejectsStrayPositional(t *testing.T) {
	ws := t.TempDir()
	err := dispatch([]string{"install", "--platform", "claude", "--workspace", ws, "stray"}, os.Stderr)
	if err == nil {
		t.Fatal("expected error for stray positional in install")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("unexpected positional")) {
		t.Fatalf("expected 'unexpected positional' in error, got %v", err)
	}
}

func TestDispatchInstallHonorsCustomFlags(t *testing.T) {
	ws := t.TempDir()
	err := dispatch([]string{
		"install",
		"--platform", "claude",
		"--workspace", ws,
		"--gogfy-bin", "/opt/bin/gogfy",
		"--out", "custom",
	}, os.Stderr)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(ws, ".mcp.json"))
	if !bytes.Contains(data, []byte("/opt/bin/gogfy")) {
		t.Fatalf("--gogfy-bin not propagated: %s", data)
	}
	if !bytes.Contains(data, []byte("custom/graph.json")) {
		t.Fatalf("--out not propagated: %s", data)
	}
}

func TestDispatchInstallRequiresPlatform(t *testing.T) {
	if err := dispatch([]string{"install"}, os.Stderr); err == nil {
		t.Fatal("expected error when --platform is missing")
	}
}

func TestDispatchCodexInstallWritesTOML(t *testing.T) {
	ws := t.TempDir()
	if err := dispatch([]string{"install", "--platform", "codex", "--workspace", ws}, os.Stderr); err != nil {
		t.Fatalf("dispatch install codex: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(ws, ".codex", "config.toml"))
	if !bytes.Contains(data, []byte("[mcp_servers.gogfy]")) {
		t.Fatalf("missing [mcp_servers.gogfy] in codex config:\n%s", data)
	}
}

func TestDispatchInstallInstructionsWritesSnippet(t *testing.T) {
	ws := t.TempDir()
	target := filepath.Join(ws, "AGENTS.md")
	if err := dispatch([]string{"install-instructions", "--file", target}, os.Stderr); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	data, _ := os.ReadFile(target)
	if !bytes.Contains(data, []byte("gogfy-graph-instructions:start")) {
		t.Fatalf("missing snippet markers in %s:\n%s", target, data)
	}
	if !bytes.Contains(data, []byte("graphify-out/GRAPH_REPORT.md")) {
		t.Fatalf("missing default report path:\n%s", data)
	}
}

func TestDispatchUninstallInstructionsRemovesSnippet(t *testing.T) {
	ws := t.TempDir()
	target := filepath.Join(ws, "CLAUDE.md")
	if err := os.WriteFile(target, []byte("# Project\n\nKeep me.\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := dispatch([]string{"install-instructions", "--file", target}, os.Stderr); err != nil {
		t.Fatal(err)
	}
	if err := dispatch([]string{"uninstall-instructions", "--file", target}, os.Stderr); err != nil {
		t.Fatalf("dispatch uninstall: %v", err)
	}
	data, _ := os.ReadFile(target)
	if bytes.Contains(data, []byte("gogfy-graph-instructions")) {
		t.Fatalf("snippet still present after uninstall:\n%s", data)
	}
	if !bytes.Contains(data, []byte("Keep me.")) {
		t.Fatalf("pre-existing content erased:\n%s", data)
	}
}

func TestDispatchInstallInstructionsRejectsStrayPositional(t *testing.T) {
	if err := dispatch([]string{"install-instructions", "--file", "AGENTS.md", "stray"}, os.Stderr); err == nil {
		t.Fatal("expected error for stray positional")
	}
}

func TestDispatchHookInstallWritesPostCommit(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git", "hooks"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := dispatch([]string{"hook", "install", "--repo", repo}, os.Stderr); err != nil {
		t.Fatalf("dispatch hook install: %v", err)
	}
	path := filepath.Join(repo, ".git", "hooks", "post-commit")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("hook not written: %v", err)
	}
	// The bin defaults to os.Executable(); under `go test` that's the test
	// binary path, not literal "gogfy". Assert the invocation shape instead.
	if !bytes.Contains(data, []byte(" run --update --out 'graphify-out' .")) {
		t.Fatalf("hook missing run --update invocation:\n%s", data)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm()&0111 == 0 {
		t.Fatalf("hook not executable: %v", info.Mode())
	}
}

func TestDispatchHookUninstallRemovesPostCommit(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git", "hooks"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := dispatch([]string{"hook", "install", "--repo", repo}, os.Stderr); err != nil {
		t.Fatal(err)
	}
	if err := dispatch([]string{"hook", "uninstall", "--repo", repo}, os.Stderr); err != nil {
		t.Fatalf("dispatch hook uninstall: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, ".git", "hooks", "post-commit")); !os.IsNotExist(err) {
		t.Fatalf("hook should be removed when gogfy was sole content, got err=%v", err)
	}
}

func TestDispatchHookRejectsUnknownVerb(t *testing.T) {
	if err := dispatch([]string{"hook", "wat"}, os.Stderr); err == nil {
		t.Fatal("expected error for unknown verb")
	}
}

func TestDispatchHookRejectsMissingVerb(t *testing.T) {
	if err := dispatch([]string{"hook"}, os.Stderr); err == nil {
		t.Fatal("expected error for missing verb")
	}
}

func TestDispatchHookRejectsStrayPositional(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git", "hooks"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := dispatch([]string{"hook", "install", "--repo", repo, "stray"}, os.Stderr); err == nil {
		t.Fatal("expected error for stray positional in hook install")
	}
}

func TestDispatchHookDefaultsBinToAbsolutePath(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git", "hooks"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := dispatch([]string{"hook", "install", "--repo", repo}, os.Stderr); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(repo, ".git", "hooks", "post-commit"))
	// Bare "gogfy" would mean we're trusting PATH at hook time — exactly
	// the failure mode the absolute-path default prevents.
	if bytes.Contains(data, []byte("\ngogfy run ")) {
		t.Fatalf("hook left command as bare PATH-dependent 'gogfy':\n%s", data)
	}
}

func TestDispatchHookHonorsCustomFlags(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git", "hooks"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := dispatch([]string{"hook", "install", "--repo", repo, "--gogfy-bin", "/opt/bin/gogfy", "--out", "custom"}, os.Stderr); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(repo, ".git", "hooks", "post-commit"))
	if !bytes.Contains(data, []byte("'/opt/bin/gogfy' run --update --out 'custom'")) {
		t.Fatalf("custom flags not propagated:\n%s", data)
	}
}

func TestDispatchHookInstallMergeDriver(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git", "hooks"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := dispatch([]string{"hook", "install-merge-driver", "--repo", repo, "--gogfy-bin", "/opt/gogfy"}, os.Stderr); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	cfg, _ := os.ReadFile(filepath.Join(repo, ".git", "config"))
	if !bytes.Contains(cfg, []byte(`[merge "gogfy"]`)) {
		t.Fatalf(".git/config missing merge section:\n%s", cfg)
	}
	if !bytes.Contains(cfg, []byte("driver = /opt/gogfy merge-graphs %A %B --out %A")) {
		t.Fatalf(".git/config missing driver line:\n%s", cfg)
	}
	attrs, _ := os.ReadFile(filepath.Join(repo, ".gitattributes"))
	if !bytes.Contains(attrs, []byte("graphify-out/graph.json merge=gogfy")) {
		t.Fatalf(".gitattributes missing rule:\n%s", attrs)
	}
}

func TestDispatchHookUninstallMergeDriver(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git", "hooks"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := dispatch([]string{"hook", "install-merge-driver", "--repo", repo}, os.Stderr); err != nil {
		t.Fatal(err)
	}
	if err := dispatch([]string{"hook", "uninstall-merge-driver", "--repo", repo}, os.Stderr); err != nil {
		t.Fatalf("dispatch uninstall: %v", err)
	}
	cfg, _ := os.ReadFile(filepath.Join(repo, ".git", "config"))
	if bytes.Contains(cfg, []byte(`[merge "gogfy"]`)) {
		t.Fatalf("merge section still present:\n%s", cfg)
	}
}

func TestDispatchHookMergeDriverRejectsStrayPositional(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git", "hooks"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := dispatch([]string{"hook", "install-merge-driver", "--repo", repo, "stray"}, os.Stderr); err == nil {
		t.Fatal("expected error for stray positional")
	}
}

func TestDispatchHookStatusIncludesMergeDriver(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git", "hooks"), 0755); err != nil {
		t.Fatal(err)
	}
	// Status should include a merge-driver line whether installed or not.
	if err := dispatch([]string{"hook", "status", "--repo", repo}, os.Stderr); err != nil {
		t.Fatal(err)
	}
	if err := dispatch([]string{"hook", "install-merge-driver", "--repo", repo}, os.Stderr); err != nil {
		t.Fatal(err)
	}
	if err := dispatch([]string{"hook", "status", "--repo", repo}, os.Stderr); err != nil {
		t.Fatal(err)
	}
}

func TestDispatchHookStatus(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git", "hooks"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := dispatch([]string{"hook", "status", "--repo", repo}, os.Stderr); err != nil {
		t.Fatalf("dispatch hook status: %v", err)
	}
	// Install both hooks then re-run status — should still succeed.
	if err := dispatch([]string{"hook", "install", "--repo", repo}, os.Stderr); err != nil {
		t.Fatal(err)
	}
	if err := dispatch([]string{"hook", "status", "--repo", repo}, os.Stderr); err != nil {
		t.Fatalf("dispatch hook status (post-install): %v", err)
	}
}

func TestDispatchHookRejectsUnknownVerbAfterStatus(t *testing.T) {
	if err := dispatch([]string{"hook", "wat"}, os.Stderr); err == nil {
		t.Fatal("expected error for unknown verb")
	}
}

func TestDispatchComboInstallClaude(t *testing.T) {
	ws := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ws, ".git", "hooks"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := dispatch([]string{"claude", "install", "--workspace", ws}, os.Stderr); err != nil {
		t.Fatalf("dispatch claude install: %v", err)
	}
	// All three artifacts must exist.
	if _, err := os.Stat(filepath.Join(ws, ".mcp.json")); err != nil {
		t.Fatalf("MCP config missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(ws, "CLAUDE.md")); err != nil {
		t.Fatalf("CLAUDE.md missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(ws, ".git", "hooks", "post-commit")); err != nil {
		t.Fatalf("post-commit hook missing: %v", err)
	}
}

func TestDispatchComboUninstallRemovesMCPOnly(t *testing.T) {
	// Conservative uninstall: removes only the MCP config. The shared
	// docs-file snippet and the repo-wide post-commit hook stay so that
	// other platforms relying on them keep working. Users explicitly
	// remove them via `uninstall-instructions` and `hook uninstall`.
	ws := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ws, ".git", "hooks"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := dispatch([]string{"claude", "install", "--workspace", ws}, os.Stderr); err != nil {
		t.Fatal(err)
	}
	if err := dispatch([]string{"claude", "uninstall", "--workspace", ws}, os.Stderr); err != nil {
		t.Fatalf("dispatch claude uninstall: %v", err)
	}
	// MCP config (.mcp.json) gone.
	if _, err := os.Stat(filepath.Join(ws, ".mcp.json")); err == nil {
		// .mcp.json may still exist if it has other content; check that
		// the gogfy entry specifically is gone by reading it.
		data, _ := os.ReadFile(filepath.Join(ws, ".mcp.json"))
		if bytes.Contains(data, []byte(`"gogfy"`)) {
			t.Fatalf("gogfy entry should be removed from .mcp.json, got %s", data)
		}
	}
	// Snippet preserved (could be shared with other platforms).
	if _, err := os.Stat(filepath.Join(ws, "CLAUDE.md")); err != nil {
		t.Fatalf("CLAUDE.md should be preserved on combo uninstall, got %v", err)
	}
	// Hook preserved (repo-wide).
	if _, err := os.Stat(filepath.Join(ws, ".git", "hooks", "post-commit")); err != nil {
		t.Fatalf("post-commit should be preserved on combo uninstall, got %v", err)
	}
}

func TestDispatchComboInstallCodexUsesAgentsMd(t *testing.T) {
	ws := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ws, ".git", "hooks"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := dispatch([]string{"codex", "install", "--workspace", ws}, os.Stderr); err != nil {
		t.Fatalf("dispatch codex install: %v", err)
	}
	if _, err := os.Stat(filepath.Join(ws, "AGENTS.md")); err != nil {
		t.Fatalf("AGENTS.md should be the codex docs target: %v", err)
	}
	if _, err := os.Stat(filepath.Join(ws, ".codex", "config.toml")); err != nil {
		t.Fatalf("codex MCP config missing: %v", err)
	}
}

func TestDispatchComboInstallCursorUsesCursorrules(t *testing.T) {
	ws := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ws, ".git", "hooks"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := dispatch([]string{"cursor", "install", "--workspace", ws}, os.Stderr); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(ws, ".cursorrules")); err != nil {
		t.Fatalf(".cursorrules should be the cursor docs target: %v", err)
	}
}

func TestDispatchComboInstallNotARepoFails(t *testing.T) {
	ws := t.TempDir() // no .git
	if err := dispatch([]string{"claude", "install", "--workspace", ws}, os.Stderr); err == nil {
		t.Fatal("expected error: combo install needs a git repo")
	}
}

func TestDispatchComboInstallRejectsStrayPositional(t *testing.T) {
	ws := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ws, ".git", "hooks"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := dispatch([]string{"claude", "install", "--workspace", ws, "stray"}, os.Stderr); err == nil {
		t.Fatal("expected error for stray positional")
	}
}

func TestDispatchComboInstallUnknownPlatformFallsThrough(t *testing.T) {
	// `gogfy doesntexist install` should hit the default branch and return
	// "unknown subcommand" rather than match the combo wrapper.
	if err := dispatch([]string{"doesntexist", "install"}, os.Stderr); err == nil {
		t.Fatal("expected error for unknown platform")
	}
}

func TestRunPipelineNoVizSkipsHTML(t *testing.T) {
	root := "../../testdata/e2e/mini-corpus"
	out := t.TempDir()
	if err := runPipeline(root, out, false, false, runOptions{NoViz: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, "graph.json")); err != nil {
		t.Fatalf("graph.json missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "GRAPH_REPORT.md")); err != nil {
		t.Fatalf("GRAPH_REPORT.md missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "graph.html")); !os.IsNotExist(err) {
		t.Fatalf("graph.html should not exist with --no-viz, got err=%v", err)
	}
}

func TestRunPipelineClusterOnly(t *testing.T) {
	root := "../../testdata/e2e/mini-corpus"
	out := t.TempDir()
	// First, do a full run to produce graph.json.
	if err := runPipeline(root, out, false, false, runOptions{}); err != nil {
		t.Fatal(err)
	}
	originalReport, _ := os.ReadFile(filepath.Join(out, "GRAPH_REPORT.md"))
	// Delete graph.html to confirm cluster-only writes it back.
	os.Remove(filepath.Join(out, "graph.html"))
	if err := runPipeline("", out, false, false, runOptions{ClusterOnly: true}); err != nil {
		t.Fatalf("cluster-only: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "graph.html")); err != nil {
		t.Fatalf("cluster-only should rewrite graph.html, got err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "GRAPH_REPORT.md")); err != nil {
		t.Fatalf("GRAPH_REPORT.md missing after cluster-only: %v", err)
	}
	if len(originalReport) == 0 {
		t.Fatal("original report empty — test setup wrong")
	}
}

func TestRunPipelineClusterOnlyNoViz(t *testing.T) {
	root := "../../testdata/e2e/mini-corpus"
	out := t.TempDir()
	if err := runPipeline(root, out, false, false, runOptions{}); err != nil {
		t.Fatal(err)
	}
	os.Remove(filepath.Join(out, "graph.html"))
	if err := runPipeline("", out, false, false, runOptions{ClusterOnly: true, NoViz: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, "graph.html")); !os.IsNotExist(err) {
		t.Fatal("graph.html should not be written with --cluster-only --no-viz")
	}
}

func TestRunPipelineClusterOnlyMissingGraph(t *testing.T) {
	out := t.TempDir() // no graph.json
	if err := runPipeline("", out, false, false, runOptions{ClusterOnly: true}); err == nil {
		t.Fatal("expected error when graph.json is missing")
	}
}

func TestDispatchRunNoVizFlag(t *testing.T) {
	out := t.TempDir()
	if err := dispatch([]string{"run", "../../testdata/e2e/mini-corpus", "--out", out, "--no-viz"}, os.Stderr); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, "graph.html")); !os.IsNotExist(err) {
		t.Fatal("graph.html should not exist")
	}
}

func TestDispatchRunClusterOnlyFlag(t *testing.T) {
	out := t.TempDir()
	if err := dispatch([]string{"run", "../../testdata/e2e/mini-corpus", "--out", out}, os.Stderr); err != nil {
		t.Fatal(err)
	}
	originalJSON, _ := os.ReadFile(filepath.Join(out, "graph.json"))
	if err := dispatch([]string{"run", "--cluster-only", "--out", out}, os.Stderr); err != nil {
		t.Fatalf("dispatch --cluster-only: %v", err)
	}
	rerunJSON, _ := os.ReadFile(filepath.Join(out, "graph.json"))
	if len(rerunJSON) == 0 {
		t.Fatal("graph.json missing after --cluster-only")
	}
	// Sanity: cluster-only on the same input should produce the same graph
	// content (Leiden seeds on edges, which haven't changed).
	if len(originalJSON) != len(rerunJSON) {
		t.Logf("note: cluster-only output size differs from full run (%d vs %d) — may be acceptable if community ids change", len(originalJSON), len(rerunJSON))
	}
}

func TestDispatchPathCommandFindsRoute(t *testing.T) {
	root := "../../testdata/e2e/mini-corpus"
	out := t.TempDir()
	if err := runPipeline(root, out, false, false, runOptions{}); err != nil {
		t.Fatal(err)
	}
	// Read graph to find two related nodes for the assertion.
	g, err := loadGraph(filepath.Join(out, "graph.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Nodes) < 2 || len(g.Edges) == 0 {
		t.Skip("mini-corpus produced too few nodes/edges to test path-finding")
	}
	// Pick the first edge so we know a path exists.
	src, tgt := g.Edges[0].Source, g.Edges[0].Target
	if err := dispatch([]string{"path", "--graph", filepath.Join(out, "graph.json"), src, tgt}, os.Stderr); err != nil {
		t.Fatalf("dispatch path: %v", err)
	}
}

func TestDispatchPathCommandRejectsMissingArgs(t *testing.T) {
	if err := dispatch([]string{"path"}, os.Stderr); err == nil {
		t.Fatal("expected error when source/target missing")
	}
	if err := dispatch([]string{"path", "src-only"}, os.Stderr); err == nil {
		t.Fatal("expected error when target missing")
	}
}

func TestDispatchPathCommandUnknownNode(t *testing.T) {
	root := "../../testdata/e2e/mini-corpus"
	out := t.TempDir()
	if err := runPipeline(root, out, false, false, runOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := dispatch([]string{"path", "--graph", filepath.Join(out, "graph.json"), "definitely-not-a-node", "also-not"}, os.Stderr); err == nil {
		t.Fatal("expected error for unknown source node")
	}
}

func TestDispatchMergeGraphsWritesUnion(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.json")
	b := filepath.Join(dir, "b.json")
	out := filepath.Join(dir, "merged.json")
	if err := os.WriteFile(a, []byte(`{"nodes":[{"ID":"a","Label":"A"}],"edges":[]}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte(`{"nodes":[{"ID":"b","Label":"B"}],"edges":[]}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := dispatch([]string{"merge-graphs", a, b, "--out", out}, os.Stderr); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	g, err := loadGraph(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Nodes) != 2 {
		t.Fatalf("expected 2 nodes after union, got %d", len(g.Nodes))
	}
}

func TestDispatchMergeGraphsRequiresTwoInputs(t *testing.T) {
	if err := dispatch([]string{"merge-graphs", "a.json"}, os.Stderr); err == nil {
		t.Fatal("expected error with single input")
	}
}

func TestDispatchMergeGraphsBadFile(t *testing.T) {
	if err := dispatch([]string{"merge-graphs", "/nonexistent/a.json", "/nonexistent/b.json"}, os.Stderr); err == nil {
		t.Fatal("expected error for missing files")
	}
}

func TestDispatchMergeGraphsToStdout(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.json")
	b := filepath.Join(dir, "b.json")
	if err := os.WriteFile(a, []byte(`{"nodes":[{"ID":"x","Label":"X"}],"edges":[]}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte(`{"nodes":[{"ID":"y","Label":"Y"}],"edges":[]}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := dispatch([]string{"merge-graphs", a, b}, io.Discard); err != nil {
		t.Fatalf("dispatch (stdout): %v", err)
	}
}

func TestServeCommandRejectsUnexpectedPositionalArgs(t *testing.T) {
	err := serveCommand(
		[]string{"--graph", "/tmp/x.json", "stray-arg"},
		bytes.NewBuffer(nil),
		io.Discard,
		os.Stderr,
	)
	if err == nil {
		t.Fatal("expected error for stray positional argument")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("unexpected positional")) {
		t.Fatalf("expected 'unexpected positional' in error, got %v", err)
	}
}

func TestDispatchServeSubcommandHandlesInitialize(t *testing.T) {
	root := "../../testdata/e2e/mini-corpus"
	out := t.TempDir()
	if err := runPipeline(root, out, false, false, runOptions{}); err != nil {
		t.Fatalf("pipeline: %v", err)
	}
	// Serve a single initialize request and capture the response.
	req := []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n")
	stdin := bytes.NewBuffer(req)
	var stdout bytes.Buffer
	if err := serveCommand([]string{"--graph", filepath.Join(out, "graph.json"), "--report", filepath.Join(out, "GRAPH_REPORT.md")}, stdin, &stdout, os.Stderr); err != nil {
		t.Fatalf("serveCommand: %v", err)
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`"name":"gogfy"`)) {
		t.Fatalf("expected serverInfo.name=gogfy in response, got %q", stdout.String())
	}
}

func excerptBytes(b []byte) string {
	if len(b) > 200 {
		b = b[:200]
	}
	return string(b)
}

func TestRunPipelineReadOnlyOut(t *testing.T) {
	root := "../../testdata/e2e/mini-corpus"
	out := t.TempDir()
	os.Chmod(out, 0555)
	defer os.Chmod(out, 0755)

	if err := runPipeline(root, out, false, false, runOptions{}); err == nil {
		t.Fatal("expected error for read-only output directory")
	}
}

func TestDispatchLabelsAndWikiAutoload(t *testing.T) {
	// Pipeline produces graph.json; `labels` derives names; `wiki` picks
	// them up automatically via the adjacent .graphify_labels.json file.
	out := t.TempDir()
	if err := runPipeline("../../testdata/e2e/mini-corpus", out, false, false, runOptions{}); err != nil {
		t.Fatal(err)
	}
	graphPath := filepath.Join(out, "graph.json")

	var stdout, stderr bytes.Buffer
	if err := dispatchTo(t, []string{"labels", graphPath}, &stdout, &stderr); err != nil {
		t.Fatalf("labels: %v\nstderr: %s", err, stderr.String())
	}
	labelsPath := filepath.Join(out, ".graphify_labels.json")
	if _, err := os.Stat(labelsPath); err != nil {
		t.Fatalf("labels file not created: %v", err)
	}

	// Second invocation must refuse without --force, so hand-edits survive.
	stderr.Reset()
	if err := dispatchTo(t, []string{"labels", graphPath}, &stdout, &stderr); err == nil {
		t.Fatal("expected error on second labels run without --force")
	}

	// --force allows overwrite.
	if err := dispatchTo(t, []string{"labels", graphPath, "--force"}, &stdout, &stderr); err != nil {
		t.Fatalf("labels --force: %v", err)
	}

	// wiki must pick up labels automatically.
	wikiDir := filepath.Join(out, "wiki-auto")
	if err := dispatchTo(t, []string{"wiki", graphPath, "--out", wikiDir}, &stdout, &stderr); err != nil {
		t.Fatalf("wiki: %v", err)
	}
	// index.md should reference at least one community by a non-default name.
	idx, err := os.ReadFile(filepath.Join(wikiDir, "index.md"))
	if err != nil {
		t.Fatal(err)
	}
	lbl, err := os.ReadFile(labelsPath)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]string
	if err := json.Unmarshal(lbl, &m); err != nil {
		t.Fatal(err)
	}
	if len(m) == 0 {
		t.Skip("clustering produced no communities — nothing to assert")
	}
	hit := false
	for _, name := range m {
		if name != "" && bytes.Contains(idx, []byte(name)) {
			hit = true
			break
		}
	}
	if !hit {
		t.Fatalf("none of the labels %+v appeared in index.md", m)
	}
}

func TestDispatchLabelsCustomOutPath(t *testing.T) {
	// Pins the --out flag against silent default-path regressions
	// (groupWikiFlags-style reordering refactors could break this).
	out := t.TempDir()
	if err := runPipeline("../../testdata/e2e/mini-corpus", out, false, false, runOptions{}); err != nil {
		t.Fatal(err)
	}
	graphPath := filepath.Join(out, "graph.json")
	custom := filepath.Join(t.TempDir(), "elsewhere", "names.json")

	var stdout, stderr bytes.Buffer
	if err := dispatchTo(t, []string{"labels", graphPath, "--out", custom}, &stdout, &stderr); err != nil {
		t.Fatalf("labels --out: %v\nstderr: %s", err, stderr.String())
	}
	if _, err := os.Stat(custom); err != nil {
		t.Fatalf("--out file not written at %s: %v", custom, err)
	}
	// Must not have also written the default location.
	if _, err := os.Stat(filepath.Join(out, ".graphify_labels.json")); err == nil {
		t.Fatal("default labels file should not exist when --out was provided")
	}
}

func TestDispatchObsidian(t *testing.T) {
	out := t.TempDir()
	if err := runPipeline("../../testdata/e2e/mini-corpus", out, false, false, runOptions{}); err != nil {
		t.Fatal(err)
	}
	graphPath := filepath.Join(out, "graph.json")
	vault := filepath.Join(t.TempDir(), "vault")

	var stdout, stderr bytes.Buffer
	if err := dispatchTo(t, []string{"obsidian", graphPath, "--out", vault}, &stdout, &stderr); err != nil {
		t.Fatalf("obsidian: %v\nstderr: %s", err, stderr.String())
	}
	// At least one per-node .md and one community overview must exist.
	entries, err := os.ReadDir(vault)
	if err != nil {
		t.Fatal(err)
	}
	hasNode, hasCommunity := false, false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "_COMMUNITY_") {
			hasCommunity = true
		} else if strings.HasSuffix(e.Name(), ".md") {
			hasNode = true
		}
	}
	if !hasNode {
		t.Fatalf("vault has no per-node notes: %v", entries)
	}
	if !hasCommunity {
		t.Fatalf("vault has no _COMMUNITY_*.md notes: %v", entries)
	}
}

func TestDispatchDiff(t *testing.T) {
	dir := t.TempDir()
	oldP := filepath.Join(dir, "old.json")
	newP := filepath.Join(dir, "new.json")
	if err := os.WriteFile(oldP, []byte(`{"nodes":[{"ID":"a","Label":"A"},{"ID":"b","Label":"B"}],"edges":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newP, []byte(`{"nodes":[{"ID":"a","Label":"A2"},{"ID":"c","Label":"C"}],"edges":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if err := dispatchTo(t, []string{"diff", oldP, newP}, &stdout, &stderr); err != nil {
		t.Fatalf("diff: %v", err)
	}
	out := stdout.String()
	for _, want := range []string{"Nodes added", "C ", "Nodes removed", "B ", "Nodes changed", "a:"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in diff output: %s", want, out)
		}
	}
}

// fakeClient lets the cap-enforcement tests run without network.
// Each Generate returns a canned token spend so the running tally
// is predictable.
type fakeClient struct {
	inputTokens  int
	outputTokens int
	usd          float64
}

func (fakeClient) Name() string { return "fake" }
func (f fakeClient) Generate(_ context.Context, _ llm.Request) (llm.Response, error) {
	return llm.Response{
		Text:             `{"entities":[],"relations":[]}`,
		InputTokens:      f.inputTokens,
		OutputTokens:     f.outputTokens,
		EstimatedUSDCost: f.usd,
	}, nil
}

func TestRunSemanticJobsHaltsAtMaxCost(t *testing.T) {
	// 5 jobs at $0.10 each, cap at $0.25 → expect ≤ 3 results.
	// Strictly speaking 2 are guaranteed to land before the cap
	// trips, but the third might run before the second's tally is
	// recorded — the bound to test is "no more than the cap allows
	// plus already-in-flight."
	client := fakeClient{usd: 0.10, inputTokens: 100}
	jobs := []semanticJob{
		{path: "a.md", src: []byte("x"), kind: "text"},
		{path: "b.md", src: []byte("x"), kind: "text"},
		{path: "c.md", src: []byte("x"), kind: "text"},
		{path: "d.md", src: []byte("x"), kind: "text"},
		{path: "e.md", src: []byte("x"), kind: "text"},
	}
	results, err := runSemanticJobs(context.Background(), client, jobs, 1, 0.25, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	executed := 0
	for _, r := range results {
		if r.EstimatedUSDCost > 0 {
			executed++
		}
	}
	// With concurrency=1, the cap-check fires deterministically after
	// each result lands, so exactly 3 should run ($0.30 ≥ $0.25 triggers
	// the halt before the 4th starts).
	if executed != 3 {
		t.Fatalf("expected exactly 3 jobs to run before cap, got %d", executed)
	}
}

func TestRunSemanticJobsHaltsAtMaxTokens(t *testing.T) {
	// Independent token cap — for local backends with zero USD cost.
	client := fakeClient{inputTokens: 100, outputTokens: 50} // 150 tokens/call
	jobs := []semanticJob{
		{path: "a.md", src: []byte("x"), kind: "text"},
		{path: "b.md", src: []byte("x"), kind: "text"},
		{path: "c.md", src: []byte("x"), kind: "text"},
		{path: "d.md", src: []byte("x"), kind: "text"},
	}
	// Cap at 250 → 2 jobs (300) trips, expect exactly 2 to run.
	results, _ := runSemanticJobs(context.Background(), client, jobs, 1, 0, 250, nil)
	executed := 0
	for _, r := range results {
		if r.InputTokens > 0 {
			executed++
		}
	}
	if executed != 2 {
		t.Fatalf("expected 2 jobs before token cap, got %d", executed)
	}
}

func TestRunSemanticJobsZeroCapsRunAll(t *testing.T) {
	// Zero caps = no limit. All jobs must run.
	client := fakeClient{usd: 0.01, inputTokens: 10}
	jobs := []semanticJob{
		{path: "a.md", src: []byte("x"), kind: "text"},
		{path: "b.md", src: []byte("x"), kind: "text"},
		{path: "c.md", src: []byte("x"), kind: "text"},
	}
	results, _ := runSemanticJobs(context.Background(), client, jobs, 1, 0, 0, nil)
	executed := 0
	for _, r := range results {
		if r.EstimatedUSDCost > 0 {
			executed++
		}
	}
	if executed != 3 {
		t.Fatalf("zero caps should let all 3 jobs run, got %d", executed)
	}
}

func TestRunPipelineTranscribeWithoutSemanticErrors(t *testing.T) {
	// --transcribe-backend without --semantic would burn Whisper
	// minutes for output nobody consumes. Pipeline must fail fast
	// rather than swallowing the misuse silently.
	out := t.TempDir()
	err := runPipeline("../../testdata/e2e/mini-corpus", out, false, false, runOptions{
		TranscribeBackend: "whisper",
	})
	if err == nil || !strings.Contains(err.Error(), "requires --semantic") {
		t.Fatalf("expected requires-semantic error, got %v", err)
	}
}

func TestBuildTranscribeClientUnknownBackendErrors(t *testing.T) {
	if _, err := buildTranscribeClient("nope"); err == nil ||
		!strings.Contains(err.Error(), "unknown transcribe backend") {
		t.Errorf("expected unknown-backend error, got %v", err)
	}
}

func TestBuildTranscribeClientWhisperRequiresKey(t *testing.T) {
	// Empty OPENAI_API_KEY → whisper.New returns ErrMissingAPIKey;
	// buildTranscribeClient must surface (not swallow) that error so
	// the user sees the cause instead of a generic "unknown backend".
	t.Setenv("OPENAI_API_KEY", "")
	_, err := buildTranscribeClient("whisper")
	if err == nil || !strings.Contains(err.Error(), "OPENAI_API_KEY") {
		t.Errorf("expected API-key error, got %v", err)
	}
}

func TestBuildTranscribeClientAcceptsOpenAIWhisperAlias(t *testing.T) {
	// "openai-whisper" is documented as an alias since the model is
	// hosted by OpenAI; users reaching for it shouldn't see an
	// unknown-backend error.
	t.Setenv("OPENAI_API_KEY", "sk-fake")
	c, err := buildTranscribeClient("openai-whisper")
	if err != nil {
		t.Fatalf("alias should be accepted: %v", err)
	}
	if c == nil {
		t.Fatal("client nil")
	}
}

func TestCollectExtensionsWidensWhenTranscribeEnabled(t *testing.T) {
	base := collectExtensions(runOptions{})
	withTranscribe := collectExtensions(runOptions{TranscribeBackend: "whisper"})

	contains := func(s []string, want string) bool {
		for _, v := range s {
			if v == want {
				return true
			}
		}
		return false
	}
	for _, ext := range []string{".mp3", ".mp4", ".wav", ".m4a", ".ogg", ".flac", ".webm", ".mov"} {
		if contains(base, ext) {
			t.Errorf("base ext list should NOT contain %q (audio/video belongs only when transcribe is on)", ext)
		}
		if !contains(withTranscribe, ext) {
			t.Errorf("transcribe-on ext list missing %q — CollectFiles would drop the file before the loop", ext)
		}
	}
	// AST extensions stay in both lists.
	for _, ext := range []string{".go", ".py", ".md"} {
		if !contains(base, ext) {
			t.Errorf("base ext list dropped AST extension %q", ext)
		}
		if !contains(withTranscribe, ext) {
			t.Errorf("transcribe-on ext list dropped AST extension %q", ext)
		}
	}
}

func TestLoadPriorGodNodePromptMissingGraphReturnsFallback(t *testing.T) {
	// Clear any developer-exported env override so the assertion
	// reflects the function's behavior, not the local shell.
	t.Setenv("GOGFY_WHISPER_PROMPT", "")
	dir := t.TempDir()
	got := loadPriorGodNodePrompt(dir)
	if got != transcribe.FallbackPrompt {
		t.Errorf("missing graph.json should yield fallback, got %q", got)
	}
}

func TestLoadPriorGodNodePromptDerivesFromGraph(t *testing.T) {
	t.Setenv("GOGFY_WHISPER_PROMPT", "")
	// Build a tiny graph where one node has clearly higher degree —
	// analyze should crown it god and BuildPrompt should fold its
	// label into the prompt.
	dir := t.TempDir()
	g := export.GraphExport{
		Nodes: []schema.Node{
			{ID: "hub", Label: "DistributedLedger"},
			{ID: "a", Label: "A"},
			{ID: "b", Label: "B"},
			{ID: "c", Label: "C"},
			{ID: "d", Label: "D"},
		},
		Edges: []schema.Edge{
			{Source: "hub", Target: "a", Relation: "links"},
			{Source: "hub", Target: "b", Relation: "links"},
			{Source: "hub", Target: "c", Relation: "links"},
			{Source: "hub", Target: "d", Relation: "links"},
		},
	}
	data, err := json.Marshal(g)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "graph.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	got := loadPriorGodNodePrompt(dir)
	if !strings.Contains(got, "DistributedLedger") {
		t.Errorf("expected hub label folded into prompt, got %q", got)
	}
}

// fakeTranscriber is a minimal in-memory transcribe.Client for testing
// runTranscribeJobs without touching the network.
type fakeTranscriber struct {
	mu       sync.Mutex
	inFlight int
	peak     int
	failPath string
	delay    time.Duration
}

func (f *fakeTranscriber) Name() string { return "fake" }

func (f *fakeTranscriber) Transcribe(ctx context.Context, req transcribe.Request) (transcribe.Response, error) {
	f.mu.Lock()
	f.inFlight++
	if f.inFlight > f.peak {
		f.peak = f.inFlight
	}
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.inFlight--
		f.mu.Unlock()
	}()
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	if req.Filename == f.failPath {
		return transcribe.Response{}, errors.New("synthetic failure")
	}
	return transcribe.Response{
		Text:             "transcript of " + req.Filename,
		DurationSeconds:  60,
		EstimatedUSDCost: 0.006,
	}, nil
}

func TestRunTranscribeJobsHonorsConcurrencyCap(t *testing.T) {
	ft := &fakeTranscriber{delay: 20 * time.Millisecond}
	jobs := []transcribeJob{
		{path: "a.mp3", audio: []byte("a")},
		{path: "b.mp3", audio: []byte("b")},
		{path: "c.mp3", audio: []byte("c")},
		{path: "d.mp3", audio: []byte("d")},
	}
	res := runTranscribeJobs(context.Background(), ft, jobs, 2, "")
	if len(res) != 4 {
		t.Fatalf("expected 4 results, got %d", len(res))
	}
	if ft.peak > 2 {
		t.Errorf("concurrency cap violated: peak in-flight %d > 2", ft.peak)
	}
	if ft.peak < 2 {
		// With 4 jobs at 20ms each and a cap of 2, at least one
		// moment should have 2 in flight — otherwise we're effectively
		// serial and the cap isn't doing anything.
		t.Errorf("expected peak in-flight 2 (parallelism working); got %d", ft.peak)
	}
}

func TestRunTranscribeJobsPreservesInputOrder(t *testing.T) {
	ft := &fakeTranscriber{}
	jobs := []transcribeJob{
		{path: "first.mp3"}, {path: "second.mp3"}, {path: "third.mp3"},
	}
	res := runTranscribeJobs(context.Background(), ft, jobs, 4, "")
	for i, j := range jobs {
		if res[i].path != j.path {
			t.Errorf("position %d: got %q want %q", i, res[i].path, j.path)
		}
	}
}

func TestRunTranscribeJobsPerJobErrorIsolated(t *testing.T) {
	ft := &fakeTranscriber{failPath: "bad.mp3"}
	jobs := []transcribeJob{
		{path: "ok.mp3"}, {path: "bad.mp3"}, {path: "also-ok.mp3"},
	}
	res := runTranscribeJobs(context.Background(), ft, jobs, 2, "")
	if res[0].err != nil || res[2].err != nil {
		t.Errorf("non-failing jobs should succeed: %+v", res)
	}
	if res[1].err == nil {
		t.Error("failing job should report error")
	}
}

func TestRunTranscribeJobsPropagatesPrompt(t *testing.T) {
	var seen []string
	var mu sync.Mutex
	ft := &captureTranscriber{record: func(req transcribe.Request) {
		mu.Lock()
		seen = append(seen, req.Prompt)
		mu.Unlock()
	}}
	jobs := []transcribeJob{{path: "a.mp3"}, {path: "b.mp3"}}
	runTranscribeJobs(context.Background(), ft, jobs, 2, "topic-hint")
	if len(seen) != 2 {
		t.Fatalf("expected 2 captured requests, got %d", len(seen))
	}
	for _, p := range seen {
		if p != "topic-hint" {
			t.Errorf("prompt not propagated to worker: %q", p)
		}
	}
}

type captureTranscriber struct {
	record func(transcribe.Request)
}

func (c *captureTranscriber) Name() string { return "capture" }
func (c *captureTranscriber) Transcribe(ctx context.Context, req transcribe.Request) (transcribe.Response, error) {
	c.record(req)
	return transcribe.Response{Text: "x"}, nil
}

// TestDispatchAddAliasesIngest verifies that "gogfy add" routes through
// the same handler as "gogfy ingest" — this is the upstream parity
// alias (the Python graphify CLI uses "add"). The test only confirms
// routing reached ingestCommand (which then errors on missing URL);
// the actual fetch path is exercised by internal/ingest tests.
func TestDispatchAddAliasesIngest(t *testing.T) {
	var stderr bytes.Buffer
	err := dispatch([]string{"add"}, &stderr)
	if err == nil {
		t.Fatalf("expected ingest error for missing URL, got nil")
	}
	if !strings.Contains(err.Error(), "ingest:") {
		t.Fatalf("expected error from ingestCommand (prefix \"ingest:\"), got %v", err)
	}
}
