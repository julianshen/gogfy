# AGENTS.md

## Scope
This file applies to the entire repository.

## Project intent
- This repository is implementing a Go-native reimplementation of `safishamsi/graphify`.
- Follow a TDD workflow: **Red -> Green -> Refactor**.
- Begin work from `SPEC.md` and `PLAN.md` before adding features.

## Mandatory TDD rules
1. **Start with a failing test** for every functional change.
2. Commit sequence should reflect TDD intent when practical:
   - Red: add/adjust test and confirm failure for the right reason.
   - Green: implement the minimum production change to pass.
   - Refactor: improve design while keeping tests green.
3. Do not add production behavior without a corresponding test.
4. Bug fixes must include a regression test that fails before the fix.
5. Keep tests deterministic (no flaky timing/network assumptions).
6. Prefer small, focused table-driven tests and golden fixtures where helpful.
7. Before opening/merging PRs, run the full suite (`go test ./...`).

## Engineering rules
- Keep production code fully in Go.
- Prefer deterministic behavior and deterministic tests.
- Add or update tests with every functional change.
- Keep I/O boundaries thin and core logic testable/pure where practical.

## Pull request expectations
- Summarize behavior changes and impacted milestones from `PLAN.md`.
- Include commands run for validation (e.g. `go test ./...`).
- Note any known gaps vs upstream `graphify` parity.
