package dedup

import (
	"testing"

	"github.com/julianshen/gogfy/internal/schema"
)

func TestDeduplicateExact(t *testing.T) {
	nodes := []schema.Node{
		{ID: "a", Label: "Main"},
		{ID: "b", Label: "main"},
		{ID: "c", Label: "Helper"},
	}
	edges := []schema.Edge{
		{Source: "a", Target: "c", Relation: "calls"},
		{Source: "b", Target: "c", Relation: "calls"},
	}
	communities := map[string]string{"a": "1", "b": "1", "c": "2"}

	d := NewDeduplicator()
	outNodes, outEdges, remap, err := d.Deduplicate(nodes, edges, communities)
	if err != nil {
		t.Fatal(err)
	}

	// a and b should be merged
	if len(outNodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(outNodes))
	}
	// Either a or b can be the winner; the other should be remapped.
	if len(remap) != 1 {
		t.Fatalf("expected 1 remap entry, got remap=%v", remap)
	}

	// Edges should be remapped and deduplicated
	if len(outEdges) != 1 {
		t.Fatalf("expected 1 edge after dedup, got %d", len(outEdges))
	}
}

func TestDeduplicateSelfLoopDropped(t *testing.T) {
	nodes := []schema.Node{
		{ID: "a", Label: "Main"},
		{ID: "b", Label: "main"},
	}
	edges := []schema.Edge{
		{Source: "a", Target: "b", Relation: "calls"},
	}
	communities := map[string]string{"a": "1", "b": "1"}

	d := NewDeduplicator()
	_, outEdges, _, err := d.Deduplicate(nodes, edges, communities)
	if err != nil {
		t.Fatal(err)
	}

	// After merging a and b, the edge becomes a self-loop and should be dropped
	if len(outEdges) != 0 {
		t.Fatalf("expected 0 edges (self-loop dropped), got %d", len(outEdges))
	}
}

func TestDeduplicateEmpty(t *testing.T) {
	d := NewDeduplicator()
	outNodes, outEdges, remap, err := d.Deduplicate([]schema.Node{}, []schema.Edge{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(outNodes) != 0 || len(outEdges) != 0 || len(remap) != 0 {
		t.Fatal("expected empty output for empty input")
	}
}

func TestDeduplicateFuzzy(t *testing.T) {
	// These are similar enough for MinHash + Jaro-Winkler to catch
	nodes := []schema.Node{
		{ID: "a", Label: "GraphExtractor"},
		{ID: "b", Label: "Graph Extractor"},
		{ID: "c", Label: "SomethingElse"},
	}
	edges := []schema.Edge{
		{Source: "a", Target: "c", Relation: "uses"},
	}
	communities := map[string]string{"a": "1", "b": "1", "c": "2"}

	d := NewDeduplicator()
	outNodes, outEdges, _, err := d.Deduplicate(nodes, edges, communities)
	if err != nil {
		t.Fatal(err)
	}

	// a and b should be merged (same community, high Jaro-Winkler)
	if len(outNodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(outNodes))
	}
	if len(outEdges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(outEdges))
	}
}

func TestDeduplicatePreservesUnmerged(t *testing.T) {
	nodes := []schema.Node{
		{ID: "a", Label: "Foo"},
		{ID: "b", Label: "Bar"},
		{ID: "c", Label: "Baz"},
	}
	edges := []schema.Edge{
		{Source: "a", Target: "b", Relation: "calls"},
		{Source: "b", Target: "c", Relation: "calls"},
	}

	d := NewDeduplicator()
	outNodes, outEdges, _, err := d.Deduplicate(nodes, edges, nil)
	if err != nil {
		t.Fatal(err)
	}

	if len(outNodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(outNodes))
	}
	if len(outEdges) != 2 {
		t.Fatalf("expected 2 edges, got %d", len(outEdges))
	}
}

// mockLLMBackend is a test double for LLMBackend.
type mockLLMBackend struct {
	answers map[string]bool
}

func (m *mockLLMBackend) AreSameConcept(labelA, labelB string) (bool, error) {
	key := labelA + "|" + labelB
	if v, ok := m.answers[key]; ok {
		return v, nil
	}
	return false, nil
}

func TestDeduplicateWithLLM(t *testing.T) {
	nodes := []schema.Node{
		{ID: "a", Label: "GraphBuilder"},
		{ID: "b", Label: "Graph Builder"},
	}
	edges := []schema.Edge{}
	communities := map[string]string{"a": "1", "b": "1"}

	backend := &mockLLMBackend{
		answers: map[string]bool{
			"GraphBuilder|Graph Builder": true,
		},
	}

	d := NewDeduplicator()
	d.LLMBackend = backend
	outNodes, _, remap, err := d.Deduplicate(nodes, edges, communities)
	if err != nil {
		t.Fatal(err)
	}

	if len(outNodes) != 1 {
		t.Fatalf("expected 1 node after LLM merge, got %d", len(outNodes))
	}
	if len(remap) != 1 {
		t.Fatalf("expected 1 remap entry, got %v", remap)
	}
}

func TestDeduplicatePicksASTNodeOverSemanticOnFuzzyMerge(t *testing.T) {
	// AST extraction yields a code node "Service" with FileType=Code.
	// Semantic extraction yields a doc node "Service" with FileType=Document.
	// Pass-2 fuzzy dedup will find them similar enough to merge; the
	// AST-grounded ID must survive since it's the authoritative anchor
	// for downstream resolution (callflow, wiki, etc.).
	nodes := []schema.Node{
		{ID: "doc:entity:/x.md:service", Label: "Service", FileType: schema.FileTypeDocument},
		{ID: "go:function:/auth.go:m:Service", Label: "Service", FileType: schema.FileTypeCode},
	}
	out, _, _, err := NewDeduplicator().Deduplicate(nodes, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("expected merge to 1 node, got %d", len(out))
	}
	if out[0].FileType != schema.FileTypeCode {
		t.Fatalf("AST node (FileType=Code) must win over semantic, got %+v", out[0])
	}
}

func TestDeduplicatePrefersCodeOverPaperOverDocument(t *testing.T) {
	// Three-way pile-up: code, paper, document all label-equal.
	// Priority: code > paper > document.
	nodes := []schema.Node{
		{ID: "doc:entity:doc1", Label: "Auth", FileType: schema.FileTypeDocument},
		{ID: "paper:entity:p1", Label: "Auth", FileType: schema.FileTypePaper},
		{ID: "go:function:/x.go:m:Auth", Label: "Auth", FileType: schema.FileTypeCode},
	}
	out, _, _, err := NewDeduplicator().Deduplicate(nodes, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].FileType != schema.FileTypeCode {
		t.Fatalf("code must win 3-way: %+v", out)
	}
}
