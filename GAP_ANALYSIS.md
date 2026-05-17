# gogfy Gap Analysis vs. upstream `safishamsi/graphify`

> Generated from source-code comparison of upstream Python graphify (`graphify/`) and Go reimplementation (`gogfy/`).

---

## 1. Features Fully Implemented

### 1.1 Core Pipeline
| Feature | Upstream Module | gogfy Package | Notes |
|---------|----------------|---------------|-------|
| File detection with extension filtering | `detect.py` | `internal/detect` | Collects files, respects extensions |
| `.graphifyignore` (gitignore semantics) | `detect.py` | `internal/detect` | Root-level only; no subdirectory layering |
| AST-based code extraction | `extract.py` | `internal/extract` | 30+ languages via tree-sitter |
| Graph build with deduplication | `build.py` | `internal/graph` | Deduplicates by (source, target, relation) |
| Community detection (Leiden) | `cluster.py` | `internal/cluster` | Uses `leiden-go`; deterministic remapping |
| God node detection | `analyze.py` | `internal/analyze` | Top 20% connected nodes |
| Surprising link detection | `analyze.py` | `internal/analyze` | Cross-community, inverse log-degree scoring |
| Exploration questions | `analyze.py` | `internal/analyze` | God-node roles + community bridge questions |
| Report generation (markdown) | `report.py` | `internal/report` | Basic sections: God Nodes, Surprising Links, Confidence, Questions |
| JSON export | `export.py` | `internal/export` | `graph.json` with nodes/edges |
| HTML interactive viewer | `export.py` | `internal/export` | Embedded SVG force-directed layout |
| GraphML export | `export.py` | `internal/export` | Gephi/yEd compatible |
| Cypher export | `export.py` | `internal/export` | Neo4j MERGE script |
| Incremental cache (SHA256) | `cache.py` | `internal/cache` | Per-file hash, atomic writes |
| Cross-file call resolution | `extract.py` (two-pass) | `internal/resolve` | Synthetic `call:` targets → INFERRED/AMBIGUOUS |
| MCP stdio server | `serve.py` | `internal/serve` | Tools: god_nodes, explain, query, path |
| Git hooks (post-commit) | `hooks.py` | `internal/githook` | Install/uninstall/status |
| Merge driver for graph.json | `hooks.py` | `internal/githook` | Union merge via `merge-graphs` |
| File watcher | `watch.py` | `internal/watch` | Auto-rebuild on changes |
| Wiki export | `wiki.py` | `internal/wiki` | Per-community + per-god-node articles |
| D3 tree HTML | `tree_html.py` | `internal/tree` | Collapsible filesystem-tree view |
| Platform installer (MCP configs) | `__main__.py` | `internal/installer` | 15+ platforms |
| Combo install (mcp + snippet + hook) | `__main__.py` | `cmd/gogfy/main.go` | `gogfy <platform> install` |
| Path command (shortest path) | `__main__.py` | `cmd/gogfy/main.go` | BFS between nodes |
| Merge-graphs command | `__main__.py` | `internal/merge` | Union multiple graph.json files |
| Validate command | `validate.py` | `cmd/gogfy/main.go` | Schema validation |
| Report command | `report.py` | `cmd/gogfy/main.go` | Render from existing graph.json |
| Security (root guard, size caps) | `security.py` | `internal/security` | Path traversal guard, symlink resolution |

### 1.2 Language Extractors
| Language | Upstream | gogfy | Notes |
|----------|----------|-------|-------|
| Go | Yes | Yes | Package-qualified calls, receiver methods |
| Python | Yes | Yes | Imports, classes, functions, lambdas |
| JavaScript/TypeScript/TSX | Yes | Yes | ESM/CJS imports, arrow functions |
| Java | Yes | Yes | Classes, interfaces, methods |
| C | Yes | Yes | Functions, includes |
| C++ | Yes | Yes | Functions, classes, includes |
| Rust | Yes | Yes | Functions, structs, traits, impls |
| Ruby | Yes | Yes | Classes, methods |
| Kotlin | Yes | Yes | Classes, objects, functions |
| Scala | Yes | Yes | Classes, objects, functions |
| PHP | Yes | Yes | Classes, functions, methods |
| Lua | Yes | Yes | Functions, require |
| Swift | Yes | Yes | Classes, protocols, functions |
| Zig | Yes | Yes | Functions, structs, @import |
| Julia | Yes | Yes | Modules, structs, functions |
| C# | Yes | Yes | Classes, interfaces, methods |
| Haskell | No | Yes | Pure AST extraction |
| OCaml | No | Yes | Pure AST extraction |
| Svelte | Yes (regex fallback) | Yes | AST-based |
| Fortran | Yes | Yes | Programs, modules, subroutines |
| Elixir | Yes | Yes | Modules, functions, imports |
| Dart | Yes | Yes | Classes, mixins, functions |
| R | No | Yes | Pure AST extraction |
| Erlang | No | Yes | Pure AST extraction |
| Bash | No | Yes | Pure AST extraction |
| YAML/TOML | No | Yes | Data key extraction |
| Markdown | Yes | Yes | Headings, code blocks |
| HTML | No | Yes | Pure AST extraction |
| Text/RST | No | Yes | Basic extraction |
| DOCX/XLSX/PPTX/PDF | Yes (conversion) | Yes | Binary document extraction |

---

## 2. Features Partially Implemented

