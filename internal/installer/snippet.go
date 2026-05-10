package installer

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"github.com/julianshen/gogfy/internal/serve"
)

// snippet markers fence the gogfy-managed instruction block inside a
// project-level documentation file (CLAUDE.md, AGENTS.md, etc.). Re-running
// InstallSnippet replaces only the fenced region, leaving surrounding
// content untouched.
const (
	snippetStartMarker = "<!-- gogfy-graph-instructions:start -->"
	snippetEndMarker   = "<!-- gogfy-graph-instructions:end -->"
)

// SnippetOptions tunes the rendered snippet.
type SnippetOptions struct {
	// ReportPath is the workspace-relative path to GRAPH_REPORT.md. Defaults
	// to "graphify-out/GRAPH_REPORT.md" so it matches the run output.
	ReportPath string
}

func (s SnippetOptions) reportPath() string {
	if s.ReportPath == "" {
		return "graphify-out/GRAPH_REPORT.md"
	}
	return s.ReportPath
}

// InstallSnippet writes (or refreshes) the gogfy instruction block in path.
// If path doesn't exist, it's created with just the snippet. If it does and
// already contains a fenced gogfy block, the block is replaced in place.
// Otherwise the snippet is appended.
func InstallSnippet(path string, opts SnippetOptions) error {
	rendered := renderSnippet(opts)
	existing, err := readFileOrEmpty(path)
	if err != nil {
		return err
	}
	if len(existing) == 0 {
		return writeFileAtomic(path, rendered)
	}
	updated, replaced, err := replaceFencedSnippet(existing, rendered)
	if err != nil {
		return err
	}
	if !replaced {
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
		updated = buf.Bytes()
	}
	if bytes.Equal(updated, existing) {
		// Idempotent: nothing changed, skip the rewrite to preserve mtime.
		return nil
	}
	return writeFileAtomic(path, updated)
}

// UninstallSnippet strips the fenced gogfy block from path. If removing it
// leaves the file empty or whitespace-only, the file is deleted (it was
// gogfy's only contribution). No-op when the file doesn't exist or has no
// fenced block.
func UninstallSnippet(path string) error {
	existing, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	updated, removed, err := stripFencedSnippet(existing)
	if err != nil {
		return err
	}
	if !removed {
		return nil
	}
	if len(bytes.TrimSpace(updated)) == 0 {
		return os.Remove(path)
	}
	return writeFileAtomic(path, updated)
}

// renderSnippet builds the fenced markdown block, ending with a newline so
// later appended content (a heading, etc.) starts on its own line.
func renderSnippet(opts SnippetOptions) []byte {
	var b strings.Builder
	b.WriteString(snippetStartMarker)
	b.WriteString("\n")
	fmt.Fprintf(&b, "## Codebase navigation: %s\n\n", opts.reportPath())
	fmt.Fprintf(&b, "Before answering questions about this codebase, read `%s` first.\n", opts.reportPath())
	b.WriteString("It contains:\n")
	b.WriteString("- **God nodes** — the most-connected concepts in the project (the hubs everything flows through).\n")
	b.WriteString("- **Surprising connections** — links between things in different files/modules, ranked by how unexpected they are.\n")
	b.WriteString("- **Confidence summary** — counts of EXTRACTED / INFERRED / AMBIGUOUS edges, so you know what was found vs. guessed.\n")
	b.WriteString("- **Suggested questions** — questions the graph is uniquely positioned to answer.\n\n")
	fmt.Fprintf(&b, "Use the gogfy MCP tools (`%s`, `%s`, `%s`) to navigate by graph structure instead of grepping.\n",
		serve.ToolGodNodes, serve.ToolExplain, serve.ToolQuery)
	b.WriteString(snippetEndMarker)
	b.WriteString("\n")
	return []byte(b.String())
}

// replaceFencedSnippet replaces the content between (and including) the
// snippet markers with rendered. Returns (updated, true, nil) if exactly
// one matched pair was found and replaced; (existing, false, nil) if no
// markers are present (caller appends); or (existing, false, err) when
// markers are mismatched (orphan start, orphan end, or multiple pairs) —
// the caller MUST surface that error rather than guessing, because picking
// the "first start + first end" can span over user content and silently
// delete it.
func replaceFencedSnippet(existing, rendered []byte) ([]byte, bool, error) {
	starts := indexAll(existing, []byte(snippetStartMarker))
	ends := indexAll(existing, []byte(snippetEndMarker))
	if len(starts) == 0 && len(ends) == 0 {
		return existing, false, nil
	}
	if len(starts) != 1 || len(ends) != 1 {
		return existing, false, fmt.Errorf("install-instructions: expected exactly one fenced gogfy block (found %d start, %d end markers); fix the file by hand and re-run", len(starts), len(ends))
	}
	startIdx, endStart := starts[0], ends[0]
	if endStart < startIdx {
		return existing, false, fmt.Errorf("install-instructions: end marker appears before start marker — fix the file by hand and re-run")
	}
	endIdx := endStart + len(snippetEndMarker)
	// Consume one trailing newline so repeated installs don't accumulate
	// blank lines.
	if endIdx < len(existing) && existing[endIdx] == '\n' {
		endIdx++
	}
	var buf bytes.Buffer
	buf.Write(existing[:startIdx])
	buf.Write(rendered)
	buf.Write(existing[endIdx:])
	return buf.Bytes(), true, nil
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

// stripFencedSnippet removes the fenced block (markers and content) from
// existing. Returns (updated, true, nil) for exactly one matched pair;
// (existing, false, nil) when no markers are present; (existing, false,
// err) for any mismatched state. Same orphan-marker hazard as
// replaceFencedSnippet — refuse to guess and risk deleting user content.
func stripFencedSnippet(existing []byte) ([]byte, bool, error) {
	starts := indexAll(existing, []byte(snippetStartMarker))
	ends := indexAll(existing, []byte(snippetEndMarker))
	if len(starts) == 0 && len(ends) == 0 {
		return existing, false, nil
	}
	if len(starts) != 1 || len(ends) != 1 {
		return existing, false, fmt.Errorf("uninstall-instructions: expected exactly one fenced gogfy block (found %d start, %d end markers); fix the file by hand and re-run", len(starts), len(ends))
	}
	startIdx, endStart := starts[0], ends[0]
	if endStart < startIdx {
		return existing, false, fmt.Errorf("uninstall-instructions: end marker appears before start marker — fix the file by hand and re-run")
	}
	endIdx := endStart + len(snippetEndMarker)
	if endIdx < len(existing) && existing[endIdx] == '\n' {
		endIdx++
	}
	// Walk back over a single blank-line separator so the seam doesn't
	// leave a double-blank gap.
	cut := startIdx
	if cut >= 2 && existing[cut-1] == '\n' && existing[cut-2] == '\n' {
		cut--
	}
	var buf bytes.Buffer
	buf.Write(existing[:cut])
	buf.Write(existing[endIdx:])
	return buf.Bytes(), true, nil
}
