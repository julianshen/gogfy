package merge

import (
	"testing"

	"github.com/julianshen/gogfy/internal/export"
	"github.com/julianshen/gogfy/internal/schema"
)

func TestMergeUnionDedupes(t *testing.T) {
	a := export.GraphExport{
		Nodes: []schema.Node{
			{ID: "n1", Label: "one"},
			{ID: "shared", Label: "shared-from-a"},
		},
		Edges: []schema.Edge{
			{Source: "n1", Target: "shared", Relation: "calls", Confidence: schema.Extracted},
		},
	}
	b := export.GraphExport{
		Nodes: []schema.Node{
			{ID: "shared", Label: "shared-from-b"},
			{ID: "n2", Label: "two"},
		},
		Edges: []schema.Edge{
			{Source: "n1", Target: "shared", Relation: "calls", Confidence: schema.Extracted}, // duplicate of a's edge
			{Source: "n2", Target: "shared", Relation: "calls", Confidence: schema.Extracted},
		},
	}
	got := Merge(a, b)
	if len(got.Nodes) != 3 {
		t.Fatalf("expected 3 unique nodes, got %d: %+v", len(got.Nodes), got.Nodes)
	}
	if len(got.Edges) != 2 {
		t.Fatalf("expected 2 unique edges, got %d: %+v", len(got.Edges), got.Edges)
	}
}

func TestMergeFirstNodeLabelWins(t *testing.T) {
	a := export.GraphExport{Nodes: []schema.Node{{ID: "x", Label: "from-a"}}}
	b := export.GraphExport{Nodes: []schema.Node{{ID: "x", Label: "from-b"}}}
	got := Merge(a, b)
	if len(got.Nodes) != 1 || got.Nodes[0].Label != "from-a" {
		t.Fatalf("expected first-label wins, got %+v", got.Nodes)
	}
}

func TestMergeIsDeterministic(t *testing.T) {
	a := export.GraphExport{
		Nodes: []schema.Node{{ID: "z"}, {ID: "a"}},
		Edges: []schema.Edge{{Source: "z", Target: "a", Relation: "r"}},
	}
	b := export.GraphExport{
		Nodes: []schema.Node{{ID: "m"}, {ID: "b"}},
		Edges: []schema.Edge{{Source: "m", Target: "b", Relation: "r"}},
	}
	first := Merge(a, b)
	second := Merge(a, b)
	if !graphsEqual(first, second) {
		t.Fatalf("merge not deterministic across calls")
	}
	// Sorted by ID — z must come last.
	if first.Nodes[0].ID != "a" || first.Nodes[len(first.Nodes)-1].ID != "z" {
		t.Fatalf("merge nodes not sorted by ID: %+v", first.Nodes)
	}
}

func TestMergeEmpty(t *testing.T) {
	got := Merge(export.GraphExport{}, export.GraphExport{})
	if len(got.Nodes) != 0 || len(got.Edges) != 0 {
		t.Fatalf("merging empties should yield empty, got %+v", got)
	}
}

func TestMergeMultipleInputs(t *testing.T) {
	a := export.GraphExport{Nodes: []schema.Node{{ID: "a"}}}
	b := export.GraphExport{Nodes: []schema.Node{{ID: "b"}}}
	c := export.GraphExport{Nodes: []schema.Node{{ID: "c"}}}
	got := MergeAll([]export.GraphExport{a, b, c})
	if len(got.Nodes) != 3 {
		t.Fatalf("expected 3 nodes from 3-way merge, got %d", len(got.Nodes))
	}
}

func TestMergeAllEmpty(t *testing.T) {
	got := MergeAll(nil)
	if len(got.Nodes) != 0 || len(got.Edges) != 0 {
		t.Fatalf("MergeAll of nothing should be empty")
	}
}

func TestMergeEdgesDifferByConfidence(t *testing.T) {
	// Same source/target/relation but different confidence levels are
	// distinct edges — graphify treats EXTRACTED, INFERRED, AMBIGUOUS as
	// genuinely different facts.
	a := export.GraphExport{Edges: []schema.Edge{
		{Source: "x", Target: "y", Relation: "calls", Confidence: schema.Extracted},
	}}
	b := export.GraphExport{Edges: []schema.Edge{
		{Source: "x", Target: "y", Relation: "calls", Confidence: schema.Inferred},
	}}
	got := Merge(a, b)
	if len(got.Edges) != 2 {
		t.Fatalf("EXTRACTED and INFERRED edges should be kept separately, got %d", len(got.Edges))
	}
}

func graphsEqual(a, b export.GraphExport) bool {
	if len(a.Nodes) != len(b.Nodes) || len(a.Edges) != len(b.Edges) {
		return false
	}
	for i := range a.Nodes {
		if a.Nodes[i] != b.Nodes[i] {
			return false
		}
	}
	for i := range a.Edges {
		if a.Edges[i] != b.Edges[i] {
			return false
		}
	}
	return true
}

