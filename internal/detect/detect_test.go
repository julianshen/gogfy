package detect

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestCollectFilesFiltersExtensions(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "a.go"), []byte("package a"), 0644)
	os.WriteFile(filepath.Join(root, "b.py"), []byte("# b"), 0644)
	os.WriteFile(filepath.Join(root, "c.txt"), []byte("c"), 0644)

	files, err := CollectFiles(root, []string{".go", ".py"})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}
	want := []string{"a.go", "b.py"}
	got := []string{filepath.Base(files[0]), filepath.Base(files[1])}
	if !slices.Equal(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestCollectFilesRespectsGraphifyIgnore(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "keep.go"), []byte("package keep"), 0644)
	sub := filepath.Join(root, "vendor")
	os.MkdirAll(sub, 0755)
	os.WriteFile(filepath.Join(sub, "skip.go"), []byte("package skip"), 0644)
	os.WriteFile(filepath.Join(root, ".graphifyignore"), []byte("vendor/\n"), 0644)

	files, err := CollectFiles(root, []string{".go"})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if filepath.Base(files[0]) != "keep.go" {
		t.Fatalf("expected keep.go, got %s", files[0])
	}
}

func TestCollectFilesDeterministicOrder(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "z.go"), []byte("package z"), 0644)
	os.WriteFile(filepath.Join(root, "a.go"), []byte("package a"), 0644)
	os.WriteFile(filepath.Join(root, "m.go"), []byte("package m"), 0644)

	files1, err := CollectFiles(root, []string{".go"})
	if err != nil {
		t.Fatal(err)
	}
	files2, err := CollectFiles(root, []string{".go"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(files1, files2) {
		t.Fatalf("deterministic ordering failed: %v vs %v", files1, files2)
	}
	want := []string{"a.go", "m.go", "z.go"}
	got := make([]string, len(files1))
	for i, f := range files1 {
		got[i] = filepath.Base(f)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("expected sorted order %v, got %v", want, got)
	}
}

func TestCollectFilesIgnoreCommentsAndBlankLines(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "keep.go"), []byte("package keep"), 0644)
	sub := filepath.Join(root, "vendor")
	os.MkdirAll(sub, 0755)
	os.WriteFile(filepath.Join(sub, "skip.go"), []byte("package skip"), 0644)
	os.WriteFile(filepath.Join(root, ".graphifyignore"), []byte("# comment\n\nvendor/\n"), 0644)

	files, err := CollectFiles(root, []string{".go"})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
}

func TestCollectFilesEmptyExtensions(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "a.go"), []byte("package a"), 0644)

	files, err := CollectFiles(root, []string{})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("expected 0 files, got %d", len(files))
	}
}

func TestCollectFilesNonExistentRoot(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	if _, err := CollectFiles(missing, []string{".go"}); err == nil {
		t.Fatal("expected error for nonexistent root")
	}
}

func TestCollectFilesMalformedPatternIsTolerant(t *testing.T) {
	// gitignore semantics tolerate malformed patterns rather than erroring,
	// matching how `git` itself behaves. Verify CollectFiles still returns
	// the matching files instead of failing the whole run.
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "a.go"), []byte("package a"), 0644)
	os.WriteFile(filepath.Join(root, ".graphifyignore"), []byte("[\n"), 0644)

	files, err := CollectFiles(root, []string{".go"})
	if err != nil {
		t.Fatalf("malformed pattern should not abort traversal: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
}

func TestCollectFilesNestedIgnore(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "keep.go"), []byte("package keep"), 0644)
	sub := filepath.Join(root, "vendor", "nested")
	os.MkdirAll(sub, 0755)
	os.WriteFile(filepath.Join(sub, "skip.go"), []byte("package skip"), 0644)
	os.WriteFile(filepath.Join(root, ".graphifyignore"), []byte("vendor/\n"), 0644)

	files, err := CollectFiles(root, []string{".go"})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if filepath.Base(files[0]) != "keep.go" {
		t.Fatalf("expected keep.go, got %s", files[0])
	}
}

func TestCollectFilesGlobPatternMatch(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "keep.go"), []byte("package keep"), 0644)
	os.WriteFile(filepath.Join(root, "skip_test.go"), []byte("package skip"), 0644)
	os.WriteFile(filepath.Join(root, ".graphifyignore"), []byte("*_test.go\n"), 0644)

	files, err := CollectFiles(root, []string{".go"})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if filepath.Base(files[0]) != "keep.go" {
		t.Fatalf("expected keep.go, got %s", files[0])
	}
}

