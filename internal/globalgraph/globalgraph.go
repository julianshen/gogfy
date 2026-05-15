// Package globalgraph maintains a cross-repo merged graph on disk
// (~/.gogfy/global-graph.json plus a manifest), letting users compose
// graphs from multiple projects into one searchable view. Per-repo
// node IDs are prefixed with "<tag>::" so collisions across repos
// stay isolated; external library nodes (no SourceFile) dedupe by
// Label across repos so each library appears once.
package globalgraph

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/julianshen/gogfy/internal/export"
	"github.com/julianshen/gogfy/internal/fsutil"
	"github.com/julianshen/gogfy/internal/merge"
	"github.com/julianshen/gogfy/internal/schema"
)

// tagSeparator is the "<tag>::<local-id>" delimiter. Reserved — tag
// strings must not contain it (validateTag enforces). Local IDs may
// (the Rust extractor emits IDs like "std::collections::HashMap");
// pruneRepo uses HasPrefix on the full "<tag>::" anchor so the
// embedded "::" inside a local segment is harmless.
const tagSeparator = "::"

// graphFile / manifestFile are the on-disk names inside the store dir.
const (
	graphFile    = "global-graph.json"
	manifestFile = "global-manifest.json"
	manifestV    = 1
)

// Store is a directory-backed cross-repo graph + manifest. Operations
// are not concurrency-safe; callers are expected to serialize add /
// remove invocations (the CLI invokes one operation per command).
type Store struct {
	dir string
}

// NewStore returns a store rooted at dir. Empty dir defaults to
// ~/.gogfy; returns an error if dir is empty and the user's home
// directory cannot be determined (most often: $HOME unset in a
// container or cron context — surfacing the error here is more
// helpful than letting a later os.Mkdir fail with "permission denied"
// on "/.gogfy").
func NewStore(dir string) (*Store, error) {
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("globalgraph: home directory unavailable; pass --dir explicitly: %w", err)
		}
		dir = filepath.Join(home, ".gogfy")
	}
	return &Store{dir: dir}, nil
}

// Path returns the absolute path to the merged graph JSON, useful for
// `gogfy global path` so callers can pipe it into other subcommands.
func (s *Store) Path() string {
	return filepath.Join(s.dir, graphFile)
}

// manifest is the persisted record of which repos have been added.
// Unexported: callers read it through List() which returns a cloned
// map of RepoEntry values; constructing a manifest by hand from
// outside the package isn't a supported workflow.
type manifest struct {
	Version int                  `json:"version"`
	Repos   map[string]RepoEntry `json:"repos"`
}

// RepoEntry records one repo's contribution to the global graph.
// Exported so callers iterating List() can name the value type.
// Counts are a snapshot at AddedAt — re-Add to refresh.
type RepoEntry struct {
	AddedAt    string `json:"added_at"`
	SourcePath string `json:"source_path"`
	NodeCount  int    `json:"node_count"`
	EdgeCount  int    `json:"edge_count"`
	SourceHash string `json:"source_hash"`
}

// AddResult summarizes one Add invocation. Skipped means the source
// file's hash matched the prior add — a no-op for idempotent re-runs.
type AddResult struct {
	RepoTag      string
	NodesAdded   int
	NodesRemoved int
	Skipped      bool
}

