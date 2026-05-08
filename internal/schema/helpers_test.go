package schema

import "testing"

func TestFormatLocation(t *testing.T) {
	if got := FormatLocation(0, 0); got != "1:1" {
		t.Fatalf("expected 1:1, got %s", got)
	}
	if got := FormatLocation(5, 10); got != "6:11" {
		t.Fatalf("expected 6:11, got %s", got)
	}
}

func TestLangID(t *testing.T) {
	if got := LangID("go", "module", "/a/b.go"); got != "go:module:/a/b.go" {
		t.Fatalf("unexpected: %s", got)
	}
	if got := LangID("py", "function", "/a/b.py:foo"); got != "py:function:/a/b.py:foo" {
		t.Fatalf("unexpected: %s", got)
	}
}

func TestParseLangIDRoundTrip(t *testing.T) {
	cases := []struct{ lang, kind, key string }{
		{"go", "call", "Println"},
		{"py", "function", "/abs/path:bar"},
		{"rust", "call", "std::collections::HashMap"},
	}
	for _, c := range cases {
		id := LangID(c.lang, c.kind, c.key)
		gotLang, gotKind, gotKey, ok := ParseLangID(id)
		if !ok {
			t.Fatalf("ParseLangID(%q) failed", id)
		}
		if gotLang != c.lang || gotKind != c.kind || gotKey != c.key {
			t.Fatalf("round-trip mismatch for %q: got (%q,%q,%q), want (%q,%q,%q)",
				id, gotLang, gotKind, gotKey, c.lang, c.kind, c.key)
		}
	}
}

func TestParseLangIDRejectsMalformed(t *testing.T) {
	for _, bad := range []string{"", "no-colons", "only:two-parts"} {
		if _, _, _, ok := ParseLangID(bad); ok {
			t.Fatalf("ParseLangID(%q) should have returned ok=false", bad)
		}
	}
}

func TestSortNodesByID(t *testing.T) {
	nodes := []Node{{ID: "c"}, {ID: "a"}, {ID: "b"}}
	SortNodesByID(nodes)
	if nodes[0].ID != "a" || nodes[1].ID != "b" || nodes[2].ID != "c" {
		t.Fatalf("unexpected order: %v", nodes)
	}
}