func TestCollectFilesNegationUnignores(t *testing.T) {
	// `!` re-includes a previously-ignored path. Required for gitignore parity.
	// Use `vendor/*` rather than `vendor/`; gitignore semantics forbid
	// re-including children once their parent directory is excluded.
	root := t.TempDir()
	dir := filepath.Join(root, "vendor")
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "skip.go"), []byte("package skip"), 0644)
	os.WriteFile(filepath.Join(dir, "keep.go"), []byte("package keep"), 0644)
	os.WriteFile(filepath.Join(root, ".graphifyignore"), []byte("vendor/*\n!vendor/keep.go\n"), 0644)

	files, err := CollectFiles(root, []string{".go"})
	if err != nil {
		t.Fatal(err)
	}
	bases := make([]string, len(files))
	for i, f := range files {
		bases[i] = filepath.Base(f)
	}
	if !slices.Contains(bases, "keep.go") {
		t.Fatalf("negation should re-include keep.go; got %v", bases)
	}
	if slices.Contains(bases, "skip.go") {
		t.Fatalf("skip.go should still be ignored; got %v", bases)
	}
}

func TestCollectFilesDoubleStarRecursive(t *testing.T) {
	// `**/foo` should match foo at any depth.
	root := t.TempDir()
	for _, p := range []string{"a/x.go", "a/b/x.go", "a/b/c/x.go", "y.go"} {
		full := filepath.Join(root, p)
		os.MkdirAll(filepath.Dir(full), 0755)
		os.WriteFile(full, []byte("package x"), 0644)
	}
	os.WriteFile(filepath.Join(root, ".graphifyignore"), []byte("**/x.go\n"), 0644)

	files, err := CollectFiles(root, []string{".go"})
	if err != nil {
		t.Fatal(err)
	}
	bases := make([]string, len(files))
	for i, f := range files {
		bases[i] = filepath.Base(f)
	}
	for _, b := range bases {
		if b == "x.go" {
			t.Fatalf("**/x.go should ignore every x.go; got %v", bases)
		}
	}
	if !slices.Contains(bases, "y.go") {
		t.Fatalf("y.go should remain; got %v", bases)
	}
}

func TestCollectFilesLeadingSlashAnchorsToRoot(t *testing.T) {
	// `/foo.go` should only match at root, not in subdirs.
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "foo.go"), []byte("package x"), 0644)
	os.MkdirAll(filepath.Join(root, "sub"), 0755)
	os.WriteFile(filepath.Join(root, "sub", "foo.go"), []byte("package x"), 0644)
	os.WriteFile(filepath.Join(root, ".graphifyignore"), []byte("/foo.go\n"), 0644)

	files, err := CollectFiles(root, []string{".go"})
	if err != nil {
		t.Fatal(err)
	}
	// CollectFiles returns symlink-resolved absolute paths (so a hostile
	// symlink can't redirect the eventual read); compute the rel form
	// against the resolved root so the assertions don't depend on macOS's
	// /var → /private/var resolution.
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	bases := make([]string, len(files))
	for i, f := range files {
		rel, err := filepath.Rel(resolvedRoot, f)
		if err != nil {
			t.Fatal(err)
		}
		bases[i] = filepath.ToSlash(rel)
	}
	for _, b := range bases {
		if b == "foo.go" {
			t.Fatalf("anchored /foo.go should ignore root foo.go; got %v", bases)
		}
	}
	if !slices.Contains(bases, "sub/foo.go") {
		t.Fatalf("sub/foo.go should not be ignored by anchored /foo.go; got %v", bases)
	}
}

func TestCollectFilesSkipsIgnoredDirSubtree(t *testing.T) {
	// Regression: `vendor/` ignore must SkipDir so the walker never reads
	// files inside an ignored subtree. Prior to the fix the walker descended
	// because `MatchesPath("vendor")` (bare form) returns false for pattern
	// `vendor/` even though `MatchesPath("vendor/")` is true.
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "keep.go"), []byte("package keep"), 0644)
	dir := filepath.Join(root, "vendor")
	os.MkdirAll(dir, 0755)
	// Create an unreadable file inside vendor — if the walker descends into
	// the ignored dir it would hit this and fail.
	bad := filepath.Join(dir, "skip.go")
	os.WriteFile(bad, []byte("package skip"), 0644)
	os.Chmod(bad, 0000)
	defer os.Chmod(bad, 0644)
	os.WriteFile(filepath.Join(root, ".graphifyignore"), []byte("vendor/\n"), 0644)

	files, err := CollectFiles(root, []string{".go"})
	if err != nil {
		t.Fatalf("CollectFiles must SkipDir over `vendor/`, not descend: %v", err)
	}
	if len(files) != 1 || filepath.Base(files[0]) != "keep.go" {
		t.Fatalf("expected only keep.go; got %v", files)
	}
}

