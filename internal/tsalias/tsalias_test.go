package tsalias

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/julianshen/gogfy/internal/schema"
)

func TestLoadParsesPathsFromTsconfig(t *testing.T) {
	root := t.TempDir()
	tsconfig := `{
  "compilerOptions": {
    "baseUrl": ".",
    "paths": {
      "@app/*": ["src/*"],
      "@util": ["src/util/index.ts"]
    }
  }
}`
	if err := os.WriteFile(filepath.Join(root, "tsconfig.json"), []byte(tsconfig), 0o644); err != nil {
		t.Fatal(err)
	}
	a, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.entries) != 2 {
		t.Fatalf("expected 2 aliases, got %d: %+v", len(a.entries), a.entries)
	}
}

func TestLoadFallsBackToJsconfig(t *testing.T) {
	// When tsconfig.json is absent, jsconfig.json should be honored.
	root := t.TempDir()
	js := `{"compilerOptions":{"baseUrl":".","paths":{"~/*":["src/*"]}}}`
	if err := os.WriteFile(filepath.Join(root, "jsconfig.json"), []byte(js), 0o644); err != nil {
		t.Fatal(err)
	}
	a, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.entries) != 1 {
		t.Fatalf("expected 1 alias, got %d", len(a.entries))
	}
}

func TestLoadNoConfigReturnsEmpty(t *testing.T) {
	a, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if a == nil {
		t.Fatal("expected non-nil empty Aliases on missing config")
	}
	if len(a.entries) != 0 {
		t.Fatalf("expected 0 aliases, got %d", len(a.entries))
	}
}

func TestResolveWildcardAlias(t *testing.T) {
	root := t.TempDir()
	tsconfig := `{
  "compilerOptions": {
    "baseUrl": ".",
    "paths": {
      "@app/*": ["src/*"]
    }
  }
}`
	if err := os.WriteFile(filepath.Join(root, "tsconfig.json"), []byte(tsconfig), 0o644); err != nil {
		t.Fatal(err)
	}
	a, _ := Load(root)
	got, ok := a.Resolve("@app/auth/login")
	if !ok {
		t.Fatal("expected alias to resolve")
	}
	want := filepath.Join(root, "src", "auth", "login")
	if got != want {
		t.Fatalf("resolve: got %q want %q", got, want)
	}
}

