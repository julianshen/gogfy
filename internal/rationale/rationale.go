// Package rationale extracts engineer-intent comments (NOTE/HACK/
// IMPORTANT/etc.) from source files and attaches them to the file's
// module node via `rationale_for` edges. Carries the comment text into
// the graph so downstream renderers (wiki, callflow) can surface the
// design rationale alongside the code that implements it.
//
// Language-agnostic: scans source text for a fixed set of comment-
// marker + keyword combinations. Less precise than per-function
// attribution but covers the highest-signal value (the markers
// themselves) without a per-language AST pass.
package rationale

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/julianshen/gogfy/internal/schema"
)

// maxLabelRunes caps stored label length so a long rationale comment
// doesn't blow up downstream renderers. graphify uses 80 chars; we use
// 256 to match the labels-package convention.
const maxLabelRunes = 256

// markerRe matches a line whose comment body starts with one of the
// known rationale keywords. Comment markers covered: `#` (Python, sh,
// Ruby, etc.), `//` (Go, Java, C-family, JS, etc.), `--` (SQL, Haskell,
// Lua), `/*` (block-comment opener — single-line shapes only).
var markerRe = regexp.MustCompile(`^\s*(#|//|--|/\*)\s*(NOTE|IMPORTANT|HACK|WHY|RATIONALE|TODO|FIXME|XXX|WARNING):`)

// Extract scans src for rationale comments and returns (nodes, edges).
// The edges point from each rationale node to the source file's module
// node so the rationale attaches to the existing graph rather than
// dangling. Empty input produces no output.
func Extract(path string, src []byte) ([]schema.Node, []schema.Edge) {
	if len(src) == 0 {
		return nil, nil
	}
	lang := langFromPath(path)
	moduleID := schema.LangID(lang, "module", path)

	var nodes []schema.Node
	var edges []schema.Edge
	// Splitting on '\n' is intentionally cheap — Windows CR is folded
	// into the trim step inside the loop. tree-sitter wouldn't add
	// value here since we only care about comment-leading line content.
	for lineno, line := range strings.Split(string(src), "\n") {
		line = strings.TrimRight(line, "\r")
		if !markerRe.MatchString(line) {
			continue
		}
		label := strings.TrimSpace(line)
		if rs := []rune(label); len(rs) > maxLabelRunes {
			label = string(rs[:maxLabelRunes])
		}
		lineOneBased := lineno + 1
		rid := schema.LangID(lang, "rationale", fmt.Sprintf("%s:L%d", path, lineOneBased))
		nodes = append(nodes, schema.Node{
			ID:             rid,
			Label:          label,
			SourceFile:     path,
			SourceLocation: fmt.Sprintf("L%d", lineOneBased),
			FileType:       "rationale",
		})
		edges = append(edges, schema.Edge{
			Source:     rid,
			Target:     moduleID,
			Relation:   "rationale_for",
			Confidence: schema.Extracted,
		})
	}
	return nodes, edges
}

// langFromPath returns a stable lang tag for a path's extension. Maps
// common multi-extension languages (.cc → cpp, .h → c, etc.) to match
// the tags emitted by the per-language extractors, so rationale IDs
// don't drift into a parallel "go.go" / "py.py" namespace.
func langFromPath(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == "" {
		return "txt"
	}
	stripped := strings.TrimPrefix(ext, ".")
	switch stripped {
	case "go":
		return "go"
	case "py":
		return "py"
	case "js", "jsx", "mjs", "cjs":
		return "js"
	case "ts", "tsx":
		return "ts"
	case "rs":
		return "rust"
	case "java":
		return "java"
	case "rb":
		return "ruby"
	case "c", "h":
		return "c"
	case "cpp", "cc", "cxx", "hpp", "hxx", "hh":
		return "cpp"
	case "cs":
		return "csharp"
	case "kt", "kts":
		return "kotlin"
	case "scala", "sc":
		return "scala"
	case "php":
		return "php"
	case "lua":
		return "lua"
	case "swift":
		return "swift"
	case "dart":
		return "dart"
	case "ex", "exs":
		return "elixir"
	case "erl", "hrl":
		return "erlang"
	case "hs":
		return "haskell"
	case "ml", "mli":
		return "ocaml"
	case "f", "f90", "f95", "f03", "f08":
		return "fortran"
	case "jl":
		return "julia"
	case "yaml", "yml":
		return "yaml"
	case "toml":
		return "toml"
	case "zig":
		return "zig"
	case "sh", "bash":
		return "bash"
	case "r":
		return "r"
	case "svelte":
		return "svelte"
	case "sql":
		return "sql"
	}
	return stripped
}
