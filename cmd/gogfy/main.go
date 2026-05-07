// Command gogfy is the CLI entry point for the gogfy graph extraction pipeline.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/julianshen/gogfy/internal/analyze"
	"github.com/julianshen/gogfy/internal/cache"
	"github.com/julianshen/gogfy/internal/cluster"
	"github.com/julianshen/gogfy/internal/detect"
	"github.com/julianshen/gogfy/internal/export"
	"github.com/julianshen/gogfy/internal/extract"
	"github.com/julianshen/gogfy/internal/graph"
	"github.com/julianshen/gogfy/internal/report"
)

func main() {
	if err := dispatch(os.Args[1:], os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// dispatch parses args and routes to the appropriate subcommand. It returns an
// error (rather than calling os.Exit) so it can be unit-tested.
func dispatch(args []string, stderr *os.File) error {
	fs := flag.NewFlagSet("gogfy", flag.ContinueOnError)
	fs.SetOutput(stderr)
	update := fs.Bool("update", false, "incremental update")
	out := fs.String("out", "graphify-out", "output directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) < 2 {
		usage(stderr)
		return fmt.Errorf("missing subcommand or argument")
	}
	switch rest[0] {
	case "run":
		return runPipeline(rest[1], *out, *update)
	case "validate":
		return validateCommand(rest[1])
	case "report":
		return reportCommand(rest[1])
	default:
		usage(stderr)
		return fmt.Errorf("unknown subcommand: %s", rest[0])
	}
}

func usage(w *os.File) {
	fmt.Fprintln(w, "usage: gogfy run <root> [--update] [--out dir]")
	fmt.Fprintln(w, "       gogfy validate <graph.json>")
	fmt.Fprintln(w, "       gogfy report <graph.json>")
}

func runPipeline(root, out string, update bool) error {
	files, err := detect.CollectFiles(root, []string{".go", ".py"})
	if err != nil {
		return fmt.Errorf("detect: %w", err)
	}

	cachePath := filepath.Join(out, ".gographify-cache")
	allFiles := files
	var c *cache.Cache
	if update {
		c = cache.NewCache(cachePath)
		changed, err := c.ChangedFiles(files)
		if err != nil {
			return fmt.Errorf("cache: %w", err)
		}
		// On a no-op --update run, leave existing artifacts untouched rather than
		// overwriting them with empty-graph output.
		if len(changed) == 0 {
			fmt.Println("No files changed, skipping extraction")
			return nil
		}
		files = changed
	}

	builder := graph.NewBuilder()
	goExtractor := extract.GoExtractor{}
	pyExtractor := extract.PythonExtractor{}

	for _, f := range files {
		var res extract.Result
		var err error
		switch filepath.Ext(f) {
		case ".go":
			res, err = goExtractor.Extract(f)
		case ".py":
			res, err = pyExtractor.Extract(f)
		default:
			continue
		}
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
	nodes := g.Nodes()
	edges := g.Edges()

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

	htmlBytes, err := export.ExportHTML(exportGraph)
	if err != nil {
		return fmt.Errorf("export html: %w", err)
	}

	if err := os.MkdirAll(out, 0755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}

	// Atomic writes: stage to .tmp then rename, so a mid-write failure can't
	// leave half-stale artifacts on disk.
	if err := atomicWrite(filepath.Join(out, "graph.json"), jsonBytes); err != nil {
		return fmt.Errorf("write graph.json: %w", err)
	}
	if err := atomicWrite(filepath.Join(out, "GRAPH_REPORT.md"), reportBytes); err != nil {
		return fmt.Errorf("write GRAPH_REPORT.md: %w", err)
	}
	if err := atomicWrite(filepath.Join(out, "graph.html"), htmlBytes); err != nil {
		return fmt.Errorf("write graph.html: %w", err)
	}

	if update {
		if c == nil {
			c = cache.NewCache(cachePath)
		}
		// Save against the full collected file list (not the changed subset),
		// so unchanged files retain hashes and aren't reported "changed" next run.
		if err := c.Save(allFiles); err != nil {
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
	for _, n := range g.Nodes {
		if err := n.Validate(); err != nil {
			return fmt.Errorf("invalid node %q: %w", n.ID, err)
		}
	}
	ids := make(map[string]struct{}, len(g.Nodes))
	for _, n := range g.Nodes {
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

func reportCommand(path string) error {
	g, err := loadGraph(path)
	if err != nil {
		return err
	}
	r := analyze.NewAnalyzer().Analyze(g.Nodes, g.Edges)
	out, err := report.Render(r)
	if err != nil {
		return fmt.Errorf("report: %w", err)
	}
	_, err = os.Stdout.Write(out)
	return err
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

