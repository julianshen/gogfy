package installer

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// codexInstaller writes the Codex CLI's TOML config (.codex/config.toml)
// at the workspace root. Codex's config is hand-edited TOML with comments,
// so we use a narrow line-based editor that replaces (or appends) the
// `[mcp_servers.gogfy]` block without touching unrelated tables, comments,
// or whitespace.
type codexInstaller struct {
	relativePath string // ".codex/config.toml"
}

func (c codexInstaller) ConfigPath(workspace string) string {
	return filepath.Join(workspace, c.relativePath)
}

func (c codexInstaller) Install(workspace string, opts Options) error {
	path := c.ConfigPath(workspace)
	existing, err := readFileOrEmpty(path)
	if err != nil {
		return err
	}
	if err := guardAgainstAlternateGogfyForms(existing, path); err != nil {
		return err
	}
	block := codexGogfyBlock(opts)
	updated, _ := replaceOrAppendBlock(existing, []byte("[mcp_servers.gogfy]"), block)
	return writeFileAtomic(path, updated)
}

func (c codexInstaller) Uninstall(workspace string) error {
	path := c.ConfigPath(workspace)
	existing, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	updated, removed := removeBlock(existing, []byte("[mcp_servers.gogfy]"))
	if !removed {
		// Block not present: leave the file byte-identical (no mtime churn).
		return nil
	}
	return writeFileAtomic(path, updated)
}

// guardAgainstAlternateGogfyForms refuses to install when the existing config
// already references mcp_servers.gogfy through an inline-table or
// array-of-tables shape that our line-based editor doesn't understand —
// blindly appending a `[mcp_servers.gogfy]` block in that case would produce
// duplicate-key TOML that Codex rejects at parse time.
func guardAgainstAlternateGogfyForms(existing []byte, path string) error {
	for i, raw := range bytes.SplitAfter(existing, []byte("\n")) {
		line := strings.TrimSpace(string(raw))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[mcp_servers.gogfy]") {
			// The standard form — replace in place, fine.
			return nil
		}
		// Hazard 1: array-of-tables `[[mcp_servers]]` followed by a line
		// containing `name = "gogfy"` (rare, but legal).
		if line == "[[mcp_servers.gogfy]]" {
			return fmt.Errorf("install: %s line %d uses array-of-tables [[mcp_servers.gogfy]] which we don't edit safely; remove it manually then re-run install", path, i+1)
		}
		// Hazard 2: an inline-table assignment such as
		// `mcp_servers = { gogfy = { ... } }` or
		// `mcp_servers.gogfy = { ... }`.
		if strings.Contains(line, "mcp_servers") && strings.Contains(line, "gogfy") && strings.Contains(line, "=") && !strings.HasPrefix(line, "[") {
			return fmt.Errorf("install: %s line %d defines mcp_servers.gogfy via an inline-table form which we don't edit safely; remove it manually then re-run install", path, i+1)
		}
	}
	return nil
}

// codexGogfyBlock builds the canonical TOML block for the gogfy server.
// The output ends with a trailing newline so that appended blocks don't
// run into following content.
func codexGogfyBlock(opts Options) []byte {
	var b strings.Builder
	b.WriteString("[mcp_servers.gogfy]\n")
	fmt.Fprintf(&b, "command = %q\n", opts.bin())
	graph := filepath.Join(opts.outDir(), "graph.json")
	report := filepath.Join(opts.outDir(), "GRAPH_REPORT.md")
	fmt.Fprintf(&b, "args = [%q, %q, %q, %q, %q]\n", "serve", "--graph", graph, "--report", report)
	return []byte(b.String())
}

// findBlock locates a [header]…[next header or EOF] region in existing.
// Returns (startLineIdx, endLineIdx, lines, found). lines is bytes.SplitAfter
// on '\n' so each entry preserves its trailing newline (joining with
// bytes.Join(lines, nil) is exactly inverse).
func findBlock(existing []byte, header []byte) (start, end int, lines [][]byte, found bool) {
	if len(existing) == 0 {
		return 0, 0, nil, false
	}
	lines = bytes.SplitAfter(existing, []byte("\n"))
	start = -1
	for i, line := range lines {
		if bytes.Equal(bytes.TrimSpace(line), header) {
			start = i
			break
		}
	}
	if start < 0 {
		return 0, 0, lines, false
	}
	end = len(lines)
	for i := start + 1; i < len(lines); i++ {
		if isTOMLHeaderLine(lines[i]) {
			end = i
			break
		}
	}
	return start, end, lines, true
}

// replaceOrAppendBlock returns existing with the block whose header line is
// `header` replaced by replacement. The block runs from its header line up
// to (but not including) the next top-level table header or end-of-file.
// If no such header is found, replacement is appended (with a separating
// blank line if needed). Returns (newContent, replaced).
func replaceOrAppendBlock(existing, header, replacement []byte) ([]byte, bool) {
	start, end, lines, found := findBlock(existing, header)
	if !found {
		out := append([]byte{}, existing...)
		// Ensure a blank line separates the appended block from prior content.
		if len(out) > 0 && !bytes.HasSuffix(out, []byte("\n")) {
			out = append(out, '\n')
		}
		if len(out) > 0 && !bytes.HasSuffix(out, []byte("\n\n")) {
			out = append(out, '\n')
		}
		out = append(out, replacement...)
		return out, false
	}
	var out bytes.Buffer
	out.Write(bytes.Join(lines[:start], nil))
	out.Write(replacement)
	if end < len(lines) {
		if !bytes.HasSuffix(replacement, []byte("\n")) {
			out.WriteByte('\n')
		}
		out.Write(bytes.Join(lines[end:], nil))
	}
	return out.Bytes(), true
}

// removeBlock strips a [header]…[next header or EOF] block from existing.
// Walks back over a single blank-line separator preceding the block so the
// seam doesn't leave a double-blank gap. Returns (newContent, removed).
func removeBlock(existing, header []byte) ([]byte, bool) {
	start, end, lines, found := findBlock(existing, header)
	if !found {
		return existing, false
	}
	prune := start
	if prune > 0 && len(bytes.TrimSpace(lines[prune-1])) == 0 {
		prune--
	}
	var out bytes.Buffer
	out.Write(bytes.Join(lines[:prune], nil))
	if end < len(lines) {
		out.Write(bytes.Join(lines[end:], nil))
	}
	return out.Bytes(), true
}

// isTOMLHeaderLine reports whether a line is a top-level table or
// array-of-tables header (`[name]`, `[name.sub]`, or `[[name]]`).
// Comment-prefixed `# [foo]` lines are not headers.
func isTOMLHeaderLine(line []byte) bool {
	trimmed := bytes.TrimSpace(line)
	return len(trimmed) > 0 && trimmed[0] == '['
}
