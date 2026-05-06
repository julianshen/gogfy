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

func TestPackageID(t *testing.T) {
	if got := PackageID("/a/b.go", "main"); got != "pkg:/a/b.go:main" {
		t.Fatalf("unexpected: %s", got)
	}
}

func TestFuncID(t *testing.T) {
	if got := FuncID("/a/b.go", "main", "foo"); got != "fn:/a/b.go:main.foo" {
		t.Fatalf("unexpected: %s", got)
	}
}

func TestImportID(t *testing.T) {
	if got := ImportID("fmt"); got != "pkg:import:fmt" {
		t.Fatalf("unexpected: %s", got)
	}
}

func TestPythonHelpers(t *testing.T) {
	if got := PythonModuleID("/a/b.py"); got != "py:module:/a/b.py" {
		t.Fatalf("unexpected: %s", got)
	}
	if got := PythonFuncID("/a/b.py", "foo"); got != "py:fn:/a/b.py:foo" {
		t.Fatalf("unexpected: %s", got)
	}
	if got := PythonClassID("/a/b.py", "Foo"); got != "py:class:/a/b.py:Foo" {
		t.Fatalf("unexpected: %s", got)
	}
	if got := PythonImportID("os"); got != "py:import:os" {
		t.Fatalf("unexpected: %s", got)
	}
}

func TestSortNodesByID(t *testing.T) {
	nodes := []Node{{ID: "c"}, {ID: "a"}, {ID: "b"}}
	SortNodesByID(nodes)
	if nodes[0].ID != "a" || nodes[1].ID != "b" || nodes[2].ID != "c" {
		t.Fatalf("unexpected order: %v", nodes)
	}
}