### 2.1 Detection / File Discovery
| Feature | Status | What's Missing |
|---------|--------|----------------|
| `.graphifyignore` | **Done** | `loadIgnoreMatcher` walks up to VCS root (.git/.hg/.svn marker) and layers each ancestor's `.graphifyignore` in order; scan root's patterns come last so gitignore last-match-wins gives them precedence — matches git's own behavior. |
| `.graphifyinclude` | Missing | Upstream has allowlist for hidden files; gogfy has no equivalent |
| File type classification | **Done (extension-based)** | `schema.ClassifyFile` maps extensions to CODE/DOCUMENT/PAPER/IMAGE/VIDEO; `schema.Node.FileType` populated at extract boundary; Corpus report section breaks down counts by type. Shebang + paper-signal heuristics deferred. |
| Sensitive file detection | **Done** | `detect.IsSensitive` + silent skip in `CollectFiles`. 6 graphify-parity regex patterns (.env, .pem/.key, credentials/secrets/tokens, SSH keypairs, .netrc/.pgpass/.htpasswd, cloud creds). |
| Corpus size warnings | **Done (tiny-only)** | Report Corpus section emits a tiny-corpus warning under 5 files. Upstream's "too large" warning isn't ported — gogfy's AST extraction has no per-token cost. |
| Google Workspace conversion | **Done (URL-only)** | `extract.GoogleWorkspaceExtractor` parses .gdoc/.gsheet/.gslides JSON shortcut files and emits a document-typed module node with the linked Drive URL in `SourceLocation`. Distinct lang tags (gdoc/gsheet/gslides) for filterability. Full content conversion via the upstream `gws` CLI deferred — gogfy doesn't currently shell out to external binaries. |
| Office structural extraction | Partial | gogfy has extractors but upstream does deeper structural node extraction for XLSX (sheets, tables, columns) |
| Incremental detection (manifest) | Missing | Upstream uses mtime + MD5 manifest for incremental scans; gogfy only has SHA256 cache for extraction |
| Symlink following | Partial | gogfy resolves symlinks via RootGuard; upstream has `follow_symlinks` flag with cycle detection |

### 2.2 Extraction
| Feature | Status | What's Missing |
|---------|--------|----------------|
| Python cross-file import resolution | **Done (scope-aware)** | `resolve.Calls` builds per-file import scope (bare-name + dotted-root) and narrows AMBIGUOUS fan-outs to candidates whose source-file stem matches an imported module. Multi-candidate calls upgrade to INFERRED only when the narrowing yields exactly one match. |
| Java cross-file import resolution | **Done (scope-aware)** | Same resolver — applies to any extractor emitting `<lang>:module:<filepath>` + `imports` edges. |
| JS/TS path alias resolution | **Done** | `internal/tsalias.Load` reads `tsconfig.json` / `jsconfig.json` `compilerOptions.paths` + `baseUrl`; `Apply` rewrites `js:import:` / `ts:import:` node IDs (and edges that target them) to resolved filesystem paths. Missing config is non-fatal. Wired into runPipeline after Build and before resolve.Calls. |
| Call graph depth | Partial | gogfy extracts calls as synthetic targets; upstream has richer call-graph with package-qualified vs receiver distinction in some languages |
| Docstring/rationale extraction | **Done (comments only)** | `internal/rationale.Extract` post-pass surfaces NOTE/IMPORTANT/HACK/WHY/RATIONALE/TODO/FIXME/XXX/WARNING comments across `#`, `//`, `--`, `/*` markers as `rationale_for` edges to the file's module node. Language-agnostic regex scan; per-function docstring attribution deferred. |
| Semantic extraction (LLM) | Missing | Upstream has full LLM pipeline for docs/papers/images; gogfy is AST-only |
| Hyperedges | Missing | Upstream generates hyperedges from semantic extraction |
| Token tracking | Missing | Upstream tracks input/output tokens per extraction |

### 2.3 Graph Build
| Feature | Status | What's Missing |
|---------|--------|----------------|
| Entity deduplication | Partial | gogfy dedupes by (source, target, relation) on edges; upstream has three-pass dedup: exact normalization → MinHash/LSH + Jaro-Winkler → optional LLM tiebreaker |
| Normalized ID reconciliation | Missing | Upstream uses Jaro-Winkler-ish normalization to reconcile LLM-generated IDs with AST extractor IDs |
| Direction preservation | Partial | gogfy preserves direction in schema; upstream stashes `_src`/`_tgt` on undirected NetworkX graphs |
| Incremental merge (`build_merge`) | Missing | Upstream can merge new extractions into existing graph.json with prune for deleted files |
| Multi-repo prefixing | **Done** | `internal/globalgraph` prefixes node IDs with `<tag>::` and dedupes external-library nodes by label. CLI: `gogfy global add/remove/list/path` ✅ |

### 2.4 Clustering
| Feature | Status | What's Missing |
|---------|--------|----------------|
| Community splitting | **Done** | Oversized communities (>25% of graph, min 10) and low-cohesion communities (<0.05, min 50) split with second Leiden pass |
| Cohesion scoring | **Done** | `cohesionScore()` computes intra-community edges / max possible |
| Louvain fallback | Partial | gogfy has ConnectedComponents fallback; upstream tries graspologic Leiden then networkx Louvain |
| Community labels | **Done (heuristic)** | `internal/labels` derives names from top-degree member node label (no LLM dependency); `gogfy labels <graph.json>` writes `.graphify_labels.json`; `gogfy wiki` auto-loads it. Hand-edits preserved unless `--force`. |

