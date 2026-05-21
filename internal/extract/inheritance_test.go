package extract

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func extractInheritance(t *testing.T, ext, src string) []string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "f"+ext)
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	var ex interface{ Extract(string) (Result, error) }
	switch ext {
	case ".py":
		ex = PythonExtractor{}
	case ".java":
		ex = JavaExtractor{}
	case ".js":
		ex = JavaScriptExtractor{}
	case ".ts":
		ex = TypeScriptExtractor{}
	case ".cs":
		ex = CSharpExtractor{}
	case ".kt":
		ex = KotlinExtractor{}
	case ".scala":
		ex = ScalaExtractor{}
	case ".swift":
		ex = SwiftExtractor{}
	case ".dart":
		ex = DartExtractor{}
	case ".rb":
		ex = RubyExtractor{}
	case ".php":
		ex = PHPExtractor{}
	case ".rs":
		ex = RustExtractor{}
	case ".go":
		ex = GoExtractor{}
	default:
		t.Fatalf("no extractor for %s", ext)
	}
	res, err := ex.Extract(p)
	if err != nil {
		t.Fatalf("%s: %v", ext, err)
	}
	label := map[string]string{}
	for _, n := range res.Nodes {
		label[n.ID] = n.Label
	}
	var got []string
	for _, e := range res.Edges {
		if e.Relation != "inherits" && e.Relation != "implements" && e.Relation != "embeds" {
			continue
		}
		got = append(got, label[e.Source]+" "+e.Relation+" "+label[e.Target])
	}
	sort.Strings(got)
	return got
}

func TestInheritanceExtractionPerLanguage(t *testing.T) {
	cases := []struct {
		name string
		ext  string
		src  string
		want []string
	}{
		{"python", ".py", "class Sub(Base, mixins.M): pass", []string{"Sub inherits Base", "Sub inherits M"}},
		{"java", ".java", "class Sub extends Base implements I, J {}\ninterface K extends L {}", []string{"K inherits L", "Sub implements I", "Sub implements J", "Sub inherits Base"}},
		{"js", ".js", "class Sub extends Base {}", []string{"Sub inherits Base"}},
		{"ts", ".ts", "class Sub extends Base implements I, J {}\ninterface K extends L {}", []string{"K inherits L", "Sub implements I", "Sub implements J", "Sub inherits Base"}},
		{"csharp", ".cs", "class Sub : Base, IFoo {}", []string{"Sub inherits Base", "Sub inherits IFoo"}},
		{"kotlin", ".kt", "class Sub : Base(), I {}", []string{"Sub inherits Base", "Sub inherits I"}},
		{"scala", ".scala", "class Sub extends Base with T1 with T2 {}", []string{"Sub inherits Base", "Sub inherits T1", "Sub inherits T2"}},
		{"swift", ".swift", "class Sub: Base, Proto {}", []string{"Sub inherits Base", "Sub inherits Proto"}},
		{"dart", ".dart", "class Sub extends Base implements I {}", []string{"Sub implements I", "Sub inherits Base"}},
		{"ruby", ".rb", "class Sub < Base\nend", []string{"Sub inherits Base"}},
		{"php", ".php", "<?php class Sub extends Base implements I, J {}", []string{"Sub implements I", "Sub implements J", "Sub inherits Base"}},
		{"rust", ".rs", "impl Trait for Foo {}\nimpl Foo {}", []string{"Foo implements Trait"}},
		{"go", ".go", "package p\nimport \"io\"\ntype S struct { Base\n X int }\ntype I interface { io.Reader }", []string{"I embeds Reader", "S embeds Base"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractInheritance(t, tc.ext, tc.src)
			if strings.Join(got, " | ") != strings.Join(tc.want, " | ") {
				t.Errorf("inheritance edges mismatch\n got: %v\nwant: %v", got, tc.want)
			}
		})
	}
}

func TestInheritanceNoBaseEmitsNoEdge(t *testing.T) {
	// A class with no superclass/interface must not emit inheritance edges.
	for _, tc := range []struct{ ext, src string }{
		{".py", "class Solo: pass"},
		{".java", "class Solo {}"},
		{".go", "package p\ntype Solo struct { X int }"},
		{".rs", "struct Solo {}\nimpl Solo {}"},
	} {
		if got := extractInheritance(t, tc.ext, tc.src); len(got) != 0 {
			t.Errorf("%s: expected no inheritance edges, got %v", tc.ext, got)
		}
	}
}
