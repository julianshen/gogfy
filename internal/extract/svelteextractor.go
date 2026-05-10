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

// importLineRE matches the common ESM import shapes used in Svelte
// `<script>` blocks: `import X from "path"`, `import {x,y} from "path"`,
// `import * as X from "path"`. Single or double quotes; trailing semicolon
// optional. Multi-line imports are handled by scanning the entire script
// text in one pass.
var importLineRE = regexp.MustCompile(`(?m)^\s*import\s+[^'"]*?\s*from\s+['"]([^'"]+)['"]\s*;?`)

// bareImportRE matches `import "side-effect-only"` style imports.
var bareImportRE = regexp.MustCompile(`(?m)^\s*import\s+['"]([^'"]+)['"]\s*;?`)

func walkSvelte(cursor *sitter.TreeCursor, src []byte, state *extractState) {
	node := cursor.Node()
	if node.Kind() == "script_element" {
		// Pull the raw script text and scan for imports — Svelte's grammar
		// keeps the JS body opaque (kind="raw_text"), so we don't have an
		// AST to walk inside the script.
		text := node.Utf8Text(src)
		for _, m := range importLineRE.FindAllStringSubmatch(text, -1) {
			state.addImport(m[1])
		}
		for _, m := range bareImportRE.FindAllStringSubmatch(text, -1) {
			state.addImport(m[1])
		}
	}
	walkChildren(cursor, func() { walkSvelte(cursor, src, state) })
}