### 2.5 Analysis
| Feature | Status | What's Missing |
|---------|--------|----------------|
| God node filtering | Partial | gogfy filters by degree; upstream excludes file nodes, method stubs, and concept nodes via `_is_file_node` / `_is_concept_node` |
| Surprising connections scoring | **Done** | Composite integer score: confidence bonus (AMBIGUOUS=3, INFERRED=2, EXTRACTED=1, zeroed for cross-lang INFERRED `calls`), +2 cross-file-type, +2 cross-repo (top-level dir differs), +1 cross-community baseline, ×1.5 for `semantically_similar_to`, +1 peripheral→hub. Tie-break: legacy inverse-log-degree, then input order. |
| Cross-file vs single-source modes | Missing | Upstream switches between `_cross_file_surprises` and `_cross_community_surprises` based on corpus size |
| Suggested questions | **Done** | All 7 upstream types covered: god-node role, ambiguous_edge, verify_inferred, isolated_nodes, low_cohesion (threshold-aligned with cluster splitter), no_signal (empty/edgeless graph short-circuit), community-bridge. Per-category budget prevents one type from crowding out others. |
| Graph diff | **Done** | `internal/graphdiff.Compute` + `gogfy diff <old.json> <new.json>` — markdown summary of added/removed/changed nodes (label/community/file-type drift) and added/removed edges. Edge identity is (source, target, relation); confidence flips on the same edge are intentionally not surfaced. |
| Betweenness centrality | **Done** | `internal/centrality.Betweenness` ports Brandes' O(V·E) algorithm (undirected, dedup self-loops/parallels, dangling-ref-safe). `analyze.Report.BridgeNodes` surfaces top-3 by score with deterministic tie-break; report writes a `## Bridge Nodes` section when non-empty. |

### 2.6 Report
| Feature | Status | What's Missing |
|---------|--------|----------------|
| Report sections | **Done (10/11)** | Added Summary, Corpus, Graph Freshness (conditional), Community Hubs, Communities (with thin-community filtering), Ambiguous Edges, Knowledge Gaps. Hyperedges omitted — gogfy doesn't model N-ary relations. `report.RenderWithOptions` carries the extended data; legacy `Render(r)` preserved as a trimmed variant. |
| Token cost reporting | Missing | Upstream reports input/output tokens and estimated cost |
| Git commit freshness | **Done** | `internal/gitmeta.HeadShortSHA` reads `.git/HEAD` + the referenced ref file (or `packed-refs` fallback) — no shell-out. Auto-populated in runPipeline, runClusterOnly, reportCommand so the Graph Freshness section appears on every report inside a repo. Worktree `.git`-file pointers resolved. |
| Community hub navigation | Missing | Upstream has wikilink navigation to community notes |
| Knowledge gaps section | **Done** | Composite digest: isolated-node count, thin-community count, ambiguous-edge count. Each line shows only when count > 0. |
| Thin community filtering | **Done** | `Options.ThinCommunityMin` (default 2) drops single-node \"communities\" from the Communities section and feeds the Knowledge Gaps thin-community counter. |

### 2.7 Export
| Feature | Status | What's Missing |
|---------|--------|----------------|
| HTML viewer | Partial | gogfy has SVG force-directed with search, click-to-inspect, community filter, AND aggregated community view above 1000 nodes (renders meta-graph with weighted cross-community edges so big repos stay navigable). Still missing: vis.js physics, hyperedge rendering (hyperedges not modeled). |
| Obsidian vault export | **Done (v1)** | `gogfy obsidian` writes per-node .md with YAML frontmatter + [[wikilinks]] + tags (graphify/{type,confidence}, community/{name}); per-community `_COMMUNITY_<name>.md` with Members + Dataview query + cross-community connections. Auto-loads `.graphify_labels.json`. v1 omits: cohesion-strength descriptor, top-bridge-nodes block, `.obsidian/graph.json` color config, canvas export. |
| Obsidian Canvas | **Done** | `obsidian.Canvas` writes `graph.canvas` alongside the vault: communities as colored groups in a √N×√N grid, member nodes as 180×60 file cards (3 per row), capped at 200 edges with relation+confidence labels. Opens in Obsidian as an infinite canvas. |
| SVG static export | Missing | Upstream has matplotlib-based static SVG |
| Neo4j direct push | Missing | Upstream can push directly to Neo4j via Python driver |
| Callflow HTML | **Done (v1)** | gogfy `callflow` subcommand: section-level overview + per-section Mermaid LR. v1 omits bilingual/labels-file/GRAPH_REPORT integration. |
| Node limit / aggregation | Missing | Upstream auto-aggregates to community-level view when graph exceeds 5000 nodes |
| Confidence score defaults | Missing | Upstream adds `confidence_score` field to edges in JSON |
| Built-at commit metadata | Missing | Upstream embeds git HEAD in graph.json |

### 2.8 MCP Server
| Feature | Status | What's Missing |
|---------|--------|----------------|
| Tools | **Done** | gogfy: god_nodes, explain (superset of upstream get_node), query, path, get_neighbors, graph_stats, get_community — all 7 upstream tools covered. |
| Resources | Partial | gogfy: report only; upstream: report, stats, god-nodes, surprises, audit, questions |
| BFS/DFS traversal | Missing | Upstream has token-budgeted subgraph traversal with context filters |
| Node scoring | Missing | Upstream scores nodes by label match relevance |

### 2.9 Cache
| Feature | Status | What's Missing |
|---------|--------|----------------|
| Semantic cache | Missing | Upstream has separate `ast/` and `semantic/` cache subdirectories with markdown frontmatter stripping |
| Legacy migration | Missing | Upstream migrates flat cache to hierarchical |
| Cache corruption handling | Missing | Upstream has recovery for corrupted cache entries |

