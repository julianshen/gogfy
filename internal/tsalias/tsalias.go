// Package tsalias parses tsconfig.json / jsconfig.json `compilerOptions.paths`
// so the JS/TS extractors' import edges can be rewritten from aliased
// shapes (`@app/foo`) to actual filesystem paths. Without this, a project
// using path aliases ends up with import edges pointing at synthetic
// nodes (`js:import:@app/foo`) that no other extractor ever produces —
// the alias becomes a structural gap in the graph.
package tsalias

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/julianshen/gogfy/internal/schema"
)

// Aliases holds resolved path-alias entries plus the resolved base
// directory. Zero value matches a project with no alias config.
type Aliases struct {
	baseDir string
	entries []entry
}

type entry struct {
	// pattern is the alias key from tsconfig (e.g. "@app/*"). Wildcards
	// only appear at the end after a `/*`; the prefix part is matched
	// literally and the wildcard tail is substituted into target.
	pattern   string
	prefix    string // pattern without trailing "/*"
	target    string // first target from the paths value array (resolved relative to baseDir)
	wildcard  bool
}

// Load reads tsconfig.json (or jsconfig.json fallback) from rootDir and
// returns the alias set. Missing config is non-fatal: callers get an
// empty Aliases that resolves nothing, so projects without a config
// still build cleanly.
func Load(rootDir string) (*Aliases, error) {
	tsconfigPath, ok := findConfig(rootDir)
	if !ok {
		return &Aliases{}, nil
	}
	data, err := os.ReadFile(tsconfigPath)
	if err != nil {
		return nil, err
	}
	var cfg struct {
		CompilerOptions struct {
			BaseURL string              `json:"baseUrl"`
			Paths   map[string][]string `json:"paths"`
		} `json:"compilerOptions"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		// Malformed config shouldn't break the whole pipeline — there
		// might be // comments (tsconfig.json officially allows them
		// despite being .json), and a strict-JSON parser will reject.
		// Warn so a config-file typo doesn't silently degrade
		// resolution — the very thing this package is meant to fix.
		fmt.Fprintf(os.Stderr, "gogfy: tsalias: skipping %s: %v\n", tsconfigPath, err)
		return &Aliases{}, nil
	}
	baseDir := rootDir
	if b := cfg.CompilerOptions.BaseURL; b != "" {
		baseDir = filepath.Join(rootDir, b)
	}
	a := &Aliases{baseDir: baseDir}
	for pat, targets := range cfg.CompilerOptions.Paths {
		if len(targets) == 0 {
			continue
		}
		e := entry{pattern: pat, target: targets[0]}
		if strings.HasSuffix(pat, "/*") {
			e.wildcard = true
			e.prefix = strings.TrimSuffix(pat, "/*")
		} else {
			e.prefix = pat
		}
		a.entries = append(a.entries, e)
	}
	return a, nil
}

// findConfig returns the path to tsconfig.json or jsconfig.json at
// rootDir, preferring tsconfig when both exist (matches typescript's
// own preference order).
func findConfig(rootDir string) (string, bool) {
	for _, name := range []string{"tsconfig.json", "jsconfig.json"} {
		p := filepath.Join(rootDir, name)
		if _, err := os.Stat(p); err == nil {
			return p, true
		}
	}
	return "", false
}

// Apply rewrites JS/TS import nodes and the edges that target them so
// aliased specifiers point at the resolved filesystem path. Returns
// new slices — input is not mutated, so the function is safe to call
// in pipelines that pass-through values.
//
// Only `js:import:` and `ts:import:` nodes are touched (Aliases are
// loaded from tsconfig/jsconfig; other languages don't use this
// scheme). Unresolved specifiers (bare module names like "react",
// relative paths) keep their original IDs.
func (a *Aliases) Apply(nodes []schema.Node, edges []schema.Edge) ([]schema.Node, []schema.Edge) {
	if a == nil || len(a.entries) == 0 {
		return nodes, edges
	}
	// First pass: discover rewrites without allocating a copy yet — the
	// common case (no aliased imports in this graph) returns the inputs
	// unchanged, so a project that just happens to live in a TS repo
	// pays nothing.
	rewrite := map[string]string{}
	for _, n := range nodes {
		lang, kind, key, ok := schema.ParseLangID(n.ID)
		if !ok || kind != "import" || (lang != "js" && lang != "ts") {
			continue
		}
		resolved, ok := a.Resolve(key)
		if !ok {
			continue
		}
		rewrite[n.ID] = schema.LangID(lang, "import", resolved)
	}
	if len(rewrite) == 0 {
		return nodes, edges
	}
	newNodes := make([]schema.Node, len(nodes))
	for i, n := range nodes {
		newNodes[i] = n
		if mapped, ok := rewrite[n.ID]; ok {
			newNodes[i].ID = mapped
			// Strip the `<lang>:import:` prefix from the rewritten ID
			// to get the resolved path back as a label.
			_, _, key, _ := schema.ParseLangID(mapped)
			newNodes[i].Label = key
		}
	}
	newEdges := make([]schema.Edge, len(edges))
	for i, e := range edges {
		newEdges[i] = e
		if mapped, ok := rewrite[e.Target]; ok {
			newEdges[i].Target = mapped
		}
		if mapped, ok := rewrite[e.Source]; ok {
			newEdges[i].Source = mapped
		}
	}
	return newNodes, newEdges
}

// Resolve maps an import specifier to its resolved filesystem path.
// Returns (path, true) when an alias matched; (_, false) otherwise —
// callers should leave the import target unchanged on miss.
func (a *Aliases) Resolve(spec string) (string, bool) {
	if a == nil || len(a.entries) == 0 {
		return "", false
	}
	for _, e := range a.entries {
		if e.wildcard {
			// Match `<prefix>/<tail>`. The prefix may be empty for a
			// pattern like `*` alone, but tsconfig requires a / so the
			// minimal-prefix case is something like "@/*" → prefix "@".
			if !strings.HasPrefix(spec, e.prefix+"/") {
				continue
			}
			tail := spec[len(e.prefix)+1:]
			// target has its own /* that gets substituted with tail.
			tgt := strings.Replace(e.target, "*", tail, 1)
			return filepath.Join(a.baseDir, tgt), true
		}
		if spec == e.pattern {
			return filepath.Join(a.baseDir, e.target), true
		}
	}
	return "", false
}
