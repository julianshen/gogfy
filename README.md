# gogfy

A Go-native reimplementation of [safishamsi/graphify](https://github.com/safishamsi/graphify) — turn your codebase (and docs, PDFs, web pages, even YouTube talks) into a knowledge graph your AI assistant can navigate instead of grepping through files.

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

gogfy is the same idea as graphify, written in Go so it ships as one static binary with no Python/pip/uv dependencies and is faster on large repos. Code extraction is local-only (tree-sitter, no API calls); the optional semantic, transcription, and ingestion features add LLM backends when you opt in.

---

## Install

```bash
go install github.com/julianshen/gogfy/cmd/gogfy@latest
```

Or grab a binary from the [releases page](https://github.com/julianshen/gogfy/releases) once cut.

Requires Go 1.25+ to build from source (tree-sitter uses cgo). Code extraction is local-only — no API calls, no telemetry. LLM features (`--semantic`, transcription, dedup tiebreaker) are opt-in and only fire when you pass a backend flag.

---

## Quick start

```bash
# 1. Build a graph (AST-only — no API key needed)
gogfy run .

# 2. Wire it up for your agent (any of)
gogfy claude install          # Claude Code
gogfy cursor install          # Cursor
gogfy vscode install          # VS Code Copilot Chat
gogfy codex install           # OpenAI Codex CLI
gogfy gemini install          # Gemini CLI
# … plus opencode, copilot, aider, claw, droid, trae, trae-cn,
#   hermes, kiro, pi, antigravity, kimi, qwen, kilocode

# 3. (Optional) auto-rebuild on every commit — already done by the
#    combo install above; otherwise:
gogfy hook install
```

Now your agent has these MCP tools:

| Tool | Purpose |
|------|---------|
| `gogfy_god_nodes` | most-connected nodes (the project's hubs) |
| `gogfy_explain` | a node's metadata + neighbors |
| `gogfy_query` | find nodes by label / ID / file substring |
| `gogfy_path` | shortest path between two nodes |
| `gogfy_get_neighbors` | direct neighbors of a node |
| `gogfy_get_community` | all members of a community |
| `gogfy_traverse` | BFS from a node up to N hops |
| `gogfy_impact` | reverse-reachability: what breaks if you change a node |
| `gogfy_repomap` | ranked, file-grouped map of the most important symbols (PageRank; optionally focused) |
| `gogfy_graph_stats` | corpus counts + breakdowns |

…plus six MCP **resources** the agent can read directly:
`gogfy://report` (the markdown report), `gogfy://stats`, `gogfy://god-nodes`, `gogfy://surprising-links`, `gogfy://questions`, `gogfy://audit`.

The snippet writer puts a pointer to the report in `CLAUDE.md` / `AGENTS.md` / `.cursorrules` so the agent reads it before answering codebase questions.

---

## Languages and formats

**Code (30 languages)** — AST-based via tree-sitter, fully local:

Go · Python · JavaScript · TypeScript / TSX · Java · C · C++ · C# · Rust · Ruby · Kotlin · Scala · PHP · Lua · Zig · Julia · Bash · Haskell · OCaml · Svelte · Fortran · Elixir · Dart · Swift · R · Erlang · plus data/markup: YAML · TOML · Markdown · HTML

**Documents & binaries** — pure-Go, no Python:

- Markdown (`.md`/`.mdx`/`.markdown`), HTML, reStructuredText (`.rst`), plain text (`.txt`)
- Word (`.docx`), Excel (`.xlsx` — sheets → tables → columns), PowerPoint (`.pptx`), PDF (`.pdf`)
- Google Workspace shortcuts (`.gdoc`/`.gsheet`/`.gslides`) — emits a document node + the linked Drive URL
- Image binaries (PNG/JPEG/GIF/WebP) when ingested by URL — fed to the vision LLM path

Minified / generated bundles (`*.min.js`, or any file with a line over 5000 chars) are auto-skipped — they'd otherwise flood the graph with garbage symbols.

---

## Semantic extraction (optional, LLM-backed)

By default gogfy extracts code structure with zero API calls. Add `--semantic` to also extract entities and relationships from prose, PDFs, and images via an LLM:

```bash
gogfy run . --semantic --backend anthropic        # needs ANTHROPIC_API_KEY
gogfy run . --semantic --backend openai            # OPENAI_API_KEY
gogfy run . --semantic --backend gemini            # GEMINI_API_KEY
gogfy run . --semantic --backend kimi              # MOONSHOT_API_KEY
gogfy run . --semantic --backend ollama            # local, free
gogfy run . --semantic --backend bedrock           # AWS env chain (pure-Go SigV4)
```

**Cost control** is built in:

```bash
gogfy run . --semantic --backend anthropic --dry-run        # estimate cost, then exit
gogfy run . --semantic --backend anthropic --max-cost-usd 1 # refuse to start if estimate exceeds cap
```

Results are cached per-file (`graphify-out/.gographify-cache/semantic/`) so re-runs only pay for changed files. Inspect or prune the cache:

```bash
gogfy cache stats                          # entries, size, oldest/newest
gogfy cache clear --older-than 168h        # drop entries older than a week
gogfy cache clear --all
```

**Transcription** — turn audio/video into text before the semantic pass:

```bash
gogfy run . --semantic --transcribe-backend whisper   # OPENAI_API_KEY
```

---

## URL ingestion

Pull a web resource into the corpus as a sidecar that the next `gogfy run` picks up:

```bash
gogfy ingest https://example.com/post              # HTML → markdown (readability-narrowed)
gogfy add    https://arxiv.org/abs/2401.12345      # arXiv → PDF (alias for ingest)
gogfy ingest https://github.com/owner/repo          # GitHub → README
gogfy ingest https://youtube.com/watch?v=… --audio  # YouTube → audio → transcribe
```

Routing is automatic: tweets/X, arXiv, GitHub, YouTube (oembed metadata, or `--audio` for the audio stream via pure-Go `kkdai/youtube`), PDFs (magic-bytes detected), image binaries, and generic HTML. SSRF-guarded throughout.

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
gogfy run . --wiki --tree                 # also emit wiki/ + tree.html
gogfy run . --follow-symlinks             # descend in-root symlinked dirs (cycle-safe)
gogfy clone <git-url>                     # pure-Go git clone, then run (--no-run to skip)
gogfy watch <root>                        # rebuild on file changes (fsnotify)
gogfy validate <graph.json>               # schema check
gogfy report <graph.json>                 # render GRAPH_REPORT.md to stdout
gogfy path <source> <target>              # shortest connectivity path
gogfy diff <old.json> <new.json>          # what changed between two runs
gogfy manifest <root> [--diff]            # mtime+SHA snapshot / changed-file list
gogfy merge-graphs a.json b.json --out merged.json   # union per-repo graphs
gogfy build-merge prior.json next.json    # incremental merge with deleted-file prune
gogfy benchmark <graph.json>              # token-reduction measurement
```

### Artifacts from an existing graph

```bash
gogfy wiki      graphify-out/graph.json   # agent-crawlable per-community markdown
gogfy site      graphify-out/graph.json   # self-contained static HTML site + search
gogfy tree      graphify-out/graph.json   # D3 collapsible filesystem tree
gogfy callflow  graphify-out/graph.json   # Mermaid architecture diagram (labels + key insights)
gogfy obsidian  graphify-out/graph.json   # Obsidian vault (.md per node + wikilinks)
gogfy svg       graphify-out/graph.json   # static force-directed SVG (no JS)
gogfy labels    graphify-out/graph.json   # generate/edit community labels
gogfy neo4j-push graphify-out/graph.json --uri neo4j://localhost:7687   # direct Bolt push
```

### CI integration

```bash
# Fail the build when the graph regresses past thresholds. Markdown
# summary on stdout (pipe to `gh pr comment`); non-zero exit on breach.
gogfy ci base.json head.json \
  --max-removed-nodes 0 \
  --max-removed-edges 10 \
  --max-new-ambiguous 5
```

### Multi-repo (global graph)

```bash
gogfy global add graphify-out/graph.json --as myrepo
gogfy global list
gogfy global path <source> <target>
```

### Agent platform integration

```bash
# Combo install (MCP config + docs snippet + post-commit hook):
gogfy <platform> install
gogfy <platform> uninstall              # add --purge to also remove snippet + hook

# Or step-by-step:
gogfy install --platform <platform>          # just the MCP config
gogfy install-instructions --file CLAUDE.md  # just the docs snippet
gogfy hook install                           # post-commit + post-checkout hooks
gogfy hook install-merge-driver              # auto-union graph.json on git merge
gogfy hook status                            # show what's wired

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
| `opencode` | `opencode.json` (under `mcp`) | `AGENTS.md` |
| `copilot` | `.github/mcp.json` | `AGENTS.md` |
| `aider` | `.aider/mcp.json` | `AGENTS.md` |
| `claw` | `.openclaw/mcp.json` | `AGENTS.md` |
| `droid` | `.factory/mcp.json` | `AGENTS.md` |
| `trae` / `trae-cn` | `.trae/mcp.json` / `.trae-cn/mcp.json` | `AGENTS.md` |
| `hermes` | `.hermes/mcp.json` | `AGENTS.md` |
| `kiro` | `.kiro/mcp.json` | `AGENTS.md` |
| `pi` | `.pi/mcp.json` | `AGENTS.md` |
| `antigravity` | `.antigravity/mcp.json` | `AGENTS.md` |
| `kimi` | `.kimi/settings.json` | `AGENTS.md` |
| `qwen` | `.qwen/settings.json` | `QWEN.md` |
| `kilocode` | `.kilocode/mcp.json` | `AGENTS.md` |

> **Best-effort note**: some platforms don't yet document workspace-relative MCP config (e.g. Aider primarily uses `.aider.conf.yml`; Copilot CLI's MCP config historically lives at user level). gogfy writes the conventional `.<platform>/` location — verify against your platform's current docs.

---

## What's in `GRAPH_REPORT.md`

- **Summary** — node/edge/community counts, confidence breakdown
- **Corpus** — file + file-type counts; tiny-corpus warning
- **Community Hubs** — top 25 communities ranked by hub degree
- **God nodes** — most-connected nodes (top 10)
- **Surprising connections** — cross-community (or cross-file on small corpora) edges, ranked by inverse expectedness
- **Ambiguous edges** + **Knowledge gaps**
- **Semantic Extraction** — token spend + cost (when `--semantic` was used)
- **Graph Freshness** — the commit SHA the graph was built from

The report is what `gogfy install-instructions` tells the agent to read first.

---

## Ignoring & including files

`.graphifyignore` at the repo root, same syntax as `.gitignore` (including `!` negation). Dotfiles (`.git/`, `.env`, etc.) are skipped by default; re-include specific ones with `.graphifyinclude` (same syntax). Sensitive files (credentials, keys, terraform state, cloud configs) are dropped silently.

```
node_modules/
dist/
*.generated.go
```

---

## Team workflow

1. One person runs `gogfy run .` and commits `graphify-out/`.
2. Everyone else pulls — their assistant reads the graph immediately.
3. `gogfy hook install` adds post-commit + post-checkout hooks so the graph stays in sync automatically.
4. Parallel graphs collide? `gogfy hook install-merge-driver` registers a union driver so `git merge` auto-unions instead of leaving conflict markers.
5. CI: gate PRs with `gogfy ci base.json head.json --max-removed-nodes N`.

`graphify-out/.gographify-cache` (the incremental cache) can be `.gitignore`'d; the rest of `graphify-out/` is meant to be committed.

---

## Status & parity

Feature parity with upstream graphify: **complete**, plus extensions beyond it.

- ✅ Core pipeline (extraction, resolve, dedup, cluster, analyze, report, export)
- ✅ Semantic extraction — 6 LLM backends (Anthropic, OpenAI, Gemini, Kimi, Ollama, AWS Bedrock)
- ✅ Three-pass entity dedup (exact → MinHash/LSH + Jaro-Winkler → LLM tiebreaker)
- ✅ Hyperedges (N-ary relations) — emitted, persisted, and fed into clustering
- ✅ Transcription (Whisper) + YouTube audio (pure-Go, no yt-dlp)
- ✅ URL ingestion (tweet / arXiv / GitHub / YouTube / PDF / image / generic HTML)
- ✅ Exports: JSON, HTML, GraphML, Cypher, Neo4j direct push, Obsidian vault, static SVG, static doc-site, wiki, D3 tree, Mermaid call-flow
- ✅ MCP server: 8 tools + 6 resources
- ✅ 19 agent-platform installers, git hooks, merge driver
- ✅ Incremental manifest, cache stats/clear, CI gate, schema versioning

**Deliberately not done** (explicit non-goals): vis.js viewer (the SVG viewer covers the same UX), bilingual call-flow rendering, PHP service-container / Java-C# field-reference edges (niche), shelling out to `yt-dlp` / `gws` (no-shell-out policy — pure-Go alternatives used where possible).

See [GAP_ANALYSIS.md](GAP_ANALYSIS.md) for the detailed feature-by-feature comparison.

---

## Project layout

```
internal/
├── extract/     per-language tree-sitter extractors + document/binary parsers
├── resolve/     upgrade <lang>:call:<name> targets to real function nodes
├── rationale/   NOTE/HACK/TODO comment → rationale_for edges
├── tsalias/     tsconfig/jsconfig path-alias resolution
├── dedup/       three-pass entity deduplication
├── cluster/     Leiden + Louvain fallback + hyperedge clique expansion
├── centrality/  degree / betweenness helpers
├── analyze/     god nodes, surprising connections, questions
├── report/      Markdown report rendering
├── export/      JSON / HTML / GraphML / Cypher / static SVG
├── site/        static doc-site bundler (HTML + client-side search)
├── obsidian/    Obsidian vault export
├── callflow/    Mermaid architecture HTML
├── wiki/        per-community + per-god-node markdown
├── tree/        D3 collapsible filesystem tree
├── neo4jpush/   direct Bolt push (pure-Go driver)
├── llm/         provider-agnostic LLM interface + 6 backends
├── semantic/    LLM entity/relation extraction + result cache
├── transcribe/  audio→text (Whisper) with parallel fan-out
├── ytaudio/     pure-Go YouTube audio download
├── ingest/      URL fetch + routing (tweet/arXiv/github/youtube/PDF/image/HTML)
├── safefetch/   SSRF-guarded HTTP fetch
├── graph/       builder + Graph type (sanitizes labels at ingest)
├── graphdiff/   structural diff between two graphs
├── schema/      Node / Edge / Hyperedge / Confidence types + schema versioning
├── cache/       hash-based incremental cache
├── manifest/    mtime+SHA detection manifest with diff mode
├── detect/      file collection (.graphifyignore/.graphifyinclude, minified-skip, sensitive-skip)
├── watch/       fsnotify-backed continuous mode
├── serve/       MCP-over-stdio server (8 tools + 6 resources)
├── installer/   per-platform config writers + snippet
├── githook/     post-commit + post-checkout + merge-driver installer
├── globalgraph/ multi-repo aggregate graph store
├── merge/       graph union + incremental build-merge
├── gitmeta/     HEAD SHA for graph-freshness stamping
├── fence/       shared fenced-block editor
├── fsutil/      atomic write + tolerant read helpers
└── security/    path-traversal guards + size caps
cmd/gogfy/       CLI entry point
```

See [SPEC.md](SPEC.md) for the original scoping doc, [docs/languages.md](docs/languages.md) for the per-language extractor detail, and [AGENTS.md](AGENTS.md) for the contributor TDD rules.

---

## License

(no license file yet — add one before going public)