### 2.10 Security
| Feature | Status | What's Missing |
|---------|--------|----------------|
| URL validation | Missing | Upstream validates http/https, blocks private IPs, cloud metadata endpoints |
| SSRF-guarded socket | Missing | Upstream has DNS rebinding protection |
| Safe fetch | Missing | Upstream has `safe_fetch()` / `safe_fetch_text()` with size caps |
| Path traversal guard | Partial | gogfy has RootGuard; upstream also has `validate_graph_path()` |
| Label sanitization | Partial | `internal/labels` strips control chars and caps at 256 runes for community names; node Label/SourceFile fields not yet sanitized at ingest. |

---

## 3. Features Not Yet Implemented

### 3.1 CLI Commands
| Command | Upstream Location | Description |
|---------|-------------------|-------------|
| `graphify extract <path>` | `__main__.py` | Headless full extraction with LLM backends |
| `graphify update <path>` | `__main__.py` | Incremental AST-only rebuild with manifest |
| `graphify query "<question>"` | `__main__.py` | BFS/DFS graph traversal with token budget |
| `graphify explain "<node>"` | `__main__.py` | Plain-language node explanation |
| `graphify save-result` | `__main__.py` | Save Q&A to memory/ for feedback loop |
| `graphify check-update` | `__main__.py` | Cron-safe update check |
| `graphify benchmark` | `__main__.py` | Token reduction measurement (gogfy: `gogfy benchmark <graph.json>`) ✅ |
| `graphify clone <github-url>` | `__main__.py` | Clone repos to cache |
| `graphify add <url>` | `__main__.py` | Fetch URL into corpus |
| `graphify export obsidian` | `__main__.py` | Obsidian vault export |
| `graphify export svg` | `__main__.py` | Static SVG export |
| `graphify export neo4j` | `__main__.py` | Direct Neo4j push |
| `graphify export callflow-html` | `__main__.py` | Mermaid architecture HTML (gogfy: `gogfy callflow <graph.json>`) ✅ v1 |
| `graphify hook-check` | `__main__.py` | Cross-platform no-op for hooks |

### 3.2 Core Pipeline Features
| Feature | Upstream Location | Description |
|---------|-------------------|-------------|
| Semantic LLM extraction | `llm.py` | Direct LLM backends (Claude, Kimi, Gemini, OpenAI, Ollama, Bedrock) |
| Adaptive retry on truncation | `llm.py` | Bisect chunks recursively on context overflow |
| Token estimation | `llm.py` | tiktoken-based token counting |
| Chunk packing by token budget | `llm.py` | Group files by directory for efficient packing |
| Parallel semantic extraction | `llm.py` | ThreadPoolExecutor for concurrent LLM calls |
| Cost estimation | `llm.py` | Per-backend pricing calculation |
| Incremental manifest (mtime + MD5) | **Done (SHA-256)** | `internal/cache` now stores `{mtime,hash}` per file; mtime-match short-circuits the hash, mtime-bumped-but-hash-match treated as unchanged (sync-tool touch). Legacy hash-only manifests auto-upgrade on next Save. |
| Deleted file pruning | `build.py` | Remove nodes for files no longer in corpus |
| Hyperedge generation | `llm.py` / `extract.py` | Group relationships from semantic extraction |
| Cross-language call detection | `analyze.py` | Detect calls between language families |

### 3.3 Platform Integrations
| Feature | Upstream Location | Description |
|---------|-------------------|-------------|
| Claude Code PreToolUse hook | `__main__.py` | Inject graph reminder before Bash/Glob/Grep |
| Gemini BeforeTool hook | `__main__.py` | .gemini/settings.json hook registration |
| VS Code Copilot Chat instructions | `__main__.py` | .github/copilot-instructions.md generation |
| Cursor rules | `__main__.py` | .cursor/rules/graphify.mdc generation |
| Kiro steering file | `__main__.py` | .kiro/steering/graphify.md |
| Antigravity rules/workflows | `__main__.py` | .agents/rules + .agents/workflows |
| OpenCode plugin | `__main__.py` | .opencode/plugins/graphify.js |
| Codex hooks.json | `__main__.py` | .codex/hooks.json PreToolUse registration |
| Version stamp management | `__main__.py` | .graphify_version files for stale-skill detection |

### 3.4 Ingestion
| Feature | Upstream Location | Description |
|---------|-------------------|-------------|
| URL fetching with type detection | `ingest.py` | Detect tweet, arxiv, github, youtube, pdf, image, webpage |
| HTML-to-markdown conversion | `ingest.py` | Convert web pages to markdown |
| arXiv abstract extraction | `ingest.py` | Fetch and format arXiv papers |
| YouTube audio download | `ingest.py` | yt-dlp integration |
| Binary download for PDFs/images | `ingest.py` | Save raw binaries |
| Query result memory | `ingest.py` | save_query_result for feedback loop |

### 3.5 Transcription
| Feature | Upstream Location | Description |
|---------|-------------------|-------------|
| Video/audio transcription | `transcribe.py` | faster-whisper integration |
| YouTube audio extraction | `transcribe.py` | yt-dlp + whisper pipeline |
| Domain hint generation | `transcribe.py` | Generate hints from god nodes |

---

## 4. Behavioral Differences