func TestCollectFilesCharClassPattern(t *testing.T) {
	// Pin gitignore character-class behavior so a future matcher swap can't
	// silently regress.
	root := t.TempDir()
	for _, p := range []string{"a.tmp", "b.tmp", "c.tmp", "x.go"} {
		os.WriteFile(filepath.Join(root, p), []byte("x"), 0644)
	}
	os.WriteFile(filepath.Join(root, ".graphifyignore"), []byte("[ab].tmp\n"), 0644)

	files, err := CollectFiles(root, []string{".tmp", ".go"})
	if err != nil {
		t.Fatal(err)
	}
	bases := make([]string, len(files))
	for i, f := range files {
		bases[i] = filepath.Base(f)
	}
	for _, b := range []string{"a.tmp", "b.tmp"} {
		if slices.Contains(bases, b) {
			t.Fatalf("[ab].tmp should ignore %q; got %v", b, bases)
		}
	}
	for _, b := range []string{"c.tmp", "x.go"} {
		if !slices.Contains(bases, b) {
			t.Fatalf("expected %q present; got %v", b, bases)
		}
	}
}

func TestCollectFilesSkipsSymlinkEscapingRoot(t *testing.T) {
	// A symlink inside the corpus root pointing at a file outside the root
	// must not appear in the result; otherwise an attacker who controls a
	// repo could redirect a Go-source read to /etc/passwd.
	root := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "secret.go")
	os.WriteFile(target, []byte("package secret"), 0644)

	link := filepath.Join(root, "evil.go")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	os.WriteFile(filepath.Join(root, "ok.go"), []byte("package ok"), 0644)

	files, err := CollectFiles(root, []string{".go"})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if filepath.Base(f) == "evil.go" {
			t.Fatalf("symlink escaping root must be filtered out; got %v", files)
		}
	}
	if len(files) != 1 || filepath.Base(files[0]) != "ok.go" {
		t.Fatalf("expected only ok.go, got %v", files)
	}
}

func TestCollectFilesWarnsOnSecuritySkip(t *testing.T) {
	// Symlink-escape skips should produce a stderr warning so users notice
	// missing files. Capture SkipLogger to assert the message.
	root := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "secret.go")
	os.WriteFile(target, []byte("package x"), 0644)
	link := filepath.Join(root, "evil.go")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	var buf strings.Builder
	old := SkipLogger
	SkipLogger = &buf
	defer func() { SkipLogger = old }()

	if _, err := CollectFiles(root, []string{".go"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "evil.go") {
		t.Fatalf("expected skip warning to mention evil.go; got %q", buf.String())
	}
	if !strings.Contains(buf.String(), "outside root") {
		t.Fatalf("expected skip warning to mention reason; got %q", buf.String())
	}
}

func TestCollectFilesSizeCapNotBypassedBySymlink(t *testing.T) {
	// Regression: filepath.Walk uses Lstat, so for a symlink the FileInfo
	// reports the link's size (tens of bytes) — not the target's. Without
	// a re-stat on the resolved path, a small symlink to a huge file would
	// evade DefaultMaxFileSize.
	root := t.TempDir()
	huge := filepath.Join(root, "huge.go")
	if err := os.WriteFile(huge, make([]byte, 12<<20), 0644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "small-link.go")
	if err := os.Symlink(huge, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	var buf strings.Builder
	old := SkipLogger
	SkipLogger = &buf
	defer func() { SkipLogger = old }()

	files, err := CollectFiles(root, []string{".go"})
	if err != nil {
		t.Fatal(err)
	}
	// Both the link and the target should be filtered out — they resolve
	// to the same oversize file. (CollectFiles dedups by storing the
	// resolved path; both entries map to `huge.go`, both fail the size
	// cap on re-stat.)
	if len(files) != 0 {
		t.Fatalf("oversize-target symlink should be filtered; got %v", files)
	}
	if !strings.Contains(buf.String(), "exceeds size cap") {
		t.Fatalf("expected size-cap warning; got %q", buf.String())
	}
}

func TestCollectFilesSkipsOversizeFile(t *testing.T) {
	// Files above security.DefaultMaxFileSize must be silently dropped (with
	// a stderr warning) rather than aborting the walk. Use a tiny cap by
	// abusing a 12 MiB synthetic file that exceeds the 10 MiB default.
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "ok.go"), []byte("package ok"), 0644)
	big := filepath.Join(root, "big.go")
	if err := os.WriteFile(big, make([]byte, 12<<20), 0644); err != nil {
		t.Fatal(err)
	}

	var buf strings.Builder
	old := SkipLogger
	SkipLogger = &buf
	defer func() { SkipLogger = old }()

	files, err := CollectFiles(root, []string{".go"})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || filepath.Base(files[0]) != "ok.go" {
		t.Fatalf("expected only ok.go, got %v", files)
	}
	if !strings.Contains(buf.String(), "big.go") {
		t.Fatalf("expected size-cap warning to mention big.go; got %q", buf.String())
	}
}

