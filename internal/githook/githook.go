// Package githook installs a post-commit git hook that auto-rebuilds the
// gogfy graph after each commit. The hook is fenced with sentinel
// comments so it coexists with any other hook content (manual scripts,
// other tools' blocks) and can be added or removed without touching
// surrounding lines.
package githook

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Sentinel comments that fence the gogfy-managed region inside
// `.git/hooks/post-commit`. Shell-comment safe (`#`), so they're inert
// when the hook runs.
const (
	hookStartMarker = "# gogfy-auto-rebuild:start"
	hookEndMarker   = "# gogfy-auto-rebuild:end"
	hookShebang     = "#!/bin/sh"
)

// Options tunes the hook script. Zero values mean "binary on PATH +
// default `graphify-out` directory" — same parity with installer.Options.
type Options struct {
	Bin    string // Path or name of the gogfy binary the hook should invoke.
	OutDir string // Workspace-relative graph output directory.
}

func (o Options) bin() string {
	if o.Bin == "" {
		return "gogfy"
	}
	return o.Bin
}

func (o Options) outDir() string {
	if o.OutDir == "" {
		return "graphify-out"
	}
	return o.OutDir
}

// HookPath returns the post-commit hook path for repoRoot. Resolves the
// hooks directory through `.git` even when it's a file (submodule or
// worktree shape, where `.git` contains `gitdir: <real-path>`). Pure-ish:
// reads `.git` only when it's a file.
func HookPath(repoRoot string) string {
	return filepath.Join(hooksDirFor(repoRoot), "post-commit")
}

// hooksDirFor returns the directory containing git hooks for repoRoot.
// For a normal repo, this is `<root>/.git/hooks`. For a submodule or
// worktree (where `.git` is a file pointing into `.git/modules/<name>`
// or `.git/worktrees/<name>`), it's the resolved hooks directory.
func hooksDirFor(repoRoot string) string {
	gitPath := filepath.Join(repoRoot, ".git")
	info, err := os.Stat(gitPath)
	if err != nil || info.IsDir() {
		return filepath.Join(gitPath, "hooks")
	}
	// `.git` is a file (submodule / worktree). Parse `gitdir: <path>`.
	data, err := os.ReadFile(gitPath)
	if err != nil {
		return filepath.Join(gitPath, "hooks") // fall back; caller will surface error
	}
	line := strings.TrimSpace(string(data))
	const prefix = "gitdir:"
	rest := strings.TrimSpace(strings.TrimPrefix(line, prefix))
	if rest == "" || rest == line {
		return filepath.Join(gitPath, "hooks")
	}
	if !filepath.IsAbs(rest) {
		rest = filepath.Join(repoRoot, rest)
	}
	return filepath.Join(rest, "hooks")
}

// Install writes (or refreshes) the gogfy auto-rebuild block in the repo's
// post-commit hook. Resolves submodule and worktree shapes (where `.git`
// is a file). Refuses bare repos (nothing to rebuild) and non-repo paths.
//
// Behavior:
//   - Hook missing → create with `#!/bin/sh` shebang + the gogfy block.
//   - Hook exists, no gogfy block → append the block.
//   - Hook exists with a single matched gogfy block → replace in place.
//   - Hook exists with mismatched markers (orphan start, multiple pairs)
//     → return error rather than risk deleting user content.
//
// The result is always chmod 0755 so git can execute it. No-op when the
// resulting bytes match the existing file (preserves mtime).
func Install(repoRoot string, opts Options) error {
	if err := requireWorkingTreeRepo(repoRoot); err != nil {
		return err
	}
	hooksDir := hooksDirFor(repoRoot)
	if info, err := os.Stat(hooksDir); err != nil || !info.IsDir() {
		return fmt.Errorf("install hook: hooks directory %s missing — submodule/worktree may not be initialized", hooksDir)
	}
	path := filepath.Join(hooksDir, "post-commit")
	existing, err := readOrEmpty(path)
	if err != nil {
		return err
	}
	rendered := renderHookBlock(opts)
	updated, err := mergeHookContent(existing, rendered)
	if err != nil {
		return err
	}
	if bytes.Equal(updated, existing) {
		// No change: skip the rewrite. But still ensure the executable bit
		// is set in case the file was made non-executable by hand.
		return ensureExecutable(path)
	}
	if err := writeAtomic(path, updated); err != nil {
		return err
	}
	return ensureExecutable(path)
}

