// Package tree renders a D3 collapsible-tree HTML view of a gogfy graph.
//
// The tree is filesystem-hierarchical: a root → directories → files → top-
// level symbol nodes. Useful as a printable / browseable counterpart to
// the force-directed graph.html in graphify-out/.
//
// Ported from safishamsi/graphify v7's graphify/tree_html.py. The HTML
// template (D3 v7 + CSS) is embedded verbatim from the Python original
// with the five substitution placeholders renamed to __TITLE__ /
// __HEADER__ / __SVG_WIDTH__ / __SVG_HEIGHT__ / __DATA_JSON__ so Go's
// strings.Replace doesn't conflict with CSS/JS braces.
package tree

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"html"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/julianshen/gogfy/internal/schema"
)

// DefaultMaxChildren caps how many sibling symbol nodes appear under
// one file; a synthetic "(+N more)" leaf takes the overflow's place
// so very wide files stay browseable.
const DefaultMaxChildren = 200

//go:embed tree.html
var htmlTemplate string

// TreeNode is the hierarchy element D3 consumes: a name, a descendant
// leaf count for the collapsed-summary display, and children.
type TreeNode struct {
	Name       string      `json:"name"`
	TotalCount int         `json:"total_count"`
	Children   []*TreeNode `json:"children"`
}

// Options tunes Build's output. Zero-value matches graphify defaults:
// MaxChildren=200, Root auto-detected, ProjectLabel auto-derived.
type Options struct {
	// Root truncates the tree to source files under this absolute path.
	// "" auto-detects the common ancestor of every node's SourceFile.
	Root string
	// MaxChildren caps the children list under any file node.
	MaxChildren int
	// ProjectLabel overrides the root node's display name. Defaults to
	// the basename of Root.
	ProjectLabel string
}

// Build constructs the directory hierarchy from graph nodes that have a
// non-empty SourceFile. Nodes without a SourceFile (e.g., synthetic
// call-target nodes) are excluded — they have no filesystem position.
//
// Each leaf is a code symbol (class / function); wide files get a
// "(+N more)" synthetic leaf when the symbol list exceeds MaxChildren.
// Each interior node's TotalCount is the sum of its leaves.
func Build(nodes []schema.Node, opts Options) *TreeNode {
	if opts.MaxChildren <= 0 {
		opts.MaxChildren = DefaultMaxChildren
	}

	// Filter to nodes anchored on a source file. Synthetic call/import
	// targets without SourceFile are irrelevant for a filesystem tree.
	withFile := make([]schema.Node, 0, len(nodes))
	for _, n := range nodes {
		if n.SourceFile != "" {
			withFile = append(withFile, n)
		}
	}
	if len(withFile) == 0 {
		return &TreeNode{Name: "(empty graph)"}
	}

	root := opts.Root
	if root == "" {
		root = commonRoot(collectPaths(withFile))
	}
	label := opts.ProjectLabel
	if label == "" {
		label = filepath.Base(root)
		if label == "" || label == "." || label == "/" {
			label = root
		}
		if label == "" {
			label = "/"
		}
	}

	// One TreeNode per directory, keyed by absolute path. The root is
	// pre-seeded; intermediate directories are created on demand.
	dirIndex := map[string]*TreeNode{}
	rootNode := &TreeNode{Name: label}
	dirIndex[root] = rootNode

	// Group symbol nodes by file so each file becomes one TreeNode.
	byFile := map[string][]schema.Node{}
	for _, n := range withFile {
		byFile[n.SourceFile] = append(byFile[n.SourceFile], n)
	}

	files := make([]string, 0, len(byFile))
	for f := range byFile {
		files = append(files, f)
	}
	sort.Strings(files)

	for _, src := range files {
		parentDir := ensureDir(dirIndex, rootNode, filepath.Dir(src), root)
		fileNode := buildFileNode(src, byFile[src], opts.MaxChildren)
		parentDir.Children = append(parentDir.Children, fileNode)
	}

	finalise(rootNode)
	return rootNode
}

// collectPaths extracts the SourceFile strings from a node slice.
func collectPaths(nodes []schema.Node) []string {
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		if n.SourceFile != "" {
			out = append(out, n.SourceFile)
		}
	}
	return out
}

// commonRoot returns the longest path prefix shared by every input,
// at directory granularity. Used when the caller didn't pin a Root.
//
// Preserves absolute-path leading separator: splitting "/repo/pkg/a"
// by `/` produces [""", "repo", "pkg", "a"] — the empty leading
// segment is what distinguishes absolute from relative. filepath.Join
// drops empties, so we reassemble manually.
func commonRoot(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	sep := string(filepath.Separator)
	split := func(p string) []string {
		return strings.Split(filepath.Clean(p), sep)
	}
	common := split(paths[0])
	for _, p := range paths[1:] {
		parts := split(p)
		i := 0
		for i < len(common) && i < len(parts) && common[i] == parts[i] {
			i++
		}
		common = common[:i]
		if len(common) == 0 {
			return ""
		}
	}
	if len(common) == 0 {
		return ""
	}
	// Reassemble. If the first segment is empty, the original was
	// absolute (e.g. /repo/...) and the join needs the leading sep.
	leading := ""
	body := common
	if common[0] == "" {
		leading = sep
		body = common[1:]
	}
	if len(body) == 0 {
		return leading
	}
	return leading + strings.Join(body, sep)
}

