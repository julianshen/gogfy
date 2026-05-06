package main

import (
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
	var (
		update = flag.Bool("update", false, "incremental update")
		out    = flag.String("out", "graphify-out", "output directory")
	)
	flag.Parse()
	args := flag.Args()
	if len(args) < 2 || args[0] != "run" {
		fmt.Fprintln(os.Stderr, "usage: gogfy run <root>")
		os.Exit(1)
	}
	root := args[1]
	if err := runPipeline(root, *out, *update); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runPipeline(root, out string, update bool) error {
	files, err := detect.CollectFiles(root, []string{".go"})
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
		files = changed
		if len(files) == 0 {
			fmt.Println("No files changed, skipping extraction")
		}
	}

	builder := graph.NewBuilder()
	extractor := extract.GoExtractor{}

	for _, f := range files {
		res, err := extractor.Extract(f)
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

	clusterer := cluster.NewConnectedComponentsClusterer()
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

	if err := os.WriteFile(filepath.Join(out, "graph.json"), jsonBytes, 0644); err != nil {
		return fmt.Errorf("write graph.json: %w", err)
	}
	if err := os.WriteFile(filepath.Join(out, "GRAPH_REPORT.md"), reportBytes, 0644); err != nil {
		return fmt.Errorf("write GRAPH_REPORT.md: %w", err)
	}
	if err := os.WriteFile(filepath.Join(out, "graph.html"), htmlBytes, 0644); err != nil {
		return fmt.Errorf("write graph.html: %w", err)
	}

	// Save cache after successful run
	if update {
		if c == nil {
			c = cache.NewCache(cachePath)
		}
		if err := c.Save(files); err != nil {
			return fmt.Errorf("cache save: %w", err)
		}
	}

	return nil
}