### 4.1 Data Model
| Aspect | Upstream | gogfy |
|--------|----------|-------|
| Node ID format | Flexible (LLM-generated or AST-derived) | Strict `schema.LangID(lang, kind, path[:name])` |
| Edge deduplication | NetworkX MultiGraph allows parallel edges; dedup happens at node level | Single edge per (source, target, relation) tuple |
| Graph structure | NetworkX Graph/DiGraph with attribute dicts | Immutable Go structs with Builder pattern |
| Confidence | String enum | Go iota enum (Extracted=0, Inferred=1, Ambiguous=2) |
| Direction | Undirected default, `_src`/`_tgt` stashed for rendering | Directed by default in schema |
| File type | `file_type` field (code/document/paper/image/concept) | Not tracked in schema |
| Source location | Optional string | Optional string |

### 4.2 Extraction Behavior
| Aspect | Upstream | gogfy |
|--------|----------|-------|
| Call target resolution | Two-pass for Python/Java; synthetic targets for others | Single-pass with post-hoc `resolve.Calls()` |
| Import edges | `imports` and `imports_from` relations | Unified `imports` relation |
| Method calls | Preserves receiver context where possible | Strips receiver to bare name (synthetic target) |
| Anonymous functions | AST method stubs with `.name()` labels | `<anonymous>` label with position key |
| Cross-language | Can detect cross-language families | Language-scoped resolution only |

### 4.3 Clustering Behavior
| Aspect | Upstream | gogfy |
|--------|----------|-------|
| Algorithm | Leiden (graspologic) → Louvain fallback | Leiden (leiden-go) → ConnectedComponents fallback |
| Community splitting | Yes (oversized + low-cohesion) | Yes |
| Isolate handling | Each isolate → own community | Each isolate → own community |
| Re-indexing | By size descending | By first member sorted |
| Determinism | Seed=42, stable ordering | Seed=42 + stable remapping by sorted members |

### 4.4 Analysis Behavior
| Aspect | Upstream | gogfy |
|--------|----------|-------|
| God node filtering | Excludes file nodes, method stubs, concept nodes | Includes all nodes (no file_type/concept filtering) |
| Surprise scoring | Composite multi-factor score | Simple inverse log-degree product |
| Question generation | 7 types with structured output | 2 types (god-node roles + community bridges) |
| Cross-file detection | Explicit source_file comparison | Implicit via community difference |

### 4.5 Report Behavior
| Aspect | Upstream | gogfy |
|--------|----------|-------|
| Sections | 10+ sections with rich metadata | 4 basic sections |
| Community detail | Full community listing with cohesion, members | Not included |
| Ambiguous edges | Dedicated section with file context | Not included |
| Knowledge gaps | Isolated nodes, thin communities, high ambiguity | Not included |
| Token cost | Reported per extraction | Not tracked |

---

## 5. Language Support Comparison

### 5.1 Languages with Full Parity (AST extraction)
- Go, Python, JavaScript, TypeScript/TSX, Java, C, C++, Rust, Ruby, Kotlin, Scala, PHP, Lua, Swift, Zig, Julia, C#, Svelte, Fortran, Elixir, Dart, Markdown

### 5.2 Languages in gogfy but not upstream
- Haskell, OCaml, R, Erlang, Bash, YAML, TOML, HTML, Text, RST

### 5.3 Languages in upstream but not gogfy
- Groovy/Gradle (with Spock fallback), PowerShell, Verilog, SQL, Objective-C, Pascal/Delphi, Vue (separate from JS)

### 5.4 Languages with Partial Parity
| Language | Gap |
|----------|-----|
| Python | Missing cross-file `from X import` resolution, docstring/rationale extraction |
| Java | Missing cross-file import resolution |
| JavaScript/TypeScript | Missing tsconfig path alias resolution |
| PHP | Missing service container bindings, event listener properties |
| Java/C# | Missing field/property reference edges |

---

## 6. Recommended Priority Order for Closing Gaps

### P0 (Critical for parity)
1. ~~**Community splitting** — Add oversized/low-cohesion community splitting (upstream `cluster.py`)~~ ✅ Done
2. **Entity deduplication** — Implement MinHash/LSH + Jaro-Winkler pipeline (upstream `dedup.py`)
3. **Surprising connections scoring** — Implement composite score with all bonus factors
4. **Report sections** — Add missing sections: Community Hubs, Hyperedges, Ambiguous Edges, Knowledge Gaps, Graph Freshness
5. **God node filtering** — Exclude file nodes and method stubs from god node detection

### P1 (High value)
6. **Semantic extraction interface** — Add LLM backend adapter port (even if no backends implemented yet)
7. **Incremental manifest** — Add mtime + MD5 manifest for faster incremental detection
8. **Obsidian export** — Full vault generation with wikilinks, YAML frontmatter, tags
9. **HTML viewer parity** — Add vis.js-based viewer with search, community filter, click-to-inspect
10. **Cross-file resolution** — Two-pass import resolution for Python and Java

### P2 (Nice to have)
11. ~~**Global graph** — Cross-repo graph merging with repo-tag prefixing~~ ✅ Done
12. **Transcription** — Video/audio pipeline via whisper
13. **URL ingestion** — Fetch and convert web content
14. **Platform hooks** — PreToolUse/BeforeTool hooks for Claude/Gemini/Codex
15. ~~**Benchmark** — Token reduction measurement~~ ✅ Done

---

*End of gap analysis*

---

## Deep Audit vs graphify Upstream (Snapshot: 2026-05-15)

This section provides an exhaustive comparison of all graphify modules and CLI commands against gogfy implementations.

### Module-by-Module Checklist

