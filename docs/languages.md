# Language support

gogfy uses tree-sitter for AST extraction. A language is supported when (a) someone publishes a maintained tree-sitter grammar, (b) that grammar's repo carries a `bindings/go/` directory with cgo wrappers, and (c) the published Go module compiles cleanly. Most graphify-listed languages clear all three; some don't.

## Supported (27 code + 8 document formats)

| Language | Extensions | Grammar source |
|----------|------------|----------------|
| Go | `.go` | `github.com/tree-sitter/tree-sitter-go` |
| Python | `.py` | `github.com/tree-sitter/tree-sitter-python` |
| JavaScript | `.js` `.jsx` `.mjs` `.cjs` | `github.com/tree-sitter/tree-sitter-javascript` |
| TypeScript | `.ts` `.tsx` | `github.com/tree-sitter/tree-sitter-typescript` |
| Java | `.java` | `github.com/tree-sitter/tree-sitter-java` |
| C | `.c` `.h` | `github.com/tree-sitter/tree-sitter-c` |
| C++ | `.cpp` `.cc` `.cxx` `.hpp` `.hxx` `.hh` | `github.com/tree-sitter/tree-sitter-cpp` |
| Rust | `.rs` | `github.com/tree-sitter/tree-sitter-rust` |
| Ruby | `.rb` | `github.com/tree-sitter/tree-sitter-ruby` |
| Kotlin | `.kt` `.kts` | `github.com/tree-sitter-grammars/tree-sitter-kotlin` |
| Scala | `.scala` `.sc` | `github.com/tree-sitter/tree-sitter-scala` |
| PHP | `.php` | `github.com/tree-sitter/tree-sitter-php` |
| Lua | `.lua` | `github.com/tree-sitter-grammars/tree-sitter-lua` |
| Zig | `.zig` | `github.com/tree-sitter-grammars/tree-sitter-zig` |
| Julia | `.jl` | `github.com/tree-sitter/tree-sitter-julia` |
| Bash | `.sh` `.bash` | `github.com/tree-sitter/tree-sitter-bash` |
| YAML | `.yaml` `.yml` | `github.com/tree-sitter-grammars/tree-sitter-yaml` |
| TOML | `.toml` | `github.com/tree-sitter-grammars/tree-sitter-toml` |
| C# | `.cs` | `github.com/tree-sitter/tree-sitter-c-sharp` |
| Haskell | `.hs` | `github.com/tree-sitter/tree-sitter-haskell` |
| OCaml | `.ml` `.mli` | `github.com/tree-sitter/tree-sitter-ocaml` (uses `LanguageOCaml()` for `.ml`, `LanguageOCamlInterface()` for `.mli`) |
| Svelte | `.svelte` | `github.com/tree-sitter-grammars/tree-sitter-svelte` (script body scanned with regex; tree-sitter-svelte treats it as opaque text) |
| Fortran | `.f` `.f90` `.f95` `.f03` `.f08` | `github.com/stadelmanma/tree-sitter-fortran` |
| Elixir | `.ex` `.exs` | `github.com/elixir-lang/tree-sitter-elixir` (via go.mod `replace` from the declared `github.com/tree-sitter/tree-sitter-elixir` path, which is unmaintained) |
| Dart | `.dart` | `github.com/UserNobody14/tree-sitter-dart` |
| Swift | `.swift` | `github.com/julianshen/tree-sitter-swift` (fork of `alex-pinkus/tree-sitter-swift` with `parser.c` committed — upstream's `.gitignore` excludes the generated artifact) |

### Document formats

| Format | Extensions | Approach |
|--------|------------|----------|
| Markdown | `.md` `.mdx` `.markdown` | Pure-Go via [goldmark](https://github.com/yuin/goldmark). Emits a module node per file (label = first H1 or basename), one section node per H1/H2/H3, and `references`-relation edges for every link. No Python, no LLM, no cgo. |
| HTML | `.html` `.htm` | Pure-Go via `golang.org/x/net/html`. Same module + section + reference schema as Markdown so cross-format graphs compose. Module label prefers `<title>` over `<h1>` over basename. Skips `href="#fragment"` self-links. |
| reStructuredText | `.rst` | Heuristic. Section detection via canonical RST adornment-line pattern (line N+1 same length as line N, composed of one of `= - ~ ^ ' " * + # : .`). Heading levels inferred by first-appearance order of the adornment character. Inline targets `\`text <url>\`_` plus bare URLs in prose extracted as references. No docutils dep — Python-free. |
| Plain text | `.txt` | Module node only; references extracted via URL regex. Trailing sentence punctuation (`.`, `,`, `)`, `;`, `:`, `!`, `?`) stripped from URLs. Useful for getting README-adjacent files (CHANGELOG, NOTES, LICENSE) into the graph. |
| Word | `.docx` | Pure-Go via `archive/zip` + `encoding/xml` (no third-party docx library). Reads `word/document.xml` for paragraphs and `word/_rels/document.xml.rels` for hyperlink targets. Module label prefers `Title`-style paragraph over first `Heading1` over basename. Heading1/2/3 → section nodes. `<w:hyperlink r:id="…">` → reference edge with the URL resolved through the rels map; anchor-only intra-document links are skipped. |
| Excel | `.xlsx` | Pure-Go via `archive/zip` + `encoding/xml` (no `excelize` dep). Workbook → module node (label = basename). Each sheet listed in `xl/workbook.xml` → section node. External hyperlinks (`<hyperlink r:id="…"/>` resolved through each sheet's `xl/worksheets/_rels/sheetN.xml.rels`) → reference edges sourced from their owning sheet section. Cell content isn't extracted — sheet names + outbound links are the high-signal extracts; cell text is mostly numeric/tabular noise. |
| PDF | `.pdf` | Pure-Go via `github.com/ledongthuc/pdf` (no cgo). Module label prefers PDF `/Info /Title` metadata over basename. References extracted via URL regex over the page-concatenated plain text. Section nodes are not emitted in the v1 — PDFs without explicit outlines have no reliable structural markers, and font-size heading heuristics are expensive and false-positive-prone. Encrypted or pathologically-encoded PDFs degrade to a bare module node rather than failing the run. |
| PowerPoint | `.pptx` | Pure-Go via `archive/zip` + `encoding/xml` (no third-party pptx lib). Presentation → module node (label = basename). Each slide listed in `ppt/presentation.xml` → section node, labeled with the title-placeholder text (`<p:ph type="title"/>` or `"ctrTitle"`) or `"Slide N"` fallback. External hyperlinks (`<a:hlinkClick r:id="…"/>` resolved through each slide's `ppt/slides/_rels/slideN.xml.rels`) → reference edges sourced from their owning slide section. |

## Not supported (graphify lists; we don't)

Each entry below has been **probed** with `go get`. Status reflects what the upstream fork currently ships at the time of probe — third-party fork state changes over time, so the specific blockers below may move; the table records the gap, not a permanent verdict.

| Language | Block | What it would take |
|----------|-------|--------------------|
| Objective-C `.m` `.mm` | `tree-sitter-grammars/tree-sitter-objc` and `jiyee/tree-sitter-objc` ship `parser.c` but **no `bindings/go/`**. | Same as Dart — fork + binding stub. |
| Groovy `.groovy` `.gradle` | `murtaza64/tree-sitter-groovy` ships `parser.c` but **no `bindings/go/`**. | Same as Dart. |
| Erlang `.erl` `.hrl` | `WhatsApp/tree-sitter-erlang` has the module-path issue + a missing-scanner `binding.go` (same shape as R). `AbstractMachinesLab/tree-sitter-erlang` lacks `bindings/go/`. | Fork + scanner include + binding stub. |
| R `.r` `.R` | `r-lib/tree-sitter-r` has `bindings/go/`, but its `binding.go` doesn't `#include` the external scanner — linker fails on `tree_sitter_r_external_scanner_*` symbols. | Fork + add `#include "../../src/scanner.c"` to the cgo block. ~30 minutes. |
| SQL `.sql` | `DerekStride/tree-sitter-sql` ships `bindings/go/binding.go` but the package doesn't include the generated `parser.c` at the path the binding expects. | Fork + commit `parser.c`. |
| PowerShell `.ps1` | `PowerShell/tree-sitter-PowerShell` is unmaintained; `airbus-cert/tree-sitter-powershell` lacks `bindings/go/`. | Fork (whichever is more current) + add binding. |
| Vue `.vue` | `ikatyang/tree-sitter-vue` and `tree-sitter-grammars/tree-sitter-vue` both lack `bindings/go/`. | Fork + binding stub. |
| Pascal `.pas` `.pp` `.dpr` `.dpk` `.lpr` | `Isopod/tree-sitter-pascal` lacks both `parser.c` and `bindings/go/`. | Fork + generate + binding. ~3 hours. |
| V `.v` | No usable upstream grammar found. | Out of scope until one appears. |

## Three escape hatches (in order of preference)

1. **`go.mod replace` to a working community fork** — zero maintenance when it exists. Used today for Fortran (a benign `replace` works around `stadelmanma/tree-sitter-fortran`'s test-file referencing a non-existent canonical path).
2. **Maintain forks under `julianshen/tree-sitter-<lang>`** — own the bindings/go and committed parser.c. ~1-2 hours per language.
3. **Vendor C grammar sources** — drop `parser.c` + `scanner.c` directly into `internal/extract/grammars/<lang>/`, hand-write a 30-line cgo binding. Worst case for languages where forks die. **Cost: tree-sitter `parser.c` files are 1-4 MB of generated C per grammar. Vendoring 5+ languages this way meaningfully inflates the repo.** Reserve for languages where (a) the upstream is dead and (b) the language is high-value enough to justify the bloat.

## Tier 1 wishlist (next up)

**Code grammars** — each needs a fork repo under `julianshen/tree-sitter-<lang>`. Swift is the precedent:

1. **R** (Hatch 2 — fork-and-include-scanner; trivial change)
2. **Erlang** (Hatch 2 — fork + scanner include + binding stub)
3. **Objective-C** (Hatch 2 — fork + add binding stub)
4. **Groovy** (Hatch 2 — fork + add binding stub)

**Document formats** — building Go-native, no Python (no markitdown dep). Markdown / HTML / RST / plain-text shipped; following the same shape:

1. **Images** — Tesseract OCR via cgo. Opt-in build tag; not core.
2. **Audio / video** — `whisper.cpp` via cgo. Opt-in build tag.

## Adding a language

The path is well-trodden:

1. Get the grammar's `bindings/go/Language()` reachable (one of the three hatches above).
2. Create `internal/extract/<lang>extractor.go` (~50-80 LOC). Follow the `goextractor.go` / `pyextractor.go` template.
3. Add three smoke tests to `internal/extract/multilang_test.go` covering: a declaration, an import, a call.
4. If the language has node kinds for member-access calls (`obj.foo()`), add them to `callTargetName` in `internal/extract/common.go`.
5. Register file extensions in `cmd/gogfy/main.go` `supportedExtensions` and `internal/extract/multilang_test.go` `TestExtractorsMissingFile`.
6. `go test ./...` should pass with >90% coverage on `internal/extract`.

PRs welcome. Each language is its own PR for clean review.
