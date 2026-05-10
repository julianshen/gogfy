package githook

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeRepo sets up a fake repo: a temp dir with a `.git/hooks` subdirectory.
func makeRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git", "hooks"), 0755); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestHookPath(t *testing.T) {
	got := HookPath("/some/repo")
	want := filepath.Join("/some/repo", ".git", "hooks", "post-commit")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestInstallRejectsNonRepoRoot(t *testing.T) {
	root := t.TempDir() // no .git directory
	if err := Install(root, Options{}); err == nil {
		t.Fatal("expected error when .git/hooks is missing")
	}
}

func TestInstallCreatesPostCommitWhenMissing(t *testing.T) {
	root := makeRepo(t)
	if err := Install(root, Options{}); err != nil {
		t.Fatal(err)
	}
	path := HookPath(root)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("hook not created: %v", err)
	}
	s := string(data)
	if !strings.HasPrefix(s, "#!") {
		t.Fatalf("hook missing shebang:\n%s", s)
	}
	if !strings.Contains(s, hookStartMarker) || !strings.Contains(s, hookEndMarker) {
		t.Fatalf("hook missing fenced markers:\n%s", s)
	}
	if !strings.Contains(s, "'gogfy' run --update") {
		t.Fatalf("hook missing run --update invocation:\n%s", s)
	}
	// Hook must be executable.
	info, _ := os.Stat(path)
	if info.Mode().Perm()&0111 == 0 {
		t.Fatalf("hook not executable: mode %v", info.Mode())
	}
}

