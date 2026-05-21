# TDD Implementation Plan (Red -> Green -> Refactor)

> **Historical document.** This is the original milestone plan that
> bootstrapped the project; all milestones below are complete. Current
> feature state lives in [README.md](README.md) and
> [GAP_ANALYSIS.md](GAP_ANALYSIS.md). Kept for the build-order rationale.

## Milestone 0 — project bootstrap
1. Initialize Go module and folder layout.
2. Add test harness (`go test ./...`), lint config, and golden test helpers.

## Milestone 1 — schema + validation
### Red
- Write failing tests for node/edge schema validation and confidence enum checks.
### Green
- Implement minimal structs + validator.
### Refactor
- Extract shared test fixtures and table-driven validators.

## Milestone 2 — file detection
### Red
- Failing tests for:
  - extension filtering
  - `.graphifyignore` behavior
  - stable ordering
### Green
- Implement `collect_files(root)`.
### Refactor
- Separate matcher and walker; benchmark with medium fixture corpus.

## Milestone 3 — AST extraction (Go first)
### Red
- Failing tests from fixtures asserting discovered nodes/edges:
  - package/function nodes
  - imports edges
  - calls edges (extracted + inferred where applicable)
### Green
- Implement Go extractor with tree-sitter-go.
### Refactor
- Introduce language extractor interface and reuse traversal utilities.

## Milestone 4 — graph build and dedupe
### Red
- Failing tests for duplicate nodes, multi-file edge merge, deterministic ids.
### Green
- Implement graph builder.
### Refactor
- Move dedupe/index logic into isolated package.

## Milestone 5 — clustering
### Red
- Failing tests on synthetic graph with expected community labels.
### Green
- Implement cluster adapter (Leiden-compatible strategy).
### Refactor
- Hide algorithm behind interface for swap/testing.

## Milestone 6 — analysis
### Red
- Failing tests for god node ranking and surprising-link scoring.
### Green
- Implement analyzers.
### Refactor
- Tune scoring constants and document rationale.

## Milestone 7 — report rendering
### Red
- Golden-file tests for `GRAPH_REPORT.md` sections.
### Green
- Implement renderer.
### Refactor
- Stabilize formatting and localization hooks.

## Milestone 8 — export artifacts
### Red
- Failing tests for JSON schema + HTML payload existence.
### Green
- Implement exporters.
### Refactor
- Separate serialization and filesystem writer.

## Milestone 9 — incremental update cache
### Red
- Failing tests to ensure unchanged files are skipped and merged graph persists.
### Green
- Implement SHA256 cache and merge logic.
### Refactor
- Add corruption handling + recovery tests.

## Milestone 10 — E2E fixtures
### Red
- End-to-end test with miniature corpus comparing expected graph/report snapshots.
### Green
- Wire full pipeline command.
### Refactor
- Performance pass and parallel extraction tuning.

## Definition of done
- `go test ./...` passing.
- Deterministic snapshots across 3 reruns.
- README documents exact supported parity vs upstream graphify.