// Add merges the given source graph.json into the global graph under
// repoTag. Re-adding an unchanged source is a no-op; re-adding a
// modified source prunes the old repoTag nodes first so stale data
// can't accumulate.
func (s *Store) Add(sourcePath, repoTag string) (AddResult, error) {
	if err := validateTag(repoTag); err != nil {
		return AddResult{}, err
	}
	man, err := s.loadManifest()
	if err != nil {
		return AddResult{}, err
	}
	hash, err := fsutil.SHA256File(sourcePath)
	if err != nil {
		return AddResult{}, fmt.Errorf("globalgraph: hash %s: %w", sourcePath, err)
	}
	// Skip only when the manifest says we already have this content
	// AND the graph file is still on disk. Without the file-presence
	// check, a user who deletes global-graph.json (or whose store
	// gets partially-wiped) cannot self-heal by re-adding — every
	// re-add would fast-path to "skipped".
	if existing, ok := man.Repos[repoTag]; ok && existing.SourceHash == hash {
		if _, statErr := os.Stat(s.Path()); statErr == nil {
			return AddResult{RepoTag: repoTag, Skipped: true}, nil
		}
	}

	src, err := export.LoadJSON(sourcePath)
	if err != nil {
		return AddResult{}, err
	}
	prefixed := prefixGraph(src, repoTag)

	gg, err := s.Load()
	if err != nil {
		return AddResult{}, err
	}
	removed := pruneRepo(&gg, repoTag)
	// Drop external-library duplicates: if the global graph already
	// has a node with the same Label and no SourceFile, point the
	// new repo's incoming edges at the existing global ID instead of
	// adding a new prefixed copy. Mirrors upstream's behavior so
	// "context.Context" / "logging.Logger" etc. appear once.
	gg, addedNodes := mergeWithExternalDedup(gg, prefixed)

	if err := s.saveGraph(gg); err != nil {
		return AddResult{}, err
	}

	abs := sourcePath
	if a, err := filepath.Abs(sourcePath); err == nil {
		abs = a
	}
	man.Repos[repoTag] = RepoEntry{
		AddedAt:    time.Now().UTC().Format(time.RFC3339),
		SourcePath: abs,
		NodeCount:  addedNodes,
		EdgeCount:  len(prefixed.Edges),
		SourceHash: hash,
	}
	if err := s.saveManifest(man); err != nil {
		return AddResult{}, err
	}

	return AddResult{
		RepoTag:      repoTag,
		NodesAdded:   addedNodes,
		NodesRemoved: removed,
	}, nil
}

// Remove drops all nodes/edges tagged with repoTag and the manifest
// entry. Returns the count of nodes removed.
func (s *Store) Remove(repoTag string) (int, error) {
	man, err := s.loadManifest()
	if err != nil {
		return 0, err
	}
	if _, ok := man.Repos[repoTag]; !ok {
		return 0, fmt.Errorf("globalgraph: repo %q not in manifest", repoTag)
	}
	gg, err := s.Load()
	if err != nil {
		return 0, err
	}
	removed := pruneRepo(&gg, repoTag)
	if err := s.saveGraph(gg); err != nil {
		return 0, err
	}
	delete(man.Repos, repoTag)
	if err := s.saveManifest(man); err != nil {
		return 0, err
	}
	return removed, nil
}

// List returns a copy of the manifest's repos map.
func (s *Store) List() (map[string]RepoEntry, error) {
	man, err := s.loadManifest()
	if err != nil {
		return nil, err
	}
	return maps.Clone(man.Repos), nil
}

// Load reads and returns the current global graph. Returns an empty
// graph if the file doesn't exist yet (first-add path).
func (s *Store) Load() (export.GraphExport, error) {
	g, err := export.LoadJSON(s.Path())
	if errors.Is(err, os.ErrNotExist) {
		return export.GraphExport{Nodes: []schema.Node{}, Edges: []schema.Edge{}}, nil
	}
	return g, err
}

// ─────────────────────────────────────────────────────────────────────────────
// helpers

func validateTag(tag string) error {
	if tag == "" {
		return errors.New("globalgraph: repo tag must not be empty")
	}
	// `.`, `..`, `/` can sneak through when the CLI's defaultRepoTag
	// derivation hits an unusable source path; reject them here too
	// rather than letting them anchor real repo data under a confusing
	// name.
	if tag == "." || tag == ".." || strings.ContainsAny(tag, "/\\") {
		return fmt.Errorf("globalgraph: repo tag %q is not a valid identifier (use --as to set one explicitly)", tag)
	}
	if strings.Contains(tag, tagSeparator) {
		return fmt.Errorf("globalgraph: repo tag %q must not contain %q (reserved as the prefix separator)", tag, tagSeparator)
	}
	return nil
}

// prefixGraph returns a copy of g with every node ID rewritten as
// "<tag>::<original-id>". External-library nodes (no SourceFile) are
// left un-prefixed and unchanged so they remain candidates for
// cross-repo dedup.
func prefixGraph(g export.GraphExport, tag string) export.GraphExport {
	rename := func(id string, isExternal bool) string {
		if isExternal {
			return id
		}
		return tag + tagSeparator + id
	}
	external := map[string]bool{}
	for _, n := range g.Nodes {
		if n.SourceFile == "" {
			external[n.ID] = true
		}
	}
	out := export.GraphExport{
		Nodes: make([]schema.Node, 0, len(g.Nodes)),
		Edges: make([]schema.Edge, 0, len(g.Edges)),
	}
	for _, n := range g.Nodes {
		clone := n
		clone.ID = rename(n.ID, external[n.ID])
		out.Nodes = append(out.Nodes, clone)
	}
	for _, e := range g.Edges {
		clone := e
		clone.Source = rename(e.Source, external[e.Source])
		clone.Target = rename(e.Target, external[e.Target])
		out.Edges = append(out.Edges, clone)
	}
	return out
}