// ensureDir walks from absPath up to root, creating TreeNodes for any
// directory not yet in dirIndex, and returns the leaf-most one.
func ensureDir(dirIndex map[string]*TreeNode, rootNode *TreeNode, absPath, root string) *TreeNode {
	if absPath == root || absPath == "." || absPath == "" {
		return rootNode
	}
	if existing, ok := dirIndex[absPath]; ok {
		return existing
	}
	parent := filepath.Dir(absPath)
	if parent == absPath {
		// Already at filesystem root; tree's root sits above.
		return rootNode
	}
	parentNode := ensureDir(dirIndex, rootNode, parent, root)
	node := &TreeNode{Name: filepath.Base(absPath)}
	dirIndex[absPath] = node
	parentNode.Children = append(parentNode.Children, node)
	return node
}

// buildFileNode bundles a file's symbol nodes into one TreeNode whose
// children are the individual symbols, truncated at MaxChildren.
func buildFileNode(src string, syms []schema.Node, maxChildren int) *TreeNode {
	name := filepath.Base(src)
	children := make([]*TreeNode, 0, len(syms))
	for _, n := range syms {
		label := n.Label
		if label == "" {
			label = n.ID
			if label == "" {
				label = "?"
			}
		}
		// Skip a redundant child node that already echoes the file's
		// name (some extractors emit a `<file>` module decl whose
		// label matches the basename). The whole file is already
		// represented by the parent TreeNode.
		if label == name {
			continue
		}
		children = append(children, &TreeNode{
			Name:       label,
			TotalCount: 1,
		})
	}
	sort.Slice(children, func(i, j int) bool {
		// Underscore-prefixed names sort to the end (private symbols
		// shouldn't dominate the top of a file's child list); then
		// case-insensitive alphabetical.
		ip := strings.HasPrefix(children[i].Name, "_")
		jp := strings.HasPrefix(children[j].Name, "_")
		if ip != jp {
			return !ip
		}
		return strings.ToLower(children[i].Name) < strings.ToLower(children[j].Name)
	})
	if len(children) > maxChildren {
		extra := len(children) - maxChildren
		children = append(children[:maxChildren], &TreeNode{
			Name:       fmt.Sprintf("(+%d more)", extra),
			TotalCount: extra,
		})
	}
	count := len(children)
	if count == 0 {
		// An empty file (no extracted symbols) still gets a count of 1
		// so it shows up in the parent dir's running total.
		count = 1
	}
	return &TreeNode{
		Name:       name,
		TotalCount: count,
		Children:   children,
	}
}

// finalise sorts each interior node's children (children-bearing first,
// then alphabetical) and rolls up the descendant leaf count into
// TotalCount so collapsed nodes can show "(Total Count: N)" without
// loading their subtree.
func finalise(n *TreeNode) int {
	if len(n.Children) == 0 {
		if n.TotalCount == 0 {
			n.TotalCount = 1
		}
		return n.TotalCount
	}
	sort.SliceStable(n.Children, func(i, j int) bool {
		ihas := len(n.Children[i].Children) > 0
		jhas := len(n.Children[j].Children) > 0
		if ihas != jhas {
			return ihas
		}
		return strings.ToLower(n.Children[i].Name) < strings.ToLower(n.Children[j].Name)
	})
	total := 0
	for _, c := range n.Children {
		total += finalise(c)
	}
	if total == 0 {
		total = 1
	}
	n.TotalCount = total
	return total
}

// HTMLOptions tunes the rendered HTML output.
type HTMLOptions struct {
	// Title is the <title> element text.
	Title string
	// Header is the <h1> heading. Defaults to Title.
	Header string
	// SVGWidth / SVGHeight set the initial SVG canvas size. Defaults
	// match graphify (6000 × 8000) — the canvas auto-grows as nodes
	// expand so initial values mostly drive scroll-bar appearance.
	SVGWidth, SVGHeight int
}

// HTML renders the embedded D3 template populated with the tree's JSON.
// The </script> sequence is escaped in the JSON payload so script-tag
// breakouts via crafted node labels are impossible.
func HTML(tree *TreeNode, opts HTMLOptions) (string, error) {
	if opts.Title == "" {
		opts.Title = tree.Name + " — gogfy tree viewer"
	}
	if opts.Header == "" {
		opts.Header = tree.Name + " — Knowledge Graph"
	}
	if opts.SVGWidth == 0 {
		opts.SVGWidth = 6000
	}
	if opts.SVGHeight == 0 {
		opts.SVGHeight = 8000
	}
	dataJSON, err := json.Marshal(tree)
	if err != nil {
		return "", fmt.Errorf("tree: marshal: %w", err)
	}
	// Escape `</` inside the JSON so a node label containing literal
	// "</script>" can't terminate the embedded data block. Matches
	// graphify's defense and the standard advice for inline JSON.
	safeJSON := strings.ReplaceAll(string(dataJSON), "</", `<\/`)
	out := htmlTemplate
	out = strings.ReplaceAll(out, "__TITLE__", html.EscapeString(opts.Title))
	out = strings.ReplaceAll(out, "__HEADER__", html.EscapeString(opts.Header))
	out = strings.ReplaceAll(out, "__SVG_WIDTH__", strconv.Itoa(opts.SVGWidth))
	out = strings.ReplaceAll(out, "__SVG_HEIGHT__", strconv.Itoa(opts.SVGHeight))
	out = strings.ReplaceAll(out, "__DATA_JSON__", safeJSON)
	return out, nil
}
