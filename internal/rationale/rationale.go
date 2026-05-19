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
//
// Keyword may be followed by either `:` (the canonical form `// NOTE:`)
// or whitespace (`// TODO add tests`) — both are common in real source.
var markerRe = regexp.MustCompile(`^\s*(#|//|--|/\*)\s*(NOTE|IMPORTANT|HACK|WHY|RATIONALE|TODO|FIXME|XXX|WARNING)(:|\s)`)

// blockCloseRe strips a trailing `*/` (with optional whitespace) so
// block-comment-style rationale labels don't keep the closer.
var blockCloseRe = regexp.MustCompile(`\s*\*/\s*$`)

// commentLineRe matches a line that's entirely a comment (any of the
// four marker families). Used to skip continuation lines when walking
// forward from a rationale comment to find the next non-comment line.
var commentLineRe = regexp.MustCompile(`^\s*(#|//|--|/\*|\*)`)

// fnDeclPatterns maps a lang tag to a regex that captures the
// function name from a declaration line. Only the high-signal
// languages are covered; for the rest (Java, C#, Kotlin, ...) the
// receiver / return-type clutter makes a regex unreliable and we
// fall back to module-level attribution. The match group name is
// always the function identifier the AST extractor emits as the
// `<lang>:function:<filepath>:<name>` node.
var fnDeclPatterns = map[string]*regexp.Regexp{
	// Go: optional receiver `func (r *T)` before name.
	"go": regexp.MustCompile(`^\s*func\s+(?:\([^)]*\)\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*\(`),
	// Python: optional async, plain def.
	"py": regexp.MustCompile(`^\s*(?:async\s+)?def\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`),
	// Rust: pub / pub(crate) / async modifiers before fn.
	"rust": regexp.MustCompile(`^\s*(?:pub(?:\([^)]*\))?\s+)?(?:async\s+)?fn\s+([A-Za-z_][A-Za-z0-9_]*)\s*[(<]`),
	// JS / TS: function declaration form. The const-arrow form is
	// matched by jsArrowRe below; this regex alone keeps the simple
	// case fast.
	"js": regexp.MustCompile(`^\s*(?:export\s+(?:default\s+)?)?(?:async\s+)?function\s*\*?\s*([A-Za-z_$][A-Za-z0-9_$]*)\s*\(`),
	"ts": regexp.MustCompile(`^\s*(?:export\s+(?:default\s+)?)?(?:async\s+)?function\s*\*?\s*([A-Za-z_$][A-Za-z0-9_$]*)\s*\(`),
}

// jsArrowRe captures the `const name = (args) => …` / `const name =
// function …` form that's idiomatic in modern JS/TS modules. Kept
// separate from fnDeclPatterns so the simple-case regex stays
// readable; both are tried for js/ts files.
var jsArrowRe = regexp.MustCompile(`^\s*(?:export\s+)?(?:const|let|var)\s+([A-Za-z_$][A-Za-z0-9_$]*)\s*=\s*(?:async\s*)?(?:function\b|\([^)]*\)\s*=>|[A-Za-z_$][A-Za-z0-9_$]*\s*=>)`)

// nextFunctionName scans forward from comment line index `from` for
// the nearest non-comment, non-blank line and returns the function
// name if that line declares one in `lang`. Returns "" when no
// function pattern applies (unsupported lang, target line is not a
// declaration, or the comment is followed by something other than a
// function). Falls back to module-level attribution silently —
// attribution is best-effort, not a contract.
func nextFunctionName(lang string, lines []string, from int) string {
	pat := fnDeclPatterns[lang]
	if pat == nil {
		return ""
	}
	for j := from + 1; j < len(lines) && j < from+12; j++ {
		t := strings.TrimSpace(lines[j])
		if t == "" {
			continue
		}
		if commentLineRe.MatchString(lines[j]) {
			continue
		}
		// Decorators (Python `@cache`) sit between the comment and the
		// def line; skip them so attribution still works.
		if lang == "py" && strings.HasPrefix(t, "@") {
			continue
		}
		if m := pat.FindStringSubmatch(lines[j]); m != nil {
			return m[1]
		}
		if (lang == "js" || lang == "ts") && jsArrowRe.MatchString(lines[j]) {
			return jsArrowRe.FindStringSubmatch(lines[j])[1]
		}
		// First non-comment, non-blank, non-decorator line isn't a
		// function declaration — the comment is module-scope.
		return ""
	}
	return ""
}

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
	lines := strings.Split(string(src), "\n")
	for lineno, line := range lines {
		line = strings.TrimRight(line, "\r")
		if !markerRe.MatchString(line) {
			continue
		}
		label := blockCloseRe.ReplaceAllString(strings.TrimSpace(line), "")
		// Truncation marker tells downstream consumers the label was
		// clipped — otherwise a "complete" comment ending mid-word looks
		// like the author wrote it that way.
		if rs := []rune(label); len(rs) > maxLabelRunes {
			label = string(rs[:maxLabelRunes-1]) + "…"
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
		// If the rationale comment immediately precedes a function
		// declaration (after skipping intervening comments / decorators
		// / blank lines), attach to that function instead of the file's
		// module node. This gives "why does this function exist"
		// rationale a precise target; downstream renderers can still
		// walk function → module for file-level views.
		target := moduleID
		if fnName := nextFunctionName(lang, lines, lineno); fnName != "" {
			target = schema.LangID(lang, "function", path+":"+fnName)
		}
		edges = append(edges, schema.Edge{
			Source:     rid,
			Target:     target,
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
