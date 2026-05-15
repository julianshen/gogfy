package globalgraph

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/julianshen/gogfy/internal/export"
	"github.com/julianshen/gogfy/internal/schema"
)

func writeGraph(t *testing.T, dir, name string, g export.GraphExport) string {
	t.Helper()
	b, err := export.ExportJSON(g)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func sampleGraph(prefix string) export.GraphExport {
	// Two real source nodes + one external (no SourceFile) — the
	// external should dedupe with same-label nodes from other repos.
	return export.GraphExport{
		Nodes: []schema.Node{
			{ID: prefix + ":1", Label: "Foo", SourceFile: "/r/foo.go"},
			{ID: prefix + ":2", Label: "Bar", SourceFile: "/r/bar.go"},
			{ID: "ext:" + prefix, Label: "context.Context"}, // external (no SourceFile)
		},
		Edges: []schema.Edge{
			{Source: prefix + ":1", Target: prefix + ":2", Relation: "calls"},
			{Source: prefix + ":1", Target: "ext:" + prefix, Relation: "uses"},
		},
	}
}

func TestAddCreatesStoreAndMergesPrefixed(t *testing.T) {
	tmp := t.TempDir()
	s, err := NewStore(tmp)
	if err != nil {
		t.Fatal(err)
	}
	src := writeGraph(t, tmp, "a.json", sampleGraph("a"))

	res, err := s.Add(src, "repoA")
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped {
		t.Fatal("first add should not be skipped")
	}
	if res.NodesAdded == 0 {
		t.Fatal("expected nodes added")
	}

	// Global graph on disk must exist with prefixed IDs.
	gg, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	gotPrefixed := false
	for _, n := range gg.Nodes {
		if strings.HasPrefix(n.ID, "repoA::") {
			gotPrefixed = true
		}
	}
	if !gotPrefixed {
		t.Fatalf("expected at least one node with repoA:: prefix; got %d nodes", len(gg.Nodes))
	}
}

func TestAddIsIdempotentBySourceHash(t *testing.T) {
	// Re-adding the same source file with the same content must be
	// a no-op (skipped=true), not a duplicate-merge that bloats the
	// global graph.
	tmp := t.TempDir()
	s, err := NewStore(tmp)
	if err != nil {
		t.Fatal(err)
	}
	src := writeGraph(t, tmp, "a.json", sampleGraph("a"))
	if _, err := s.Add(src, "repoA"); err != nil {
		t.Fatal(err)
	}
	res2, err := s.Add(src, "repoA")
	if err != nil {
		t.Fatal(err)
	}
	if !res2.Skipped {
		t.Fatalf("second add of unchanged source should be skipped, got %+v", res2)
	}
}

func TestAddReplacesPriorVersionOnContentChange(t *testing.T) {
	// Adding a new graph.json for the same tag must prune the old
	// nodes/edges before merging. Otherwise stale data accumulates
	// across re-runs (the most common workflow: re-run `gogfy run`,
	// then re-add the result).
	tmp := t.TempDir()
	s, err := NewStore(tmp)
	if err != nil {
		t.Fatal(err)
	}
	src := writeGraph(t, tmp, "a.json", sampleGraph("a"))
	if _, err := s.Add(src, "repoA"); err != nil {
		t.Fatal(err)
	}
	// Overwrite with a graph that doesn't contain the "1" node.
	newG := export.GraphExport{
		Nodes: []schema.Node{
			{ID: "a:3", Label: "Baz", SourceFile: "/r/baz.go"},
		},
	}
	src2 := writeGraph(t, tmp, "a.json", newG)
	res, err := s.Add(src2, "repoA")
	if err != nil {
		t.Fatal(err)
	}
	if res.NodesRemoved == 0 {
		t.Fatal("expected stale repoA nodes to be removed before re-merge")
	}
	gg, _ := s.Load()
	for _, n := range gg.Nodes {
		if n.ID == "repoA::a:1" {
			t.Fatalf("stale node from prior version still present: %+v", n)
		}
	}
}

func TestAddMergesMultipleReposIsolated(t *testing.T) {
	// Two repos with overlapping local IDs (both have ":1", ":2") must
	// produce 4 prefixed nodes plus deduped externals, not 2 with the
	// last-write winning.
	tmp := t.TempDir()
	s, err := NewStore(tmp)
	if err != nil {
		t.Fatal(err)
	}
	srcA := writeGraph(t, tmp, "a.json", sampleGraph("a"))
	srcB := writeGraph(t, tmp, "b.json", sampleGraph("b"))
	if _, err := s.Add(srcA, "repoA"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add(srcB, "repoB"); err != nil {
		t.Fatal(err)
	}
	gg, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	// 4 source nodes (2 per repo, distinct because prefixed) + 1
	// deduped external "context.Context" = 5 nodes total.
	if len(gg.Nodes) != 5 {
		t.Fatalf("expected 5 nodes (4 prefixed + 1 deduped external), got %d:\n%+v", len(gg.Nodes), gg.Nodes)
	}
	// Both repos' edges should survive.
	if len(gg.Edges) < 4 {
		t.Fatalf("expected at least 4 edges across the two repos, got %d", len(gg.Edges))
	}
}

func TestRemoveDropsAllRepoNodesAndEdges(t *testing.T) {
	tmp := t.TempDir()
	s, err := NewStore(tmp)
	if err != nil {
		t.Fatal(err)
	}
	srcA := writeGraph(t, tmp, "a.json", sampleGraph("a"))
	srcB := writeGraph(t, tmp, "b.json", sampleGraph("b"))
	if _, err := s.Add(srcA, "repoA"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add(srcB, "repoB"); err != nil {
		t.Fatal(err)
	}
	n, err := s.Remove("repoA")
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("expected positive node removal count")
	}
	gg, _ := s.Load()
	for _, node := range gg.Nodes {
		if strings.HasPrefix(node.ID, "repoA::") {
			t.Fatalf("repoA node survived remove: %s", node.ID)
		}
	}
	for _, e := range gg.Edges {
		if strings.HasPrefix(e.Source, "repoA::") || strings.HasPrefix(e.Target, "repoA::") {
			t.Fatalf("repoA edge survived remove: %+v", e)
		}
	}
	// Manifest must also be cleaned.
	man, _ := s.List()
	if _, ok := man["repoA"]; ok {
		t.Fatal("repoA still in manifest after remove")
	}
}

func TestRemoveUnknownRepoErrors(t *testing.T) {
	tmp := t.TempDir()
	s, err := NewStore(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Remove("never-added"); err == nil {
		t.Fatal("expected error when removing repo not in manifest")
	}
}

func TestListReflectsAddedRepos(t *testing.T) {
	tmp := t.TempDir()
	s, err := NewStore(tmp)
	if err != nil {
		t.Fatal(err)
	}
	src := writeGraph(t, tmp, "a.json", sampleGraph("a"))
	if _, err := s.Add(src, "alpha"); err != nil {
		t.Fatal(err)
	}
	man, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := man["alpha"]; !ok {
		t.Fatalf("alpha missing from manifest: %+v", man)
	}
	if man["alpha"].NodeCount == 0 {
		t.Fatal("alpha node count not populated")
	}
}

func TestAddMissingSourceErrors(t *testing.T) {
	tmp := t.TempDir()
	s, err := NewStore(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add(filepath.Join(tmp, "nope.json"), "x"); err == nil {
		t.Fatal("expected error for missing source path")
	}
}

func TestAddRejectsEmptyTag(t *testing.T) {
	tmp := t.TempDir()
	s, err := NewStore(tmp)
	if err != nil {
		t.Fatal(err)
	}
	src := writeGraph(t, tmp, "a.json", sampleGraph("a"))
	if _, err := s.Add(src, ""); err == nil {
		t.Fatal("expected error for empty repo tag")
	}
	if _, err := s.Add(src, "has::separator"); err == nil {
		t.Fatal("expected error: tags must not contain the :: separator that's reserved for prefixing")
	}
	// Path-shaped fallbacks from defaultRepoTag must be rejected too,
	// not silently anchor data under a confusing label.
	for _, bad := range []string{".", "..", "/", "a/b", `c\d`} {
		if _, err := s.Add(src, bad); err == nil {
			t.Fatalf("expected error for path-shaped tag %q", bad)
		}
	}
}

func TestAddRecoversWhenGraphFileDeleted(t *testing.T) {
	// Self-healing path: if global-graph.json is deleted while the
	// manifest still has an entry, the next Add must NOT short-circuit
	// to skipped — otherwise the user has no way to rebuild the store
	// without manually editing manifest state.
	tmp := t.TempDir()
	s, err := NewStore(tmp)
	if err != nil {
		t.Fatal(err)
	}
	src := writeGraph(t, tmp, "a.json", sampleGraph("a"))
	if _, err := s.Add(src, "repoA"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(s.Path()); err != nil {
		t.Fatal(err)
	}
	res, err := s.Add(src, "repoA")
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped {
		t.Fatal("missing graph file must NOT short-circuit to skipped")
	}
	if _, err := os.Stat(s.Path()); err != nil {
		t.Fatalf("graph file should be regenerated, got: %v", err)
	}
}

func TestRemovePrunesOrphanExternalNodes(t *testing.T) {
	// When the removed repo was the only contributor citing an
	// external (no-SourceFile) node, the external should be cleaned
	// up too. Otherwise externals accumulate as orphan stale data
	// across the store's lifetime.
	tmp := t.TempDir()
	s, err := NewStore(tmp)
	if err != nil {
		t.Fatal(err)
	}
	// Only repoA cites this external; repoB doesn't.
	a := export.GraphExport{
		Nodes: []schema.Node{
			{ID: "ext:a", Label: "uniquelib.Anchor"},
			{ID: "fn:1", Label: "Foo", SourceFile: "/r/foo.go"},
		},
		Edges: []schema.Edge{{Source: "fn:1", Target: "ext:a", Relation: "uses"}},
	}
	srcA := writeGraph(t, tmp, "a.json", a)
	if _, err := s.Add(srcA, "repoA"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Remove("repoA"); err != nil {
		t.Fatal(err)
	}
	gg, _ := s.Load()
	for _, n := range gg.Nodes {
		if n.Label == "uniquelib.Anchor" {
			t.Fatalf("orphan external survived Remove: %+v", n)
		}
	}
}

func TestExternalNodesDedupedAcrossRepos(t *testing.T) {
	// Both repos cite the same external library node "context.Context".
	// The merged graph should contain only ONE node for it, with its
	// original (un-prefixed) ID so other repos' edges can reference
	// the same anchor.
	tmp := t.TempDir()
	s, err := NewStore(tmp)
	if err != nil {
		t.Fatal(err)
	}
	a := export.GraphExport{
		Nodes: []schema.Node{
			{ID: "ext:a", Label: "context.Context"}, // external
			{ID: "fn:1", Label: "DoA", SourceFile: "/r/a.go"},
		},
		Edges: []schema.Edge{{Source: "fn:1", Target: "ext:a", Relation: "uses"}},
	}
	b := export.GraphExport{
		Nodes: []schema.Node{
			{ID: "ext:b", Label: "context.Context"}, // same external, different local ID
			{ID: "fn:2", Label: "DoB", SourceFile: "/r/b.go"},
		},
		Edges: []schema.Edge{{Source: "fn:2", Target: "ext:b", Relation: "uses"}},
	}
	srcA := writeGraph(t, tmp, "a.json", a)
	srcB := writeGraph(t, tmp, "b.json", b)
	if _, err := s.Add(srcA, "repoA"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add(srcB, "repoB"); err != nil {
		t.Fatal(err)
	}
	gg, _ := s.Load()
	ctxNodes := 0
	for _, n := range gg.Nodes {
		if n.Label == "context.Context" {
			ctxNodes++
		}
	}
	if ctxNodes != 1 {
		t.Fatalf("expected exactly 1 deduped external 'context.Context' node, got %d:\n%+v", ctxNodes, gg.Nodes)
	}
}

func TestPathPointsAtGlobalGraphFile(t *testing.T) {
	tmp := t.TempDir()
	s, err := NewStore(tmp)
	if err != nil {
		t.Fatal(err)
	}
	p := s.Path()
	if !strings.HasSuffix(p, "global-graph.json") {
		t.Fatalf("Path() should end with global-graph.json, got %q", p)
	}
	if !strings.HasPrefix(p, tmp) {
		t.Fatalf("Path() should be inside the store dir %q, got %q", tmp, p)
	}
}