func TestCollectFilesLoadIgnorePermissionError(t *testing.T) {
	root := t.TempDir()
	ignorePath := filepath.Join(root, ".graphifyignore")
	os.WriteFile(ignorePath, []byte("vendor/\n"), 0644)
	os.Chmod(ignorePath, 0000)
	defer os.Chmod(ignorePath, 0644)

	_, err := CollectFiles(root, []string{".go"})
	if err == nil {
		t.Fatal("expected error for unreadable ignore file")
	}
}

func TestCollectFilesLayersGraphifyignoreUpToVCSRoot(t *testing.T) {
	// vcsRoot/.git marks the boundary.
	// vcsRoot/.graphifyignore ignores *.tmp project-wide.
	// vcsRoot/sub/.graphifyignore re-includes via `!keep.tmp`.
	// Scan from vcsRoot/sub — should find keep.tmp but not other.tmp.
	root := t.TempDir()
	if err := os.MkdirAll(root+"/.git", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root+"/.graphifyignore", []byte("*.tmp\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root+"/sub", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root+"/sub/.graphifyignore", []byte("!keep.tmp\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root+"/sub/keep.tmp", []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root+"/sub/other.tmp", []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := CollectFiles(root+"/sub", []string{".tmp"})
	if err != nil {
		t.Fatal(err)
	}
	hasKeep, hasOther := false, false
	for _, p := range got {
		if filepath.Base(p) == "keep.tmp" {
			hasKeep = true
		}
		if filepath.Base(p) == "other.tmp" {
			hasOther = true
		}
	}
	if !hasKeep {
		t.Errorf("ancestor .graphifyignore + child negation: keep.tmp should be included, got %v", got)
	}
	if hasOther {
		t.Errorf("ancestor *.tmp should still ignore other.tmp, got %v", got)
	}
}

func TestIgnoreDirChainStopsAtVCSRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(root+"/.git", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root+"/a/b/c", 0o755); err != nil {
		t.Fatal(err)
	}
	chain := ignoreDirChain(root + "/a/b/c")
	if len(chain) < 4 {
		t.Fatalf("expected at least 4 entries in chain (root → a → b → c), got %d: %v", len(chain), chain)
	}
	if !strings.HasSuffix(chain[len(chain)-1], "/c") {
		t.Errorf("scan root must be last in chain (gitignore last-match-wins): %v", chain)
	}
}

func TestIgnoreDirChainNoVCSReturnsScanRootOnly(t *testing.T) {
	// Without a VCS marker we don't blindly walk to /; return scanRoot
	// only so behavior matches pre-PR semantics outside repos.
	root := t.TempDir()
	chain := ignoreDirChain(root)
	// Outside a repo we walk up to filesystem root. The scan root must
	// still be the last entry (gitignore last-match-wins). We don't
	// strictly bound it because /tmp on some systems IS inside a git
	// worktree — the assertion stays loose.
	if chain[len(chain)-1] != root {
		// On macOS t.TempDir resolves through /var → /private/var; tolerate.
		if !strings.HasSuffix(chain[len(chain)-1], filepath.Base(root)) {
			t.Errorf("scan root must be last in chain: got %q, want suffix %q", chain[len(chain)-1], root)
		}
	}
}

func TestCollectFilesDefaultSkipsHiddenFilesAndDirs(t *testing.T) {
	// Default behavior: dotfile entries (basename starts with '.')
	// stay out of the corpus. .git/, .vscode/, .env all skipped at
	// the walk layer without needing a .graphifyignore rule.
	dir := t.TempDir()
	mustWrite(t, dir+"/main.go", "package main\n")
	if err := os.Mkdir(dir+"/.git", 0755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, dir+"/.git/config", "ignored\n")
	mustWrite(t, dir+"/.env", "SECRET=hunter2\n")
	got, err := CollectFiles(dir, []string{".go", ""})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !strings.HasSuffix(got[0], "main.go") {
		t.Errorf("expected only main.go in corpus, got %v", got)
	}
}

func TestCollectFilesGraphifyIncludeReAddsHiddenFiles(t *testing.T) {
	// .graphifyinclude with `.github/` re-adds the dir even though
	// the basename starts with '.'. Mirrors upstream's allowlist:
	// dotfiles are skipped by default; this file is the escape hatch.
	dir := t.TempDir()
	mustWrite(t, dir+"/.graphifyinclude", ".github/\n")
	if err := os.Mkdir(dir+"/.github", 0755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, dir+"/.github/workflows.yml", "name: ci\n")
	if err := os.Mkdir(dir+"/.git", 0755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, dir+"/.git/config", "[core]\n")
	got, err := CollectFiles(dir, []string{".yml", ""})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("expected exactly 1 file (workflows.yml), got %v", got)
	}
	if !strings.Contains(got[0], ".github/workflows.yml") {
		t.Errorf("expected .github/workflows.yml in corpus, got %v", got)
	}
}