func TestIncrementalMergePrunesDeletedFiles(t *testing.T) {
	// File b.go disappeared between extractions. Its nodes (and their
	// edges) must be pruned even though prior still has them.
	prior := export.GraphExport{
		Nodes: []schema.Node{
			{ID: "go:module:/a.go", SourceFile: "/a.go"},
			{ID: "go:module:/b.go", SourceFile: "/b.go"},
			{ID: "go:function:/a.go:Foo", SourceFile: "/a.go"},
			{ID: "go:function:/b.go:Bar", SourceFile: "/b.go"},
		},
		Edges: []schema.Edge{
			{Source: "go:module:/a.go", Target: "go:function:/a.go:Foo", Relation: "contains"},
			{Source: "go:module:/b.go", Target: "go:function:/b.go:Bar", Relation: "contains"},
			{Source: "go:function:/a.go:Foo", Target: "go:function:/b.go:Bar", Relation: "calls"},
		},
	}
	next := export.GraphExport{Nodes: []schema.Node{}, Edges: []schema.Edge{}}
	currentFiles := []string{"/a.go"} // /b.go gone
	got := IncrementalMerge(prior, next, currentFiles)

	for _, n := range got.Nodes {
		if n.SourceFile == "/b.go" {
			t.Errorf("deleted-file node survived prune: %s", n.ID)
		}
	}
	for _, e := range got.Edges {
		if e.Target == "go:function:/b.go:Bar" || e.Source == "go:function:/b.go:Bar" {
			t.Errorf("edge to deleted-file node survived: %+v", e)
		}
	}
	// /a.go's contents must still be present.
	want := map[string]bool{"go:module:/a.go": false, "go:function:/a.go:Foo": false}
	for _, n := range got.Nodes {
		if _, ok := want[n.ID]; ok {
			want[n.ID] = true
		}
	}
	for id, present := range want {
		if !present {
			t.Errorf("expected %s in merged graph", id)
		}
	}
}

func TestIncrementalMergeRefreshesFileEntriesFromNext(t *testing.T) {
	// File /a.go was re-extracted. next contains the fresh entries;
	// prior's stale Foo node should be dropped in favor of next's
	// updated one (different Label simulates a rename).
	prior := export.GraphExport{
		Nodes: []schema.Node{
			{ID: "go:function:/a.go:Foo", Label: "Foo (old)", SourceFile: "/a.go"},
		},
	}
	next := export.GraphExport{
		Nodes: []schema.Node{
			{ID: "go:function:/a.go:Foo", Label: "Foo (new)", SourceFile: "/a.go"},
		},
	}
	got := IncrementalMerge(prior, next, []string{"/a.go"})
	if len(got.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d: %+v", len(got.Nodes), got.Nodes)
	}
	if got.Nodes[0].Label != "Foo (new)" {
		t.Errorf("next should win on refresh: got %q", got.Nodes[0].Label)
	}
}

func TestIncrementalMergeKeepsSyntheticNodes(t *testing.T) {
	// Nodes with empty SourceFile are synthetic (semantic extraction
	// concepts, cross-file resolution targets) — they must NOT be
	// pruned even though they don't correspond to a current file.
	prior := export.GraphExport{
		Nodes: []schema.Node{
			{ID: "concept:logging", Label: "Logging"},
			{ID: "go:module:/gone.go", SourceFile: "/gone.go"},
		},
	}
	got := IncrementalMerge(prior, export.GraphExport{}, []string{"/a.go"})
	var keptConcept, keptDeleted bool
	for _, n := range got.Nodes {
		if n.ID == "concept:logging" {
			keptConcept = true
		}
		if n.SourceFile == "/gone.go" {
			keptDeleted = true
		}
	}
	if !keptConcept {
		t.Errorf("synthetic node (empty SourceFile) should survive prune")
	}
	if keptDeleted {
		t.Errorf("deleted-file node should NOT survive prune")
	}
}

func TestIncrementalMergeUnchangedFileSurvivesWithoutNext(t *testing.T) {
	// File /a.go is still in currentFiles but didn't appear in next
	// (cache hit — we didn't bother re-extracting). Its prior entries
	// must survive untouched.
	prior := export.GraphExport{
		Nodes: []schema.Node{
			{ID: "go:module:/a.go", SourceFile: "/a.go"},
		},
	}
	got := IncrementalMerge(prior, export.GraphExport{}, []string{"/a.go"})
	if len(got.Nodes) != 1 || got.Nodes[0].ID != "go:module:/a.go" {
		t.Errorf("cached-file entry lost: %+v", got.Nodes)
	}
}
