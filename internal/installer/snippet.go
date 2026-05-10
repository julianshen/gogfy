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
	updated, replaced := replaceFencedSnippet(existing, rendered)
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
	updated, removed := stripFencedSnippet(existing)
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
// snippet markers with rendered. Returns (updated, true) if a fenced block
// was found and replaced; (existing, false) otherwise. The replacement
// preserves leading/trailing context exactly.
func replaceFencedSnippet(existing, rendered []byte) ([]byte, bool) {
	startIdx := bytes.Index(existing, []byte(snippetStartMarker))
	if startIdx < 0 {
		return existing, false
	}
	// Look for the matching end marker AFTER the start marker.
	endIdx := bytes.Index(existing[startIdx:], []byte(snippetEndMarker))
	if endIdx < 0 {
		// Start marker without an end marker — leave alone (don't guess).
		return existing, false
	}
	endIdx += startIdx + len(snippetEndMarker)
	// Consume one trailing newline if present so we don't accumulate blank
	// lines on repeated installs.
	if endIdx < len(existing) && existing[endIdx] == '\n' {
		endIdx++
	}
	var buf bytes.Buffer
	buf.Write(existing[:startIdx])
	buf.Write(rendered)
	buf.Write(existing[endIdx:])
	return buf.Bytes(), true
}

// stripFencedSnippet removes the fenced block (markers and content) from
// existing. Also drops a leading blank-line separator if the block was
// preceded by one — keeps the file clean rather than leaving stranded
// whitespace.
func stripFencedSnippet(existing []byte) ([]byte, bool) {
	startIdx := bytes.Index(existing, []byte(snippetStartMarker))
	if startIdx < 0 {
		return existing, false
	}
	endIdx := bytes.Index(existing[startIdx:], []byte(snippetEndMarker))
	if endIdx < 0 {
		return existing, false
	}
	endIdx += startIdx + len(snippetEndMarker)
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
	return buf.Bytes(), true
}
