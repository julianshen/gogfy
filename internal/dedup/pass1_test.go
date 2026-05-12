package dedup

import (
	"testing"

	"github.com/julianshen/gogfy/internal/schema"
)

func TestPass1Exact(t *testing.T) {
	nodes := []schema.Node{
		{ID: "a", Label: "Main"},
		{ID: "b", Label: "main"},
		{ID: "c", Label: "Helper"},
		{ID: "d", Label: "helper"},
		{ID: "e", Label: "Unique"},
	}
	uf := pass1Exact(nodes)
	if uf.Find("a") != uf.Find("b") {
		t.Fatal("a and b should be merged (Main == main)")
	}
	if uf.Find("c") != uf.Find("d") {
		t.Fatal("c and d should be merged (Helper == helper)")
	}
	if uf.Find("a") == uf.Find("c") {
		t.Fatal("a and c should NOT be merged")
	}
	if uf.Find("e") != "e" {
		t.Fatal("e should be its own set")
	}
}

func TestPass1ExactEmptyLabel(t *testing.T) {
	nodes := []schema.Node{
		{ID: "a", Label: ""},
		{ID: "b", Label: ""},
	}
	uf := pass1Exact(nodes)
	// Empty labels are skipped, so no merge
	if uf.Find("a") == uf.Find("b") {
		t.Fatal("empty labels should not be merged")
	}
}

func TestPass1ExactFallsBackToID(t *testing.T) {
	nodes := []schema.Node{
		{ID: "pkg:main", Label: ""},
		{ID: "pkg:main_c1", Label: ""},
	}
	uf := pass1Exact(nodes)
	// Falls back to ID for normalization, but IDs are different
	if uf.Find("pkg:main") == uf.Find("pkg:main_c1") {
		t.Fatal("different IDs should not be merged even with empty labels")
	}
}