| graphify Module | Lines | gogfy Package | Status | Complexity | Bucket | Notes |
|---|---|---|---|---|---|---|
| `__init__.py` | 28 | `cmd/gogfy/main.go` | Done | - | - | Lazy imports; gogfy uses direct imports |
| `__main__.py` | 2697 | `cmd/gogfy/main.go` + `internal/installer` | Partial | Medium | 🔴 Infra | Missing: extract, add, query, explain, clone, ingest, transcribe, platform hooks |
| `analyze.py` | 575 | `internal/analyze` | Partial | Medium | 🟢 Code | Missing: semantic similarity bonus, cross-file mode switching |
| `benchmark.py` | 152 | `internal/benchmark` | Done | - | 🟢 Code | ✅ |
| `build.py` | 325 | `internal/graph` | Done | - | 🟢 Code | ✅ |
| `cache.py` | 241 | `internal/cache` | Partial | Small | 🔴 Infra | Missing: ast/ vs semantic/ split, frontmatter stripping |
| `callflow_html.py` | 2014 | `internal/callflow` | Partial | Medium | 🟡 LLM | v1 complete; missing bilingual, label integration, report context |
| `cluster.py` | 150 | `internal/cluster` | Done | - | 🟢 Code | ✅ |
| `dedup.py` | 343 | `internal/dedup` | Partial | Large | 🟢 Code | Missing: MinHash, LSH, Jaro-Winkler, LLM tiebreaker |
| `detect.py` | 877 | `internal/detect` | Partial | Small | 🔴 Infra | Missing: file type classification, sensitive file detection, corpus warnings, manifest, symlink follow |
| `export.py` | 1264 | `internal/export` | Partial | Large | 🔵 Artifact | Missing: obsidian vault, neo4j push, SVG static, vis.js viewer |
| `extract.py` | 5947 | `internal/extract` | Partial | Medium | 🟡 LLM | Missing: docstring/rationale edges, cross-file resolution, semantic extraction |
| `global_graph.py` | 155 | `internal/globalgraph` | Done | - | 🟢 Code | ✅ |
| `google_workspace.py` | 223 | (missing) | Missing | Medium | 🔵 Artifact | Convert .gdoc/.gsheet/.gslides shortcuts |
| `hooks.py` | 282 | `internal/githook` | Done | - | 🔴 Infra | ✅ |
| `ingest.py` | 331 | (missing) | Missing | Medium | 🔵 Artifact | URL fetcher, HTML-to-markdown, tweet/arXiv/github/youtube extraction |
| `llm.py` | 954 | (missing) | Missing | Large | 🟡 LLM | 6 backends (Claude, OpenAI, Gemini, Kimi, Ollama, Bedrock), token est, adaptive retry, cost |
| `manifest.py` | 4 | (missing) | Missing | Small | 🔴 Infra | mtime + MD5 tracking for incremental detection |
| `report.py` | 196 | `internal/report` | Partial | Small | 🟢 Code | Done: Summary, Corpus, Communities, Ambiguous, Knowledge Gaps. Missing: token cost, commit hash |
| `security.py` | 242 | `internal/security` | Partial | Small | 🔴 Infra | Missing: URL validation, SSRF guard, safe_fetch |
| `serve.py` | 582 | `internal/serve` | Partial | Small | 🔴 Infra | Missing: BFS/DFS traversal, context filters, node scoring |
| `transcribe.py` | 184 | (missing) | Missing | Medium | 🔵 Artifact | faster-whisper + yt-dlp pipeline, whisper prompt from god nodes |
| `tree_html.py` | 580 | `internal/tree` | Done | - | 🔵 Artifact | ✅ |
| `validate.py` | 72 | `cmd/gogfy/main.go` | Done | - | 🔴 Infra | ✅ |
| `watch.py` | 456 | `internal/watch` | Done | - | 🟢 Code | ✅ |
| `wiki.py` | 255 | `internal/wiki` | Done | - | 🔵 Artifact | ✅ |

**Legend:**
- Status: Done / Partial / Missing
- Complexity: Small (1 PR) / Medium (2 PRs) / Large (3+ PRs)
- Bucket: 🟢 Code reach | 🔵 New artifact types | 🟡 LLM-dependent | 🔴 Infrastructure

### CLI Command Parity

