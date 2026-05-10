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
	if bytes.Equal(updated, existing) {
		// No change — skip the rewrite to preserve mtime (parity with the
		// snippet writer).
		return nil
	}
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

// guardAgainstAlternateGogfyForms refuses to install when the existing
// config already references mcp_servers.gogfy through an array-of-tables
// shape, or via an inline-table assignment whose left-hand side is a real
// TOML key path for mcp_servers.gogfy. Our line-based editor only
// understands the standard `[mcp_servers.gogfy]` block; appending one when
// an alternate form already exists would produce duplicate-key TOML that
// Codex rejects at parse time.
//
// Detection is deliberately narrow: we only flag lines whose KEY position
// is `mcp_servers.gogfy = ...` or `mcp_servers = { ... gogfy = ... }`
// (inline). Strings or comments that happen to mention both words ("see
// mcp_servers and gogfy docs" inside a description value) must NOT trigger
// a refusal.
func guardAgainstAlternateGogfyForms(existing []byte, path string) error {
	for i, raw := range bytes.SplitAfter(existing, []byte("\n")) {
		line := bytes.TrimSpace(raw)
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		// Strip trailing comment so its content can't influence detection.
		code := stripTrailingComment(line)
		if hasGogfyHeader(code) {
			// Standard form: `[mcp_servers.gogfy]` (with or without inline
			// comment). Replace-in-place handles it.
			return nil
		}
		if bytes.Equal(code, []byte("[[mcp_servers.gogfy]]")) {
			return fmt.Errorf("install: %s line %d uses array-of-tables [[mcp_servers.gogfy]] which we don't edit safely; remove it manually then re-run install", path, i+1)
		}
		if isAlternateGogfyAssignment(code) {
			return fmt.Errorf("install: %s line %d defines mcp_servers.gogfy via an inline-table form which we don't edit safely; remove it manually then re-run install", path, i+1)
		}
	}
	return nil
}

// hasGogfyHeader reports whether code is `[mcp_servers.gogfy]` (or any
// whitespace-tolerant variant TOML accepts as the same header).
func hasGogfyHeader(code []byte) bool {
	return bytes.Equal(normalizeHeader(code), []byte("[mcp_servers.gogfy]"))
}

// isAlternateGogfyAssignment reports whether code's KEY position defines
// mcp_servers.gogfy via an alternate (non-table) form. Only the LHS of `=`
// is inspected, so values that happen to mention "mcp_servers" or "gogfy"
// are immune.
func isAlternateGogfyAssignment(code []byte) bool {
	eq := bytes.IndexByte(code, '=')
	if eq < 0 {
		return false
	}
	lhs := bytes.TrimSpace(code[:eq])
	rhs := bytes.TrimSpace(code[eq+1:])
	// `mcp_servers.gogfy = ...`
	if bytes.Equal(lhs, []byte("mcp_servers.gogfy")) {
		return true
	}
	// `mcp_servers = { gogfy = ... }` — the inline opener. Match only when
	// `gogfy` appears as a real key (followed by optional whitespace then
	// `=`), so server names that merely contain "gogfy" as a substring
	// (e.g., `mygogfyproxy = {...}`) don't trigger a false refusal.
	if bytes.Equal(lhs, []byte("mcp_servers")) && bytes.HasPrefix(rhs, []byte("{")) && containsGogfyKey(rhs) {
		return true
	}
	return false
}

// containsGogfyKey reports whether buf contains a TOML key named exactly
// `gogfy` followed by `=` (with any amount of whitespace). Avoids matching
// substrings like `mygogfyproxy`.
func containsGogfyKey(buf []byte) bool {
	for i := 0; i < len(buf); i++ {
		j := bytes.Index(buf[i:], []byte("gogfy"))
		if j < 0 {
			return false
		}
		j += i
		// Preceding char must be a key boundary: `{`, `,`, whitespace, or
		// the buffer start.
		if j > 0 {
			prev := buf[j-1]
			if prev != '{' && prev != ',' && prev != ' ' && prev != '\t' && prev != '\n' {
				i = j + 1
				continue
			}
		}
		// Following content (after `gogfy`, skipping whitespace) must be `=`.
		k := j + len("gogfy")
		for k < len(buf) && (buf[k] == ' ' || buf[k] == '\t') {
			k++
		}
		if k < len(buf) && buf[k] == '=' {
			return true
		}
		i = j + 1
	}
	return false
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
//
// Header matching tolerates trailing inline comments — `[name] # note` is
// the same block as `[name]` — since Codex configs are hand-edited and
// users frequently annotate sections.
func findBlock(existing []byte, header []byte) (start, end int, lines [][]byte, found bool) {
	if len(existing) == 0 {
		return 0, 0, nil, false
	}
	lines = bytes.SplitAfter(existing, []byte("\n"))
	start = -1
	for i, line := range lines {
		if bytes.Equal(normalizeHeader(stripTrailingComment(bytes.TrimSpace(line))), header) {
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

// normalizeHeader collapses whitespace inside a TOML table header so that
// `[ mcp_servers.gogfy ]` and `[mcp_servers . gogfy]` (both TOML-valid)
// match the canonical `[mcp_servers.gogfy]` shape. Returns line unchanged
// when it isn't a `[...]` form.
func normalizeHeader(line []byte) []byte {
	if len(line) < 2 || line[0] != '[' || line[len(line)-1] != ']' {
		return line
	}
	// Strip space/tab characters from inside the brackets only.
	inner := line[1 : len(line)-1]
	var b bytes.Buffer
	b.WriteByte('[')
	for _, c := range inner {
		if c == ' ' || c == '\t' {
			continue
		}
		b.WriteByte(c)
	}
	b.WriteByte(']')
	return b.Bytes()
}

// stripTrailingComment drops a `# ...` suffix from a TOML line, ignoring
// `#` characters that appear inside basic-string literals (`"..."`). Returns
// the leading content with trailing whitespace trimmed.
func stripTrailingComment(line []byte) []byte {
	inString := false
	for i := 0; i < len(line); i++ {
		c := line[i]
		if c == '\\' && i+1 < len(line) {
			i++ // skip escaped char inside a string literal
			continue
		}
		if c == '"' {
			inString = !inString
			continue
		}
		if c == '#' && !inString {
			return bytes.TrimRight(line[:i], " \t")
		}
	}
	return line
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
		// Preserve TOML's idiomatic blank-line separator between tables.
		// The previous block may have included a trailing blank line we
		// already swallowed; re-add one if the replacement doesn't end
		// with the `\n\n` shape and a real next-table line follows.
		if !bytes.HasSuffix(replacement, []byte("\n")) {
			out.WriteByte('\n')
		}
		if !bytes.HasSuffix(replacement, []byte("\n\n")) {
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
