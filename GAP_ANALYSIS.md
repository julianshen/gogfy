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
| `.graphifyignore` | Partial | Only root-level file read; upstream walks up to VCS root and layers per-directory ignore files with last-match-wins semantics |
| `.graphifyinclude` | Missing | Upstream has allowlist for hidden files; gogfy has no equivalent |
| File type classification | Partial | gogfy uses extension map only; upstream classifies into CODE/DOCUMENT/PAPER/IMAGE/VIDEO with heuristics (shebang, paper signals, asset dir detection) |
| Sensitive file detection | Missing | Upstream skips .env, .pem, credentials, etc. via regex patterns |
| Corpus size warnings | Missing | Upstream warns at 50K words (too small) and 500K words (too large) |
| Google Workspace conversion | Missing | Upstream converts .gdoc, .gsheet, .gslides shortcuts |
| Office structural extraction | Partial | gogfy has extractors but upstream does deeper structural node extraction for XLSX (sheets, tables, columns) |
| Incremental detection (manifest) | Missing | Upstream uses mtime + MD5 manifest for incremental scans; gogfy only has SHA256 cache for extraction |
| Symlink following | Partial | gogfy resolves symlinks via RootGuard; upstream has `follow_symlinks` flag with cycle detection |

### 2.2 Extraction
| Feature | Status | What's Missing |
|---------|--------|----------------|
| Python cross-file import resolution | Partial | gogfy emits `py:call:<name>` synthetic targets; upstream does two-pass resolution for `from .X import Name` → INFERRED `uses` edges |
| Java cross-file import resolution | Partial | Same gap — upstream has two-pass for `import a.b.C` → EXTRACTED edges |
| JS/TS path alias resolution | Missing | Upstream resolves tsconfig path aliases |
| Call graph depth | Partial | gogfy extracts calls as synthetic targets; upstream has richer call-graph with package-qualified vs receiver distinction in some languages |
| Docstring/rationale extraction | Missing | Upstream extracts `# NOTE:`, `# IMPORTANT:`, `# HACK:` etc. as rationale edges |
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
| Community labels | Missing | Upstream generates `.graphify_labels.json` with human-readable community names |

### 2.5 Analysis
| Feature | Status | What's Missing |
|---------|--------|----------------|
| God node filtering | Partial | gogfy filters by degree; upstream excludes file nodes, method stubs, and concept nodes via `_is_file_node` / `_is_concept_node` |
| Surprising connections scoring | Partial | gogfy uses inverse log-degree product; upstream has composite score with confidence weight, cross-file-type bonus, cross-repo bonus, cross-community bonus, peripheral→hub bonus, semantic similarity bonus |
| Cross-file vs single-source modes | Missing | Upstream switches between `_cross_file_surprises` and `_cross_community_surprises` based on corpus size |
| Suggested questions | Partial | gogfy generates god-node + community-bridge questions; upstream generates 7 types: ambiguous_edge, bridge_node, verify_inferred, isolated_nodes, low_cohesion, no_signal |
| Graph diff | Missing | Upstream can compare two graph snapshots |
| Betweenness centrality | Missing | Upstream uses bridge node detection via betweenness |

### 2.6 Report
| Feature | Status | What's Missing |
|---------|--------|----------------|
| Report sections | Partial | gogfy has 4 sections; upstream has 10+ sections: Corpus Check, Summary, Graph Freshness, Community Hubs, God Nodes, Surprising Connections, Hyperedges, Communities, Ambiguous Edges, Knowledge Gaps, Suggested Questions |
| Token cost reporting | Missing | Upstream reports input/output tokens and estimated cost |
| Git commit freshness | Missing | Upstream embeds `built_at_commit` hash |
| Community hub navigation | Missing | Upstream has wikilink navigation to community notes |
| Knowledge gaps section | Missing | Upstream reports isolated nodes, thin communities, high ambiguity |
| Thin community filtering | Missing | Upstream omits communities below min size from report |

### 2.7 Export
| Feature | Status | What's Missing |
|---------|--------|----------------|
| HTML viewer | Partial | gogfy has SVG force-directed; upstream has vis.js with search, click-to-inspect, community filter, physics, hyperedge rendering, aggregated community view for large graphs |
| Obsidian vault export | Missing | Upstream generates full vault with .md per node, wikilinks, YAML frontmatter, tags, Dataview queries, community overviews, .obsidian/graph.json config |
| Obsidian Canvas | Missing | Upstream generates .canvas file with community groups |
| SVG static export | Missing | Upstream has matplotlib-based static SVG |
| Neo4j direct push | Missing | Upstream can push directly to Neo4j via Python driver |
| Callflow HTML | **Done (v1)** | gogfy `callflow` subcommand: section-level overview + per-section Mermaid LR. v1 omits bilingual/labels-file/GRAPH_REPORT integration. |
| Node limit / aggregation | Missing | Upstream auto-aggregates to community-level view when graph exceeds 5000 nodes |
| Confidence score defaults | Missing | Upstream adds `confidence_score` field to edges in JSON |
| Built-at commit metadata | Missing | Upstream embeds git HEAD in graph.json |

### 2.8 MCP Server
| Feature | Status | What's Missing |
|---------|--------|----------------|
| Tools | Partial | gogfy: god_nodes, explain, query, path; upstream: query_graph, get_node, get_neighbors, get_community, god_nodes, graph_stats, shortest_path |
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
| Label sanitization | Missing | Upstream strips control chars and caps at 256 chars |

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
| `graphify global add/remove/list/path` | `__main__.py` | Cross-repo global graph management (gogfy: `gogfy global add/remove/list/path`) ✅ |
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
| Incremental manifest (mtime + MD5) | `detect.py` | Fast-path mtime check, slow-path MD5 verification |
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