// Uninstall strips the gogfy block from the post-commit hook. If the
// resulting content is empty, whitespace-only, or just the shebang, the
// hook file is removed entirely. No-op when the file doesn't exist or
// has no gogfy block.
func Uninstall(repoRoot string) error {
	path := HookPath(repoRoot)
	existing, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	updated, removed, err := stripHookBlock(existing)
	if err != nil {
		return err
	}
	if !removed {
		return nil
	}
	if hookContentIsEmpty(updated) {
		return os.Remove(path)
	}
	if err := writeAtomic(path, updated); err != nil {
		return err
	}
	return ensureExecutable(path)
}

// renderHookBlock builds the fenced shell snippet. The snippet runs gogfy
// in the foreground so the next commit waits if a rebuild is in progress —
// simpler than the alternative (background) and avoids race conditions on
// graph artifacts. Tradeoff: a slow rebuild slows the commit; users can
// uninstall if it's annoying.
//
// stderr/stdout are redirected to a log inside the git directory so a
// consistently-broken rebuild leaves a trail (silent /dev/null discard
// would hide breakage indefinitely). `|| true` keeps the hook from
// failing the commit even when gogfy errors.
func renderHookBlock(opts Options) []byte {
	var b strings.Builder
	b.WriteString(hookStartMarker)
	b.WriteString("\n")
	b.WriteString(`GOGFY_LOG="$(git rev-parse --git-dir 2>/dev/null)/gogfy-rebuild.log"` + "\n")
	fmt.Fprintf(&b, "%s run --update --out %s . >>\"$GOGFY_LOG\" 2>&1 || true\n", opts.bin(), opts.outDir())
	b.WriteString(hookEndMarker)
	b.WriteString("\n")
	return []byte(b.String())
}

// requireWorkingTreeRepo refuses bare repositories (where there's no
// working tree to rebuild against) and unrecognized paths.
func requireWorkingTreeRepo(repoRoot string) error {
	gitPath := filepath.Join(repoRoot, ".git")
	if _, err := os.Stat(gitPath); err != nil {
		return fmt.Errorf("install hook: %s is not a git repository (no .git entry)", repoRoot)
	}
	// Detect bare-repo shape by looking for HEAD + objects in repoRoot.
	// In a working-tree repo these live under .git, not at the root.
	if _, err := os.Stat(filepath.Join(repoRoot, "HEAD")); err == nil {
		if _, err := os.Stat(filepath.Join(repoRoot, "objects")); err == nil {
			return fmt.Errorf("install hook: %s looks like a bare repository — auto-rebuild needs a working tree", repoRoot)
		}
	}
	return nil
}

// mergeHookContent inserts/refreshes the rendered gogfy block in existing.
// Returns the new bytes plus any mismatched-marker error.
func mergeHookContent(existing, rendered []byte) ([]byte, error) {
	if len(existing) == 0 {
		// Brand new hook: shebang + block.
		var buf bytes.Buffer
		buf.WriteString(hookShebang)
		buf.WriteString("\n")
		buf.Write(rendered)
		return buf.Bytes(), nil
	}
	updated, replaced, err := replaceFencedBlock(existing, rendered)
	if err != nil {
		return nil, err
	}
	if replaced {
		return updated, nil
	}
	// Append. Ensure separation from prior content.
	var buf bytes.Buffer
	buf.Write(existing)
	if !bytes.HasSuffix(existing, []byte("\n")) {
		buf.WriteByte('\n')
	}
	if !bytes.HasSuffix(buf.Bytes(), []byte("\n\n")) {
		buf.WriteByte('\n')
	}
	buf.Write(rendered)
	return buf.Bytes(), nil
}

