# GoGraphify Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a fully Go-native CLI/library that reproduces the core `graphify` knowledge-graph pipeline: `detect -> extract -> build_graph -> cluster -> analyze -> report -> export`.

**Architecture:** Pure Go implementation with thin IO adapters. Domain logic in internal packages behind explicit interfaces. Immutable-ish structs. Deterministic outputs. No global mutable state.

**Tech Stack:** Go 1.24+, tree-sitter-go (CGO), standard library for JSON/Markdown/HTML.

---

## File Structure

```
.
├── cmd/gographify/
│   └── main.go                 # CLI entrypoint (cobra or std flags)
├── internal/
│   ├── schema/
│   │   ├── schema.go           # Node, Edge, Confidence types
│   │   └── schema_test.go      # validation tests
│   ├── detect/
│   │   ├── detect.go           # file collection with ignore rules
│   │   └── detect_test.go      # extension + ignore tests
│   ├── extract/
│   │   ├── extract.go          # Extractor interface + registry
│   │   ├── goextractor.go      # Go AST extraction (tree-sitter)
│   │   └── extract_test.go     # fixture-based tests
│   ├── graph/
│   │   ├── graph.go            # Graph builder + dedupe + merge
│   │   └── graph_test.go       # deterministic id + merge tests
│   ├── cluster/
│   │   ├── cluster.go          # Clusterer interface + Leiden adapter
│   │   └── cluster_test.go     # synthetic graph community tests
│   ├── analyze/
│   │   ├── analyze.go          # god nodes, surprising links, questions
│   │   └── analyze_test.go     # ranking + scoring tests
│   ├── report/
│   │   ├── report.go           # GRAPH_REPORT.md renderer
│   │   └── report_test.go      # golden file tests
│   ├── export/
│   │   ├── export.go           # JSON + HTML exporters
│   │   └── export_test.go      # schema + payload tests
│   ├── cache/
│   │   ├── cache.go            # SHA256 incremental cache
│   │   └── cache_test.go       # skip unchanged + merge tests
│   └── security/
│       └── security.go         # (stub for Phase 1)
├── testdata/
│   ├── fixtures/
│   │   ├── go/
│   │   │   ├── simple/
│   │   │   │   ├── main.go
│   │   │   │   └── lib.go
│   │   │   └── .graphifyignore
│   │   └── golden/
│   │       ├── graph.json
│   │       └── GRAPH_REPORT.md
│   └── e2e/
│       └── mini-corpus/        # end-to-end fixture
├── go.mod
├── go.sum
├── Makefile                    # test, lint, build targets
└── README.md
```

---

## Chunk 1: Milestone 0 — Project Bootstrap

### Task 1: Initialize Go module and folder layout

**Files:**
- Create: `go.mod`
- Create: `Makefile`
- Create: `.gitignore`
- Create: `cmd/gographify/main.go`
- Create: `internal/schema/schema.go`
- Create: `internal/detect/detect.go`
- Create: `internal/extract/extract.go`
- Create: `internal/graph/graph.go`
- Create: `internal/cluster/cluster.go`
- Create: `internal/analyze/analyze.go`
- Create: `internal/report/report.go`
- Create: `internal/export/export.go`
- Create: `internal/cache/cache.go`
- Create: `internal/security/security.go`

- [ ] **Step 1: Initialize module**

Run: `go mod init github.com/julianshen/gogfy`
Expected: `go.mod` created

- [ ] **Step 2: Create Makefile with test/lint/build targets**

```makefile
.PHONY: test lint build

test:
	go test ./...

lint:
	golangci-lint run ./...

build:
	go build -o bin/gographify ./cmd/gographify
```

- [ ] **Step 3: Create stub main.go**

```go
package main

import "fmt"

func main() {
    fmt.Println("gographify")
}
```

- [ ] **Step 4: Create package stubs**

Each internal package gets a minimal `package X` file.

- [ ] **Step 5: Verify build**

Run: `go build ./...`
Expected: success