| Command | Upstream | gogfy | Status | Notes |
|---|---|---|---|---|
| **Core Pipeline** |
| `run` (extract + cluster + build) | Implicit (skill.md) | `run <root> [--update] [--out] ...` | Done | ✅ |
| `extract <path> [--backend]` | Full LLM pipeline | Missing | Missing | Requires llm.go backend orchestration |
| `update <path> [--force]` | Incremental AST | `run --update` | Partial | No manifest; full re-scan |
| `watch <path>` | File watcher | `watch <root> [--out]` | Done | ✅ |
| `cluster-only <path>` | Re-cluster only | `run --cluster-only` | Done | ✅ |
| **Query/Analysis** |
| `query "<question>" [--dfs]` | BFS/DFS traversal | (serve tools) | Partial | Query in serve tools, not CLI |
| `path <src> <tgt>` | Shortest path | `path <source> <target>` | Done | ✅ |
| `explain "<node>"` | Node explanation | (serve tools) | Partial | Explain in serve tools, not CLI |
| `save-result` | Memory feedback loop | Missing | Missing | Requires ingest.go module |
| **Utilities** |
| `add <url>` | URL ingestion | Missing | Missing | Requires ingest.go (URL fetch, HTML→md) |
| `clone <github-url>` | Repo caching | Missing | Missing | Nice-to-have |
| `check-update <path>` | Cron-safe update check | Missing | Missing | Low priority |
| `merge-driver` | Git merge union | `hook` commands | Done (via githook) | ✅ |
| `merge-graphs <g1> <g2>` | Merge multiple graphs | `merge-graphs <g1> <g2>` | Done | ✅ |
| **Export** |
| `export html [--graph]` | vis.js viewer | `run` output + serve | Partial | SVG+D3, no vis.js features |
| `export callflow-html` | Mermaid diagrams | `callflow <graph.json>` | Partial (v1) | Missing bilingual, label integration |
| `export obsidian` | Full vault | Missing | Missing | High user demand |
| `export wiki` | Community + god-node docs | `wiki <graph.json>` | Done | ✅ |
| `export svg` | Static SVG | Missing | Missing | Low demand |
| `export graphml` | Gephi/yEd | `run --graphml` | Done | ✅ |
| `export neo4j` | Direct Neo4j push | `run --cypher` (script only) | Partial | No direct push; Cypher script only |
| **Install/Hooks** |
| `install [--platform]` | Platform install | `install [--platform]` + `<platform> install` | Done | ✅ |
| `uninstall [--purge]` | Platform uninstall | `uninstall [--platform]` | Done | Missing `--purge` |
| `hook install/uninstall/status` | Git hooks | `hook install/uninstall/status` | Done | ✅ |
| Platform-specific (`claude`/`gemini`/etc) | 14+ platforms | 15+ via combo wrapper | Done | ✅ |
| **Analysis** |
| `benchmark [graph.json]` | Token reduction | `benchmark <graph.json>` | Done | ✅ |
| `tree [--graph]` | D3 tree HTML | `tree <graph.json>` + `run --tree` | Done | ✅ |
| `global add/remove/list/path` | Multi-repo graphs | `global add/remove/list/path` | Done | ✅ |
| **Serve** |
| `serve` (MCP stdio) | MCP tools + resources | `serve [--graph] [--port]` | Partial | Missing: BFS/DFS, context filters, node scoring |

### Feature Density by Category

#### 🟢 Pure Code Reach (15/27 features complete)
- ✅ 30+ language extractors
- ✅ AST-based extraction for code
- ✅ Graph build + deduplication (basic)
- ✅ Community detection (Leiden)
- ✅ God node analysis
- ✅ Cross-file call resolution (synthetic targets)
- ✅ Report generation (basic)
- ⚠️ Entity deduplication (schema-based only; missing MinHash/Jaro-Winkler)
- ⚠️ Surprising connection scoring (simple; missing semantic bonus)
- ⚠️ File type classification (missing)
- ⚠️ Sensitive file detection (missing)

#### 🔵 New Artifact Types (7/11 complete)
- ✅ JSON export
- ✅ HTML interactive viewer (SVG)
- ✅ GraphML export
- ✅ Cypher export (Neo4j script)
- ✅ Wiki export (per-community + god-node)
- ✅ D3 tree HTML (filesystem hierarchy)
- ✅ Callflow HTML (v1, Mermaid diagrams)
- ⚠️ Callflow HTML v2 (bilingual, labels integration, report context)
- ❌ Obsidian vault (high demand)
- ❌ SVG static export
- ❌ Neo4j direct push

#### 🟡 LLM-Dependent (0/10 complete)
- ❌ Semantic extraction (docs, images, PDFs, videos) — **NONE**
- ❌ LLM backend orchestration (Claude, OpenAI, Gemini, Kimi, Ollama, Bedrock)
- ❌ Token estimation (tiktoken-based)
- ❌ Adaptive retry on truncation
- ❌ Chunk packing by token budget
- ❌ Docstring/rationale edge extraction
- ❌ Semantic similarity scoring (analyze)
- ❌ Entity deduplication tiebreaker
- ❌ Cross-file import resolution (two-pass)
- ❌ Transcription (faster-whisper, yt-dlp)

#### 🔴 Infrastructure (8/13 complete)
- ✅ Git hooks (post-commit, merge-driver)
- ✅ File watcher (auto-rebuild)
- ✅ Incremental cache (SHA256)
- ✅ Security (path traversal guard, root guard)
- ✅ Schema validation
- ✅ MCP server (stdio mode)
- ✅ Platform installer (15+ platforms)
- ✅ Merge driver (union merge)
- ⚠️ Cache (missing ast/ vs semantic/ split)
- ⚠️ Security (missing URL validation, SSRF guard, safe_fetch)
- ❌ Incremental manifest (mtime + MD5)
- ❌ Google Workspace conversion
- ❌ URL ingestion (webpage, tweet, arXiv, github, youtube)

### Top 10 Highest-Value Gaps (Ranked by user_value × inverse_complexity)

1. **Semantic LLM extraction** (3 PRs, 🟡 HIGH) — Enables `gogfy run --backend openai` for docs/images/PDFs. Foundation for cost-aware extraction.
2. **Artifact ingestion (URL + transcription)** (2 PRs, 🔵 HIGH) — Enables `gogfy add <url>` (webpage, tweet, arXiv, youtube, PDF). Completes artifact workflow.
3. **Obsidian vault export** (3 PRs, 🔵 HIGH) — .md per node + wikilinks + YAML frontmatter + Dataview queries. Highest user demand for artifact type.
4. **File type classification** (1 PR, 🟢 MEDIUM) — CODE/DOCUMENT/PAPER/IMAGE/VIDEO. Foundation for report fidelity + sensitive file detection.
5. **Incremental manifest** (2 PRs, 🟢 MEDIUM) — mtime + MD5 tracking. 10-50x speedup on large codebases (10K+ files).
6. **Cross-file import resolution** (2 PRs, 🟢 MEDIUM) — Two-pass for Python/Java. Reduces synthetic `call:` nodes, improves precision.
7. **Google Workspace conversion** (2 PRs, 🔵 MEDIUM) — .gdoc/.gsheet/.gslides → markdown/JSON. Enables hybrid code+docs workflows.
8. **Advanced analysis scoring** (2 PRs, 🟢 MEDIUM) — Composite surprise score with cross-file, cross-repo, peripheral→hub bonuses. Improves ranking quality.
9. **HTML viewer parity (vis.js)** (3 PRs, 🔵 MEDIUM) — Physics engine, search, community filter, click-to-inspect. UX enhancement.
10. **Entity deduplication v2** (3 PRs, 🟢 MEDIUM) — MinHash/LSH + Jaro-Winkler. Reduces spurious nodes; improves edge quality.

