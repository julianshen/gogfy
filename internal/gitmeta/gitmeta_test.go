package gitmeta

import (
	"os"
	"path/filepath"
	"testing"
)

const dummySHA = "abc123def456789abc123def456789abc123def4"

func TestHeadShortSHASymbolicRef(t *testing.T) {
	// Standard repo: HEAD → refs/heads/main → loose ref file with SHA.
	root := t.TempDir()
	mkRefRepo(t, root, "ref: refs/heads/main\n", "refs/heads/main", dummySHA+"\n", "")
	got, ok := HeadShortSHA(root)
	if !ok {
		t.Fatal("expected SHA, got !ok")
	}
	if got != dummySHA[:7] {
		t.Errorf("got %q, want %q (first 7 chars of SHA)", got, dummySHA[:7])
	}
}

func TestHeadShortSHADetachedHEAD(t *testing.T) {
	// Detached: HEAD itself holds the SHA.
	root := t.TempDir()
	mkRefRepo(t, root, dummySHA+"\n", "", "", "")
	got, ok := HeadShortSHA(root)
	if !ok {
		t.Fatal("detached HEAD should still resolve")
	}
	if got != dummySHA[:7] {
		t.Errorf("got %q, want %q", got, dummySHA[:7])
	}
}

func TestHeadShortSHAPackedRefsFallback(t *testing.T) {
	// HEAD → branch, but loose ref missing — must check packed-refs.
	root := t.TempDir()
	packed := dummySHA + " refs/heads/main\n"
	mkRefRepo(t, root, "ref: refs/heads/main\n", "", "", packed)
	got, ok := HeadShortSHA(root)
	if !ok {
		t.Fatal("packed-refs fallback should resolve")
	}
	if got != dummySHA[:7] {
		t.Errorf("got %q, want %q", got, dummySHA[:7])
	}
}

func TestHeadShortSHAWalksUpToFindGitDir(t *testing.T) {
	// .git lives at root; query a deeply nested subdir.
	root := t.TempDir()
	mkRefRepo(t, root, "ref: refs/heads/main\n", "refs/heads/main", dummySHA+"\n", "")
	nested := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	got, ok := HeadShortSHA(nested)
	if !ok {
		t.Fatal("findGitDir should walk up to root")
	}
	if got != dummySHA[:7] {
		t.Errorf("got %q, want %q", got, dummySHA[:7])
	}
}

func TestHeadShortSHANoGitReturnsFalse(t *testing.T) {
	if _, ok := HeadShortSHA(t.TempDir()); ok {
		t.Fatal("non-repo should return !ok")
	}
}

func TestHeadShortSHAWorktreeGitFile(t *testing.T) {
	// `.git` is a file (worktree pointer): `gitdir: <real .git path>`.
	realDir := t.TempDir()
	mkRefRepo(t, realDir, "ref: refs/heads/main\n", "refs/heads/main", dummySHA+"\n", "")
	worktree := t.TempDir()
	if err := os.WriteFile(filepath.Join(worktree, ".git"), []byte("gitdir: "+filepath.Join(realDir, ".git")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, ok := HeadShortSHA(worktree)
	if !ok {
		t.Fatal("worktree `.git` file should resolve to the pointed-to dir")
	}
	if got != dummySHA[:7] {
		t.Errorf("got %q, want %q", got, dummySHA[:7])
	}
}

// mkRefRepo creates a minimal `.git/` skeleton under root.
//   - headContent: bytes for .git/HEAD
//   - refPath: optional ref path under .git/ (empty → skip)
//   - refContent: bytes for that ref file
//   - packed: optional packed-refs content (empty → skip)
func mkRefRepo(t *testing.T, root, headContent, refPath, refContent, packed string) {
	t.Helper()
	gitDir := filepath.Join(root, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte(headContent), 0o644); err != nil {
		t.Fatal(err)
	}
	if refPath != "" {
		full := filepath.Join(gitDir, refPath)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(refContent), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if packed != "" {
		if err := os.WriteFile(filepath.Join(gitDir, "packed-refs"), []byte(packed), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