- [ ] **Step 6: Commit**

```bash
git add .
git commit -m "chore: bootstrap go module and package layout"
```

---

## Chunk 2: Milestone 1 — Schema + Validation

### Task 2: Schema types and validation

**Files:**
- Create: `internal/schema/schema.go`
- Create: `internal/schema/schema_test.go`

- [ ] **Step 1: Write failing test**

```go
package schema

import "testing"

func TestConfidenceEnum(t *testing.T) {
    if Extracted != "EXTRACTED" {
        t.Fatal("Extracted confidence mismatch")
    }
    if Inferred != "INFERRED" {
        t.Fatal("Inferred confidence mismatch")
    }
    if Ambiguous != "AMBIGUOUS" {
        t.Fatal("Ambiguous confidence mismatch")
    }
}

func TestNodeValidation(t *testing.T) {
    n := Node{ID: "", Label: "test"}
    if err := n.Validate(); err == nil {
        t.Fatal("expected error for empty ID")
    }
}
```

Run: `go test ./internal/schema/...`
Expected: FAIL (types and Validate not defined)

- [ ] **Step 2: Implement minimal schema**

```go
package schema

import "errors"

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
    Community      string
}

func (n Node) Validate() error {
    if n.ID == "" {
        return errors.New("node ID required")
    }
    return nil
}

type Edge struct {
    Source     string
    Target     string
    Relation   string
    Confidence Confidence
}

func (e Edge) Validate() error {
    if e.Source == "" || e.Target == "" {
        return errors.New("edge source and target required")
    }
    return nil
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/schema/...`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/schema/
git commit -m "feat(schema): add Node, Edge, Confidence types with validation"
```

---

## Chunk 3: Milestone 2 — File Detection

### Task 3: File collection with extension filtering and ignore rules

**Files:**
- Create: `internal/detect/detect.go`
- Create: `internal/detect/detect_test.go`
- Create: `testdata/fixtures/go/simple/main.go`
- Create: `testdata/fixtures/go/simple/lib.go`
- Create: `testdata/fixtures/go/.graphifyignore`

- [ ] **Step 1: Write failing tests**

```go
package detect

import (
    "os"
    "path/filepath"
    "testing"
)

func TestCollectFilesFiltersExtensions(t *testing.T) {
    root := t.TempDir()
    os.WriteFile(filepath.Join(root, "a.go"), []byte("package a"), 0644)
    os.WriteFile(filepath.Join(root, "b.py"), []byte("# b"), 0644)
    os.WriteFile(filepath.Join(root, "c.txt"), []byte("c"), 0644)

    files, err := CollectFiles(root, []string{".go", ".py"})
    if err != nil {
        t.Fatal(err)
    }
    if len(files) != 2 {
        t.Fatalf("expected 2 files, got %d", len(files))
    }
}

func TestCollectFilesRespectsGraphifyIgnore(t *testing.T) {
    root := t.TempDir()
    os.WriteFile(filepath.Join(root, "keep.go"), []byte("package keep"), 0644)
    sub := filepath.Join(root, "vendor")
    os.MkdirAll(sub, 0755)
    os.WriteFile(filepath.Join(sub, "skip.go"), []byte("package skip"), 0644)
    os.WriteFile(filepath.Join(root, ".graphifyignore"), []byte("vendor/\n"), 0644)

    files, err := CollectFiles(root, []string{".go"})
    if err != nil {
        t.Fatal(err)
    }
    if len(files) != 1 {
        t.Fatalf("expected 1 file, got %d", len(files))
    }
    if filepath.Base(files[0]) != "keep.go" {
        t.Fatalf("expected keep.go, got %s", files[0])
    }
}
```

Run: `go test ./internal/detect/...`
Expected: FAIL (CollectFiles not defined)

- [ ] **Step 2: Implement CollectFiles**

```go
package detect

import (
    "bufio"
    "os"
    "path/filepath"
    "strings"
)

