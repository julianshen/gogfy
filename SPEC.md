# GoGraphify Specification (Go reimplementation of `safishamsi/graphify`)

## 1. Objective
Build a **fully Go** CLI/library that reproduces the core behavior of graphify’s local knowledge-graph pipeline:

`detect -> extract -> build_graph -> cluster -> analyze -> report -> export`

Primary compatibility goal: identical conceptual outputs (`graph.json`, `GRAPH_REPORT.md`, optional `graph.html`) from the same input corpus.

## 2. Product scope (Phase 1)
- Local directory corpus ingestion.
- Deterministic code extraction for selected languages (start with Go + Python, extensible).
- Semantic extraction interface (provider-agnostic), initially behind adapter ports.
- Knowledge graph build + typed edge confidence (`EXTRACTED`, `INFERRED`, `AMBIGUOUS`).
- Community detection (Leiden or equivalent modularity clustering in Go binding/service mode).
- Analysis outputs:
  - god nodes (high centrality)
  - surprising connections (cross-community/high-signal links)
  - suggested exploration questions
- Output artifacts:
  - `graphify-out/graph.json`
  - `graphify-out/GRAPH_REPORT.md`
  - `graphify-out/graph.html` (interactive)
- Ignore rules via `.graphifyignore` semantics.
- Cache for incremental reruns keyed by file hash.

## 3. Non-goals (Phase 1)
- Multi-platform installer glue for every AI coding assistant.
- Video/audio transcription pipeline.
- Every language parser supported by upstream.

## 4. Functional requirements
1. `collect-files` honors include extensions and `.graphifyignore` patterns.
2. `extract` produces normalized schema:
   - nodes: `id`, `label`, `source_file`, `source_location`
   - edges: `source`, `target`, `relation`, `confidence`
3. `build-graph` merges all extraction outputs into one graph with de-duplication.
4. `cluster` annotates each node with `community`.
5. `analyze` computes ranked summaries and diagnostics.
6. `report` renders deterministic markdown report sections.
7. `export` writes graph JSON and HTML viewer payload.
8. `--update` mode reprocesses changed files only and merges correctly.

## 5. Quality attributes
- Deterministic output for deterministic inputs.
- No global mutable state.
- No network requirement for purely AST-based extraction path.
- Testable pure functions with thin IO boundary adapters.

## 6. Architecture (Go)
Suggested packages:
- `internal/detect`
- `internal/extract`
- `internal/graph`
- `internal/cluster`
- `internal/analyze`
- `internal/report`
- `internal/export`
- `internal/cache`
- `internal/security`
- `cmd/gographify`

Data flow uses immutable-ish structs and explicit interfaces.

## 7. Data model
```go
type Confidence string
const (
    Extracted Confidence = "EXTRACTED"
    Inferred  Confidence = "INFERRED"
    Ambiguous Confidence = "AMBIGUOUS"
)

type Node struct {
    ID             string
    Label          string
    SourceFile     string
    SourceLocation string
    Community      string // optional post-cluster
}

type Edge struct {
    Source     string
    Target     string
    Relation   string
    Confidence Confidence
}
```

## 8. CLI contract (initial)
- `gographify run <root> [--update] [--out graphify-out]`
- `gographify validate <graph.json>`
- `gographify report <graph.json>`

## 9. Risks
- Leiden/community parity in pure Go may differ from Python stack.
- Tree-sitter binding behavior can differ by language grammar versions.
- Semantic extraction parity depends on provider prompts and schemas.
