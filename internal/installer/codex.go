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
	existing, err := readBytesOrEmpty(path)
	if err != nil {
		return err
	}
	block := codexGogfyBlock(opts)
	updated, _ := replaceOrAppendBlock(existing, []byte("[mcp_servers.gogfy]"), block)
	return writeBytes(path, updated)
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
	return writeBytes(path, updated)
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

// replaceOrAppendBlock returns existing with the block whose header line is
// `header` replaced by replacement. The block runs from its header line up
// to (but not including) the next top-level table header (a line starting
// with `[`) or end-of-file. If no such header is found, replacement is
// appended to existing (with a separating blank line if needed). Returns
// (newContent, replaced).
//
// "Top-level table header" detection is intentionally simple: we treat any
// line whose first non-whitespace character is `[` as a header boundary.
// Codex's config is hand-edited and conventional — nested arrays-of-tables
// or pathological indented headers don't appear in practice.
func replaceOrAppendBlock(existing []byte, header, replacement []byte) ([]byte, bool) {
	lines := splitLines(existing)
	headerStr := string(header)
	// Find the start of our block.
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == headerStr {
			start = i
			break
		}
	}
	if start < 0 {
		// Append. Ensure a blank line separator if existing doesn't end with one.
		out := append([]byte{}, existing...)
		if len(out) > 0 && !bytes.HasSuffix(out, []byte("\n")) {
			out = append(out, '\n')
		}
		if len(out) > 0 && !bytes.HasSuffix(out, []byte("\n\n")) {
			out = append(out, '\n')
		}
		out = append(out, replacement...)
		return out, false
	}
	// Find the end of the block (next header line or EOF).
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if isTOMLHeaderLine(lines[i]) {
			end = i
			break
		}
	}
	// Splice: lines[:start] + replacement + lines[end:]
	var out bytes.Buffer
	out.Write(joinLines(lines[:start]))
	out.Write(replacement)
	if end < len(lines) {
		// Ensure replacement ends with a newline before the trailing block.
		if !bytes.HasSuffix(replacement, []byte("\n")) {
			out.WriteByte('\n')
		}
		out.Write(joinLines(lines[end:]))
	}
	return out.Bytes(), true
}

// removeBlock strips a [header]...[next header or EOF] block from existing.
// Returns (newContent, removed) where removed=false means the header was
// not found (caller should treat as no-op).
func removeBlock(existing []byte, header []byte) ([]byte, bool) {
	lines := splitLines(existing)
	headerStr := string(header)
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == headerStr {
			start = i
			break
		}
	}
	if start < 0 {
		return existing, false
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if isTOMLHeaderLine(lines[i]) {
			end = i
			break
		}
	}
	// Walk back from start over leading blank lines that belong to the block
	// (one separator line is fair game; we keep earlier content intact).
	prune := start
	if prune > 0 && strings.TrimSpace(lines[prune-1]) == "" {
		prune--
	}
	var out bytes.Buffer
	out.Write(joinLines(lines[:prune]))
	if end < len(lines) {
		out.Write(joinLines(lines[end:]))
	}
	return out.Bytes(), true
}

// splitLines splits b into lines, preserving each line's trailing newline
// so that joinLines is exactly inverse. The final line has no trailing
// newline iff b didn't end with one.
func splitLines(b []byte) []string {
	if len(b) == 0 {
		return nil
	}
	out := []string{}
	start := 0
	for i := 0; i < len(b); i++ {
		if b[i] == '\n' {
			out = append(out, string(b[start:i+1]))
			start = i + 1
		}
	}
	if start < len(b) {
		out = append(out, string(b[start:]))
	}
	return out
}

func joinLines(lines []string) []byte {
	var b bytes.Buffer
	for _, l := range lines {
		b.WriteString(l)
	}
	return b.Bytes()
}

// isTOMLHeaderLine reports whether a line is a top-level table or
// array-of-tables header (`[name]`, `[name.sub]`, or `[[name]]`).
// Comment-prefixed `# [foo]` lines are not headers.
func isTOMLHeaderLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "[")
}

// readBytesOrEmpty reads path or returns nil for ENOENT (mirrors
// readOrEmpty for the JSON path; separate because this returns raw bytes).
func readBytesOrEmpty(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return data, nil
}

// writeBytes writes data atomically (.tmp + rename), creating parent dirs.
func writeBytes(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
