package extract

import (
	"regexp"

	sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_svelte "github.com/tree-sitter-grammars/tree-sitter-svelte/bindings/go"
)

// SvelteExtractor extracts module nodes plus import edges from Svelte
// (.svelte) component files.
//
// Svelte's grammar extends tree-sitter-html and treats `<script>` content
// as opaque text — it doesn't parse the embedded JS/TS. We could spin up
// a sub-parser to walk the script's AST, but for graph purposes the
// import names are the highest-value signal and they're trivially
// recoverable with a regex scan over the script text. Bringing in the
// full JS grammar for one block kind isn't worth the build/dep cost
// until users ask.
type SvelteExtractor struct{}

func (SvelteExtractor) Extract(path string) (Result, error) {
	return runExtraction(path, tree_sitter_svelte.Language(), "svelte", walkSvelte)
}

// importLineRE matches the common ESM `import` shapes used in Svelte
// `<script>` blocks: `import X from "path"`, `import {x,y} from "path"`,
// `import * as X from "path"`.
var importLineRE = regexp.MustCompile(`(?m)^\s*import\s+[^'"]*?\s*from\s+['"]([^'"]+)['"]\s*;?`)

// reexportLineRE matches `export … from "path"` re-export shapes — common
// in Svelte index/barrel files: `export {x} from "path"`,
// `export * from "path"`, `export * as X from "path"`.
var reexportLineRE = regexp.MustCompile(`(?m)^\s*export\s+[^'"]*?\s*from\s+['"]([^'"]+)['"]\s*;?`)

// bareImportRE matches `import "side-effect-only"` style imports.
var bareImportRE = regexp.MustCompile(`(?m)^\s*import\s+['"]([^'"]+)['"]\s*;?`)

func walkSvelte(cursor *sitter.TreeCursor, src []byte, state *extractState) {
	node := cursor.Node()
	if node.Kind() == "raw_text" {
		// Inside a Svelte <script>/<style>, the body is exposed as a single
		// `raw_text` child. Scan it (not the full `script_element` text,
		// which would also include the surrounding tags and could match
		// import-like substrings inside attribute values).
		text := node.Utf8Text(src)
		for _, m := range importLineRE.FindAllStringSubmatch(text, -1) {
			state.addImport(m[1])
		}
		for _, m := range reexportLineRE.FindAllStringSubmatch(text, -1) {
			state.addImport(m[1])
		}
		for _, m := range bareImportRE.FindAllStringSubmatch(text, -1) {
			state.addImport(m[1])
		}
	}
	walkChildren(cursor, func() { walkSvelte(cursor, src, state) })
}
