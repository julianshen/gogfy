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
	if !strings.Contains(s, "gogfy run --update") {
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
	if !strings.Contains(s, "/opt/gogfy run --update") {
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
	if !strings.Contains(string(data), "--out custom-out") {
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
	if err := Install(root, Options{}); err == nil {
		t.Fatal("expected read error on unreadable hook")
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
