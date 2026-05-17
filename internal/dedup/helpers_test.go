package dedup

import (
	"testing"

	"github.com/julianshen/gogfy/internal/schema"
)

func TestNormalize(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Hello World", "hello world"},
		{"  Spaces  ", "spaces"},
		{"Punctuation!!!", "punctuation"},
		{"Mixed123", "mixed123"},
		{"", ""},
		{"a-b_c.d", "a b c d"},
	}
	for _, tt := range tests {
		got := normalize(tt.input)
		if got != tt.expected {
			t.Errorf("normalize(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestEntropy(t *testing.T) {
	// "aaa" has 0 entropy (all same char)
	if got := entropy("aaa"); got != 0 {
		t.Errorf("entropy(aaa) = %f, want 0", got)
	}
	// "ab" has 1 bit/char
	if got := entropy("ab"); got != 1.0 {
		t.Errorf("entropy(ab) = %f, want 1.0", got)
	}
	// empty string
	if got := entropy(""); got != 0 {
		t.Errorf("entropy(\"\") = %f, want 0", got)
	}
}

func TestShingles(t *testing.T) {
	got := shingles("hello", 3)
	want := []string{"hel", "ell", "llo"}
	if len(got) != len(want) {
		t.Fatalf("shingles(hello,3) = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("shingles[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	// Spaces stripped
	got2 := shingles("he llo", 3)
	want2 := []string{"hel", "ell", "llo"}
	if len(got2) != len(want2) {
		t.Fatalf("shingles(he llo,3) = %v, want %v", got2, want2)
	}

	// Short text
	got3 := shingles("ab", 3)
	if len(got3) != 1 || got3[0] != "ab" {
		t.Fatalf("shingles(ab,3) = %v, want [ab]", got3)
	}
}

func TestPickWinner(t *testing.T) {
	// No suffix preferred over suffix
	nodes := []schema.Node{
		{ID: "pkg:main_c1", Label: "main"},
		{ID: "pkg:main", Label: "main"},
	}
	winner := pickWinner(nodes)
	if winner.ID != "pkg:main" {
		t.Fatalf("expected pkg:main, got %s", winner.ID)
	}

	// Shorter ID preferred when both have no suffix
	nodes2 := []schema.Node{
		{ID: "pkg:main_long", Label: "main"},
		{ID: "pkg:main", Label: "main"},
	}
	winner2 := pickWinner(nodes2)
	if winner2.ID != "pkg:main" {
		t.Fatalf("expected pkg:main, got %s", winner2.ID)
	}

	// Empty input
	winner3 := pickWinner([]schema.Node{})
	if winner3.ID != "" {
		t.Fatalf("expected empty, got %s", winner3.ID)
	}
}

func TestScoreLess(t *testing.T) {
	// FileType-rank dominates. (rank, suffix, idLen)
	// Code (0) beats Document (2) regardless of suffix/length.
	codeWin := score{fileTypeRank: 0, hasSuffix: 1, idLen: 100}
	docLose := score{fileTypeRank: 2, hasSuffix: 0, idLen: 5}
	if !codeWin.less(docLose) {
		t.Fatal("code FileType should outrank document regardless of suffix/length")
	}
	// Within same FileType: no suffix < suffix
	s1 := score{fileTypeRank: 0, hasSuffix: 0, idLen: 10}
	s2 := score{fileTypeRank: 0, hasSuffix: 1, idLen: 5}
	if !s1.less(s2) {
		t.Fatal("no suffix should be better than suffix within same FileType")
	}
	// Same FileType + same suffix: shorter ID wins
	s3 := score{fileTypeRank: 0, hasSuffix: 0, idLen: 5}
	s4 := score{fileTypeRank: 0, hasSuffix: 0, idLen: 10}
	if !s3.less(s4) {
		t.Fatal("shorter should be better when FileType + suffix match")
	}
}
