package resolve

import (
	"path/filepath"
	"strings"

	"github.com/julianshen/gogfy/internal/schema"
)

// Imports resolves `imports` edges whose import path corresponds to a
// module/package defined in the corpus, ADDING an INFERRED `imports`
// edge from the importing module to the resolved corpus module node(s).
//
// The original `imports`→stub edge is preserved as the observed
// "imported this path" fact; external imports (stdlib, third-party)
// simply gain no new edge. This turns the import relation into real
// cross-file/cross-package dependency edges — what the MCP traverse/path
// tools need to answer "what does this package depend on" — mirroring
// the resolved `uses` edge upstream graphify emits.
//
// Matching is by trailing path-segment overlap. An import path is split
// per language (Go/JS/TS on '/', Python/Java/Kotlin/Scala on '.', Rust on
// '::', PHP on '\') and matched against each corpus module's directory
// segments and its extension-stripped file segments, requiring at least
// two trailing segments in common to avoid spurious single-name hits.
// Relative imports (leading '.'/'..', JS/TS) resolve against the
// importing file's directory exactly.
func Imports(nodes []schema.Node, edges []schema.Edge) []schema.Edge {
	fileByID := buildFileByID(nodes)

	// Index modules by the last two segments of both their directory and
	// their extension-stripped file path, so a candidate import need only
	// probe a small bucket before a full trailing-suffix verification.
	type modRef struct {
		id   string
		segs []string
	}
	byKey := map[string][]modRef{}
	add := func(id string, segs []string) {
		if len(segs) < 2 {
			return
		}
		k := segs[len(segs)-2] + "/" + segs[len(segs)-1]
		byKey[k] = append(byKey[k], modRef{id, segs})
	}
	for _, n := range nodes {
		_, kind, _, ok := schema.ParseLangID(n.ID)
		if !ok || kind != "module" || n.SourceFile == "" {
			continue
		}
		add(n.ID, pathSegs(strings.TrimSuffix(n.SourceFile, filepath.Ext(n.SourceFile))))
		add(n.ID, pathSegs(filepath.Dir(n.SourceFile)))
	}

	var out []schema.Edge
	out = append(out, edges...)
	seen := map[string]bool{} // dedupe importer→target

	emit := func(importer, target string) {
		if target == "" || importer == target {
			return
		}
		key := importer + "\x00" + target
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, schema.Edge{
			Source:     importer,
			Target:     target,
			Relation:   "imports",
			Confidence: schema.Inferred,
		})
	}

	for _, e := range edges {
		if e.Relation != "imports" {
			continue
		}
		lang, path, ok := parseImportTarget(e.Target)
		if !ok {
			continue
		}
		importer := e.Source
		importerFile := fileByID[importer]

		// Relative import (JS/TS): resolve against the importing dir.
		if strings.HasPrefix(path, ".") && importerFile != "" {
			rel := strings.TrimLeft(path, "./")
			abs := filepath.Join(filepath.Dir(importerFile), filepath.FromSlash(rel))
			target := pathSegs(abs)
			for _, refs := range byKey {
				for _, m := range refs {
					if suffixOverlap(target, m.segs) == len(target) && len(target) >= 1 {
						emit(importer, m.id)
					}
				}
			}
			continue
		}

		segs := splitImport(lang, path)
		// Try the full path, then the path with its trailing segment
		// dropped (Python `from a.b import X` → import target `a.b.X`,
		// where the module is `a.b` and `X` the symbol).
		for _, cand := range [][]string{segs, dropLast(segs)} {
			if len(cand) < 2 {
				continue
			}
			k := cand[len(cand)-2] + "/" + cand[len(cand)-1]
			matched := false
			for _, m := range byKey[k] {
				if suffixOverlap(cand, m.segs) >= 2 {
					emit(importer, m.id)
					matched = true
				}
			}
			if matched {
				break
			}
		}
	}
	return out
}

// parseImportTarget extracts (lang, path) from a `<lang>:import:<path>` ID.
func parseImportTarget(id string) (lang, path string, ok bool) {
	l, kind, p, ok := schema.ParseLangID(id)
	if !ok || kind != "import" {
		return "", "", false
	}
	return l, p, true
}

// splitImport breaks an import path into segments by the separator that
// language uses between path/namespace components.
func splitImport(lang, path string) []string {
	switch lang {
	case "py", "java", "kotlin", "scala":
		return nonEmpty(strings.Split(path, "."))
	case "rust":
		return nonEmpty(strings.Split(path, "::"))
	case "php":
		return nonEmpty(strings.Split(path, `\`))
	default: // go, js, ts, and anything path-shaped
		return pathSegs(path)
	}
}

// pathSegs splits a filesystem-style path into non-empty segments,
// tolerating either separator.
func pathSegs(p string) []string {
	p = strings.ReplaceAll(p, "\\", "/")
	return nonEmpty(strings.Split(p, "/"))
}

func nonEmpty(in []string) []string {
	out := in[:0]
	for _, s := range in {
		if s != "" && s != "." && s != ".." {
			out = append(out, s)
		}
	}
	return out
}

func dropLast(segs []string) []string {
	if len(segs) == 0 {
		return segs
	}
	return segs[:len(segs)-1]
}

// suffixOverlap returns the number of trailing segments a and b share.
func suffixOverlap(a, b []string) int {
	i, j, n := len(a)-1, len(b)-1, 0
	for i >= 0 && j >= 0 && a[i] == b[j] {
		n++
		i--
		j--
	}
	return n
}
