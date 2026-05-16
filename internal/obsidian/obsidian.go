// Package obsidian exports a clustered graph as an Obsidian vault: one
// .md note per node with YAML frontmatter and [[wikilinks]] to neighbors,
// plus a `_COMMUNITY_<name>.md` overview note per community (underscore
// prefix sorts these to the top of Obsidian's file panel).
//
// The vault is meant to be opened directly in Obsidian — community tags
// produce colored groupings in the graph view, and the inline tag list
// at the bottom of each note feeds Obsidian's tag panel.
package obsidian

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/julianshen/gogfy/internal/fsutil"
	"github.com/julianshen/gogfy/internal/schema"
)

// Options carries vault-generation knobs. CommunityLabels overrides the
// default "Community <id>" naming and threads through to filenames,
// frontmatter, and tags so cross-tool consumers see a single source of
// truth (typically `.graphify_labels.json`).
type Options struct {
	OutDir          string
	CommunityLabels map[string]string
}

// Generate writes the vault and returns the number of notes (node + community).
func Generate(nodes []schema.Node, edges []schema.Edge, opts Options) (int, error) {
	if opts.OutDir == "" {
		return 0, fmt.Errorf("obsidian: OutDir required")
	}
	if err := os.MkdirAll(opts.OutDir, 0755); err != nil {
		return 0, fmt.Errorf("obsidian: mkdir %s: %w", opts.OutDir, err)
	}

	// Filenames first so wikilinks resolve to actual file names rather
	// than labels, which can collide (two nodes called "Helper").
	filenames := buildFilenames(nodes)
	neighbors, edgeData := buildAdjacency(edges)
	nodeCommunity := map[string]string{}
	communities := map[string][]schema.Node{}
	for _, n := range nodes {
		if n.Community != "" {
			nodeCommunity[n.ID] = n.Community
			communities[n.Community] = append(communities[n.Community], n)
		}
	}

	written := 0
	for _, n := range nodes {
		body := nodeNote(n, filenames, neighbors[n.ID], edgeData, nodeCommunity, opts.CommunityLabels)
		path := filepath.Join(opts.OutDir, filenames[n.ID]+".md")
		if err := fsutil.WriteFileAtomic(path, []byte(body), 0644); err != nil {
			return written, fmt.Errorf("obsidian: write %s: %w", path, err)
		}
		written++
	}

	// Iterate communities by sorted ID so the on-disk write order is
	// independent of map iteration; combined with stable input order
	// this gives idempotent re-runs.
	cids := make([]string, 0, len(communities))
	for cid := range communities {
		cids = append(cids, cid)
	}
	sort.Strings(cids)

	interCommunity := buildInterCommunity(edges, nodeCommunity)
	for _, cid := range cids {
		members := communities[cid]
		body := communityNote(cid, members, filenames, opts.CommunityLabels, interCommunity[cid])
		fname := "_COMMUNITY_" + communityFilenameSegment(cid, opts.CommunityLabels) + ".md"
		path := filepath.Join(opts.OutDir, fname)
		if err := fsutil.WriteFileAtomic(path, []byte(body), 0644); err != nil {
			return written, fmt.Errorf("obsidian: write %s: %w", path, err)
		}
		written++
	}

	return written, nil
}

// buildFilenames assigns each node a vault-safe, unique filename. Same
// label → suffix `_1`, `_2`, … in node-iteration order. Falls back to
// "unnamed" if sanitization strips everything.
//
// The collision check is case-insensitive so two labels that differ only
// in case (Foo vs foo) don't both produce "Foo.md"/"foo.md" — on
// case-insensitive filesystems (macOS APFS, Windows NTFS) WriteFileAtomic
// would otherwise silently clobber one of them and leave a dangling
// wikilink in every note that referenced the loser.
func buildFilenames(nodes []schema.Node) map[string]string {
	out := make(map[string]string, len(nodes))
	seen := map[string]int{}
	for _, n := range nodes {
		base := safeFilename(labelOrID(n))
		if base == "" {
			base = "unnamed"
		}
		base = escapeReservedBasename(base)
		key := strings.ToLower(base)
		if _, dup := seen[key]; dup {
			seen[key]++
			out[n.ID] = fmt.Sprintf("%s_%d", base, seen[key])
		} else {
			seen[key] = 0
			out[n.ID] = base
		}
	}
	return out
}

