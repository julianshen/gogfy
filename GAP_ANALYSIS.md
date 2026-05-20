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
| `.graphifyinclude` | **Done** | Dotfile entries (basename starts with `.`) skipped by default at the walk layer — `.git/`, `.vscode/`, `.env` etc. stay out of the corpus without needing a `.graphifyignore` rule. `.graphifyinclude` is the allowlist escape hatch: same gitignore syntax (`.github/`, `.eslintrc.json`, etc.) and same VCS-ancestor chain layering as `.graphifyignore`, but matched paths re-enter the corpus despite the dotfile prefix. |
| File type classification | **Done (extension-based)** | `schema.ClassifyFile` maps extensions to CODE/DOCUMENT/PAPER/IMAGE/VIDEO; `schema.Node.FileType` populated at extract boundary; Corpus report section breaks down counts by type. Shebang + paper-signal heuristics deferred. |
| Sensitive file detection | **Done (extended beyond upstream)** | `detect.IsSensitive` + silent skip in `CollectFiles`. Upstream-parity patterns (.env, .pem/.key, credentials/secrets/tokens, SSH keypairs, .netrc/.pgpass/.htpasswd, cloud creds) plus modern additions: path-anchored `.aws/credentials`, `.aws/config`, `.kube/config`, `.docker/config.json`, `.gnupg/`; package-manager auth (`.npmrc`, `.pypirc`, `.gem/credentials`, `.cargo/credentials.toml`); terraform state + tfvars (highest-impact infra leak class); firebase admin SDK JSON; `apikey`/`access-token`/`client-secret` basenames. Path-anchored patterns are necessary for generic-basename cases (`credentials`, `config`) — basename-only matching would either miss them or false-positive on every `config.go`. |
| Corpus size warnings | **Done (tiny-only)** | Report Corpus section emits a tiny-corpus warning under 5 files. Upstream's "too large" warning isn't ported — gogfy's AST extraction has no per-token cost. |
| Google Workspace conversion | **Done (URL-only)** | `extract.GoogleWorkspaceExtractor` parses .gdoc/.gsheet/.gslides JSON shortcut files and emits a document-typed module node with the linked Drive URL in `SourceLocation`. Distinct lang tags (gdoc/gsheet/gslides) for filterability. Full content conversion via the upstream `gws` CLI deferred — gogfy doesn't currently shell out to external binaries. |
| Office structural extraction | **Done (XLSX)** | XLSX extractor now emits defined-table + column structure: each sheet → `xlsx:section`, each defined Excel table → `xlsx:table` with `contains` edge from sheet, each `<tableColumn>` → `xlsx:column` with `contains` edge from table. Hyperlinks still attribute to the originating sheet's section. `resolveOOXMLPartPath` now normalizes `..` segments via `path.Clean` so worksheet → table rels (which use `../tables/tableN.xml`) resolve to the actual zip entry. |
| Incremental detection (manifest) | **Done** | `internal/manifest` persists `<out>/.gographify-manifest.json` with per-file `{path, mtime, size, sha256, ext}`. `gogfy manifest <root>` writes a snapshot; `gogfy manifest <root> --diff` prints added/removed/modified file lists vs the prior snapshot without running the pipeline. Tooling-facing (stable file schema, sorted/diffable JSON). Diff is content-based — mtime-only changes (rsync, git checkout) do NOT appear as modifications. Mtime fast-path mirrors `internal/cache` so a 10K-file refresh pays N stat()s, not N SHA-256 reads. |
| Symlink following | **Done** | RootGuard still resolves symlinks for security checks (no in-root escape). `gogfy run --follow-symlinks` now also descends into symlinked dirs whose resolved targets stay inside the corpus root. Cycle-safe via a `visitedDirs` set keyed on resolved path — a→b→a cycle terminates instead of looping. Default off (preserves historical behavior of treating symlinked dirs as opaque entries). |

