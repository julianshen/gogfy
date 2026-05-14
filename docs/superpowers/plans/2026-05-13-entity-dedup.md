# Plan: Entity Deduplication (P0)

## Goal
Implement upstream's three-pass entity deduplication pipeline in Go.

## Background
Upstream `graphify` deduplicates nodes using:
1. **Pass 1**: Exact normalization — group nodes by normalized label, merge exact matches
2. **Pass 2**: MinHash/LSH + Jaro-Winkler — fuzzy matching for near-duplicate labels
3. **Pass 3**: LLM tiebreaker — resolve ambiguous pairs with LLM (optional)

## Algorithm Details

### Pass 1: Exact Normalization
- Normalize: `re.sub(r"[^a-z0-9]+", " ", label.lower()).strip()`
- Group nodes by normalized label
- Merge groups with >1 member using `pick_winner`

### Pass 2: MinHash/LSH + Jaro-Winkler
- **Entropy gate**: Only nodes with `entropy(label) >= 2.5` bits/char enter this pass
- **Shingling**: Character-level 3-grams, spaces stripped first
- **MinHash**: 128 permutations
- **LSH**: threshold=0.7, query each candidate's MinHash
- **Jaro-Winkler**: Verify LSH candidates, threshold=92.0 (with +5.0 community boost)
- **Merge**: Union-find for transitive closure

### Pass 3: LLM Tiebreaker (optional)
- Process pairs with Jaro-Winkler score in [75.0, 92.0)
- Batch 30 pairs per LLM call
- Prompt: "are they the same real-world concept? yes/no"
- Merge if LLM says yes

### Winner Selection
- Prefer node without `_c\d+$` chunk suffix
- Prefer shorter ID when tied

### Edge Rewiring
- Remap edge endpoints using dedup mapping
- Drop self-loops

## Go Dependencies
- `github.com/ekzhu/minhashlsh` — MinHash + LSH index
- `github.com/xrash/smetrics` — Jaro-Winkler similarity

## Implementation Plan

### Phase 1: Foundation (1 day)
- [ ] Create `internal/dedup/` package
- [ ] Implement `normalize(label string) string`
- [ ] Implement `entropy(s string) float64` (Shannon entropy in bits/char)
- [ ] Implement `shingles(text string, k int) []string` (character k-grams, spaces stripped)
- [ ] Implement `pick_winner(nodes []schema.Node) schema.Node`
- [ ] Implement `UnionFind` data structure
- [ ] Write tests for all helpers

### Phase 2: Pass 1 — Exact Normalization (1 day)
- [ ] Implement `pass1Exact(nodes []schema.Node) (map[string]string, error)`
- [ ] Returns remap: duplicate ID → winner ID
- [ ] Test with exact matches, case differences, punctuation differences

### Phase 3: Pass 2 — MinHash/LSH + Jaro-Winkler (2 days)
- [ ] Implement `minhash(shingles []string, numPerm int) *minhashlsh.MinHash`
- [ ] Implement `pass2Fuzzy(nodes []schema.Node, remap map[string]string, communities map[string]string) (map[string]string, error)`
- [ ] Build LSH index, query candidates, Jaro-Winkler verify
- [ ] Apply community boost (+5.0 if same community)
- [ ] Test with near-duplicate labels, community boost edge cases

### Phase 4: Pass 3 — LLM Tiebreaker (1 day, optional)
- [ ] Define `LLMBackend` interface
- [ ] Implement `pass3LLM(candidates []schema.Node, remap map[string]string, communities map[string]string, backend LLMBackend) error`
- [ ] Batch pairs, construct prompt, parse responses
- [ ] Test with mock backend

### Phase 5: Integration (1 day)
- [ ] Implement `Deduplicator` struct with `Deduplicate(nodes, edges, communities)` method
- [ ] Wire edge rewiring (remap endpoints, drop self-loops)
- [ ] Integrate into `runPipeline` after graph build, before clustering
- [ ] Add CLI flag `--dedup` (default: true)
- [ ] End-to-end test

### Phase 6: Coverage & Polish (1 day)
- [ ] Achieve >90% test coverage for `internal/dedup`
- [ ] Add benchmarks
- [ ] Go docstrings for all exported identifiers

## Total Estimate: 6-7 days

## Files to Create/Modify
- `internal/dedup/dedup.go` — main deduplicator
- `internal/dedup/normalize.go` — normalization helpers
- `internal/dedup/minhash.go` — MinHash/LSH wrappers
- `internal/dedup/unionfind.go` — Union-Find DS
- `internal/dedup/dedup_test.go` — tests
- `cmd/gogfy/main.go` — wire dedup into pipeline

## Key Thresholds (from upstream)
| Constant | Value |
|----------|-------|
| Entropy threshold | 2.5 bits/char |
| LSH threshold | 0.7 |
| Jaro-Winkler threshold | 92.0 |
| Community boost | +5.0 |
| MinHash permutations | 128 |
| Shingle size | 3 |
| LLM low bound | 75.0 |
| LLM high bound | 92.0 |
| LLM batch size | 30 |