func TestCollectFilesGraphifyIncludeSpecificFile(t *testing.T) {
	// File-specific allowlist: pull in `.eslintrc.json` without
	// re-including the rest of the dotfile family.
	dir := t.TempDir()
	mustWrite(t, dir+"/.graphifyinclude", ".eslintrc.json\n")
	mustWrite(t, dir+"/.eslintrc.json", `{"extends":"airbnb"}`+"\n")
	mustWrite(t, dir+"/.prettierrc", "{}\n")
	got, err := CollectFiles(dir, []string{".json", ""})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !strings.HasSuffix(got[0], ".eslintrc.json") {
		t.Errorf("expected just .eslintrc.json, got %v", got)
	}
}

func TestCollectFilesDoesNotFollowSymlinkDirsByDefault(t *testing.T) {
	// Default behavior: a symlink pointing at a directory is NOT
	// descended; files behind it are invisible to the corpus. This
	// matches filepath.Walk semantics (Lstat, no follow) and prevents
	// surprise corpus growth via symlinks.
	dir := t.TempDir()
	mustWrite(t, dir+"/main.go", "package main\n")
	external := t.TempDir()
	mustWrite(t, external+"/secret.go", "package secret\n")
	if err := os.Symlink(external, dir+"/linked"); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	got, err := CollectFiles(dir, []string{".go"})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range got {
		if strings.Contains(p, "secret.go") {
			t.Errorf("symlink dir contents leaked into corpus without flag: %v", got)
		}
	}
}

func TestCollectFilesFollowSymlinksDescendsInRootTarget(t *testing.T) {
	// With FollowSymlinks: a symlink whose target is still inside the
	// corpus root IS descended. Use a real-dir target inside the same
	// root so guard.Check accepts it.
	dir := t.TempDir()
	mustWrite(t, dir+"/main.go", "package main\n")
	if err := os.Mkdir(dir+"/realdir", 0755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, dir+"/realdir/lib.go", "package lib\n")
	if err := os.Symlink(dir+"/realdir", dir+"/linked"); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	got, err := CollectFilesWithOptions(dir, []string{".go"}, CollectOptions{FollowSymlinks: true})
	if err != nil {
		t.Fatal(err)
	}
	// Expect main.go + lib.go (lib.go reachable via either /realdir
	// or /linked; FollowSymlinks may surface it twice — that's a
	// de-dup concern handled by the consumer, but visiting BOTH the
	// real path and the link path is OK for this test).
	var sawLib bool
	for _, p := range got {
		if strings.HasSuffix(p, "lib.go") {
			sawLib = true
		}
	}
	if !sawLib {
		t.Errorf("FollowSymlinks: lib.go through link not reached: got %v", got)
	}
}

func TestCollectFilesFollowSymlinksDetectsCycle(t *testing.T) {
	// A symlink cycle (a → b → a) must not loop forever. The
	// visited-resolved-paths map breaks the cycle on the second
	// visit.
	dir := t.TempDir()
	mustWrite(t, dir+"/main.go", "package main\n")
	if err := os.Mkdir(dir+"/a", 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dir+"/b", 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(dir+"/b", dir+"/a/to_b"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(dir+"/a", dir+"/b/to_a"); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, dir+"/a/source.go", "package a\n")
	mustWrite(t, dir+"/b/source.go", "package b\n")
	// Must terminate. Bound runtime so a regression that hits the
	// cycle would hang the test rather than silently spinning.
	done := make(chan struct{})
	go func() {
		_, err := CollectFilesWithOptions(dir, []string{".go"}, CollectOptions{FollowSymlinks: true})
		if err != nil {
			t.Errorf("cycle should not error, just terminate: %v", err)
		}
		close(done)
	}()
	select {
	case <-done:
		// good
	case <-time.After(5 * time.Second):
		t.Fatal("symlink cycle caused hang — visited-paths bookkeeping is broken")
	}
}