### 2.2 Extraction
| Feature | Status | What's Missing |
|---------|--------|----------------|
| Python cross-file import resolution | **Done (scope-aware)** | `resolve.Calls` builds per-file import scope (bare-name + dotted-root) and narrows AMBIGUOUS fan-outs to candidates whose source-file stem matches an imported module. Multi-candidate calls upgrade to INFERRED only when the narrowing yields exactly one match. |
| Java cross-file import resolution | **Done (scope-aware)** | Same resolver — applies to any extractor emitting `<lang>:module:<filepath>` + `imports` edges. |
| JS/TS path alias resolution | **Done** | `internal/tsalias.Load` reads `tsconfig.json` / `jsconfig.json` `compilerOptions.paths` + `baseUrl`; `Apply` rewrites `js:import:` / `ts:import:` node IDs (and edges that target them) to resolved filesystem paths. Missing config is non-fatal. Wired into runPipeline after Build and before resolve.Calls. |
| Call graph depth | Partial | gogfy extracts calls as synthetic targets; upstream has richer call-graph with package-qualified vs receiver distinction in some languages |
| Docstring/rationale extraction | **Done (with function attribution)** | `internal/rationale.Extract` post-pass surfaces NOTE/IMPORTANT/HACK/WHY/RATIONALE/TODO/FIXME/XXX/WARNING comments across `#`, `//`, `--`, `/*` markers as `rationale_for` edges. When a comment immediately precedes a function declaration (Go / Python / Rust / JS / TS — including method receivers, Python `@decorator` stacks, and JS/TS `const arrow` form), the edge targets the function node directly (`<lang>:function:<path>:<name>`, matching `emitDecl`'s ID scheme) so rationale answers "why does this function exist". File-scope rationales fall back to module attribution. Unsupported langs (Java / Kotlin / C# — too noisy to regex) keep module-only attribution. |
| Semantic extraction (LLM) | **Done (markdown + PDF + images, 6 backends)** | `internal/llm` provider-agnostic interface plus six implementations: `anthropic` (Claude direct), `openai` (GPT-4o-mini), `gemini` (Flash, key in query param), `ollama` (local, no API key), `kimi` (Moonshot, OpenAI-API-compatible), `bedrock` (AWS Bedrock Runtime for Claude 3/3.5/3.7 with pure-Go SigV4 — no aws-sdk dependency). All six absorb vision via `llm.Request.Images` — wire formats (Anthropic source-object, OpenAI/Kimi image_url data URI, Gemini inlineData, Ollama base64 array, Bedrock = Anthropic shape under SigV4) hidden behind one interface. `semantic.Extract` for text; `semantic.ExtractImage` for visual content with a vision-specific prompt. Pipeline routes documents raw, PDFs via `extract.PDFPlainText`, images via `http.DetectContentType` (PNG/JPEG/WebP/GIF supported). Parallel fan-out with 4 workers. |
| Hyperedges | **Done (semantic emit + export)** | New `schema.Hyperedge{Members, Relation, Confidence}` type for N-ary (3+ party) relations. Semantic extraction LLM prompt now requests a `hyperedges` field; `parseResponse` translates member IDs through the `doc:entity:<path>:<id>` scheme, drops pairwise hyperedges (those belong as Relations), defaults missing relation to `co_occurs`. `GraphExport.Hyperedges` (omitempty) carries them through to `graph.json`; `gogfy://stats` MCP resource exposes the hyperedge count. Pipeline accumulates them in `pipelineHyperedges` and attaches at export time — they don't pass through `graph.Builder` (which indexes binary edges only). Hyperedge-aware dedup / cluster / viz are follow-ups. |
| Token tracking | **Done (+ budget guardrails)** | `llm.Response` carries `InputTokens` / `OutputTokens` / `EstimatedUSDCost`; runPipeline sums across all semantic-extract calls and emits a one-line stderr summary. New `--max-cost-usd` and `--max-tokens` flags halt semantic dispatch (in-flight jobs still finish) when the running tally trips the cap; halt reason printed to stderr. |

### 2.3 Graph Build
| Feature | Status | What's Missing |
|---------|--------|----------------|
| Entity deduplication | **Done (three-pass, with LLM tiebreaker wired)** | `internal/dedup.Deduplicator` runs three passes: (1) exact normalization → union-find, (2) MinHash/LSH + Jaro-Winkler with community boost (entropy gating, 128 perms, shingle size 3, Jaro-Winkler threshold 92.0), (3) LLM tiebreaker over the ambiguous [75, 92) Jaro-Winkler band. Pass 3 now bridges to the pipeline's semantic LLM client via a `dedupLLMAdapter` — the same `--backend` that gates extraction also drives the tiebreaker. Per-component member sort makes winner selection deterministic across runs (previously a flake under `-count >= 3`). |
| Normalized ID reconciliation | **Done** | The three-pass dedup already runs Jaro-Winkler across all nodes regardless of source. The missing piece was tie-break direction: `pickWinner` now ranks by FileType (code < paper < document < image < video < rationale) so when a fuzzy-merged pair includes both an AST-extracted code node and an LLM-emitted document node, the AST-grounded ID survives. |
| Direction preservation | Partial | gogfy preserves direction in schema; upstream stashes `_src`/`_tgt` on undirected NetworkX graphs |
| Incremental merge (`build_merge`) | **Done** | `gogfy build-merge <prior.json> <next.json> [--files list.txt] [--out merged.json]` folds a freshly-extracted graph into a prior graph.json, pruning entries whose SourceFile is no longer in `--files`. Synthetic nodes (empty SourceFile — semantic concepts, cross-file resolution targets) survive prune. Edges follow their endpoints: drop iff either endpoint was pruned. `next` wins ID collisions (refresh semantics). When `--files` is empty, defaults to every SourceFile in `next` — gives a sensible default for the common "re-extract everything" case without forcing users to pre-list files. |
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
| God node filtering | **Done (file + stub)** | `isFileHubOrStub` filters `<lang>:module:<filepath>` hubs (accumulate edges mechanically via contains), AST method stubs labeled `.foo()` (extractor placeholders for unresolved receivers), and degree-≤1 `foo()` function stubs from god-node ranking. Concept-node filter deferred (gogfy doesn't model concept nodes yet). |
| Surprising connections scoring | **Done** | Composite integer score: confidence bonus (AMBIGUOUS=3, INFERRED=2, EXTRACTED=1, zeroed for cross-lang INFERRED `calls`), +2 cross-file-type, +2 cross-repo (top-level dir differs), +1 cross-community baseline, ×1.5 for `semantically_similar_to`, +1 peripheral→hub. Tie-break: legacy inverse-log-degree, then input order. |
| Cross-file vs single-source modes | Missing | Upstream switches between `_cross_file_surprises` and `_cross_community_surprises` based on corpus size |
| Suggested questions | **Done** | All 7 upstream types covered: god-node role, ambiguous_edge, verify_inferred, isolated_nodes, low_cohesion (threshold-aligned with cluster splitter), no_signal (empty/edgeless graph short-circuit), community-bridge. Per-category budget prevents one type from crowding out others. |
| Graph diff | **Done** | `internal/graphdiff.Compute` + `gogfy diff <old.json> <new.json>` — markdown summary of added/removed/changed nodes (label/community/file-type drift) and added/removed edges. Edge identity is (source, target, relation); confidence flips on the same edge are intentionally not surfaced. |
| Betweenness centrality | **Done** | `internal/centrality.Betweenness` ports Brandes' O(V·E) algorithm (undirected, dedup self-loops/parallels, dangling-ref-safe). `analyze.Report.BridgeNodes` surfaces top-3 by score with deterministic tie-break; report writes a `## Bridge Nodes` section when non-empty. |

### 2.6 Report
| Feature | Status | What's Missing |
|---------|--------|----------------|
| Report sections | **Done (10/11)** | Added Summary, Corpus, Graph Freshness (conditional), Community Hubs, Communities (with thin-community filtering), Ambiguous Edges, Knowledge Gaps. Hyperedges omitted — gogfy doesn't model N-ary relations. `report.RenderWithOptions` carries the extended data; legacy `Render(r)` preserved as a trimmed variant. |
| Token cost reporting | **Done** | `report.Options.SemanticCost` (pointer; nil omits the section) feeds a `## Semantic Extraction` section listing backend, file count, input/output/total tokens, and USD estimate. Populated by runPipeline when `--semantic` is active. |
| Git commit freshness | **Done** | `internal/gitmeta.HeadShortSHA` reads `.git/HEAD` + the referenced ref file (or `packed-refs` fallback) — no shell-out. Auto-populated in runPipeline, runClusterOnly, reportCommand so the Graph Freshness section appears on every report inside a repo. Worktree `.git`-file pointers resolved. |
| Community hub navigation | **Done** | `index.md` lists every community as `[label](slug.md)` link sorted by member count; per-community articles' Relationships section cross-references other communities as `[label](slug.md)` links. Markdown link syntax (not Obsidian `[[wikilinks]]`) so navigation works in any viewer; the Obsidian vault export uses `[[...]]` separately. |
| Knowledge gaps section | **Done** | Composite digest: isolated-node count, thin-community count, ambiguous-edge count. Each line shows only when count > 0. |
| Thin community filtering | **Done** | `Options.ThinCommunityMin` (default 2) drops single-node \"communities\" from the Communities section and feeds the Knowledge Gaps thin-community counter. |

### 2.7 Export
| Feature | Status | What's Missing |
|---------|--------|----------------|
| HTML viewer | Partial | gogfy has SVG force-directed with search, click-to-inspect, community filter, AND aggregated community view above 1000 nodes (renders meta-graph with weighted cross-community edges so big repos stay navigable). Still missing: vis.js physics, hyperedge rendering (hyperedges not modeled). |
| Obsidian vault export | **Done (v1)** | `gogfy obsidian` writes per-node .md with YAML frontmatter + [[wikilinks]] + tags (graphify/{type,confidence}, community/{name}); per-community `_COMMUNITY_<name>.md` with Members + Dataview query + cross-community connections. Auto-loads `.graphify_labels.json`. v1 omits: cohesion-strength descriptor, top-bridge-nodes block, `.obsidian/graph.json` color config, canvas export. |
| Obsidian Canvas | **Done** | `obsidian.Canvas` writes `graph.canvas` alongside the vault: communities as colored groups in a √N×√N grid, member nodes as 180×60 file cards (3 per row), capped at 200 edges with relation+confidence labels. Opens in Obsidian as an infinite canvas. |
| SVG static export | **Done** | `gogfy svg <graph.json> [--out path.svg]` writes a self-contained .svg (no JavaScript, no D3). `internal/export.StaticSVG` runs a small Fruchterman-Reingold force-directed pass in pure Go (deterministic — seeded PRNG + ID-sorted layout — for stable CI snapshots) then emits circles + lines + optional labels. Nodes colored by community for visual structure. Suitable for embedding in docs / PRs / README files; opens in any SVG viewer. Layout is O(iters · N²); pre-aggregate to community level for very large corpora. |
| Neo4j direct push | Missing | Upstream can push directly to Neo4j via Python driver |
| Callflow HTML | **Done (v1)** | gogfy `callflow` subcommand: section-level overview + per-section Mermaid LR. v1 omits bilingual/labels-file/GRAPH_REPORT integration. |
| Node limit / aggregation | Missing | Upstream auto-aggregates to community-level view when graph exceeds 5000 nodes |
| Confidence score defaults | **Done** | `Confidence.Score()` maps Extracted=1.0, Inferred=0.5, Ambiguous=0.25; `Edge.MarshalJSON` emits a derived `confidence_score` field next to the existing int. Additive — int Confidence stays authoritative for round-trip. |
| Built-at commit metadata | **Done** | `GraphExport.BuiltAtCommit` (json `built_at_commit`, omitempty for backwards compat) populated from `gitmeta.HeadShortSHA` at both runPipeline and runClusterOnly export sites. Cross-tool consumers can detect a stale snapshot against a fresh repo. |

### 2.8 MCP Server
| Feature | Status | What's Missing |
|---------|--------|----------------|
| Tools | **Done** | gogfy: god_nodes, explain (superset of upstream get_node), query, path, get_neighbors, graph_stats, get_community — all 7 upstream tools covered. |
| Resources | **Done** | MCP server exposes six resources: `gogfy://report` (markdown), plus `gogfy://stats` (corpus counts + breakdowns), `gogfy://god-nodes`, `gogfy://surprising-links`, `gogfy://questions`, `gogfy://audit` (confidence-summary). All five JSON resources expose existing analyze-report slices that previously required parsing the markdown report. |
| BFS/DFS traversal | **Done (BFS)** | New `gogfy_traverse` MCP tool: BFS from a starting node up to `depth` hops, capped at `limit` total nodes, returns the visited subgraph grouped by hop distance. Treats edges as undirected (direction is extractor implementation detail; agents want local context). |
| Node scoring | **Done** | `gogfy_query` ranks matches by tier: exact label (100) > prefix (50) > contains (25) > ID-contains (15) > source-file-contains (10). Degree adds a capped tie-break bonus so popular nodes outrank obscure ones at the same match quality. |

### 2.9 Cache
| Feature | Status | What's Missing |
|---------|--------|----------------|
| Semantic cache | **Done** | `internal/semantic.Cache` stores per-file extraction results at `<out>/.gographify-cache/semantic/<key>.json`. Cache key is `sha256(clientName ‖ systemPrompt ‖ mode ‖ frontmatter-stripped-src)` — switching LLM backends or revising the prompt invalidates everything; swapping markdown frontmatter (`kind: tweet` ↔ `kind: github`) hits the same slot. Per-entry layout gives cheap atomicity (rename-on-write), trivial invalidation (delete one file), and corruption recovery (one bad JSON treated as a miss, not poisoned cache). LLM errors do NOT populate the cache so a transient failure doesn't shadow future healthy responses. AST result cache is deferred — AST extraction is cheap; the cost-driving cache is the LLM one.|
| Legacy migration | Missing | Upstream migrates flat cache to hierarchical |
| Cache corruption handling | **Done (semantic)** | One bad JSON entry in the semantic cache directory is treated as a miss (not a fatal error); the next successful extraction rewrites it atomically via fsutil.WriteFileAtomic. The file-hash cache (`internal/cache`) already had partial-write recovery via atomic rename. |

### 2.10 Security
| Feature | Status | What's Missing |
|---------|--------|----------------|
| URL validation | **Done** | `safefetch.Fetch` rejects non-http(s) schemes, validates hostname, optional suffix-allowlist. |
| SSRF-guarded socket | **Done** | `validateHost` resolves the URL's hostname BEFORE the request and rejects any IP in private/loopback/link-local/cloud-metadata ranges. Redirects re-run the check at every hop so a server can't bounce to an internal address. Resolves all IPs (not just first) to defeat DNS rebinding. |
| Safe fetch | **Done** | `internal/safefetch` ships SSRF guard + 10 MiB default size cap + 30s timeout + 5-redirect cap. Drop-in replacement for raw `http.Get` for user-supplied URLs. |
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
| URL fetching with type detection | **Done (PDF + HTML + tweet/OG + GitHub + YouTube; arXiv URL rewrite)** | `Ingest` detects PDF responses via `%PDF-` magic bytes plus URL-suffix fallback. arXiv `abs/<id>` URLs are rewritten to `pdf/<id>.pdf`. GitHub repo URLs are rewritten to `raw.githubusercontent.com/<owner>/<repo>/HEAD/README.md`; `/blob/<ref>/<path>` URLs are rewritten to the matching raw-content URL so file fetches land plain text rather than HTML chrome. YouTube URLs (`watch`, `youtu.be/`, `shorts/`, `embed/`) take a separate branch that fetches the oembed JSON endpoint and writes a metadata-only sidecar (title/author/thumbnail). HTML responses have OpenGraph + Twitter-Card meta tags extracted and prepended as a quoted metadata block. Sidecar frontmatter is labeled `kind: tweet` / `kind: github` / `kind: youtube` for downstream routing. Host-spoofing tests cover all detectors. |
| HTML-to-markdown conversion | **Done (with readability narrowing)** | `internal/ingest.htmlToMarkdown` narrows to the largest `<article>` (or `<main>`) block when present, then strips script/style/noscript and boilerplate regions (`<nav>`/`<footer>`/`<aside>`/`<header>`) before converting h1-h6/p/li/br and decoding entities. Two-pass cleanup drops most blog/docs chrome before semantic extraction sees the text. Largest-block selection guards against pages that wrap related-post thumbnails in nested `<article>` tags. |
| arXiv abstract extraction | **Done (PDF fetch)** | arXiv `abs/<id>[v<n>]` URLs (including pre-2007 cross-list ids like `cs/0601001`) are rewritten to their PDF endpoint at fetch time; the response lands as a `.pdf` sidecar that the existing PDF extractor processes during the next `gogfy run`. |
| YouTube audio download | Missing | Upstream uses yt-dlp; no-shell-out policy makes this a larger lift. |
| Binary download for PDFs/images | **Done (PDF + image)** | PDF bodies save verbatim as `.pdf`; PNG/JPEG/GIF/WebP responses are detected via magic bytes (with URL-suffix fallback for servers that send octet-stream) and saved as `.png`/`.jpg`/`.gif`/`.webp` sidecars. The extension comes from the actual bytes so an attacker can't trick the classifier by lying about either the URL or the content type alone. SVG stays on the markdown path — it's XML and text extraction indexes its `<text>` content. Sidecars feed downstream `semantic.ExtractImage` directly. |
| Query result memory | `ingest.py` | save_query_result for feedback loop |

### 3.5 Transcription
| Feature | Status | Notes |
|---------|--------|-------|
| Video/audio transcription | **Done (Whisper backend + parallel pipeline)** | `internal/transcribe` defines a pluggable `Client` interface plus `IsTranscribable(ext)` covering common audio/video extensions. `internal/transcribe/whisper` implements an OpenAI `/v1/audio/transcriptions` backend (multipart upload, verbose_json for duration-based cost). `runPipeline` collects transcribable files into `transcribeJobs` during the file walk and fans out the Whisper calls in parallel (concurrency cap 2 — tighter than semantic's 4 because Whisper rate-limits are stricter and each request is heavier). Results are converted to semantic-text jobs before `runSemanticJobs` runs. `--transcribe-backend` requires `--semantic` (validated). Per-run transcribe cost summary printed to stderr alongside the semantic-cost line. |
| YouTube audio extraction | Missing | Upstream uses yt-dlp; gogfy's no-shell-out policy makes this a larger lift. |
| Domain hint generation | **Done** | `transcribe.BuildPrompt(godNodes)` folds up to 5 top god-node labels into a "Technical discussion about X, Y, Z. Use proper punctuation..." prompt (mirrors upstream graphify's `build_whisper_prompt`). Pipeline calls `loadPriorGodNodePrompt(out)` which re-analyzes the previous run's `graph.json` to derive labels — bootstrap-only for first runs but accumulates across iterations. `GOGFY_WHISPER_PROMPT` env override wins when set (lets coding agents inject hand-crafted domain hints). |

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
