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
	if !bytes.Contains(data, []byte("gogfy run --update")) {
		t.Fatalf("hook missing run --update:\n%s", data)
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

func TestDispatchHookHonorsCustomFlags(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git", "hooks"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := dispatch([]string{"hook", "install", "--repo", repo, "--gogfy-bin", "/opt/bin/gogfy", "--out", "custom"}, os.Stderr); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(repo, ".git", "hooks", "post-commit"))
	if !bytes.Contains(data, []byte("/opt/bin/gogfy run --update --out custom")) {
		t.Fatalf("custom flags not propagated:\n%s", data)
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
