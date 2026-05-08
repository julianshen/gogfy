// Command gogfy is the CLI entry point for the gogfy graph extraction pipeline.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"os/signal"
	"syscall"

	"github.com/julianshen/gogfy/internal/analyze"
	"github.com/julianshen/gogfy/internal/cache"
	"github.com/julianshen/gogfy/internal/cluster"
	"github.com/julianshen/gogfy/internal/detect"
	"github.com/julianshen/gogfy/internal/export"
	"github.com/julianshen/gogfy/internal/extract"
	"github.com/julianshen/gogfy/internal/graph"
	"github.com/julianshen/gogfy/internal/report"
	"github.com/julianshen/gogfy/internal/resolve"
	"github.com/julianshen/gogfy/internal/watch"
)

func main() {
	if err := dispatch(os.Args[1:], os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func dispatch(args []string, stderr io.Writer) error {
	if len(args) == 0 {
		usage(stderr)
		return fmt.Errorf("missing subcommand")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "run":
		// Reorder so flags precede positionals before handing to flag.FlagSet.
		// This lets `gogfy run <root> --update` work as documented in SPEC §8,
		// since flag.Parse stops at the first non-flag token.
		ordered, err := groupRunFlags(rest)
		if err != nil {
			return err
		}
		fs := flag.NewFlagSet("run", flag.ContinueOnError)
		fs.SetOutput(stderr)
		update := fs.Bool("update", false, "incremental update")
		out := fs.String("out", "graphify-out", "output directory")
		directed := fs.Bool("directed", false, "render edges with arrowheads in graph.html")
		graphml := fs.Bool("graphml", false, "also emit graph.graphml (Gephi/yEd)")
		cypher := fs.Bool("cypher", false, "also emit graph.cypher (Neo4j MERGE script)")
		if err := fs.Parse(ordered); err != nil {
			return err
		}
		if fs.NArg() < 1 {
			usage(stderr)
			return fmt.Errorf("run: missing <root>")
		}
		return runPipeline(fs.Arg(0), *out, *update, *directed, runOptions{GraphML: *graphml, Cypher: *cypher})
	case "validate":
		if len(rest) < 1 {
			usage(stderr)
			return fmt.Errorf("validate: missing <graph.json>")
		}
		return validateCommand(rest[0])
	case "report":
		if len(rest) < 1 {
			usage(stderr)
			return fmt.Errorf("report: missing <graph.json>")
		}
		return reportCommand(rest[0], os.Stdout)
	case "watch":
		ordered, err := groupRunFlags(rest)
		if err != nil {
			return err
		}
		fs := flag.NewFlagSet("watch", flag.ContinueOnError)
		fs.SetOutput(stderr)
		out := fs.String("out", "graphify-out", "output directory")
		directed := fs.Bool("directed", false, "render edges with arrowheads in graph.html")
		if err := fs.Parse(ordered); err != nil {
			return err
		}
		if fs.NArg() < 1 {
			usage(stderr)
			return fmt.Errorf("watch: missing <root>")
		}
		return watchCommand(fs.Arg(0), *out, *directed, stderr)
	default:
		usage(stderr)
		return fmt.Errorf("unknown subcommand: %s", sub)
	}
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "usage: gogfy run <root> [--update] [--out dir] [--directed] [--graphml] [--cypher]")
	fmt.Fprintln(w, "       gogfy watch <root> [--out dir] [--directed]")
	fmt.Fprintln(w, "       gogfy validate <graph.json>")
	fmt.Fprintln(w, "       gogfy report <graph.json>")
}

// watchCommand runs an initial pipeline build, then keeps the artifact set
// in sync with corpus changes. Returns when the OS signals SIGINT/SIGTERM
// or the watcher errors out unrecoverably.
func watchCommand(root, out string, directed bool, stderr io.Writer) error {
	if err := runPipeline(root, out, false, directed, runOptions{}); err != nil {
		return fmt.Errorf("initial build: %w", err)
	}
	stop := make(chan struct{})
	sigC := make(chan os.Signal, 1)
	signal.Notify(sigC, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigC
		close(stop)
	}()
	return watch.Run(root, watch.Options{
		Extensions: supportedExtensionsList(),
		Logger:     stderr,
	}, stop, func(_ []string) error {
		// Re-run with --update so the cache filters to changed files only.
		return runPipeline(root, out, true, directed, runOptions{})
	})
}

// supportedExtensions maps source file extensions to the extractor that
// handles them. Adding a new language means: (1) implement an Extractor in
// internal/extract, (2) register its extension(s) here.
var supportedExtensions = map[string]extract.Extractor{
	".go":    extract.GoExtractor{},
	".py":    extract.PythonExtractor{},
	".js":    extract.JavaScriptExtractor{},
	".jsx":   extract.JavaScriptExtractor{},
	".mjs":   extract.JavaScriptExtractor{},
	".cjs":   extract.JavaScriptExtractor{},
	".ts":    extract.TypeScriptExtractor{},
	".tsx":   extract.TypeScriptExtractor{TSX: true},
	".java":  extract.JavaExtractor{},
	".c":     extract.CExtractor{},
	".h":     extract.CExtractor{},
	".cpp":   extract.CppExtractor{},
	".cc":    extract.CppExtractor{},
	".cxx":   extract.CppExtractor{},
	".hpp":   extract.CppExtractor{},
	".hxx":   extract.CppExtractor{},
	".hh":    extract.CppExtractor{},
	".rs":    extract.RustExtractor{},
	".rb":    extract.RubyExtractor{},
	".yaml":  extract.YAMLExtractor{},
	".yml":   extract.YAMLExtractor{},
	".toml":  extract.TOMLExtractor{},
	".kt":    extract.KotlinExtractor{},
	".kts":   extract.KotlinExtractor{},
	".scala": extract.ScalaExtractor{},
	".sc":    extract.ScalaExtractor{},
	".php":   extract.PHPExtractor{},
	".lua":   extract.LuaExtractor{},
	".zig":   extract.ZigExtractor{},
	".jl":    extract.JuliaExtractor{},
	".sh":    extract.BashExtractor{},
	".bash":  extract.BashExtractor{},
}

func supportedExtensionsList() []string {
	exts := make([]string, 0, len(supportedExtensions))
	for ext := range supportedExtensions {
		exts = append(exts, ext)
	}
	return exts
}

// runOptions bundles optional artifact toggles that have grown beyond the
// signature comfort zone. New formats land here rather than as positional
// bools.
type runOptions struct {
	GraphML bool
	Cypher  bool
}

func runPipeline(root, out string, update, directed bool, opts runOptions) error {
	files, err := detect.CollectFiles(root, supportedExtensionsList())
	if err != nil {
		return fmt.Errorf("detect: %w", err)
	}

	cachePath := filepath.Join(out, ".gographify-cache")
	var c *cache.Cache
	if update {
		c = cache.NewCache(cachePath)
		changed, err := c.ChangedFiles(files)
		if err != nil {
			return fmt.Errorf("cache: %w", err)
		}
		// No-op --update must leave prior artifacts untouched, but only if they
		// actually exist — on a first run (empty corpus or fresh out dir) we
		// must still produce the standard artifacts.
		if len(changed) == 0 && artifactsExist(out) {
			fmt.Println("No files changed, skipping extraction")
			return nil
		}
		files = changed
	}

	builder := graph.NewBuilder()

	for _, f := range files {
		ex, ok := supportedExtensions[filepath.Ext(f)]
		if !ok {
			continue
		}
		res, err := ex.Extract(f)
		if err != nil {
			return fmt.Errorf("extract %s: %w", f, err)
		}
		for _, n := range res.Nodes {
			if err := builder.AddNode(n); err != nil {
				return fmt.Errorf("add node: %w", err)
			}
		}
		for _, e := range res.Edges {
			if err := builder.AddEdge(e); err != nil {
				return fmt.Errorf("add edge: %w", err)
			}
		}
	}

	g := builder.Build()
	// Resolve `<lang>:call:<name>` synthetic targets into INFERRED edges
	// pointing at real function nodes (or AMBIGUOUS edges fanned out across
	// multiple candidates). Cross-file calls otherwise stay EXTRACTED with
	// the synthetic target preserved.
	nodes, edges := resolve.Calls(g.Nodes(), g.Edges())

	clusterer := cluster.NewLeidenClusterer()
	clusteredNodes, err := clusterer.Cluster(nodes, edges)
	if err != nil {
		return fmt.Errorf("cluster: %w", err)
	}

	analyzer := analyze.NewAnalyzer()
	reportData := analyzer.Analyze(clusteredNodes, edges)

	reportBytes, err := report.Render(reportData)
	if err != nil {
		return fmt.Errorf("report: %w", err)
	}

	exportGraph := export.GraphExport{
		Nodes: clusteredNodes,
		Edges: edges,
	}

	jsonBytes, err := export.ExportJSON(exportGraph)
	if err != nil {
		return fmt.Errorf("export json: %w", err)
	}

	htmlBytes, err := export.ExportHTML(exportGraph, export.HTMLOptions{Directed: directed})
	if err != nil {
		return fmt.Errorf("export html: %w", err)
	}

	if err := os.MkdirAll(out, 0755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}

	artifacts := []struct {
		name string
		data []byte
	}{
		{"graph.json", jsonBytes},
		{"GRAPH_REPORT.md", reportBytes},
		{"graph.html", htmlBytes},
	}
	if opts.GraphML {
		b, err := export.ExportGraphML(exportGraph)
		if err != nil {
			return fmt.Errorf("export graphml: %w", err)
		}
		artifacts = append(artifacts, struct {
			name string
			data []byte
		}{"graph.graphml", b})
	}
	if opts.Cypher {
		b, err := export.ExportCypher(exportGraph)
		if err != nil {
			return fmt.Errorf("export cypher: %w", err)
		}
		artifacts = append(artifacts, struct {
			name string
			data []byte
		}{"graph.cypher", b})
	}
	for _, a := range artifacts {
		if err := atomicWrite(filepath.Join(out, a.name), a.data); err != nil {
			return fmt.Errorf("write %s: %w", a.name, err)
		}
	}

	if update {
		if err := c.Save(files); err != nil {
			return fmt.Errorf("cache save: %w", err)
		}
	}

	return nil
}

func validateCommand(path string) error {
	g, err := loadGraph(path)
	if err != nil {
		return err
	}
	ids := make(map[string]struct{}, len(g.Nodes))
	for _, n := range g.Nodes {
		if err := n.Validate(); err != nil {
			return fmt.Errorf("invalid node %q: %w", n.ID, err)
		}
		ids[n.ID] = struct{}{}
	}
	for _, e := range g.Edges {
		if err := e.Validate(); err != nil {
			return fmt.Errorf("invalid edge %s->%s (%s): %w", e.Source, e.Target, e.Relation, err)
		}
		if _, ok := ids[e.Source]; !ok {
			return fmt.Errorf("edge source %q not present in nodes", e.Source)
		}
		if _, ok := ids[e.Target]; !ok {
			return fmt.Errorf("edge target %q not present in nodes", e.Target)
		}
	}
	fmt.Printf("OK: %d nodes, %d edges\n", len(g.Nodes), len(g.Edges))
	return nil
}

func reportCommand(path string, w io.Writer) error {
	g, err := loadGraph(path)
	if err != nil {
		return err
	}
	r := analyze.NewAnalyzer().Analyze(g.Nodes, g.Edges)
	out, err := report.Render(r)
	if err != nil {
		return fmt.Errorf("report: %w", err)
	}
	_, err = w.Write(out)
	return err
}

// groupRunFlags reorders args for the `run` subcommand so all known flags
// come before positional args. Required because flag.Parse stops at the first
// non-flag, but SPEC §8 documents `run <root> [--update] [--out dir]`.
func groupRunFlags(args []string) ([]string, error) {
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--update", a == "-update", a == "--directed", a == "-directed",
			a == "--graphml", a == "-graphml", a == "--cypher", a == "-cypher":
			flags = append(flags, a)
		case a == "--out", a == "-out":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("flag %s requires a value", a)
			}
			flags = append(flags, a, args[i+1])
			i++
		case strings.HasPrefix(a, "--out="), strings.HasPrefix(a, "-out="),
			strings.HasPrefix(a, "--update="), strings.HasPrefix(a, "-update="),
			strings.HasPrefix(a, "--directed="), strings.HasPrefix(a, "-directed="),
			strings.HasPrefix(a, "--graphml="), strings.HasPrefix(a, "-graphml="),
			strings.HasPrefix(a, "--cypher="), strings.HasPrefix(a, "-cypher="):
			flags = append(flags, a)
		case strings.HasPrefix(a, "-"):
			return nil, fmt.Errorf("unknown flag: %s", a)
		default:
			positional = append(positional, a)
		}
	}
	return append(flags, positional...), nil
}

func artifactsExist(out string) bool {
	for _, name := range []string{"graph.json", "GRAPH_REPORT.md", "graph.html"} {
		if _, err := os.Stat(filepath.Join(out, name)); err != nil {
			return false
		}
	}
	return true
}

func loadGraph(path string) (export.GraphExport, error) {
	var g export.GraphExport
	data, err := os.ReadFile(path)
	if err != nil {
		return g, fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, &g); err != nil {
		return g, fmt.Errorf("parse %s: %w", path, err)
	}
	return g, nil
}

// atomicWrite writes data to path via a sibling .tmp file followed by rename,
// so a partial write cannot replace a previously-good file with a truncated one.
func atomicWrite(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
