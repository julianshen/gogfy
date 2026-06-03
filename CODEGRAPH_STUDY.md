# Deep Study: "CodeGraph" — the field, and what gogfy can learn from it

_Compiled June 2026. gogfy is itself a codegraph tool (tree-sitter → nodes/edges →
Leiden communities + centrality → MCP server + report), so this is a competitive
benchmark of the field with concrete takeaways for gogfy._

---

## 1. The landscape: two families share the name

**Family A — classic code-intelligence / static analysis** (powers IDE nav & security):

| Project | Lang | Core idea | Storage | Notes |
|---|---|---|---|---|
| **Joern** (`joernio/joern`, ~3.2k★) | Scala | **Code Property Graph (CPG)** = AST + CFG + PDG in one graph; query via Scala DSL | FlatGraph (columnar embedded) | Reference impl of the CPG concept; security/vuln focus |
| **SCIP** (`sourcegraph/scip`, ~644★) | Go | Protobuf code-intel format; successor to MS **LSIF** | — (transport format) | Human-readable string symbol IDs → ~4–5× smaller than LSIF |
| **stack-graphs** (`github/stack-graphs`, ~877★, **archived Sep 2025**) | Rust | Name resolution at scale via scope graphs; resolution = path-finding | per-file subgraphs | File-incremental; powers GitHub "go to definition" |
| **Glean** (`facebookincubator/Glean`, ~1.4k★) | Haskell | Facts about code, derived & queried with Datalog-flavored **Angle** | RocksDB | Per-language schemas + neutral "views"; stacked immutable DBs |
| **CodeQL** | — | Code → relational DB, query with QL (Datalog) | relational | Recursion/taint first-class |

