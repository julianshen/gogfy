package dedup

import "testing"

func TestUnionFind(t *testing.T) {
	uf := NewUnionFind()
	uf.Union("a", "b")
	uf.Union("b", "c")
	if uf.Find("a") != uf.Find("c") {
		t.Fatal("a and c should be in the same set")
	}
	if uf.Find("a") == uf.Find("d") {
		t.Fatal("a and d should be in different sets")
	}
}

func TestUnionFindComponents(t *testing.T) {
	uf := NewUnionFind()
	uf.Union("a", "b")
	uf.Union("c", "d")
	comps := uf.Components()
	if len(comps) != 2 {
		t.Fatalf("expected 2 components, got %d", len(comps))
	}
	for _, members := range comps {
		if len(members) != 2 {
			t.Fatalf("expected component size 2, got %d", len(members))
		}
	}
}

func TestUnionFindSingleElement(t *testing.T) {
	uf := NewUnionFind()
	if uf.Find("x") != "x" {
		t.Fatal("single element should be its own parent")
	}
}
