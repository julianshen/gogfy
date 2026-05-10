// Package fence implements the marker-fenced block editor used by
// internal/installer/snippet (HTML-comment markers in CLAUDE.md /
// AGENTS.md) and internal/githook (shell-comment markers in
// `.git/hooks/post-commit`). Both consumers had near-identical
// hand-rolled copies; this package consolidates them.
//
// All functions refuse to operate on mismatched marker pairs (orphan
// start, orphan end, multiple pairs, end-before-start) — guessing the
// region's bounds in those cases is the silent-data-loss hazard the
// snippet writer learned about the hard way.
package fence

import (
	"bytes"
	"fmt"
)

// IndexAll returns every starting index of needle within hay.
func IndexAll(hay, needle []byte) []int {
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

// Replace replaces the content between (and including) start and end with
// rendered. Returns:
//
//   - (updated, true, nil)   — exactly one matched pair was found and replaced.
//   - (existing, false, nil) — no markers present at all (caller may append).
//   - (existing, false, err) — mismatched markers (orphan, multiple pairs,
//     end-before-start); refuse to modify the file.
//
// One trailing newline after the end marker is consumed so repeated
// replacements don't accumulate blank lines.
func Replace(existing, start, end, rendered []byte) ([]byte, bool, error) {
	startIdx, endIdx, err := matchPair(existing, start, end)
	if err != nil {
		return existing, false, err
	}
	if startIdx < 0 {
		return existing, false, nil
	}
	if endIdx < len(existing) && existing[endIdx] == '\n' {
		endIdx++
	}
	var buf bytes.Buffer
	buf.Write(existing[:startIdx])
	buf.Write(rendered)
	buf.Write(existing[endIdx:])
	return buf.Bytes(), true, nil
}

// Strip removes the fenced block (markers and content) from existing.
// Drops one preceding blank-line separator so the seam doesn't leave a
// double-blank gap. Same mismatch policy as Replace.
func Strip(existing, start, end []byte) ([]byte, bool, error) {
	startIdx, endIdx, err := matchPair(existing, start, end)
	if err != nil {
		return existing, false, err
	}
	if startIdx < 0 {
		return existing, false, nil
	}
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

// matchPair locates the (startIdx, endIdx) of a single matched marker
// pair in existing. endIdx is the byte offset *after* the end marker.
//
// Returns:
//   - (startIdx, endIdx, nil) for one matched pair.
//   - (-1, -1, nil) when neither marker is present.
//   - (-1, -1, err) for any mismatched state — the caller MUST surface
//     that error rather than guess.
func matchPair(existing, start, end []byte) (int, int, error) {
	starts := IndexAll(existing, start)
	ends := IndexAll(existing, end)
	if len(starts) == 0 && len(ends) == 0 {
		return -1, -1, nil
	}
	if len(starts) != 1 || len(ends) != 1 {
		return -1, -1, fmt.Errorf("fence: expected exactly one fenced block (found %d start, %d end markers); fix the file by hand and re-run", len(starts), len(ends))
	}
	startIdx, endStart := starts[0], ends[0]
	if endStart < startIdx {
		return -1, -1, fmt.Errorf("fence: end marker appears before start marker — fix the file by hand and re-run")
	}
	return startIdx, endStart + len(end), nil
}