**Family B — AI-agent codebase knowledge graphs** (the 2024–2026 wave, gogfy's neighborhood):

| Project | Lang | Parse | Store | Hook |
|---|---|---|---|---|
| **potpie** (~5.4k★) | Py | AST | Neo4j | CrewAI agents over the graph |
| **code-graph-rag** (vitali87, ~5–6k★) | Py | tree-sitter (~12 langs) | Memgraph | NL→**Cypher** RAG; MCP server |
| **blarify** (~229★) | Py | tree-sitter + **LSP/SCIP** | Neo4j/FalkorDB | SCIP ~330× faster refs than LSP |
| **colbymchenry/codegraph** (~38k★, Jan 2026) | TS | tree-sitter | **SQLite + FTS5** | 100% local, MCP — token-cost framing (hype-driven outlier) |
| **CodeGraphContext** (~3.6k★) | Py | tree-sitter (22 langs) | Falkor/Kuzu/Neo4j | MCP server |
| **FalkorDB/code-graph** (~309★) | Py | tree-sitter | FalkorDB | GraphRAG demo |

**Common pattern of Family B = exactly gogfy's pattern:** tree-sitter parse →
nodes (file/class/function) + edges (import/call/extends/implements) → graph DB →
exposed to LLMs via MCP / NL→Cypher.

---

## 2. Architecture concepts worth knowing

- **Code Property Graph (Joern / Yamaguchi 2014):** overlay AST + control-flow +
  program-dependence (data + control) on a shared node set, built in **composable
  layers/overlays** (a `META_DATA` node records applied overlays). Enables
  "vulnerability = graph traversal." Found 18 unknown Linux kernel bugs.
- **Stack graphs (GitHub, arXiv:2211.01224):** name binding as graph paths with
  push/pop **symbol & scope stacks**; resolution is path-finding. Scales because
  each file is an **isolated subgraph** and cross-file answers are assembled by
  **partial-path stitching** at query time → O(changed files). Built purely
  syntactically via the **tree-sitter-graph** DSL (no compiler, no build).
- **SCIP vs LSIF:** SCIP is symbol-centric (human-readable structured symbol
  strings) instead of a graph of opaque integer IDs → smaller, debuggable,
  streamable per-document, and **unblocks incremental indexing**.
- **GraphRAG (Microsoft):** index-time graph + **Leiden communities** +
  LLM-generated **hierarchical community summaries**; query-time "global search"
  (over summaries) vs "local search" (neighborhood expansion). _gogfy already does
  the structural half of this — Leiden + GRAPH_REPORT.md is essentially GraphRAG's
  global-summary idea applied to code._

---

## 3. Retrieval techniques that beat / complement embeddings

The 2025–2026 consensus flipped against default vector RAG for code:

- **Structural retrieval beats embeddings on reasoning-heavy tasks.** CodexGraph
  (NAACL 2025): SWE-bench-Lite Pass@1 **22.96% vs 3.11% BM25**. RepoGraph (ICLR
  2025): adding a code graph to 4 agent frameworks gave **avg +32.8% relative**.
  Embeddings find *similar* code but miss *structurally required* code (the actual
  caller, the overridden base method) — and they **fail silently**; grep/graph
  fail loudly with file:line. (Caveat: SWE-bench is small/keyword-friendly; treat
  numbers as relative signal.)
- **Best practice = hybrid + agent-chooses-tool:** vector/grep to *find* seed
  nodes, graph traversal to *expand* context. Don't replace retrieval — **expose
  grep + embeddings + graph as MCP tools** and let the agent pick.
- **Aider's repo map (highest-leverage cheap trick):** files = nodes, symbol
  references = edges; run **Personalized PageRank** biased toward files/symbols in
  the current task; render only **signatures** of top-ranked defs into a **token
  budget**, recomputed per LLM call.
- **RepoGraph k-hop ego-graph:** seed on a symbol, pull 1–2 hop neighborhood (k>2
  adds noise), flatten into prompt or expose as a `search_graph` action.
- **CodexGraph NL→Cypher dual-agent:** one agent writes the NL query, a separate
  agent translates to Cypher — lets a generalist drive any schema, iterate rounds.

---

## 4. Go-specific tooling (for a "deep mode" on Go repos)

gogfy's Go edges today are **syntactic** (tree-sitter, name-based). For *semantic*
accuracy on Go specifically:

- **`go/packages`** (`LoadAllSyntax`) is the canonical loader → ASTs + `types.Info`.
- **`go/types`**: `types.Object` are canonical → ideal stable node keys.
  `TypesInfo.Uses`/`Defs` give exact use→definition (reference) edges;
  `Selections` give field/method-access edges with receiver types.
- **Interface-impl edges (the valuable one Go does uniquely well):**
  `types.Implements(T, I)` **and** `types.Implements(NewPointer(T), I)`
  (pointer-vs-value receivers differ!). Naive is O(types×interfaces) — use a
  **method-set fingerprint index** (gopls's `methodsets` pattern) to scale.
- **Call edges:** `go/ssa` + `go/callgraph` with **RTA** (reachability/dead-code)
  or **VTA** (best precision among practical algos). Note: `go/pointer` is
  **removed/unsupported**; `guru` deleted (use gopls).
- **gopls internals** are the production blueprint for an incremental Go graph:
  per-package serialized indexes `xrefs` (reference graph), `methodsets`
  (implementations), `typerefs` (pruned symbol graph for invalidation); memory
  sublinear in repo size.
- **`go/analysis` + Facts** = modular, cacheable, cross-package extraction
  (analyze each package once, persist edges as gob facts) — the incremental path.

Package-level quick wins: `goda` (query language over the import graph), `godepgraph`,
`go-callvis` (SSA call-graph viz, `-algo rta|vta`).

---

## 5. Tradeoffs & pitfalls (production "what we learned")

1. **Sound vs precise — pick one.** Static call graphs are undecidable →
   over-approximate (sound, for safety) or under-approximate (precise, for nav UX).
   Don't market "complete call graph" for dynamic languages.
2. **Recall ≈ independent of precision** (ICSE 2020). Recall wins come from
   handling **dynamic features** (reflection, dispatch, eval), not context
   sensitivity. Spend budget there.
3. **Design for O(fanout), not O(change).** Glean's honest admission: a header/
   interface edit forces reprocessing all dependents. Maintain a dependency graph
   and recompute the transitive frontier.
4. **Typed, versioned schema + human-readable symbol IDs from day one.** LSIF's
   opaque integer IDs broke incrementality and debugging; SCIP's string IDs fixed
   it. _gogfy's `<lang>:<kind>:<path>:<name>` IDs already follow this lesson._
5. **Per-language schemas + neutral views**, not one rigid cross-language schema
   (Glean). Let clients pick precision level.
6. **tree-sitter is the right default for broad/agent coverage** — error-tolerant
   on broken/in-edit code (Python's `ast` throws). Compiler-grade indexers only
   where precision pays.
7. **Storage:** SQLite + recursive CTEs + FTS5 ships fastest for <~1M nodes /
   single machine but degrades on deep traversals >100k entities; native graph DB
   for deep/expressive path queries; embedded KV + custom logic + replication for
   monorepo scale.
8. **Agent guardrails:** iteration limits (15–25), explicit roles, aggressive
   memory management — graph-driving agents loop forever and blow the context
   window otherwise.
9. **MCP is the convergence interface** (Anthropic/MS/OpenAI). The 2026 selling
   point is **token savings** — front-load the agent's discovery phase into offline
   indexing so it queries the graph instead of re-reading files. _gogfy's whole
   pitch._

---

## 6. What gogfy could learn / borrow (prioritized)

gogfy already nails: tree-sitter multi-lang, human-readable IDs, Leiden
communities + report (≈ GraphRAG global), centrality/god-nodes/bridge-nodes, MCP
server, Neo4j/GraphML export, watch + githook rebuild, local-only + token-cost
framing. Gaps & opportunities:

**High leverage**
- **Token-budgeted "repo map" MCP tool (Aider pattern).** gogfy has centrality;
  add a tool that returns top-N **signatures only** ranked by **Personalized
  PageRank** seeded on the agent's current symbols/files, within a token budget.
  This is the single highest-ROI addition for agent UX.
- **k-hop ego-graph expansion tool (RepoGraph).** `gogfy_neighbors` exists; add a
  flatten-to-prompt ego-graph (k≤2) tool tuned for direct LLM context injection.
- **Hybrid retrieval contract.** Position the semantic layer as *find seeds*, the
  graph as *expand* — document and expose both as distinct MCP tools so the agent
  chooses. Add **AST-boundary chunking** (CocoIndex) for the vector half.

**Medium leverage**
- **Optional Go "deep mode"** using `go/packages` + `go/types` + `ssa/callgraph`
  (RTA/VTA): semantically resolved call edges, exact references, and
  `types.Implements`-based interface-impl edges — far more accurate than the
  current syntactic edges, and gogfy is a Go binary so this ships natively. Gate
  it behind a flag; keep tree-sitter as the default cross-language path.
- **NL→Cypher tool** (CodexGraph dual-agent) over the existing Neo4j push, so
  agents can ask arbitrary structural questions without bespoke tools.
- **Truly incremental indexing (O(fanout)).** Confirm watch/githook recompute only
  changed files + their dependency frontier (stack-graphs/gopls model), not a full
  rebuild. Per-file subgraphs + an import-graph invalidation pass.
- **SQLite (FTS5 + recursive CTE) local store** as an alternative to loading the
  whole `graph.json` — lets large repos be queried without holding the graph in
  memory, matching the colbymchenry/codegraph and CodeGraphContext approach.

**Lower / situational**
- **Data-flow / def-use edges** (a light CPG overlay) for impact-analysis queries
  ("what does changing X affect?") — valuable but expensive; Go-deep-mode first.
- **Agent guardrails** in the MCP layer (traversal depth caps, result-size limits)
  to prevent context blowups.
- **Method-set fingerprint index** if interface-impl edge computation becomes a
  bottleneck at scale.
- **Schema versioning** stamp in `graph.json` (SCIP/Glean lesson) for forward
  compat as the model evolves.

---

## 7. Key sources

- Joern / CPG: https://docs.joern.io/code-property-graph/ · https://cpg.joern.io/ · paper https://comsecuris.com/papers/06956589.pdf
- SCIP: https://sourcegraph.com/blog/announcing-scip · https://github.com/sourcegraph/scip
- Stack graphs: https://arxiv.org/abs/2211.01224 · https://github.blog/open-source/introducing-stack-graphs/
- Glean: https://engineering.fb.com/2024/12/19/developer-tools/glean-open-source-code-indexing/
- GraphRAG: https://github.com/microsoft/graphrag · https://www.microsoft.com/en-us/research/blog/graphrag-unlocking-llm-discovery-on-narrative-private-data/
- CodexGraph: https://arxiv.org/abs/2408.03910 · RepoGraph: https://arxiv.org/abs/2410.14684
- Aider repo map: https://aider.chat/2023/10/22/repomap.html
- Sourcegraph Cody (deprecated embeddings): https://sourcegraph.com/blog/how-cody-understands-your-codebase
- Go tooling: https://pkg.go.dev/golang.org/x/tools/go/packages · /go/ssa · /go/callgraph · https://go.dev/blog/gopls-scalability
- "Why grep beat embeddings": https://jxnl.co/writing/2025/09/11/why-grep-beat-embeddings-in-our-swe-bench-agent-lessons-from-augment/
- potpie https://github.com/potpie-ai/potpie · blarify https://github.com/blarApp/blarify · code-graph-rag https://github.com/vitali87/code-graph-rag · colbymchenry https://github.com/colbymchenry/codegraph