func CollectFiles(root string, extensions []string) ([]string, error) {
    ignorePatterns, err := loadIgnorePatterns(root)
    if err != nil {
        return nil, err
    }

    extSet := make(map[string]bool, len(extensions))
    for _, e := range extensions {
        extSet[e] = true
    }

    var files []string
    err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
        if err != nil {
            return err
        }
        rel, _ := filepath.Rel(root, path)
        for _, pat := range ignorePatterns {
            matched, _ := filepath.Match(pat, rel)
            if matched || strings.HasPrefix(rel, pat) {
                if info.IsDir() {
                    return filepath.SkipDir
                }
                return nil
            }
        }
        if !info.IsDir() {
            ext := filepath.Ext(path)
            if extSet[ext] {
                files = append(files, path)
            }
        }
        return nil
    })
    return files, err
}

func loadIgnorePatterns(root string) ([]string, error) {
    f, err := os.Open(filepath.Join(root, ".graphifyignore"))
    if err != nil {
        if os.IsNotExist(err) {
            return nil, nil
        }
        return nil, err
    }
    defer f.Close()

    var patterns []string
    scanner := bufio.NewScanner(f)
    for scanner.Scan() {
        line := strings.TrimSpace(scanner.Text())
        if line != "" && !strings.HasPrefix(line, "#") {
            patterns = append(patterns, line)
        }
    }
    return patterns, scanner.Err()
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/detect/...`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/detect/ testdata/
git commit -m "feat(detect): collect files with extension filter and .graphifyignore"
```

---

## Chunk 4: Milestone 3 — AST Extraction (Go)

### Task 4: Go extractor with tree-sitter

**Files:**
- Create: `internal/extract/extract.go` (interface)
- Create: `internal/extract/goextractor.go`
- Create: `internal/extract/extract_test.go`
- Create: `testdata/fixtures/go/simple/main.go`
- Create: `testdata/fixtures/go/simple/lib.go`

- [ ] **Step 1: Write failing test**

```go
package extract

import (
    "path/filepath"
    "testing"
)

func TestGoExtractor(t *testing.T) {
    ex := &GoExtractor{}
    result, err := ex.Extract("testdata/fixtures/go/simple/main.go")
    if err != nil {
        t.Fatal(err)
    }
    if len(result.Nodes) == 0 {
        t.Fatal("expected nodes")
    }
    if len(result.Edges) == 0 {
        t.Fatal("expected edges")
    }
}
```

Run: `go test ./internal/extract/...`
Expected: FAIL (GoExtractor not defined)

- [ ] **Step 2: Implement Extractor interface and GoExtractor**

```go
package extract

import "github.com/julianshen/gogfy/internal/schema"

type Result struct {
    Nodes []schema.Node
    Edges []schema.Edge
}

type Extractor interface {
    Extract(path string) (Result, error)
}
```

```go
package extract

// GoExtractor uses tree-sitter-go to extract package/function nodes
// and import/call edges. Stub for initial green phase.
type GoExtractor struct{}

func (g *GoExtractor) Extract(path string) (Result, error) {
    // TODO: integrate tree-sitter-go
    return Result{}, nil
}
```

- [ ] **Step 3: Run test (should still fail — needs real extraction)**

Run: `go test ./internal/extract/...`
Expected: FAIL (no nodes/edges)

- [ ] **Step 4: Implement minimal tree-sitter extraction**

Integrate `github.com/smacker/go-tree-sitter` with Go grammar. Parse file, walk AST, emit package/function nodes and import edges.

- [ ] **Step 5: Run tests**

Run: `go test ./internal/extract/...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/extract/
git commit -m "feat(extract): Go AST extractor with tree-sitter"
```

---

## Chunk 5: Milestone 4 — Graph Build and Dedupe

### Task 5: Graph builder with deterministic IDs

**Files:**
- Create: `internal/graph/graph.go`
- Create: `internal/graph/graph_test.go`

- [ ] **Step 1: Write failing test**

```go
package graph

import (
    "testing"
    "github.com/julianshen/gogfy/internal/schema"
)

func TestGraphBuilderDedupesNodes(t *testing.T) {
    b := NewBuilder()
    b.AddNode(schema.Node{ID: "pkg:main", Label: "main"})
    b.AddNode(schema.Node{ID: "pkg:main", Label: "main"})
    g := b.Build()
    if len(g.Nodes) != 1 {
        t.Fatalf("expected 1 node, got %d", len(g.Nodes))
    }
}

func TestGraphBuilderMergesEdges(t *testing.T) {
    b := NewBuilder()
    b.AddEdge(schema.Edge{Source: "a", Target: "b", Relation: "imports"})
    b.AddEdge(schema.Edge{Source: "a", Target: "b", Relation: "imports"})
    g := b.Build()
    if len(g.Edges) != 1 {
        t.Fatalf("expected 1 edge, got %d", len(g.Edges))
    }
}
```

Run: `go test ./internal/graph/...`
Expected: FAIL

- [ ] **Step 2: Implement Builder**

```go
package graph

import "github.com/julianshen/gogfy/internal/schema"

type Graph struct {
    Nodes []schema.Node
    Edges []schema.Edge
}

type Builder struct {
    nodes map[string]schema.Node
    edges map[edgeKey]schema.Edge
}

type edgeKey struct {
    Source   string
    Target   string
    Relation string
}

func NewBuilder() *Builder {
    return &Builder{
        nodes: make(map[string]schema.Node),
        edges: make(map[edgeKey]schema.Edge),
    }
}

func (b *Builder) AddNode(n schema.Node) {
    b.nodes[n.ID] = n
}

func (b *Builder) AddEdge(e schema.Edge) {
    b.edges[edgeKey{e.Source, e.Target, e.Relation}] = e
}

func (b *Builder) Build() Graph {
    g := Graph{
        Nodes: make([]schema.Node, 0, len(b.nodes)),
        Edges: make([]schema.Edge, 0, len(b.edges)),
    }
    for _, n := range b.nodes {
        g.Nodes = append(g.Nodes, n)
    }
    for _, e := range b.edges {
        g.Edges = append(g.Edges, e)
    }
    return g
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/graph/...`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/graph/
git commit -m "feat(graph): builder with node dedupe and edge merge"
```

---

## Chunk 6: Milestone 5 — Clustering

### Task 6: Community detection adapter

**Files:**
- Create: `internal/cluster/cluster.go`
- Create: `internal/cluster/cluster_test.go`

- [ ] **Step 1: Write failing test**

```go
package cluster

import (
    "testing"
    "github.com/julianshen/gogfy/internal/schema"
)

func TestClustererAssignsCommunities(t *testing.T) {
    nodes := []schema.Node{
        {ID: "a"}, {ID: "b"}, {ID: "c"},
    }
    edges := []schema.Edge{
        {Source: "a", Target: "b"},
        {Source: "b", Target: "c"},
    }
    c := NewLeidenClusterer()
    result, err := c.Cluster(nodes, edges)
    if err != nil {
        t.Fatal(err)
    }
    for _, n := range result {
        if n.Community == "" {
            t.Fatalf("node %s missing community", n.ID)
        }
    }
}
```

Run: `go test ./internal/cluster/...`
Expected: FAIL

- [ ] **Step 2: Implement Clusterer interface and stub**

```go
package cluster

import "github.com/julianshen/gogfy/internal/schema"

type Clusterer interface {
    Cluster(nodes []schema.Node, edges []schema.Edge) ([]schema.Node, error)
}

type LeidenClusterer struct{}

func NewLeidenClusterer() *LeidenClusterer {
    return &LeidenClusterer{}
}

func (l *LeidenClusterer) Cluster(nodes []schema.Node, edges []schema.Edge) ([]schema.Node, error) {
    // TODO: real Leiden implementation
    for i := range nodes {
        nodes[i].Community = "0"
    }
    return nodes, nil
}
```

- [ ] **Step 3: Run test (should pass with stub)**

Run: `go test ./internal/cluster/...`
Expected: PASS

- [ ] **Step 4: Replace stub with real Leiden or modularity algorithm**

Research and integrate a Go Leiden implementation or Louvain fallback. Ensure deterministic output.

- [ ] **Step 5: Run tests**

Run: `go test ./internal/cluster/...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/cluster/
git commit -m "feat(cluster): community detection with Leiden adapter"
```

---

## Chunk 7: Milestone 6 — Analysis

### Task 7: God nodes, surprising links, exploration questions

**Files:**
- Create: `internal/analyze/analyze.go`
- Create: `internal/analyze/analyze_test.go`

- [ ] **Step 1: Write failing tests**

```go
package analyze

import (
    "testing"
    "github.com/julianshen/gogfy/internal/schema"
)

func TestGodNodes(t *testing.T) {
    nodes := []schema.Node{
        {ID: "hub"}, {ID: "a"}, {ID: "b"}, {ID: "c"},
    }
    edges := []schema.Edge{
        {Source: "hub", Target: "a"},
        {Source: "hub", Target: "b"},
        {Source: "hub", Target: "c"},
    }
    a := NewAnalyzer()
    report := a.Analyze(nodes, edges)
    if len(report.GodNodes) == 0 {
        t.Fatal("expected god nodes")
    }
    if report.GodNodes[0].ID != "hub" {
        t.Fatalf("expected hub, got %s", report.GodNodes[0].ID)
    }
}
```

Run: `go test ./internal/analyze/...`
Expected: FAIL

- [ ] **Step 2: Implement Analyzer**

```go
package analyze

import "github.com/julianshen/gogfy/internal/schema"

type Report struct {
    GodNodes           []schema.Node
    SurprisingLinks    []schema.Edge
    ExplorationQuestions []string
}

type Analyzer struct{}

func NewAnalyzer() *Analyzer {
    return &Analyzer{}
}

func (a *Analyzer) Analyze(nodes []schema.Node, edges []schema.Edge) Report {
    // TODO: real centrality + scoring
    return Report{}
}
```

- [ ] **Step 3: Implement centrality and scoring**

Compute degree centrality for god nodes. Identify cross-community edges as surprising links. Generate questions based on high-centrality nodes and cross-community connections.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/analyze/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/analyze/
git commit -m "feat(analyze): god nodes, surprising links, and questions"
```

---

## Chunk 8: Milestone 7 — Report Rendering

### Task 8: GRAPH_REPORT.md renderer

**Files:**
- Create: `internal/report/report.go`
- Create: `internal/report/report_test.go`
- Create: `testdata/golden/GRAPH_REPORT.md`

- [ ] **Step 1: Write golden test**

```go
package report

import (
    "os"
    "path/filepath"
    "testing"
    "github.com/julianshen/gogfy/internal/analyze"
    "github.com/julianshen/gogfy/internal/schema"
)

func TestRenderReport(t *testing.T) {
    r := analyze.Report{
        GodNodes: []schema.Node{{ID: "hub", Label: "Hub"}},
        SurprisingLinks: []schema.Edge{{Source: "a", Target: "b", Relation: "calls"}},
        ExplorationQuestions: []string{"What does hub do?"},
    }
    out, err := Render(r)
    if err != nil {
        t.Fatal(err)
    }
    golden, _ := os.ReadFile("testdata/golden/GRAPH_REPORT.md")
    if string(out) != string(golden) {
        t.Fatal("output does not match golden file")
    }
}
```

Run: `go test ./internal/report/...`
Expected: FAIL

- [ ] **Step 2: Implement Render**

```go
package report

import (
    "bytes"
    "fmt"
    "github.com/julianshen/gogfy/internal/analyze"
)

func Render(r analyze.Report) ([]byte, error) {
    var b bytes.Buffer
    fmt.Fprintf(&b, "# Graph Report\n\n")
    fmt.Fprintf(&b, "## God Nodes\n")
    for _, n := range r.GodNodes {
        fmt.Fprintf(&b, "- %s\n", n.Label)
    }
    fmt.Fprintf(&b, "\n## Surprising Links\n")
    for _, e := range r.SurprisingLinks {
        fmt.Fprintf(&b, "- %s -> %s (%s)\n", e.Source, e.Target, e.Relation)
    }
    fmt.Fprintf(&b, "\n## Exploration Questions\n")
    for _, q := range r.ExplorationQuestions {
        fmt.Fprintf(&b, "- %s\n", q)
    }
    return b.Bytes(), nil
}
```

- [ ] **Step 3: Generate golden file and run tests**

Run: `go test ./internal/report/...`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/report/ testdata/golden/
git commit -m "feat(report): markdown report renderer with golden tests"
```

---

## Chunk 9: Milestone 8 — Export Artifacts

### Task 9: JSON and HTML exporters

**Files:**
- Create: `internal/export/export.go`
- Create: `internal/export/export_test.go`

- [ ] **Step 1: Write failing tests**

```go
package export

import (
    "encoding/json"
    "testing"
    "github.com/julianshen/gogfy/internal/schema"
)

func TestExportJSON(t *testing.T) {
    g := GraphExport{
        Nodes: []schema.Node{{ID: "a", Label: "A"}},
        Edges: []schema.Edge{{Source: "a", Target: "b", Relation: "calls"}},
    }
    data, err := ExportJSON(g)
    if err != nil {
        t.Fatal(err)
    }
    var decoded GraphExport
    if err := json.Unmarshal(data, &decoded); err != nil {
        t.Fatal(err)
    }
    if len(decoded.Nodes) != 1 {
        t.Fatal("node count mismatch")
    }
}
```

Run: `go test ./internal/export/...`
Expected: FAIL

- [ ] **Step 2: Implement exporters**

```go
package export

import (
    "encoding/json"
    "fmt"
    "github.com/julianshen/gogfy/internal/schema"
)

type GraphExport struct {
    Nodes []schema.Node `json:"nodes"`
    Edges []schema.Edge `json:"edges"`
}

func ExportJSON(g GraphExport) ([]byte, error) {
    return json.MarshalIndent(g, "", "  ")
}

func ExportHTML(g GraphExport) ([]byte, error) {
    // TODO: embed D3 or Cytoscape.js viewer
    return []byte(fmt.Sprintf("<html><body>Nodes: %d</body></html>", len(g.Nodes))), nil
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/export/...`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/export/
git commit -m "feat(export): JSON and HTML exporters"
```

---

## Chunk 10: Milestone 9 — Incremental Cache

### Task 10: SHA256 cache for incremental updates

**Files:**
- Create: `internal/cache/cache.go`
- Create: `internal/cache/cache_test.go`

- [ ] **Step 1: Write failing tests**

```go
package cache

import (
    "os"
    "path/filepath"
    "testing"
)

func TestCacheSkipsUnchangedFiles(t *testing.T) {
    root := t.TempDir()
    f := filepath.Join(root, "main.go")
    os.WriteFile(f, []byte("package main"), 0644)

    c := NewCache(filepath.Join(root, ".gographify-cache"))
    changed, err := c.ChangedFiles([]string{f})
    if err != nil {
        t.Fatal(err)
    }
    if len(changed) != 1 {
        t.Fatalf("expected 1 changed, got %d", len(changed))
    }

    // Save and check again
    if err := c.Save([]string{f}); err != nil {
        t.Fatal(err)
    }
    changed, err = c.ChangedFiles([]string{f})
    if err != nil {
        t.Fatal(err)
    }
    if len(changed) != 0 {
        t.Fatalf("expected 0 changed, got %d", len(changed))
    }
}
```

Run: `go test ./internal/cache/...`
Expected: FAIL

- [ ] **Step 2: Implement Cache**

```go
package cache

import (
    "crypto/sha256"
    "encoding/hex"
    "encoding/json"
    "os"
)

type Cache struct {
    path string
}

func NewCache(path string) *Cache {
    return &Cache{path: path}
}

func (c *Cache) ChangedFiles(files []string) ([]string, error) {
    oldHashes, _ := c.load()
    var changed []string
    for _, f := range files {
        h, err := hashFile(f)
        if err != nil {
            return nil, err
        }
        if oldHashes[f] != h {
            changed = append(changed, f)
        }
    }
    return changed, nil
}

func (c *Cache) Save(files []string) error {
    hashes := make(map[string]string, len(files))
    for _, f := range files {
        h, err := hashFile(f)
        if err != nil {
            return err
        }
        hashes[f] = h
    }
    data, _ := json.Marshal(hashes)
    return os.WriteFile(c.path, data, 0644)
}

func (c *Cache) load() (map[string]string, error) {
    data, err := os.ReadFile(c.path)
    if err != nil {
        return nil, err
    }
    var hashes map[string]string
    if err := json.Unmarshal(data, &hashes); err != nil {
        return nil, err
    }
    return hashes, nil
}

func hashFile(path string) (string, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return "", err
    }
    sum := sha256.Sum256(data)
    return hex.EncodeToString(sum[:]), nil
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/cache/...`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/cache/
git commit -m "feat(cache): SHA256 incremental cache"
```

---

## Chunk 11: Milestone 10 — E2E and CLI

### Task 11: Wire full pipeline and E2E test

**Files:**
- Modify: `cmd/gographify/main.go`
- Create: `test/e2e_test.go`
- Create: `testdata/e2e/mini-corpus/main.go`
- Create: `testdata/e2e/mini-corpus/lib.go`

- [ ] **Step 1: Write failing E2E test**

```go
package e2e

import (
    "os"
    "path/filepath"
    "testing"
)

func TestE2EPipeline(t *testing.T) {
    root := "testdata/e2e/mini-corpus"
    out := t.TempDir()

    // TODO: run CLI or library pipeline
    // gographify run root --out out

    if _, err := os.Stat(filepath.Join(out, "graph.json")); os.IsNotExist(err) {
        t.Fatal("graph.json not created")
    }
    if _, err := os.Stat(filepath.Join(out, "GRAPH_REPORT.md")); os.IsNotExist(err) {
        t.Fatal("GRAPH_REPORT.md not created")
    }
}
```

Run: `go test ./test/...`
Expected: FAIL

- [ ] **Step 2: Implement CLI and pipeline**

```go
package main

import (
    "flag"
    "fmt"
    "os"
)

func main() {
    var (
        update = flag.Bool("update", false, "incremental update")
        out    = flag.String("out", "graphify-out", "output directory")
    )
    flag.Parse()
    if len(flag.Args()) < 2 || flag.Args()[0] != "run" {
        fmt.Fprintln(os.Stderr, "usage: gographify run <root>")
        os.Exit(1)
    }
    root := flag.Args()[1]
    if err := runPipeline(root, *out, *update); err != nil {
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }
}

func runPipeline(root, out string, update bool) error {
    // TODO: wire detect -> extract -> graph -> cluster -> analyze -> report -> export
    return nil
}
```

- [ ] **Step 3: Wire all packages into pipeline**

Implement `runPipeline` using all internal packages. Write `graph.json`, `GRAPH_REPORT.md`, and `graph.html` to output directory.

- [ ] **Step 4: Run E2E test**

Run: `go test ./test/...`
Expected: PASS

- [ ] **Step 5: Run full suite**

Run: `go test ./...`
Expected: ALL PASS

- [ ] **Step 6: Commit**

```bash
git add cmd/gographify/ test/
git commit -m "feat(cli): wire full pipeline and add E2E test"
```

---

## Definition of Done

- [ ] `go test ./...` passing
- [ ] Deterministic snapshots across 3 reruns
- [ ] README documents exact supported parity vs upstream graphify

---

## Review Loop

After each chunk:
1. Run tests
2. Verify deterministic output
3. Commit with TDD intent (Red/Green/Refactor)

If any test fails unexpectedly, debug before proceeding.
