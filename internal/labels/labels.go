// Package labels persists per-community human-readable names to
// `.graphify_labels.json` so the wiki layer has stable non-numeric names.
// Heuristic-driven (no LLM), but the file is treated as user-editable —
// re-runs preserve hand-edits unless callers opt into overwrite.
package labels

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"unicode"

	"github.com/julianshen/gogfy/internal/fsutil"
	"github.com/julianshen/gogfy/internal/schema"
)

// DefaultFilename is the on-disk name the wiki CLI auto-loads from a
// graph directory. Exported so callers don't drift from this literal.
const DefaultFilename = ".graphify_labels.json"

// maxLabelRunes caps stored label length. graphify upstream uses 256 chars
// — kept identical so cross-tool consumers can rely on the bound.
const maxLabelRunes = 256

// Generate returns a community-ID → human-readable-label map. The label
// for each community is the highest-degree member node's label, with ties
// broken alphabetically. Nodes without a Community are ignored.
func Generate(nodes []schema.Node, edges []schema.Edge) map[string]string {
	degree := map[string]int{}
	for _, e := range edges {
		degree[e.Source]++
		degree[e.Target]++
	}

	type pick struct {
		label  string
		degree int
	}
	best := map[string]pick{}
	for _, n := range nodes {
		if n.Community == "" {
			continue
		}
		d := degree[n.ID]
		cur, seen := best[n.Community]
		if !seen || d > cur.degree || (d == cur.degree && n.Label < cur.label) {
			best[n.Community] = pick{label: n.Label, degree: d}
		}
	}
	out := make(map[string]string, len(best))
	for cid, p := range best {
		out[cid] = sanitize(p.label)
	}
	return out
}

// sanitize strips control chars (newlines, NULs, tabs, etc.) and caps the
// result at maxLabelRunes. An empty result after stripping returns "" — the
// wiki layer falls back to "Community <cid>" when a label is missing or
// blank, so this stays compatible.
func sanitize(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if unicode.IsControl(r) {
			continue
		}
		b.WriteRune(r)
	}
	out := strings.TrimSpace(b.String())
	if rs := []rune(out); len(rs) > maxLabelRunes {
		out = string(rs[:maxLabelRunes])
	}
	return out
}

// Save writes labels to path. encoding/json sorts map[string]T keys, and
// fsutil.WriteFileAtomic guarantees crash-safe replacement.
func Save(path string, labels map[string]string) error {
	data, err := json.MarshalIndent(labels, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(path, append(data, '\n'), 0644)
}

// Load reads `.graphify_labels.json` at path. Missing-file errors wrap
// os.ErrNotExist (via the underlying os.ReadFile error) so callers can
// errors.Is-detect first-run vs corruption.
func Load(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("labels: read %s: %w", path, err)
	}
	var out map[string]string
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("labels: parse %s: %w", path, err)
	}
	// Re-apply sanitize so hand-edited labels also honor the 256-rune /
	// no-control-char contract that the package doc promises to all
	// cross-tool consumers (downstream wiki/mermaid renderers).
	for k, v := range out {
		out[k] = sanitize(v)
	}
	return out, nil
}