// windowsReserved are basenames Windows refuses to open regardless of
// extension. A node labeled "CON" or "aux" would silently fail the
// atomic write on Windows; suffix with "_" so the name is taken
// off the reserved list while staying readable.
var windowsReserved = map[string]struct{}{
	"con": {}, "prn": {}, "aux": {}, "nul": {},
	"com1": {}, "com2": {}, "com3": {}, "com4": {}, "com5": {},
	"com6": {}, "com7": {}, "com8": {}, "com9": {},
	"lpt1": {}, "lpt2": {}, "lpt3": {}, "lpt4": {}, "lpt5": {},
	"lpt6": {}, "lpt7": {}, "lpt8": {}, "lpt9": {},
}

func escapeReservedBasename(s string) string {
	if _, ok := windowsReserved[strings.ToLower(s)]; ok {
		return s + "_"
	}
	return s
}

// unsafeChars matches characters that are illegal in Obsidian filenames
// or on common filesystems (Windows being the strictest). Newlines are
// folded to spaces before this strip so multi-line labels collapse.
var unsafeChars = regexp.MustCompile(`[\\/*?:"<>|#^\[\]]`)

// trailingMdSuffix strips a literal .md/.mdx/.qmd/.markdown so a label
// like "README.md" doesn't end up as "README.md.md".
var trailingMdSuffix = regexp.MustCompile(`(?i)\.(md|mdx|qmd|markdown)$`)

func safeFilename(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = unsafeChars.ReplaceAllString(s, "")
	s = strings.TrimSpace(s)
	s = trailingMdSuffix.ReplaceAllString(s, "")
	return s
}

func labelOrID(n schema.Node) string {
	if n.Label != "" {
		return n.Label
	}
	return n.ID
}

// obsidianTagRe restricts to chars Obsidian's tag parser accepts
// (alphanumerics, hyphens, underscores, slashes). Everything else is
// folded to "_" so a community named "Auth Layer" becomes the tag
// `community/Auth_Layer`.
var obsidianTagRe = regexp.MustCompile(`[^A-Za-z0-9_/\-]`)

func obsidianTag(s string) string {
	return obsidianTagRe.ReplaceAllString(s, "_")
}

func communityName(cid string, labels map[string]string) string {
	if name, ok := labels[cid]; ok && name != "" {
		return name
	}
	return "Community " + cid
}

func communityFilenameSegment(cid string, labels map[string]string) string {
	return safeFilename(communityName(cid, labels))
}

type edgeKey struct{ a, b string }

// canonEdgeKey orders endpoints lexically so lookups from either side of
// the edge hit the same map entry — half the storage of the double-key
// approach, and consistent with how the wiki/callflow packages treat
// undirected edges for display purposes.
func canonEdgeKey(a, b string) edgeKey {
	if a > b {
		a, b = b, a
	}
	return edgeKey{a, b}
}

func buildAdjacency(edges []schema.Edge) (map[string][]string, map[edgeKey]schema.Edge) {
	neighbors := map[string][]string{}
	data := map[edgeKey]schema.Edge{}
	for _, e := range edges {
		neighbors[e.Source] = append(neighbors[e.Source], e.Target)
		neighbors[e.Target] = append(neighbors[e.Target], e.Source)
		data[canonEdgeKey(e.Source, e.Target)] = e
	}
	return neighbors, data
}

func buildInterCommunity(edges []schema.Edge, nodeCommunity map[string]string) map[string]map[string]int {
	out := map[string]map[string]int{}
	for _, e := range edges {
		cu := nodeCommunity[e.Source]
		cv := nodeCommunity[e.Target]
		if cu == "" || cv == "" || cu == cv {
			continue
		}
		if out[cu] == nil {
			out[cu] = map[string]int{}
		}
		if out[cv] == nil {
			out[cv] = map[string]int{}
		}
		out[cu][cv]++
		out[cv][cu]++
	}
	return out
}

