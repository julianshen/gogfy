# Language support

gogfy uses tree-sitter for AST extraction. A language is supported when (a) someone publishes a maintained tree-sitter grammar, (b) that grammar's repo carries a `bindings/go/` directory with cgo wrappers, and (c) the published Go module compiles cleanly. Most graphify-listed languages clear all three; some don't.

## Supported (24)

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

## Not supported (graphify lists; we don't)

Each entry below has been **probed** with `go get`. Status reflects what the upstream fork currently ships (as of v0.1.x).

| Language | Block | What it would take |
|----------|-------|--------------------|
| Swift `.swift` | `alex-pinkus/tree-sitter-swift` ships **no committed `parser.c`** — needs `tree-sitter generate` at install. CGo can't run that. | Maintain a fork that commits the generated parser. ~2 hours setup, then minimal sync. |
| Dart `.dart` | `UserNobody14/tree-sitter-dart` has `parser.c` and module path resolves, but **no `bindings/go/` directory**. | Fork + add a 12-line `bindings/go/binding.go` cgo stub. ~1 hour. |
| Objective-C `.m` `.mm` | `tree-sitter-grammars/tree-sitter-objc` and `jiyee/tree-sitter-objc` ship `parser.c` but **no `bindings/go/`**. | Same as Dart — fork + binding stub. |
| Groovy `.groovy` `.gradle` | `murtaza64/tree-sitter-groovy` ships `parser.c` but **no `bindings/go/`**. | Same as Dart. |
| Elixir `.ex` `.exs` | `elixir-lang/tree-sitter-elixir` declares its module path as `github.com/tree-sitter/tree-sitter-elixir` (a repo that doesn't exist). | Either submit a PR upstream to fix the module path, or fork. |
| Erlang `.erl` `.hrl` | `WhatsApp/tree-sitter-erlang` has the same module-path issue as Elixir. `AbstractMachinesLab/tree-sitter-erlang` is reachable but missing `bindings/go/`. | Fork + binding stub. |
| R `.r` `.R` | `r-lib/tree-sitter-r` has `bindings/go/`, but its `binding.go` doesn't `#include` the external scanner — linker fails on `tree_sitter_r_external_scanner_*` symbols. | Fork + add `#include "../../src/scanner.c"` to the cgo block. ~30 minutes. |
| SQL `.sql` | `DerekStride/tree-sitter-sql` ships `bindings/go/binding.go` but the package doesn't include the generated `parser.c` at the path the binding expects. | Fork + commit `parser.c`. |
| PowerShell `.ps1` | `PowerShell/tree-sitter-PowerShell` is unmaintained; `airbus-cert/tree-sitter-powershell` lacks `bindings/go/`. | Fork (whichever is more current) + add binding. |
| Vue `.vue` | `ikatyang/tree-sitter-vue` and `tree-sitter-grammars/tree-sitter-vue` both lack `bindings/go/`. | Fork + binding stub. |
| Pascal `.pas` `.pp` `.dpr` `.dpk` `.lpr` | `Isopod/tree-sitter-pascal` lacks both `parser.c` and `bindings/go/`. | Fork + generate + binding. ~3 hours. |
| V `.v` | No usable upstream grammar found. | Out of scope until one appears. |

## Three escape hatches (in order of preference)

1. **`go.mod replace` to a working community fork** — zero maintenance when it exists. Used today for Fortran (a benign `replace` works around `stadelmanma/tree-sitter-fortran`'s test-file referencing a non-existent canonical path).
2. **Maintain forks under `julianshen/tree-sitter-<lang>`** — own the bindings/go and committed parser.c. ~1-2 hours per language.
3. **Vendor C grammar sources** — drop `parser.c` + `scanner.c` directly into `internal/extract/grammars/<lang>/`, hand-write a 30-line cgo binding. Worst case for languages where forks die.

## Tier 1 wishlist (next up)

In approximate order of demand vs. effort:

1. **Swift** (Hatch 2 — fork-and-commit-parser)
2. **Dart** (Hatch 2 — fork-and-add-binding)
3. **Elixir** (Hatch 1 — go.mod replace once the right path is identified)
4. **R** (Hatch 2 — fork-and-include-scanner; trivial change)

## Adding a language

The path is well-trodden:

1. Get the grammar's `bindings/go/Language()` reachable (one of the three hatches above).
2. Create `internal/extract/<lang>extractor.go` (~50-80 LOC). Follow the `goextractor.go` / `pyextractor.go` template.
3. Add three smoke tests to `internal/extract/multilang_test.go` covering: a declaration, an import, a call.
4. If the language has node kinds for member-access calls (`obj.foo()`), add them to `callTargetName` in `internal/extract/common.go`.
5. Register file extensions in `cmd/gogfy/main.go` `supportedExtensions` and `internal/extract/multilang_test.go` `TestExtractorsMissingFile`.
6. `go test ./...` should pass with >90% coverage on `internal/extract`.

PRs welcome. Each language is its own PR for clean review.
