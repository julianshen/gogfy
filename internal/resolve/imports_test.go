package resolve

import (
	"testing"

	"github.com/julianshen/gogfy/internal/schema"
)

func resolvedImports(edges []schema.Edge) [][2]string {
	var out [][2]string
	for _, e := range edges {
		if e.Relation == "imports" && e.Confidence == schema.Inferred {
			out = append(out, [2]string{e.Source, e.Target})
		}
	}
	return out
}

func TestImportsResolvesGoLocalPackage(t *testing.T) {
	nodes := []schema.Node{
		{ID: "go:module:/repo/cmd/app/main.go", SourceFile: "/repo/cmd/app/main.go"},
		{ID: "go:module:/repo/pkg/config/config.go", SourceFile: "/repo/pkg/config/config.go"},
		{ID: "go:module:/repo/pkg/config/load.go", SourceFile: "/repo/pkg/config/load.go"},
		{ID: "go:import:github.com/x/repo/pkg/config", Label: "github.com/x/repo/pkg/config"},
	}
	edges := []schema.Edge{
		{Source: "go:module:/repo/cmd/app/main.go", Target: "go:import:github.com/x/repo/pkg/config", Relation: "imports", Confidence: schema.Extracted},
	}
	got := resolvedImports(Imports(nodes, edges))
	// Should resolve to BOTH files in pkg/config (a package = directory).
	want := map[string]bool{
		"go:module:/repo/pkg/config/config.go": false,
		"go:module:/repo/pkg/config/load.go":   false,
	}
	for _, g := range got {
		if g[0] != "go:module:/repo/cmd/app/main.go" {
			t.Errorf("importer should be main.go, got %s", g[0])
		}
		if _, ok := want[g[1]]; ok {
			want[g[1]] = true
		} else {
			t.Errorf("unexpected resolved target %s", g[1])
		}
	}
	for tgt, hit := range want {
		if !hit {
			t.Errorf("expected resolved edge to %s", tgt)
		}
	}
}

func TestImportsKeepsStubAndIgnoresExternal(t *testing.T) {
	nodes := []schema.Node{
		{ID: "go:module:/repo/a.go", SourceFile: "/repo/a.go"},
		{ID: "go:import:fmt", Label: "fmt"},
	}
	edges := []schema.Edge{
		{Source: "go:module:/repo/a.go", Target: "go:import:fmt", Relation: "imports", Confidence: schema.Extracted},
	}
	out := Imports(nodes, edges)
	// stub edge preserved, no INFERRED edge added for stdlib fmt.
	if len(resolvedImports(out)) != 0 {
		t.Errorf("external import fmt must not resolve")
	}
	if len(out) != 1 {
		t.Errorf("original stub edge must be preserved, got %d edges", len(out))
	}
}

func TestImportsPythonFromImportDropsSymbol(t *testing.T) {
	// `from pkg.config import Loader` → stub `pkg.config.Loader`; module is
	// pkg/config.py, Loader the symbol. Dropping the trailing segment matches.
	nodes := []schema.Node{
		{ID: "py:module:/repo/app.py", SourceFile: "/repo/app.py"},
		{ID: "py:module:/repo/pkg/config.py", SourceFile: "/repo/pkg/config.py"},
		{ID: "py:import:pkg.config.Loader", Label: "pkg.config.Loader"},
	}
	edges := []schema.Edge{
		{Source: "py:module:/repo/app.py", Target: "py:import:pkg.config.Loader", Relation: "imports", Confidence: schema.Extracted},
	}
	got := resolvedImports(Imports(nodes, edges))
	if len(got) != 1 || got[0][1] != "py:module:/repo/pkg/config.py" {
		t.Fatalf("python from-import should resolve to pkg/config.py, got %v", got)
	}
}

func TestImportsRelativeJS(t *testing.T) {
	nodes := []schema.Node{
		{ID: "js:module:/repo/src/a.js", SourceFile: "/repo/src/a.js"},
		{ID: "js:module:/repo/src/util/fmt.js", SourceFile: "/repo/src/util/fmt.js"},
		{ID: "js:import:./util/fmt", Label: "./util/fmt"},
	}
	edges := []schema.Edge{
		{Source: "js:module:/repo/src/a.js", Target: "js:import:./util/fmt", Relation: "imports", Confidence: schema.Extracted},
	}
	got := resolvedImports(Imports(nodes, edges))
	if len(got) != 1 || got[0][1] != "js:module:/repo/src/util/fmt.js" {
		t.Fatalf("relative import should resolve to util/fmt.js, got %v", got)
	}
}

func TestImportsNoSingleSegmentFalsePositive(t *testing.T) {
	// A bare one-segment import must not match a same-named dir/file
	// somewhere unrelated (overlap < 2).
	nodes := []schema.Node{
		{ID: "go:module:/repo/a.go", SourceFile: "/repo/a.go"},
		{ID: "go:module:/other/config/x.go", SourceFile: "/other/config/x.go"},
		{ID: "go:import:config", Label: "config"},
	}
	edges := []schema.Edge{
		{Source: "go:module:/repo/a.go", Target: "go:import:config", Relation: "imports", Confidence: schema.Extracted},
	}
	if got := resolvedImports(Imports(nodes, edges)); len(got) != 0 {
		t.Errorf("single-segment import should not resolve (false-positive guard), got %v", got)
	}
}
