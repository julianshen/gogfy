# gogfy

A Go-native reimplementation of [safishamsi/graphify](https://github.com/safishamsi/graphify) — turn your codebase into a knowledge graph your AI assistant can navigate instead of grepping through files.

```bash
gogfy run .
# graphify-out/
# ├── graph.html       open in any browser
# ├── GRAPH_REPORT.md  god nodes, surprising connections, suggested questions
# └── graph.json       the full graph
```

Then point your agent at it:

```bash
gogfy claude install     # MCP server + CLAUDE.md snippet + post-commit auto-rebuild
```

That's it. 19 supported agent platforms — Claude Code, Cursor, VS Code Copilot Chat, Codex, Gemini CLI, OpenCode, GitHub Copilot CLI, Aider, OpenClaw, Factory Droid, Trae (incl. Trae CN), Hermes, Kiro, Pi, Google Antigravity, Kimi CLI, Qwen Code, Kilo Code, plus anything that reads `AGENTS.md` — now read the graph before answering questions about your repo.

---

## Why

Big repos are hard. Grep finds substrings; AI assistants exhaust their context reading file after file looking for the right one. A code graph collapses the search: god nodes tell you where the hubs are, the report names the surprising connections, and the MCP tools let an agent ask structural questions (`what connects A to B?`) without blindly reading source.

gogfy is the same idea as graphify, written in Go so it ships as one static binary with no Python/pip/uv dependencies and is faster on large repos.

---

## Install

```bash
go install github.com/julianshen/gogfy/cmd/gogfy@latest
```

Or grab a binary from the [releases page](https://github.com/julianshen/gogfy/releases) once cut.

Requires Go 1.24+ to build from source. Code extraction is local-only via tree-sitter (cgo) — no API calls, no telemetry.

---

## Quick start

```bash
# 1. Build a graph
gogfy run .

# 2. Wire it up for your agent (any of)
gogfy claude install          # Claude Code
gogfy codex install           # OpenAI Codex CLI
gogfy cursor install          # Cursor
gogfy vscode install          # VS Code Copilot Chat
gogfy gemini install          # Gemini CLI
gogfy opencode install        # OpenCode
gogfy copilot install         # GitHub Copilot CLI
gogfy aider install           # Aider
gogfy claw install            # OpenClaw
gogfy droid install           # Factory Droid
gogfy trae install            # Trae  (or `gogfy trae-cn install` for Trae CN)
gogfy hermes install          # Hermes
gogfy kiro install            # Kiro
gogfy pi install              # Pi coding agent
gogfy antigravity install     # Google Antigravity
gogfy kimi install            # Moonshot Kimi CLI
gogfy qwen install            # Qwen Code
gogfy kilocode install        # Kilo Code

# 3. (Optional) auto-rebuild on every commit
# Already done by the combo install above; otherwise:
gogfy hook install
```

Now your agent has three new MCP tools:

- `gogfy_god_nodes` — list the most-connected concepts
- `gogfy_explain` — show a node's metadata and neighbors
- `gogfy_query` — find nodes by label/ID/file substring
- `gogfy_path` — shortest path between two nodes

Plus a `gogfy://report` resource pointing at `GRAPH_REPORT.md`, which the agent reads before answering codebase questions (the snippet writer puts a pointer in `CLAUDE.md` / `AGENTS.md` / `.cursorrules`).

---

## Languages and document formats supported

**Code (29 languages)** — AST-based via tree-sitter:

Go · Python · JavaScript · TypeScript · Java · C · C++ · Rust · Ruby · YAML · TOML · Kotlin · Scala · PHP · Lua · Zig · Julia · Bash · C# · Haskell · OCaml · Svelte · Fortran · Elixir · Dart · Swift · R · Erlang

**Documents (8, growing)** — pure-Go, no Python or LLM dependency:

- Markdown (`.md` / `.mdx` / `.markdown`) via [goldmark](https://github.com/yuin/goldmark)
- HTML (`.html` / `.htm`) via [golang.org/x/net/html](https://pkg.go.dev/golang.org/x/net/html)
- reStructuredText (`.rst`) — heuristic adornment-line detection + inline-target regex
- Plain text (`.txt`) — module + URL-extraction regex
- Word (`.docx`) — `archive/zip` + `encoding/xml` over `word/document.xml`, hyperlinks resolved via `word/_rels/document.xml.rels`
- Excel (`.xlsx`) — `archive/zip` + `encoding/xml` over `xl/workbook.xml` and per-sheet rels; sheets become section nodes, external hyperlinks become references
- PDF (`.pdf`) — text via [ledongthuc/pdf](https://github.com/ledongthuc/pdf); module label from `/Info /Title` metadata, references via URL regex
- PowerPoint (`.pptx`) — `archive/zip` + `encoding/xml`; slides become section nodes (title placeholder or "Slide N" fallback), `<a:hlinkClick>` becomes references

All eight emit a consistent module + reference-edges shape (most also emit section nodes) so cross-format links compose seamlessly.

File extensions: `.go .py .js .jsx .mjs .cjs .ts .tsx .java .c .h .cpp .cc .cxx .hpp .hxx .hh .rs .rb .yaml .yml .toml .kt .kts .scala .sc .php .lua .zig .jl .sh .bash .cs .hs .ml .mli .svelte .f .f90 .f95 .f03 .f08 .ex .exs .dart .swift .r .R .erl .hrl .md .mdx .markdown .html .htm .rst .txt .docx .xlsx .pdf .pptx`

---

## Subcommand reference

### Build / explore the graph

```bash
gogfy run <root>                          # full pipeline → graphify-out/
gogfy run . --update                      # incremental (only changed files)
gogfy run . --cluster-only                # re-cluster without re-extracting
gogfy run . --no-viz                      # skip graph.html
gogfy run . --graphml --cypher            # also emit Gephi/yEd + Neo4j scripts
gogfy run . --directed                    # arrowheads on graph.html
gogfy watch <root>                        # rebuild on file changes (fsnotify)
gogfy validate <graph.json>               # schema check
gogfy report <graph.json>                 # render GRAPH_REPORT.md to stdout
gogfy path <source> <target>              # shortest connectivity path between two nodes
gogfy merge-graphs a.json b.json --out merged.json    # union per-repo graphs
```

### Agent platform integration

```bash
# Combo install (MCP config + docs snippet + post-commit hook):
gogfy <claude|codex|cursor|vscode|gemini|opencode|copilot|aider|claw|droid|trae|trae-cn|hermes|kiro|pi|antigravity|kimi|qwen|kilocode> install
gogfy <claude|codex|cursor|vscode|gemini|opencode|copilot|aider|claw|droid|trae|trae-cn|hermes|kiro|pi|antigravity|kimi|qwen|kilocode> uninstall

# Or step-by-step:
gogfy install --platform <platform>       # just the MCP config
gogfy install-instructions --file CLAUDE.md   # just the docs snippet
gogfy hook install                            # just the post-commit + post-checkout hooks
gogfy hook install-merge-driver               # auto-union graph.json on `git merge`
gogfy hook status                             # show what's wired (hooks + merge driver)

# MCP server (stdio):
gogfy serve --graph graphify-out/graph.json
```

### Files written per platform

| Platform | MCP config | Docs snippet target |
|----------|------------|---------------------|
| `claude` | `.mcp.json` | `CLAUDE.md` |
| `codex`  | `.codex/config.toml` | `AGENTS.md` |
| `cursor` | `.cursor/mcp.json` | `.cursorrules` |
| `vscode` | `.vscode/mcp.json` (under `servers`) | `AGENTS.md` |
| `gemini` | `.gemini/settings.json` | `GEMINI.md` |
| `opencode` | `opencode.json` (under `mcp`, flattened `{type,command[]}`) | `AGENTS.md` |
| `copilot` | `.copilot/mcp.json` | `AGENTS.md` |
| `aider` | `.aider/mcp.json` | `AGENTS.md` |
| `claw` | `.openclaw/mcp.json` | `AGENTS.md` |
| `droid` | `.factory/mcp.json` | `AGENTS.md` |
| `trae` | `.trae/mcp.json` | `AGENTS.md` |
| `trae-cn` | `.trae-cn/mcp.json` | `AGENTS.md` |
| `hermes` | `.hermes/mcp.json` | `AGENTS.md` |
| `kiro` | `.kiro/mcp.json` | `AGENTS.md` |
| `pi` | `.pi/mcp.json` | `AGENTS.md` |
| `antigravity` | `.antigravity/mcp.json` | `AGENTS.md` |
| `kimi` | `.kimi/settings.json` | `AGENTS.md` |
| `qwen` | `.qwen/settings.json` | `QWEN.md` |
| `kilocode` | `.kilocode/mcp.json` | `AGENTS.md` |

> **Best-effort note**: some platforms in the registry don't yet have officially documented workspace-relative MCP server config support (e.g., Aider primarily uses `.aider.conf.yml`; Copilot CLI's MCP config historically lives at user level). gogfy writes the conventional `.<platform>/` location each tool's config-dir suggests — verify against your platform's current docs. Platform names and config paths track [safishamsi/graphify](https://github.com/safishamsi/graphify)'s per-platform install paths.

---

## What's in `GRAPH_REPORT.md`

- **God nodes** — most-connected nodes (capped at top 10)
- **Surprising connections** — cross-community edges, ranked by inverse expectedness so leaf-to-leaf links rank above hub-involving ones (capped at top 10)
- **Confidence summary** — counts of `EXTRACTED` / `INFERRED` / `AMBIGUOUS` edges
- **Suggested questions** — templated from the god nodes and community pairs (capped at 5)

The report is what `gogfy install-instructions` tells the agent to read first.

---

## Ignoring files

`.graphifyignore` at the repo root, same syntax as `.gitignore` (including `!` negation):

```
node_modules/
dist/
*.generated.go

# Only index src/, ignore everything else
*
!src/
!src/**
```

---

## Team workflow

1. One person runs `gogfy run .` and commits `graphify-out/`.
2. Everyone else pulls — their assistant reads the graph immediately.
3. `gogfy hook install` adds post-commit + post-checkout hooks so the graph stays in sync without anyone thinking about it. Hook script is fenced so it coexists with other tools' hook content.
4. Two devs committed parallel graphs? Run `gogfy hook install-merge-driver` once — it registers a graph.json union driver in `.git/config` plus a matching `merge=gogfy` rule in `.gitattributes`. From then on, `git merge` auto-unions parallel graphs instead of leaving conflict markers.

`graphify-out/.gographify-cache` (the hash-based incremental cache) can be `.gitignore`'d to keep repos lean; the rest of `graphify-out/` is meant to be committed.

---

## Status & parity

Code-graph parity with graphify: ✅ complete (extraction, resolve, cluster, analyze, report, export, MCP server, installers, hooks, merge, path, cluster-only).

**Not implemented** (graphify has these; gogfy doesn't):
- Document extraction — Markdown / PDF / images / video transcription. All require LLM API integration.
- Long-tail languages where Go bindings aren't published (V, SQL, Dart, Obj-C, Groovy, Elixir, Erlang, R, PowerShell, Fortran, Pascal).
- Per-platform installers for Aider / Trae / Kimi / Kiro / Pi / Antigravity / Factory Droid (most read `AGENTS.md`, so `gogfy install-instructions --file AGENTS.md` covers them at the agent-rules layer).

---

## Project layout

```
internal/
├── extract/    per-language tree-sitter extractors (one file per language)
├── resolve/    upgrade <lang>:call:<name> targets to real function nodes
├── cluster/    Leiden community detection
├── analyze/    god nodes, surprising connections, confidence summary
├── report/     Markdown rendering
├── export/     JSON / HTML / GraphML / Cypher exporters
├── graph/      builder + Graph type
├── schema/     Node / Edge / Confidence types
├── cache/      hash-based incremental cache
├── detect/     file collection with .graphifyignore
├── watch/      fsnotify-backed continuous mode
├── serve/      MCP-over-stdio server
├── installer/  per-platform JSON/TOML config writers + snippet
├── githook/    post-commit + post-checkout hook installer
├── merge/      graph union for cross-repo aggregation
├── fence/      shared fenced-block editor (snippet + hook share it)
├── fsutil/     atomic write + tolerant read helpers
└── security/   path-traversal guards
cmd/gogfy/      CLI entry point
```

See [SPEC.md](SPEC.md) for the original scoping doc and [AGENTS.md](AGENTS.md) for the contributor TDD rules.

---

## License

(no license file yet — add one before going public)