func nodeNote(n schema.Node, filenames map[string]string, neighbors []string, edgeData map[edgeKey]schema.Edge, nodeCommunity, labels map[string]string) string {
	var b strings.Builder
	cid := n.Community
	cname := communityName(cid, labels)
	ctag := "community/" + obsidianTag(cname)
	ftype := string(n.FileType)
	ftypeTag := "graphify/document"
	if ftype != "" {
		ftypeTag = "graphify/" + obsidianTag(ftype)
	}
	confTag := "graphify/" + dominantConfidence(n.ID, neighbors, edgeData)

	// YAML frontmatter. Values quoted with %q so a label containing a
	// quote or newline can't break out and inject a sibling key.
	fmt.Fprintln(&b, "---")
	fmt.Fprintf(&b, "source_file: %q\n", n.SourceFile)
	fmt.Fprintf(&b, "type: %q\n", ftype)
	fmt.Fprintf(&b, "community: %q\n", cname)
	if n.SourceLocation != "" {
		fmt.Fprintf(&b, "location: %q\n", n.SourceLocation)
	}
	fmt.Fprintln(&b, "tags:")
	for _, tag := range []string{ftypeTag, confTag, ctag} {
		fmt.Fprintf(&b, "  - %s\n", tag)
	}
	fmt.Fprintln(&b, "---")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "# %s\n\n", labelOrID(n))

	if len(neighbors) > 0 {
		// Sort by neighbor's label so ordering is stable across runs.
		ns := append([]string(nil), neighbors...)
		sort.Slice(ns, func(i, j int) bool { return filenames[ns[i]] < filenames[ns[j]] })
		// Dedupe (parallel edges produce duplicate neighbor entries).
		seen := map[string]struct{}{}
		fmt.Fprintln(&b, "## Connections")
		for _, nb := range ns {
			if _, dup := seen[nb]; dup {
				continue
			}
			seen[nb] = struct{}{}
			e := edgeData[canonEdgeKey(n.ID, nb)]
			fmt.Fprintf(&b, "- [[%s]] - `%s` [%s]\n", filenames[nb], e.Relation, e.Confidence)
		}
		fmt.Fprintln(&b)
	}

	// Inline tag line at the bottom — fuels Obsidian's tag panel.
	fmt.Fprintf(&b, "#%s #%s #%s\n", ftypeTag, confTag, ctag)
	return b.String()
}

func dominantConfidence(nodeID string, neighbors []string, edgeData map[edgeKey]schema.Edge) string {
	if len(neighbors) == 0 {
		return schema.Extracted.String()
	}
	counts := map[string]int{}
	for _, nb := range neighbors {
		e := edgeData[canonEdgeKey(nodeID, nb)]
		counts[e.Confidence.String()]++
	}
	// Highest count wins; ties broken by string order so output is stable.
	best := ""
	bestN := -1
	for k, v := range counts {
		if v > bestN || (v == bestN && k < best) {
			best = k
			bestN = v
		}
	}
	if best == "" {
		return schema.Extracted.String()
	}
	return best
}

func communityNote(cid string, members []schema.Node, filenames map[string]string, labels map[string]string, cross map[string]int) string {
	var b strings.Builder
	cname := communityName(cid, labels)

	fmt.Fprintln(&b, "---")
	fmt.Fprintln(&b, "type: community")
	fmt.Fprintf(&b, "members: %d\n", len(members))
	fmt.Fprintln(&b, "---")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "# %s\n\n", cname)
	fmt.Fprintf(&b, "**Members:** %d nodes\n\n", len(members))

	ms := append([]schema.Node(nil), members...)
	sort.Slice(ms, func(i, j int) bool { return filenames[ms[i].ID] < filenames[ms[j].ID] })
	fmt.Fprintln(&b, "## Members")
	for _, m := range ms {
		entry := fmt.Sprintf("- [[%s]]", filenames[m.ID])
		if m.FileType != "" {
			entry += " - " + string(m.FileType)
		}
		if m.SourceFile != "" {
			entry += " - " + m.SourceFile
		}
		fmt.Fprintln(&b, entry)
	}
	fmt.Fprintln(&b)

	// Dataview live query so users with the plugin get an auto-refreshed
	// member table without the writer having to update it on every run.
	commTagName := obsidianTag(cname)
	fmt.Fprintln(&b, "## Live Query (requires Dataview plugin)")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "```dataview")
	fmt.Fprintf(&b, "TABLE source_file, type FROM #community/%s\n", commTagName)
	fmt.Fprintln(&b, "SORT file.name ASC")
	fmt.Fprintln(&b, "```")
	fmt.Fprintln(&b)

	if len(cross) > 0 {
		type pair struct {
			cid   string
			count int
		}
		ps := make([]pair, 0, len(cross))
		for c, n := range cross {
			ps = append(ps, pair{c, n})
		}
		// Highest edge count first; ties broken by cid so output diffs cleanly.
		sort.Slice(ps, func(i, j int) bool {
			if ps[i].count != ps[j].count {
				return ps[i].count > ps[j].count
			}
			return ps[i].cid < ps[j].cid
		})
		fmt.Fprintln(&b, "## Connections to other communities")
		for _, p := range ps {
			otherName := communityName(p.cid, labels)
			otherSafe := safeFilename(otherName)
			s := "s"
			if p.count == 1 {
				s = ""
			}
			fmt.Fprintf(&b, "- %d edge%s to [[_COMMUNITY_%s]]\n", p.count, s, otherSafe)
		}
		fmt.Fprintln(&b)
	}

	return b.String()
}