func TestInstallAppendsToExistingPostCommit(t *testing.T) {
	root := makeRepo(t)
	path := HookPath(root)
	original := "#!/bin/sh\n# pre-existing hook from another tool\necho hello\n"
	if err := os.WriteFile(path, []byte(original), 0755); err != nil {
		t.Fatal(err)
	}
	if err := Install(root, Options{}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	s := string(data)
	if !strings.Contains(s, "# pre-existing hook from another tool") {
		t.Fatalf("existing content erased:\n%s", s)
	}
	if !strings.Contains(s, "echo hello") {
		t.Fatalf("existing command erased:\n%s", s)
	}
	if !strings.Contains(s, hookStartMarker) {
		t.Fatalf("gogfy block not appended:\n%s", s)
	}
}

func TestInstallReplacesExistingGogfyBlock(t *testing.T) {
	root := makeRepo(t)
	if err := Install(root, Options{}); err != nil {
		t.Fatal(err)
	}
	if err := Install(root, Options{Bin: "/opt/gogfy"}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(HookPath(root))
	s := string(data)
	if got := strings.Count(s, hookStartMarker); got != 1 {
		t.Fatalf("expected one fenced block after reinstall, got %d:\n%s", got, s)
	}
	if !strings.Contains(s, "'/opt/gogfy' run --update") {
		t.Fatalf("custom Bin not propagated:\n%s", s)
	}
}

func TestInstallNReinstallsAreFixedPoint(t *testing.T) {
	root := makeRepo(t)
	if err := Install(root, Options{}); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(HookPath(root))
	for i := 0; i < 5; i++ {
		if err := Install(root, Options{}); err != nil {
			t.Fatalf("reinstall #%d: %v", i, err)
		}
	}
	got, _ := os.ReadFile(HookPath(root))
	if !bytes.Equal(first, got) {
		t.Fatalf("5 reinstalls drifted from 1:\nfirst:  %q\nafter5: %q", first, got)
	}
}

func TestUninstallRemovesGogfyBlockOnly(t *testing.T) {
	root := makeRepo(t)
	path := HookPath(root)
	original := "#!/bin/sh\n# pre-existing hook\necho hello\n"
	if err := os.WriteFile(path, []byte(original), 0755); err != nil {
		t.Fatal(err)
	}
	if err := Install(root, Options{}); err != nil {
		t.Fatal(err)
	}
	if err := Uninstall(root); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	s := string(data)
	if strings.Contains(s, hookStartMarker) {
		t.Fatalf("gogfy block still present:\n%s", s)
	}
	if !strings.Contains(s, "# pre-existing hook") {
		t.Fatalf("pre-existing hook content erased:\n%s", s)
	}
}

func TestUninstallNoOpWhenHookMissing(t *testing.T) {
	root := makeRepo(t)
	if err := Uninstall(root); err != nil {
		t.Fatalf("expected no-op, got %v", err)
	}
}

func TestUninstallNoOpWhenNoGogfyBlock(t *testing.T) {
	root := makeRepo(t)
	path := HookPath(root)
	original := []byte("#!/bin/sh\necho unrelated\n")
	if err := os.WriteFile(path, original, 0755); err != nil {
		t.Fatal(err)
	}
	preInfo, _ := os.Stat(path)
	if err := Uninstall(root); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	postInfo, _ := os.Stat(path)
	if !bytes.Equal(got, original) {
		t.Fatalf("file modified when no gogfy block to remove:\nbefore: %s\nafter:  %s", original, got)
	}
	if !preInfo.ModTime().Equal(postInfo.ModTime()) {
		t.Fatalf("uninstall touched mtime when nothing to remove")
	}
}

func TestUninstallDeletesHookCreatedSolelyByGogfy(t *testing.T) {
	// When Install creates the hook (no prior content), Uninstall should
	// delete it rather than leave a shebang-only stub.
	root := makeRepo(t)
	if err := Install(root, Options{}); err != nil {
		t.Fatal(err)
	}
	if err := Uninstall(root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(HookPath(root)); !os.IsNotExist(err) {
		t.Fatalf("expected hook to be removed, stat err=%v", err)
	}
}

func TestInstallRefusesMismatchedMarkers(t *testing.T) {
	// Same silent-data-loss class as the snippet writer: reject mismatched
	// marker pairs rather than guessing where the block ends.
	root := makeRepo(t)
	path := HookPath(root)
	hostile := []byte("#!/bin/sh\n" +
		hookStartMarker + "\nUSER WROTE THIS\n\n" +
		hookStartMarker + "\nold gogfy\n" + hookEndMarker + "\n")
	if err := os.WriteFile(path, hostile, 0755); err != nil {
		t.Fatal(err)
	}
	if err := Install(root, Options{}); err == nil {
		t.Fatal("expected error on duplicate start markers")
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, hostile) {
		t.Fatal("file modified despite returned error")
	}
}

func TestInstallHonorsCustomOutDir(t *testing.T) {
	root := makeRepo(t)
	if err := Install(root, Options{OutDir: "custom-out"}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(HookPath(root))
	if !strings.Contains(string(data), "--out 'custom-out'") {
		t.Fatalf("custom OutDir not propagated:\n%s", data)
	}
}

func TestUninstallRefusesMismatchedMarkers(t *testing.T) {
	root := makeRepo(t)
	path := HookPath(root)
	hostile := []byte("#!/bin/sh\n" + hookStartMarker + "\nfoo\n" + hookStartMarker + "\nbar\n" + hookEndMarker + "\n")
	if err := os.WriteFile(path, hostile, 0755); err != nil {
		t.Fatal(err)
	}
	if err := Uninstall(root); err == nil {
		t.Fatal("expected error on duplicate start markers")
	}
}

func TestUninstallPreservesHookWithRealContent(t *testing.T) {
	// When stripping the gogfy block leaves more than just a shebang, the
	// hook file must stay (with executable bit re-asserted).
	root := makeRepo(t)
	path := HookPath(root)
	original := "#!/bin/sh\necho hello\n"
	if err := os.WriteFile(path, []byte(original), 0755); err != nil {
		t.Fatal(err)
	}
	if err := Install(root, Options{}); err != nil {
		t.Fatal(err)
	}
	if err := Uninstall(root); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("hook unexpectedly removed: %v", err)
	}
	if !strings.Contains(string(data), "echo hello") {
		t.Fatalf("real content erased:\n%s", data)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm()&0111 == 0 {
		t.Fatalf("executable bit lost after uninstall: %v", info.Mode())
	}
}

func TestInstallReassertsExecutableBitOnReinstall(t *testing.T) {
	// If the user (or another tool) chmod-cleared the executable bit,
	// reinstall — even when content is unchanged — must restore it.
	root := makeRepo(t)
	if err := Install(root, Options{}); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(HookPath(root), 0644); err != nil {
		t.Fatal(err)
	}
	if err := Install(root, Options{}); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(HookPath(root))
	if info.Mode().Perm()&0111 == 0 {
		t.Fatalf("executable bit not reasserted: %v", info.Mode())
	}
}

func TestHookContentIsEmptyShebangOnly(t *testing.T) {
	if !hookContentIsEmpty([]byte("#!/bin/sh\n")) {
		t.Fatal("shebang-only should be empty")
	}
	if !hookContentIsEmpty([]byte("#!/bin/sh")) {
		t.Fatal("shebang without newline should be empty")
	}
	if hookContentIsEmpty([]byte("#!/bin/sh\necho hi\n")) {
		t.Fatal("shebang + content should NOT be empty")
	}
	if hookContentIsEmpty([]byte("# just a comment\n")) {
		t.Fatal("non-shebang non-empty content should NOT be empty")
	}
}

func TestInstallFailsOnUnreadableHook(t *testing.T) {
	root := makeRepo(t)
	path := HookPath(root)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0); err != nil {
		t.Skipf("chmod 0 not honored: %v", err)
	}
	defer os.Chmod(path, 0755)
	// Some environments (root user, certain CI sandboxes) can read
	// chmod-0 files anyway. Probe before asserting so the test stays
	// deterministic instead of flaking on those hosts.
	if _, err := os.ReadFile(path); err == nil {
		t.Skip("chmod 0 doesn't block reads on this filesystem; skipping unreadable-hook assertion")
	}
	if err := Install(root, Options{}); err == nil {
		t.Fatal("expected read error on unreadable hook")
	}
}

// TestInstallRedirectsLogToGitDir — the hook must capture stderr/stdout
// to a log file inside the git directory rather than discarding them, so
// a consistently-broken rebuild leaves a discoverable trail.
func TestInstallRedirectsLogToGitDir(t *testing.T) {
	root := makeRepo(t)
	if err := Install(root, Options{}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(HookPath(root))
	s := string(data)
	// The gogfy invocation must capture both streams to the log file, not
	// discard them. (`git rev-parse 2>/dev/null` is fine — that's just
	// silencing the path-lookup probe.)
	if strings.Contains(s, "run --update --out") && strings.Contains(s, ">/dev/null 2>&1") {
		t.Fatalf("gogfy invocation still pipes to /dev/null:\n%s", s)
	}
	if !strings.Contains(s, "gogfy-rebuild.log") {
		t.Fatalf("hook missing log file path:\n%s", s)
	}
	if !strings.Contains(s, ">>\"$GOGFY_LOG\" 2>&1") {
		t.Fatalf("hook missing log redirection:\n%s", s)
	}
}

// TestHookPathResolvesGitFile — submodule and worktree repos have `.git`
// as a file containing `gitdir: <path>`. HookPath must follow the
// indirection rather than assuming a directory.
func TestHookPathResolvesGitFile(t *testing.T) {
	root := t.TempDir()
	realGitDir := filepath.Join(root, ".git-real")
	if err := os.MkdirAll(filepath.Join(realGitDir, "hooks"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git"), []byte("gitdir: ./.git-real\n"), 0644); err != nil {
		t.Fatal(err)
	}
	got := HookPath(root)
	want := filepath.Join(realGitDir, "hooks", "post-commit")
	if got != want {
		t.Fatalf("HookPath did not follow gitdir: got %q, want %q", got, want)
	}
}

// TestInstallSucceedsInWorktreeShape — install should work end-to-end
// when `.git` is a file pointing into a sibling git directory.
func TestInstallSucceedsInWorktreeShape(t *testing.T) {
	root := t.TempDir()
	realGitDir := filepath.Join(root, ".git-worktrees", "feature")
	if err := os.MkdirAll(filepath.Join(realGitDir, "hooks"), 0755); err != nil {
		t.Fatal(err)
	}
	gitdirLine := []byte("gitdir: " + realGitDir + "\n")
	if err := os.WriteFile(filepath.Join(root, ".git"), gitdirLine, 0644); err != nil {
		t.Fatal(err)
	}
	if err := Install(root, Options{}); err != nil {
		t.Fatalf("install in worktree shape: %v", err)
	}
	if _, err := os.Stat(filepath.Join(realGitDir, "hooks", "post-commit")); err != nil {
		t.Fatalf("hook not created in resolved hooks dir: %v", err)
	}
}

// TestInstallRefusesBareRepo — bare repos have HEAD + objects at the
// root and no working tree, so `gogfy run --update .` would fail.
func TestInstallRefusesBareRepo(t *testing.T) {
	root := t.TempDir()
	// Synthesize a bare-repo shape.
	for _, dir := range []string{"hooks", "objects", "refs"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "HEAD"), []byte("ref: refs/heads/main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// Also put a `.git` entry so the requireWorkingTreeRepo gate sees a repo.
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := Install(root, Options{}); err == nil {
		t.Fatal("expected error on bare-repo shape")
	}
}

// TestInstallShellQuotesBinAndOutDir — paths with spaces or shell
// metacharacters must be safely quoted in the rendered hook.
func TestInstallShellQuotesBinAndOutDir(t *testing.T) {
	root := makeRepo(t)
	if err := Install(root, Options{Bin: "/Users/Test User/bin/gogfy", OutDir: "weird; rm -rf /"}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(HookPath(root))
	s := string(data)
	// Bin path with a space must appear inside single quotes.
	if !strings.Contains(s, "'/Users/Test User/bin/gogfy'") {
		t.Fatalf("bin path not single-quoted:\n%s", s)
	}
	// OutDir with shell metacharacters must be neutralized.
	if !strings.Contains(s, "'weird; rm -rf /'") {
		t.Fatalf("outDir not single-quoted:\n%s", s)
	}
	// Sanity: the metacharacter must NOT appear unquoted as a real
	// command separator (i.e., a `; rm` outside any single-quoted span).
	if strings.Contains(s, "out weird; rm") {
		t.Fatalf("shell injection survived quoting:\n%s", s)
	}
}

// TestHookPathResolvesWorktreeCommonDir — linked worktrees execute hooks
// from the main repo's hooks directory, identified by the `commondir`
// file inside the worktree's gitdir. Writing to <gitdir>/hooks would
// install a hook Git never runs.
func TestHookPathResolvesWorktreeCommonDir(t *testing.T) {
	main := t.TempDir()
	mainGit := filepath.Join(main, ".git")
	if err := os.MkdirAll(filepath.Join(mainGit, "hooks"), 0755); err != nil {
		t.Fatal(err)
	}
	worktree := t.TempDir()
	wtGit := filepath.Join(mainGit, "worktrees", "feature")
	if err := os.MkdirAll(wtGit, 0755); err != nil {
		t.Fatal(err)
	}
	// `.git` file in the worktree points at the per-worktree gitdir.
	if err := os.WriteFile(filepath.Join(worktree, ".git"), []byte("gitdir: "+wtGit+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// `commondir` inside the worktree gitdir points at the main `.git`.
	if err := os.WriteFile(filepath.Join(wtGit, "commondir"), []byte("../..\n"), 0644); err != nil {
		t.Fatal(err)
	}
	got := HookPath(worktree)
	want := filepath.Join(mainGit, "hooks", "post-commit")
	if got != want {
		t.Fatalf("worktree HookPath: got %q, want %q (must point at main repo's hooks dir)", got, want)
	}
}

// TestHookPathRespectsCoreHooksPath — repos using Husky or centralized
// hooks set core.hooksPath. Writing to .git/hooks in that case installs
// to a path Git ignores. HookPath must read .git/config and follow.
func TestHookPathRespectsCoreHooksPath(t *testing.T) {
	root := makeRepo(t)
	if err := os.MkdirAll(filepath.Join(root, ".husky"), 0755); err != nil {
		t.Fatal(err)
	}
	cfg := "[core]\n\thooksPath = .husky\n"
	if err := os.WriteFile(filepath.Join(root, ".git", "config"), []byte(cfg), 0644); err != nil {
		t.Fatal(err)
	}
	got := HookPath(root)
	want := filepath.Join(root, ".husky", "post-commit")
	if got != want {
		t.Fatalf("HookPath did not honor core.hooksPath: got %q, want %q", got, want)
	}
}

func TestHookPathIgnoresCoreHooksPathSubsections(t *testing.T) {
	// Subsections on `core` (e.g. `[core "x"]`) are a different config
	// scope and must not be treated as the unscoped `[core]` block.
	root := makeRepo(t)
	cfg := "[core \"sub\"]\n\thooksPath = .ignored\n"
	if err := os.WriteFile(filepath.Join(root, ".git", "config"), []byte(cfg), 0644); err != nil {
		t.Fatal(err)
	}
	got := HookPath(root)
	want := filepath.Join(root, ".git", "hooks", "post-commit")
	if got != want {
		t.Fatalf("HookPath wrongly honored subsection hookspath: got %q, want %q", got, want)
	}
}

func TestInstallHonorsCoreHooksPathEndToEnd(t *testing.T) {
	root := makeRepo(t)
	if err := os.MkdirAll(filepath.Join(root, ".githooks"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "config"), []byte("[core]\nhookspath = .githooks\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := Install(root, Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".githooks", "post-commit")); err != nil {
		t.Fatalf("hook not written to core.hooksPath dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".git", "hooks", "post-commit")); !os.IsNotExist(err) {
		t.Fatal("hook should not have been written to .git/hooks when core.hooksPath is set")
	}
}

func TestInstallWritesBothPostCommitAndPostCheckout(t *testing.T) {
	root := makeRepo(t)
	if err := Install(root, Options{}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"post-commit", "post-checkout"} {
		path := HookPathFor(root, name)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("%s not installed: %v", name, err)
		}
	}
}

func TestPostCheckoutHookGuardsOnBranchFlag(t *testing.T) {
	// post-checkout receives ($1=prev_HEAD $2=new_HEAD $3=branch_flag).
	// Single-file checkouts pass branch_flag=0 — those must not trigger
	// a rebuild. Only branch_flag=1 (real branch checkout) should run gogfy.
	root := makeRepo(t)
	if err := Install(root, Options{}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(HookPathFor(root, "post-checkout"))
	s := string(data)
	if !strings.Contains(s, `"${3-0}" != "1"`) {
		t.Fatalf("post-checkout missing branch_flag guard:\n%s", s)
	}
	// post-commit must NOT have the guard (it always runs).
	commit, _ := os.ReadFile(HookPathFor(root, "post-commit"))
	if strings.Contains(string(commit), `"${3-0}" != "1"`) {
		t.Fatalf("post-commit must not have branch_flag guard:\n%s", commit)
	}
}

func TestUninstallRemovesBothHooks(t *testing.T) {
	root := makeRepo(t)
	if err := Install(root, Options{}); err != nil {
		t.Fatal(err)
	}
	if err := Uninstall(root); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"post-commit", "post-checkout"} {
		if _, err := os.Stat(HookPathFor(root, name)); !os.IsNotExist(err) {
			t.Fatalf("%s should be removed (gogfy was sole content), err=%v", name, err)
		}
	}
}

func TestStatusReportsBothHooks(t *testing.T) {
	root := makeRepo(t)
	statuses := Status(root)
	if len(statuses) != 2 {
		t.Fatalf("expected 2 statuses, got %d: %+v", len(statuses), statuses)
	}
	for _, st := range statuses {
		if st.Installed {
			t.Fatalf("expected uninstalled before Install: %+v", st)
		}
	}
	if err := Install(root, Options{}); err != nil {
		t.Fatal(err)
	}
	statuses = Status(root)
	for _, st := range statuses {
		if !st.Installed {
			t.Fatalf("expected installed after Install: %+v", st)
		}
	}
}

func TestStatusIgnoresUnrelatedHookContent(t *testing.T) {
	// A user's hand-written post-commit (with no gogfy block) should
	// report Installed=false rather than reusing the file's existence
	// as the install flag.
	root := makeRepo(t)
	if err := os.WriteFile(HookPathFor(root, "post-commit"), []byte("#!/bin/sh\necho hi\n"), 0755); err != nil {
		t.Fatal(err)
	}
	for _, st := range Status(root) {
		if st.Name == "post-commit" && st.Installed {
			t.Fatalf("user-written post-commit (no gogfy block) reported as installed")
		}
	}
}

func TestInstallSkipsRewriteWhenContentUnchanged(t *testing.T) {
	root := makeRepo(t)
	if err := Install(root, Options{}); err != nil {
		t.Fatal(err)
	}
	preInfo, _ := os.Stat(HookPath(root))
	if err := Install(root, Options{}); err != nil {
		t.Fatal(err)
	}
	postInfo, _ := os.Stat(HookPath(root))
	if !preInfo.ModTime().Equal(postInfo.ModTime()) {
		t.Fatal("idempotent reinstall touched mtime")
	}
}
