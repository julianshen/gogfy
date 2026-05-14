package dedup

import (
	"errors"
	"testing"

	"github.com/julianshen/gogfy/internal/schema"
)

func TestPass2FuzzyEmptyCandidates(t *testing.T) {
	// Low-entropy labels should be skipped
	nodes := []schema.Node{
		{ID: "a", Label: "ab"},
		{ID: "b", Label: "cd"},
	}
	uf := NewUnionFind()
	if err := pass2Fuzzy(nodes, uf, nil); err != nil {
		t.Fatal(err)
	}
	if uf.Find("a") != "a" || uf.Find("b") != "b" {
		t.Fatal("low-entropy nodes should not be merged")
	}
}

func TestPass2FuzzySingleCandidate(t *testing.T) {
	nodes := []schema.Node{
		{ID: "a", Label: "GraphExtractor"},
	}
	uf := NewUnionFind()
	if err := pass2Fuzzy(nodes, uf, nil); err != nil {
		t.Fatal(err)
	}
	if uf.Find("a") != "a" {
		t.Fatal("single candidate should not be merged")
	}
}

func TestPass2FuzzyDifferentCommunities(t *testing.T) {
	// Same labels but different communities — no community boost
	nodes := []schema.Node{
		{ID: "a", Label: "GraphExtractor"},
		{ID: "b", Label: "Graph Extractor"},
	}
	uf := NewUnionFind()
	communities := map[string]string{"a": "1", "b": "2"}
	if err := pass2Fuzzy(nodes, uf, communities); err != nil {
		t.Fatal(err)
	}
	// Without community boost, Jaro-Winkler might not reach 92.0
	// This is a smoke test to ensure no panic
}

func TestPass3LLMNilBackend(t *testing.T) {
	nodes := []schema.Node{
		{ID: "a", Label: "Foo"},
		{ID: "b", Label: "Bar"},
	}
	uf := NewUnionFind()
	if err := pass3LLM(nodes, uf, nil, nil); err != nil {
		t.Fatal(err)
	}
}

func TestPass3LLMErrorHandling(t *testing.T) {
	nodes := []schema.Node{
		{ID: "a", Label: "GraphBuilder"},
		{ID: "b", Label: "Graph Builder"},
	}
	uf := NewUnionFind()
	communities := map[string]string{"a": "1", "b": "1"}

	// Backend that returns error
	backend := &errorLLMBackend{}
	if err := pass3LLM(nodes, uf, communities, backend); err != nil {
		t.Fatal(err)
	}
	// Should not panic even with errors
}

type errorLLMBackend struct{}

func (e *errorLLMBackend) AreSameConcept(_, _ string) (bool, error) {
	return false, errors.New("simulated error")
}

func TestDeduplicateEdgeRemapping(t *testing.T) {
	nodes := []schema.Node{
		{ID: "a", Label: "Main"},
		{ID: "b", Label: "main"},
		{ID: "c", Label: "Helper"},
		{ID: "d", Label: "Helper"},
	}
	edges := []schema.Edge{
		{Source: "a", Target: "c", Relation: "calls"},
		{Source: "b", Target: "d", Relation: "calls"},
	}

	d := NewDeduplicator()
	outNodes, outEdges, _, err := d.Deduplicate(nodes, edges, nil)
	if err != nil {
		t.Fatal(err)
	}

	if len(outNodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(outNodes))
	}
	if len(outEdges) != 1 {
		t.Fatalf("expected 1 edge after dedup, got %d", len(outEdges))
	}
}

func TestDeduplicateDeterministicOutput(t *testing.T) {
	nodes := []schema.Node{
		{ID: "c", Label: "Main"},
		{ID: "a", Label: "main"},
		{ID: "b", Label: "Helper"},
	}
	edges := []schema.Edge{
		{Source: "c", Target: "b", Relation: "calls"},
	}

	d := NewDeduplicator()
	outNodes1, _, _, err := d.Deduplicate(nodes, edges, nil)
	if err != nil {
		t.Fatal(err)
	}
	outNodes2, _, _, err := d.Deduplicate(nodes, edges, nil)
	if err != nil {
		t.Fatal(err)
	}

	if len(outNodes1) != len(outNodes2) {
		t.Fatal("output length not deterministic")
	}
	for i := range outNodes1 {
		if outNodes1[i].ID != outNodes2[i].ID {
			t.Fatalf("node order not deterministic at index %d", i)
		}
	}
}

func TestPass3LLMBatchProcessing(t *testing.T) {
	// Use labels that fall in the ambiguous range [75, 92) after normalization
	// Need labels where JW is in [70, 87) so with +5 community boost it's in [75, 92)
	nodes := []schema.Node{
		{ID: "a", Label: "HelloWorld"},
		{ID: "b", Label: "HelloThere"},
	}

	uf := NewUnionFind()
	communities := map[string]string{"a": "1", "b": "1"}

	backend := &mockLLMBackend{
		answers: map[string]bool{
			"HelloWorld|HelloThere": true,
			"HelloThere|HelloWorld": true,
		},
	}

	if err := pass3LLM(nodes, uf, communities, backend); err != nil {
		t.Fatal(err)
	}

	if uf.Find("a") != uf.Find("b") {
		t.Fatal("a and b should be merged by LLM")
	}
}

func TestPass2FuzzyAlreadyUnioned(t *testing.T) {
	nodes := []schema.Node{
		{ID: "a", Label: "GraphExtractor"},
		{ID: "b", Label: "Graph Extractor"},
	}
	uf := NewUnionFind()
	uf.Union("a", "b") // Already unioned
	communities := map[string]string{"a": "1", "b": "1"}

	if err := pass2Fuzzy(nodes, uf, communities); err != nil {
		t.Fatal(err)
	}
	// Should not panic; already unioned nodes are skipped
}
