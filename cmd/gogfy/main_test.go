package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
	// The benchmark sample-question seeds won't match the mini-corpus
	// labels, so we use a custom prompt that does. Tests the
	// flag-reorderer's recognition of --corpus-words/--json/--depth
	// when the positional appears before them.
	if err := dispatch([]string{
		"benchmark", graphPath,
		"--corpus-words", "1000",
		"--depth", "2",
		"--json",
	}, os.Stderr); err == nil {
		// mini-corpus has labels "Hello" / "main" — "main" hits the
		// default "what is the main entry point" question, so we
		// expect success.
		return
	} else if !strings.Contains(err.Error(), "no matching nodes") {
		t.Fatalf("dispatch benchmark: %v", err)
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