// pruneRepo removes every node prefixed with "<tag>::" plus any edge
// referencing such a node. It additionally garbage-collects external
// (no-SourceFile) nodes that no surviving edge still references —
// these were dedupe-shared by the repo being removed and otherwise
// linger as orphan stale data. Returns count of "real" nodes removed
// (the tag's prefixed nodes); orphan-external cleanup is not reported.
// Operates in-place via slices.DeleteFunc.
func pruneRepo(g *export.GraphExport, tag string) int {
	pfx := tag + tagSeparator
	before := len(g.Nodes)
	g.Nodes = slices.DeleteFunc(g.Nodes, func(n schema.Node) bool {
		return strings.HasPrefix(n.ID, pfx)
	})
	g.Edges = slices.DeleteFunc(g.Edges, func(e schema.Edge) bool {
		return strings.HasPrefix(e.Source, pfx) || strings.HasPrefix(e.Target, pfx)
	})
	removed := before - len(g.Nodes)
	// Garbage-collect orphan externals.
	referenced := make(map[string]bool, 2*len(g.Edges))
	for _, e := range g.Edges {
		referenced[e.Source] = true
		referenced[e.Target] = true
	}
	g.Nodes = slices.DeleteFunc(g.Nodes, func(n schema.Node) bool {
		return n.SourceFile == "" && !referenced[n.ID]
	})
	return removed
}

// mergeWithExternalDedup merges prefixed into base, but for each
// prefixed external node (no SourceFile) whose Label already exists
// in base as an external, rewrites its incoming edges to point at the
// pre-existing global ID. Returns the merged graph and the count of
// new nodes that ended up in base (= prefixed nodes minus deduped
// externals).
func mergeWithExternalDedup(base, prefixed export.GraphExport) (export.GraphExport, int) {
	externalByLabel := map[string]string{}
	for _, n := range base.Nodes {
		if n.SourceFile == "" && n.Label != "" {
			externalByLabel[n.Label] = n.ID
		}
	}
	idRemap := map[string]string{} // prefixed-external-id → base-external-id
	keptNodes := make([]schema.Node, 0, len(prefixed.Nodes))
	for _, n := range prefixed.Nodes {
		if n.SourceFile == "" && n.Label != "" {
			if existing, ok := externalByLabel[n.Label]; ok {
				idRemap[n.ID] = existing
				continue
			}
			externalByLabel[n.Label] = n.ID
		}
		keptNodes = append(keptNodes, n)
	}
	keptEdges := make([]schema.Edge, 0, len(prefixed.Edges))
	for _, e := range prefixed.Edges {
		if remap, ok := idRemap[e.Source]; ok {
			e.Source = remap
		}
		if remap, ok := idRemap[e.Target]; ok {
			e.Target = remap
		}
		keptEdges = append(keptEdges, e)
	}
	repoSlice := export.GraphExport{Nodes: keptNodes, Edges: keptEdges}
	merged := merge.MergeAll([]export.GraphExport{base, repoSlice})
	return merged, len(keptNodes)
}

func (s *Store) loadManifest() (manifest, error) {
	b, err := os.ReadFile(filepath.Join(s.dir, manifestFile))
	if errors.Is(err, os.ErrNotExist) {
		return manifest{Version: manifestV, Repos: map[string]RepoEntry{}}, nil
	}
	if err != nil {
		return manifest{}, fmt.Errorf("globalgraph: read manifest: %w", err)
	}
	var m manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return manifest{}, fmt.Errorf("globalgraph: parse manifest: %w", err)
	}
	if m.Repos == nil {
		m.Repos = map[string]RepoEntry{}
	}
	return m, nil
}

func (s *Store) saveManifest(m manifest) error {
	if m.Version == 0 {
		m.Version = manifestV
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("globalgraph: marshal manifest: %w", err)
	}
	return fsutil.WriteFileAtomic(filepath.Join(s.dir, manifestFile), b, 0o644)
}

func (s *Store) saveGraph(g export.GraphExport) error {
	b, err := export.ExportJSON(g)
	if err != nil {
		return fmt.Errorf("globalgraph: marshal graph: %w", err)
	}
	return fsutil.WriteFileAtomic(s.Path(), b, 0o644)
}

