package githook

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/julianshen/gogfy/internal/fence"
	"github.com/julianshen/gogfy/internal/fsutil"
)

// Sentinel comments fencing the gogfy-managed regions inside .git/config
// and .gitattributes. Both files use shell-style `#` comments at the
// regions we touch (gitconfig honors `#` and `;`; gitattributes is
// hash-comment), so the markers are inert when git parses them.
const (
	mergeDriverStart = "# gogfy-merge-driver:start"
	mergeDriverEnd   = "# gogfy-merge-driver:end"
)

// MergeDriverOptions tunes the rendered driver invocation. Bin defaults
// to "gogfy" so the driver matches a PATH-resolved install; pass an
// absolute path to pin the binary.
type MergeDriverOptions struct {
	Bin string
}

func (o MergeDriverOptions) bin() string {
	if o.Bin == "" {
		return "gogfy"
	}
	return o.Bin
}

// InstallMergeDriver registers a graph.json union merge driver in
// repoRoot's .git/config and a matching merge=gogfy rule in
// .gitattributes. Both writes use a fenced block so install/uninstall
// don't clobber unrelated entries.
//
// Driver semantics: git invokes the configured command with %A, %B as
// the "ours" and "theirs" file contents. Our `merge-graphs` reads both,
// computes the deterministic union, and writes the result to %A. The
// merge-base (%O) is intentionally not consulted — graph.json is always
// regenerated, never hand-edited, so a 3-way merge that "removes" nodes
// because the ancestor has them and one side doesn't would just churn.
func InstallMergeDriver(repoRoot string, opts MergeDriverOptions) error {
	if err := requireWorkingTreeRepo(repoRoot); err != nil {
		return fmt.Errorf("install merge-driver: %w", err)
	}
	if err := writeFencedBlock(filepath.Join(repoRoot, ".git", "config"), renderMergeDriverConfig(opts)); err != nil {
		return fmt.Errorf("install merge-driver: %w", err)
	}
	if err := writeFencedBlock(filepath.Join(repoRoot, ".gitattributes"), renderMergeDriverAttrs()); err != nil {
		return fmt.Errorf("install merge-driver: %w", err)
	}
	return nil
}

// UninstallMergeDriver removes the gogfy fenced blocks from .git/config
// and .gitattributes. Files left empty (whitespace-only) are deleted.
// No-op when neither block is present.
func UninstallMergeDriver(repoRoot string) error {
	for _, p := range []string{
		filepath.Join(repoRoot, ".git", "config"),
		filepath.Join(repoRoot, ".gitattributes"),
	} {
		if err := stripFencedBlock(p); err != nil {
			return fmt.Errorf("uninstall merge-driver: %w", err)
		}
	}
	return nil
}

// MergeDriverInstalled reports whether the gogfy merge-driver block is
// present in repoRoot's .git/config. We use the config side as the
// canonical signal because .gitattributes can carry the rule
// independently (e.g., committed once, then .git/config edited).
func MergeDriverInstalled(repoRoot string) bool {
	data, err := os.ReadFile(filepath.Join(repoRoot, ".git", "config"))
	if err != nil {
		return false
	}
	return bytes.Contains(data, []byte(mergeDriverStart))
}

func renderMergeDriverConfig(opts MergeDriverOptions) []byte {
	var b bytes.Buffer
	b.WriteString(mergeDriverStart)
	b.WriteString("\n")
	b.WriteString(`[merge "gogfy"]` + "\n")
	b.WriteString("\tname = gogfy graph.json union merge\n")
	fmt.Fprintf(&b, "\tdriver = %s merge-graphs %%A %%B --out %%A\n", opts.bin())
	b.WriteString(mergeDriverEnd)
	b.WriteString("\n")
	return b.Bytes()
}

func renderMergeDriverAttrs() []byte {
	var b bytes.Buffer
	b.WriteString(mergeDriverStart)
	b.WriteString("\n")
	b.WriteString("graphify-out/graph.json merge=gogfy\n")
	b.WriteString(mergeDriverEnd)
	b.WriteString("\n")
	return b.Bytes()
}

// writeFencedBlock installs (or refreshes) a fenced gogfy block in path.
// Creates the file if missing; appends if a non-fenced version exists;
// replaces in place if the fence already exists.
func writeFencedBlock(path string, rendered []byte) error {
	existing, err := fsutil.ReadFileOrEmpty(path)
	if err != nil {
		return err
	}
	if len(existing) == 0 {
		return fsutil.WriteFileAtomic(path, rendered, 0644)
	}
	updated, replaced, err := fence.Replace(existing, []byte(mergeDriverStart), []byte(mergeDriverEnd), rendered)
	if err != nil {
		return err
	}
	if !replaced {
		var buf bytes.Buffer
		buf.Write(existing)
		if !bytes.HasSuffix(existing, []byte("\n")) {
			buf.WriteByte('\n')
		}
		if !bytes.HasSuffix(buf.Bytes(), []byte("\n\n")) {
			buf.WriteByte('\n')
		}
		buf.Write(rendered)
		updated = buf.Bytes()
	}
	if bytes.Equal(updated, existing) {
		return nil
	}
	return fsutil.WriteFileAtomic(path, updated, 0644)
}

// stripFencedBlock removes the gogfy fenced block from path. Files left
// empty after the strip are deleted (so install-then-uninstall on a
// previously-absent file leaves no trace). No-op when path doesn't
// exist or has no fenced block.
func stripFencedBlock(path string) error {
	existing, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	updated, removed, err := fence.Strip(existing, []byte(mergeDriverStart), []byte(mergeDriverEnd))
	if err != nil {
		return err
	}
	if !removed {
		return nil
	}
	if len(bytes.TrimSpace(updated)) == 0 {
		return os.Remove(path)
	}
	return fsutil.WriteFileAtomic(path, updated, 0644)
}