// replaceFencedBlock replaces the content between the gogfy hook markers
// with rendered. Returns (updated, true, nil) on a single matched pair,
// (existing, false, nil) when no markers are present, or (existing, false,
// err) for any mismatched state — refuse to guess and risk deleting user
// content (same hazard the snippet writer guards against).
func replaceFencedBlock(existing, rendered []byte) ([]byte, bool, error) {
	starts := indexAll(existing, []byte(hookStartMarker))
	ends := indexAll(existing, []byte(hookEndMarker))
	if len(starts) == 0 && len(ends) == 0 {
		return existing, false, nil
	}
	if len(starts) != 1 || len(ends) != 1 {
		return existing, false, fmt.Errorf("hook install: expected exactly one fenced gogfy block in post-commit (found %d start, %d end markers); fix the file by hand and re-run", len(starts), len(ends))
	}
	startIdx, endStart := starts[0], ends[0]
	if endStart < startIdx {
		return existing, false, fmt.Errorf("hook install: end marker appears before start marker — fix the file by hand and re-run")
	}
	endIdx := endStart + len(hookEndMarker)
	if endIdx < len(existing) && existing[endIdx] == '\n' {
		endIdx++
	}
	var buf bytes.Buffer
	buf.Write(existing[:startIdx])
	buf.Write(rendered)
	buf.Write(existing[endIdx:])
	return buf.Bytes(), true, nil
}

// stripHookBlock mirrors replaceFencedBlock but removes the fenced region
// instead of replacing it. Drops one preceding blank-line separator so the
// seam doesn't leave a double-blank gap.
func stripHookBlock(existing []byte) ([]byte, bool, error) {
	starts := indexAll(existing, []byte(hookStartMarker))
	ends := indexAll(existing, []byte(hookEndMarker))
	if len(starts) == 0 && len(ends) == 0 {
		return existing, false, nil
	}
	if len(starts) != 1 || len(ends) != 1 {
		return existing, false, fmt.Errorf("hook uninstall: expected exactly one fenced gogfy block (found %d start, %d end markers); fix the file by hand and re-run", len(starts), len(ends))
	}
	startIdx, endStart := starts[0], ends[0]
	if endStart < startIdx {
		return existing, false, fmt.Errorf("hook uninstall: end marker appears before start marker — fix the file by hand and re-run")
	}
	endIdx := endStart + len(hookEndMarker)
	if endIdx < len(existing) && existing[endIdx] == '\n' {
		endIdx++
	}
	cut := startIdx
	if cut >= 2 && existing[cut-1] == '\n' && existing[cut-2] == '\n' {
		cut--
	}
	var buf bytes.Buffer
	buf.Write(existing[:cut])
	buf.Write(existing[endIdx:])
	return buf.Bytes(), true, nil
}

// hookContentIsEmpty reports whether buf is "effectively empty" — nothing
// but the shebang and whitespace. Used to decide whether Uninstall should
// drop the file entirely vs. write a near-empty stub.
func hookContentIsEmpty(buf []byte) bool {
	trimmed := bytes.TrimSpace(buf)
	if len(trimmed) == 0 {
		return true
	}
	// Strip the shebang line if present; check what's left.
	if bytes.HasPrefix(trimmed, []byte("#!")) {
		if i := bytes.IndexByte(trimmed, '\n'); i >= 0 {
			rest := bytes.TrimSpace(trimmed[i+1:])
			return len(rest) == 0
		}
		return true
	}
	return false
}

// indexAll returns every starting index of needle within hay.
func indexAll(hay, needle []byte) []int {
	var out []int
	for offset := 0; ; {
		i := bytes.Index(hay[offset:], needle)
		if i < 0 {
			return out
		}
		out = append(out, offset+i)
		offset += i + len(needle)
	}
}

// readOrEmpty reads path or returns nil for ENOENT. Local copy because
// internal/installer's helper isn't exported and we don't want to widen
// that package's API just for one consumer.
func readOrEmpty(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return data, nil
}

// writeAtomic writes data via .tmp + rename, with parent-dir creation.
// Same semantics as installer.writeFileAtomic; avoids exporting that
// helper purely for symmetry.
func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0755); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// ensureExecutable sets the user/group/other execute bits on path so git
// can run the hook. Idempotent.
func ensureExecutable(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	want := info.Mode() | 0111
	if info.Mode() == want {
		return nil
	}
	return os.Chmod(path, want)
}