func TestResolveExactAliasNoWildcard(t *testing.T) {
	root := t.TempDir()
	tsconfig := `{"compilerOptions":{"baseUrl":".","paths":{"@util":["src/util/index.ts"]}}}`
	if err := os.WriteFile(filepath.Join(root, "tsconfig.json"), []byte(tsconfig), 0o644); err != nil {
		t.Fatal(err)
	}
	a, _ := Load(root)
	got, ok := a.Resolve("@util")
	if !ok {
		t.Fatal("expected exact alias to resolve")
	}
	want := filepath.Join(root, "src", "util", "index.ts")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestResolveUnaliasedReturnsFalse(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "tsconfig.json"), []byte(`{"compilerOptions":{"paths":{"@app/*":["src/*"]}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	a, _ := Load(root)
	// Bare external module name — not an alias.
	if _, ok := a.Resolve("react"); ok {
		t.Fatal("bare 'react' must not be treated as an alias")
	}
	if _, ok := a.Resolve("./local"); ok {
		t.Fatal("relative path must not be aliased")
	}
}

func TestResolveBaseUrlSubdir(t *testing.T) {
	// baseUrl can point to a subdir; alias resolution is relative to it.
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "tsconfig.json"), []byte(`{"compilerOptions":{"baseUrl":"./project","paths":{"@/*":["src/*"]}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	a, _ := Load(root)
	got, _ := a.Resolve("@/main")
	want := filepath.Join(root, "project", "src", "main")
	if got != want {
		t.Fatalf("baseUrl subdir: got %q want %q", got, want)
	}
}

func TestApplyRewritesAliasedImportEdges(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "tsconfig.json"), []byte(`{"compilerOptions":{"baseUrl":".","paths":{"@app/*":["src/*"]}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	a, _ := Load(root)

	nodes := []schema.Node{
		{ID: "ts:module:/proj/main.ts", Label: "main.ts"},
		{ID: "ts:import:@app/auth", Label: "@app/auth"},
		{ID: "ts:import:react", Label: "react"}, // unaliased, must survive
	}
	edges := []schema.Edge{
		{Source: "ts:module:/proj/main.ts", Target: "ts:import:@app/auth", Relation: "imports"},
		{Source: "ts:module:/proj/main.ts", Target: "ts:import:react", Relation: "imports"},
	}
	gotN, gotE := a.Apply(nodes, edges)

	// Aliased import node ID must rewrite.
	wantID := "ts:import:" + filepath.Join(root, "src", "auth")
	found := false
	for _, n := range gotN {
		if n.ID == wantID {
			found = true
			if n.Label != filepath.Join(root, "src", "auth") {
				t.Errorf("Label should also rewrite: %q", n.Label)
			}
		}
	}
	if !found {
		t.Fatalf("rewrite missing — expected %s in %v", wantID, gotN)
	}
	// Unaliased "react" must be untouched.
	reactOK := false
	for _, n := range gotN {
		if n.ID == "ts:import:react" {
			reactOK = true
			break
		}
	}
	if !reactOK {
		t.Fatal("react import was incorrectly rewritten")
	}

	// Edge target must also rewrite.
	hits := 0
	for _, e := range gotE {
		if e.Target == wantID {
			hits++
		}
	}
	if hits != 1 {
		t.Fatalf("expected 1 edge retargeted to %s, got %d", wantID, hits)
	}
}

func TestApplyNoConfigPassesThroughUnchanged(t *testing.T) {
	a, _ := Load(t.TempDir())
	nodes := []schema.Node{{ID: "ts:import:@app/foo", Label: "@app/foo"}}
	edges := []schema.Edge{{Source: "m", Target: "ts:import:@app/foo"}}
	gotN, gotE := a.Apply(nodes, edges)
	if gotN[0].ID != nodes[0].ID {
		t.Fatal("no-config Apply must pass nodes through unchanged")
	}
	if gotE[0].Target != edges[0].Target {
		t.Fatal("no-config Apply must pass edges through unchanged")
	}
}

func TestResolveLongestPrefixWinsOnOverlap(t *testing.T) {
	// Overlapping aliases: `@/*` and `@util/*` both match `@util/foo`.
	// TypeScript's rule is most-specific-first. Map iteration in Load
	// would otherwise make the winner depend on Go's map randomization.
	root := t.TempDir()
	cfg := `{"compilerOptions":{"baseUrl":".","paths":{"@/*":["generic/*"],"@util/*":["lib/util/*"]}}}`
	if err := os.WriteFile(filepath.Join(root, "tsconfig.json"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	a, _ := Load(root)
	got, ok := a.Resolve("@util/foo")
	if !ok {
		t.Fatal("expected match")
	}
	want := filepath.Join(root, "lib", "util", "foo")
	if got != want {
		t.Fatalf("longest-prefix rule broken: got %q want %q", got, want)
	}
}

func TestResolveWildcardTargetWithoutStarKeepsTail(t *testing.T) {
	// `"@app/*": ["src/app"]` (target has no `*`) — the spec's tail
	// must still be appended; otherwise `@app/foo/bar` collapses onto
	// `src/app` with everything else.
	root := t.TempDir()
	cfg := `{"compilerOptions":{"baseUrl":".","paths":{"@app/*":["src/app"]}}}`
	if err := os.WriteFile(filepath.Join(root, "tsconfig.json"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	a, _ := Load(root)
	got, _ := a.Resolve("@app/users/login")
	want := filepath.Join(root, "src", "app", "users", "login")
	if got != want {
		t.Fatalf("starless target: got %q want %q", got, want)
	}
}

func TestLoadPrefersTsconfigOverJsconfig(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "tsconfig.json"), []byte(`{"compilerOptions":{"paths":{"@ts/*":["from-ts/*"]}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "jsconfig.json"), []byte(`{"compilerOptions":{"paths":{"@js/*":["from-js/*"]}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	a, _ := Load(root)
	if _, ok := a.Resolve("@ts/x"); !ok {
		t.Error("tsconfig alias missing — jsconfig wrongly took precedence")
	}
	if _, ok := a.Resolve("@js/x"); ok {
		t.Error("jsconfig alias leaked when tsconfig was present")
	}
}

func TestLoadNoBaseUrlFallsBackToRoot(t *testing.T) {
	// Without baseUrl, resolution should be relative to the project root.
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "tsconfig.json"), []byte(`{"compilerOptions":{"paths":{"@/*":["src/*"]}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	a, _ := Load(root)
	got, _ := a.Resolve("@/main")
	want := filepath.Join(root, "src", "main")
	if got != want {
		t.Fatalf("no-baseUrl fallback: got %q want %q", got, want)
	}
}

func TestLoadFirstTargetWinsForMultiTargetAlias(t *testing.T) {
	// Document the multi-target policy: graphify-equivalent without
	// filesystem checks — first target wins. Test pins the contract
	// so a future "try each" refactor consciously flips it.
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "tsconfig.json"), []byte(`{"compilerOptions":{"baseUrl":".","paths":{"@app/*":["primary/*","fallback/*"]}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	a, _ := Load(root)
	got, _ := a.Resolve("@app/foo")
	want := filepath.Join(root, "primary", "foo")
	if got != want {
		t.Fatalf("first-target should win: got %q want %q", got, want)
	}
}