### LLM-Dependent Features (Separate Policy Decision)

**These require explicit backend selection and LLM API key at runtime:**

1. **extract_corpus_parallel()** — Multi-file semantic extraction
   - Supported backends: Claude (via Anthropic), OpenAI (GPT-4), Gemini, Kimi (Moonshot), Ollama (local), AWS Bedrock
   - Features: Token estimation (tiktoken), adaptive retry (bisect on overflow), chunk packing (token budget + directory locality), parallel dispatch
   - Cost estimation per backend
   - Policy: Opt-in; `gogfy run --backend openai --model gpt-4 .` syntax
   - Implementation effort: Large (3+ PRs for full feature parity)

2. **Transcription pipeline** — Audio/video → text
   - Local: faster-whisper (no API needed)
   - Remote: YouTube audio extraction (yt-dlp)
   - Whisper prompt generation from god nodes
   - Policy: Treat as separate subcommand: `gogfy transcribe <video.mp4>` or `gogfy transcribe <youtube-url>`
   - Implementation effort: Medium (2 PRs)

3. **Artifact ingestion** — URL → corpus
   - HTML → markdown (readability-based, no LLM needed)
   - Special cases: tweet, arXiv, github, youtube (API-based, no LLM needed)
   - PDF/image download (binary, no LLM needed)
   - Policy: `gogfy add <url> [--dir ./raw]` is LLM-free
   - Implementation effort: Medium (2 PRs)

4. **Surprise edge scoring** — Semantic similarity bonus (OPTIONAL)
   - Enhances `analyze.surprises()` with LLM-derived node embeddings
   - Policy: Heuristic scoring is sufficient; semantic is optional enhancement
   - Implementation effort: Large (3+ PRs)

5. **Entity deduplication tiebreaker** — LLM resolves ambiguous merge candidates (OPTIONAL)
   - Fallback: Jaro-Winkler + MinHash is sufficient
   - Policy: Implement heuristic first; LLM tiebreaker is optimization
   - Implementation effort: Large (3+ PRs)

### gogfy Extras (Not in Upstream graphify)

1. **`internal/fence`** — Code fence extraction from markdown
   - Upstream inlines fence extraction in markdown handler
   - gogfy has dedicated fence.go for reusability

2. **Heuristic community labels** (`internal/labels`)
   - gogfy derives community labels from top-degree member node (no LLM dependency)
   - Upstream relies on LLM for rich naming
   - Trade-off: Simple names vs. rich descriptions; gogfy is faster + deterministic

3. **Integrated artifact export in `run`**
   - Upstream: `gogfy export graphml` requires separate command
   - gogfy: `gogfy run --graphml --cypher --wiki --tree .` (all in one shot)
   - User experience: Faster, fewer commands

4. **Dedup as optional flag** (`run --no-dedup`)
   - Upstream: dedup always-on
   - gogfy: Allows users to skip dedup for speed (trade-off: duplicates)

5. **Explicit label loading in wiki/callflow**
   - gogfy can load `.graphify_labels.json` to customize community names
   - Upstream generates labels from LLM; gogfy allows hand-editing + preservation

### Surprising Findings

#### 1. gogfy is Ahead on Artifact Parity
- gogfy integrates GraphML + Cypher + Wiki + Tree into a single `run` command
- graphify requires separate `export` subcommands (4 separate CLI invocations)
- **User experience win:** `gogfy run --graphml --cypher --wiki --tree .` is an order of magnitude faster than running graphify 4 times

#### 2. graphify's Scale Assumption (LLM-Centricity)
- ~30% of graphify.__main__.py (~800 lines) is devoted to LLM backend orchestration
- Platform integrations (15 platforms) occupy ~20% of __main__.py (~540 lines)
- **Decision insight:** graphify assumes semantic extraction is core; gogfy treats it as orthogonal
- **Strategic observation:** gogfy can remain competitive on CODE extraction alone; semantic extraction should be opt-in, not mandatory

#### 3. gogfy's Missing Artifact Types
- No Obsidian vault generation (graphify has full vault with .md per node + wikilinks + Dataview)
- No Neo4j direct push (only Cypher script output)
- No SVG static export (only interactive HTML)
- **User impact:** Obsidian integration has highest demand; other formats are lower priority

#### 4. File Type Classification is Foundation Work
- Upstream graphify.detect classifies files into CODE/DOCUMENT/PAPER/IMAGE/VIDEO
- This enables: report fidelity, sensitive file detection, selective extraction
- gogfy has **zero file type tracking** in schema
- **Effort assessment:** Small (1 PR) but unlocks 3+ downstream features

#### 5. Incremental Manifest Pays Off at Scale
- graphify.detect has mtime + MD5 tracking (manifest.py is 4 lines)
- gogfy.detect rescans entire directory every time
- **Performance gap:** On 10K+ file codebases, incremental detection is 10-50x faster
- **Trade-off:** Full rescans are simpler; incremental is more complex but scales better

---

*End of deep audit section (2026-05-15)*
