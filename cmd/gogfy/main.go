// Command gogfy is the CLI entry point for the gogfy graph extraction pipeline.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"os/signal"
	"syscall"

	"github.com/julianshen/gogfy/internal/analyze"
	"github.com/julianshen/gogfy/internal/benchmark"
	"github.com/julianshen/gogfy/internal/cache"
	"github.com/julianshen/gogfy/internal/callflow"
	"github.com/julianshen/gogfy/internal/globalgraph"
	"github.com/julianshen/gogfy/internal/cluster"
	"github.com/julianshen/gogfy/internal/dedup"
	"github.com/julianshen/gogfy/internal/detect"
	"github.com/julianshen/gogfy/internal/export"
	"github.com/julianshen/gogfy/internal/extract"
	"github.com/julianshen/gogfy/internal/fsutil"
	"github.com/julianshen/gogfy/internal/githook"
	"github.com/julianshen/gogfy/internal/graph"
	"github.com/julianshen/gogfy/internal/installer"
	"github.com/julianshen/gogfy/internal/merge"
	"github.com/julianshen/gogfy/internal/report"
	"github.com/julianshen/gogfy/internal/gitmeta"
	"github.com/julianshen/gogfy/internal/graphdiff"
	"github.com/julianshen/gogfy/internal/ingest"
	"sync"

	"github.com/julianshen/gogfy/internal/llm"
	"github.com/julianshen/gogfy/internal/llm/anthropic"
	"github.com/julianshen/gogfy/internal/llm/ollama"
	"github.com/julianshen/gogfy/internal/llm/openai"
	"github.com/julianshen/gogfy/internal/rationale"
	"github.com/julianshen/gogfy/internal/safefetch"
	"github.com/julianshen/gogfy/internal/semantic"
	"github.com/julianshen/gogfy/internal/resolve"
	"github.com/julianshen/gogfy/internal/tsalias"
	"github.com/julianshen/gogfy/internal/schema"
	"github.com/julianshen/gogfy/internal/serve"
	"github.com/julianshen/gogfy/internal/tree"
	"github.com/julianshen/gogfy/internal/watch"
	"errors"

	"github.com/julianshen/gogfy/internal/labels"
	"github.com/julianshen/gogfy/internal/obsidian"
	"github.com/julianshen/gogfy/internal/wiki"
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
		clusterOnly := fs.Bool("cluster-only", false, "skip extraction; re-cluster the existing graph.json under --out")
		noViz := fs.Bool("no-viz", false, "skip graph.html (faster runs, smaller artifact set)")
		emitWiki := fs.Bool("wiki", false, "also emit <out>/wiki/ (index + per-community + per-god-node markdown)")
		emitTree := fs.Bool("tree", false, "also emit <out>/tree.html (D3 collapsible filesystem-tree view)")
		noDedup := fs.Bool("no-dedup", false, "skip entity deduplication (faster, may produce duplicate nodes)")
		semantic := fs.Bool("semantic", false, "extract entities from document files via LLM (requires ANTHROPIC_API_KEY)")
		semanticBackend := fs.String("backend", "", "LLM backend when --semantic is set (default: anthropic)")
		if err := fs.Parse(ordered); err != nil {
			return err
		}
		if !*clusterOnly && fs.NArg() < 1 {
			usage(stderr)
			return fmt.Errorf("run: missing <root>")
		}
		root := ""
		if fs.NArg() >= 1 {
			root = fs.Arg(0)
		}
		return runPipeline(root, *out, *update, *directed, runOptions{
			GraphML:         *graphml,
			Cypher:          *cypher,
			ClusterOnly:     *clusterOnly,
			NoViz:           *noViz,
			Wiki:            *emitWiki,
			Tree:            *emitTree,
			NoDedup:         *noDedup,
			Semantic:        *semantic,
			SemanticBackend: *semanticBackend,
		})
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
	case "install":
		return installCommand(rest, false, stderr)
	case "uninstall":
		return installCommand(rest, true, stderr)
	case "install-instructions":
		return instructionsCommand(rest, false, stderr)
	case "uninstall-instructions":
		return instructionsCommand(rest, true, stderr)
	case "hook":
		return hookCommand(rest, stderr)
	case "serve":
		return serveCommand(rest, os.Stdin, os.Stdout, stderr)
	case "path":
		return pathCommand(rest, os.Stdout, stderr)
	case "merge-graphs":
		return mergeGraphsCommand(rest, os.Stdout, stderr)
	case "diff":
		return diffCommand(rest, os.Stdout, stderr)
	case "ingest":
		return ingestCommand(rest, stderr)
	case "wiki":
		return wikiCommand(rest, stderr)
	case "labels":
		return labelsCommand(rest, stderr)
	case "obsidian":
		return obsidianCommand(rest, stderr)
	case "tree":
		return treeCommand(rest, stderr)
	case "benchmark":
		return benchmarkCommand(rest, os.Stdout, stderr)
	case "callflow":
		return callflowCommand(rest, stderr)
	case "global":
		return globalCommand(rest, os.Stdout, stderr)
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
		// Combo wrapper: `gogfy <platform> install` runs install +
		// install-instructions + hook install in one shot. Detected here
		// so the explicit install/uninstall/hook subcommands above still
		// take precedence.
		if comboPlatformDocs(sub) != "" && len(rest) >= 1 && (rest[0] == "install" || rest[0] == "uninstall") {
			return comboCommand(sub, rest[0], rest[1:], stderr)
		}
		usage(stderr)
		return fmt.Errorf("unknown subcommand: %s", sub)
	}
}

// comboPlatformDocs returns the conventional project-level docs file for
// the named platform, or "" if the platform isn't a known combo target.
// Used by the combo wrapper to decide where to write the gogfy
// instructions snippet.
func comboPlatformDocs(platform string) string {
	switch platform {
	case "claude":
		return "CLAUDE.md"
	case "codex":
		return "AGENTS.md"
	case "cursor":
		return ".cursorrules"
	case "gemini":
		return "GEMINI.md"
	case "vscode", "opencode", "kilocode", "kimi",
		"aider", "claw", "copilot", "droid", "trae", "trae-cn",
		"hermes", "kiro", "pi", "antigravity":
		// Default to AGENTS.md when no platform-specific docs path is
		// wired up. Users with platform-specific docs files can override
		// via the explicit install-instructions subcommand.
		return "AGENTS.md"
	case "qwen":
		// Qwen Code (Gemini CLI fork) reads QWEN.md; falls back to AGENTS.md
		// if absent — but writing QWEN.md by default keeps the snippet
		// discoverable to the agent that owns this combo target.
		return "QWEN.md"
	default:
		return ""
	}
}

// comboCommand runs the full install/uninstall sequence for one platform:
// MCP config + project-level instructions snippet + git post-commit hook.
// On install, fails fast on the first error so partial state is visible
// rather than silently wedged. On uninstall, attempts every step and
// aggregates errors — partial cleanup is still useful.
func comboCommand(platform, verb string, args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet(platform+" "+verb, flag.ContinueOnError)
	fs.SetOutput(stderr)
	workspace := fs.String("workspace", ".", "workspace root (defaults to cwd)")
	bin := fs.String("gogfy-bin", "", "path or name of the gogfy binary (defaults to absolute path of running gogfy)")
	outDir := fs.String("out", "graphify-out", "graph output directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("%s %s: unexpected positional arguments: %v", platform, verb, fs.Args())
	}
	abs, err := filepath.Abs(*workspace)
	if err != nil {
		return fmt.Errorf("%s %s: resolve workspace: %w", platform, verb, err)
	}
	resolvedBin := *bin
	if resolvedBin == "" {
		if exe, err := os.Executable(); err == nil {
			resolvedBin = exe
		} else {
			resolvedBin = "gogfy"
		}
	}
	docsFile := filepath.Join(abs, comboPlatformDocs(platform))

	if verb == "uninstall" {
		// Conservative: only remove the platform's MCP config. The hook
		// and the docs-file snippet are commonly *shared* across platforms
		// — codex and vscode both target AGENTS.md; the post-commit hook
		// is repo-wide and serves whichever platforms read graphify-out.
		// Removing them on a single-platform uninstall would break the
		// other platforms still in use. Tell the user to run the explicit
		// teardown commands if they want those gone too.
		inst, err := installer.For(platform)
		if err != nil {
			return fmt.Errorf("%s uninstall: %w", platform, err)
		}
		if err := inst.Uninstall(abs); err != nil {
			return fmt.Errorf("%s uninstall (mcp): %w", platform, err)
		}
		fmt.Fprintf(stderr, "gogfy: %s uninstall complete (MCP config removed).\n", platform)
		fmt.Fprintf(stderr, "      The %s snippet and post-commit hook are repo-wide and may be shared\n", comboPlatformDocs(platform))
		fmt.Fprintf(stderr, "      with other platforms; remove them explicitly with:\n")
		fmt.Fprintf(stderr, "        gogfy uninstall-instructions --file %s\n", docsFile)
		fmt.Fprintf(stderr, "        gogfy hook uninstall --repo %s\n", abs)
		return nil
	}

	// install — fail fast.
	inst, err := installer.For(platform)
	if err != nil {
		return fmt.Errorf("%s install: %w", platform, err)
	}
	if err := inst.Install(abs, installer.Options{Bin: resolvedBin, OutDir: *outDir}); err != nil {
		return fmt.Errorf("%s install (mcp): %w", platform, err)
	}
	if err := installer.InstallSnippet(docsFile, installer.SnippetOptions{
		ReportPath: filepath.Join(*outDir, "GRAPH_REPORT.md"),
	}); err != nil {
		return fmt.Errorf("%s install (snippet): %w", platform, err)
	}
	if err := githook.Install(abs, githook.Options{Bin: resolvedBin, OutDir: *outDir}); err != nil {
		return fmt.Errorf("%s install (hook): %w", platform, err)
	}
	fmt.Fprintf(stderr, "gogfy: %s install complete (mcp config + %s snippet + post-commit hook)\n",
		platform, comboPlatformDocs(platform))
	return nil
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "usage: gogfy run <root> [--update] [--out dir] [--directed] [--graphml] [--cypher] [--no-viz] [--wiki] [--tree]")
	fmt.Fprintln(w, "       gogfy run --cluster-only [--out dir] [--directed] [--no-viz] [--wiki] [--tree]")
	fmt.Fprintln(w, "       gogfy watch <root> [--out dir] [--directed]")
	fmt.Fprintln(w, "       gogfy validate <graph.json>")
	fmt.Fprintln(w, "       gogfy report <graph.json>")
	fmt.Fprintln(w, "       gogfy serve [--graph <graph.json>] [--report <GRAPH_REPORT.md>]")
	fmt.Fprintln(w, "       gogfy install --platform <claude|codex|cursor|vscode|gemini|opencode|kilocode|qwen|kimi|aider|claw|copilot|droid|trae|trae-cn|hermes|kiro|pi|antigravity> [--workspace <dir>] [--gogfy-bin <path>] [--out <dir>]")
	fmt.Fprintln(w, "       gogfy uninstall --platform <claude|codex|cursor|vscode|gemini|opencode|kilocode|qwen|kimi|aider|claw|copilot|droid|trae|trae-cn|hermes|kiro|pi|antigravity> [--workspace <dir>]")
	fmt.Fprintln(w, "       gogfy install-instructions [--file <path>] [--report <path>]")
	fmt.Fprintln(w, "       gogfy uninstall-instructions [--file <path>]")
	fmt.Fprintln(w, "       gogfy hook install [--repo <dir>] [--gogfy-bin <path>] [--out <dir>]")
	fmt.Fprintln(w, "       gogfy hook uninstall [--repo <dir>]")
	fmt.Fprintln(w, "       gogfy hook status [--repo <dir>]")
	fmt.Fprintln(w, "       gogfy hook install-merge-driver [--repo <dir>] [--gogfy-bin <path>]")
	fmt.Fprintln(w, "       gogfy hook uninstall-merge-driver [--repo <dir>]")
	fmt.Fprintln(w, "       gogfy path <source> <target> [--graph <graph.json>]")
	fmt.Fprintln(w, "       gogfy merge-graphs <a.json> <b.json> [<...>] [--out <merged.json>]")
	fmt.Fprintln(w, "       gogfy diff <old.json> <new.json>")
	fmt.Fprintln(w, "       gogfy ingest <url> [--out <dir>]")
	fmt.Fprintln(w, "       gogfy wiki <graph.json> [--out <dir>]")
	fmt.Fprintln(w, "       gogfy labels <graph.json> [--out <path>] [--force]")
	fmt.Fprintln(w, "       gogfy obsidian <graph.json> [--out <vault-dir>]")
	fmt.Fprintln(w, "       gogfy tree <graph.json> [--out <html-path>]")
	fmt.Fprintln(w, "       gogfy benchmark <graph.json> [--corpus-words N] [--depth D] [--json]")
	fmt.Fprintln(w, "       gogfy callflow <graph.json> [--out <html-path>] [--max-sections N] [--max-nodes M] [--max-edges E] [--project NAME]")
	fmt.Fprintln(w, "       gogfy global add <graph.json> [--as TAG] [--dir <store-dir>]")
	fmt.Fprintln(w, "       gogfy global remove <TAG> [--dir <store-dir>]")
	fmt.Fprintln(w, "       gogfy global list [--dir <store-dir>]")
	fmt.Fprintln(w, "       gogfy global path [--dir <store-dir>]")
	fmt.Fprintln(w, "       gogfy <claude|codex|cursor|vscode|gemini|opencode|kilocode|qwen|kimi|aider|claw|copilot|droid|trae|trae-cn|hermes|kiro|pi|antigravity> install   # combo: mcp + snippet + hook in one shot")
	fmt.Fprintln(w, "       gogfy <claude|codex|cursor|vscode|gemini|opencode|kilocode|qwen|kimi|aider|claw|copilot|droid|trae|trae-cn|hermes|kiro|pi|antigravity> uninstall # remove all three")
}

// pathCommand finds the shortest connectivity path between two nodes
// (treating edges as undirected, same semantics as the gogfy_path MCP tool).
// Reads the graph from --graph (default graphify-out/graph.json) and prints
// the path as a numbered list to stdout, or "no path" if disconnected.
func pathCommand(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("path", flag.ContinueOnError)
	fs.SetOutput(stderr)
	graphPath := fs.String("graph", "graphify-out/graph.json", "path to graph.json")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 2 {
		return fmt.Errorf("path: expected <source> <target>, got %d positional argument(s)", fs.NArg())
	}
	g, err := loadGraph(*graphPath)
	if err != nil {
		return fmt.Errorf("path: %w", err)
	}
	srv := serve.New(g, nil)
	src, srcCands, ok := srv.FindNode(fs.Arg(0))
	if !ok {
		return fmt.Errorf("path: source not found: %q", fs.Arg(0))
	}
	if len(srcCands) > 1 {
		fmt.Fprintf(stderr, "path: source label %q matches %d nodes; using %s. Pass the full ID to disambiguate.\n",
			fs.Arg(0), len(srcCands), src.ID)
	}
	tgt, tgtCands, ok := srv.FindNode(fs.Arg(1))
	if !ok {
		return fmt.Errorf("path: target not found: %q", fs.Arg(1))
	}
	if len(tgtCands) > 1 {
		fmt.Fprintf(stderr, "path: target label %q matches %d nodes; using %s. Pass the full ID to disambiguate.\n",
			fs.Arg(1), len(tgtCands), tgt.ID)
	}
	hops := srv.ShortestPath(src.ID, tgt.ID)
	if len(hops) == 0 {
		fmt.Fprintf(stdout, "no path from %q to %q\n", src.Label, tgt.Label)
		return nil
	}
	for i, id := range hops {
		fmt.Fprintf(stdout, "%d. %s (%s)\n", i+1, srv.LabelFor(id), id)
	}
	return nil
}

// mergeGraphsCommand unions two or more graph.json inputs into a single
// graph and writes it (atomically) to --out, or to stdout if --out is
// omitted.
func mergeGraphsCommand(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("merge-graphs", flag.ContinueOnError)
	fs.SetOutput(stderr)
	out := fs.String("out", "", "write merged graph to this path (default: stdout)")
	// Allow `merge-graphs a.json b.json --out c.json` shape — flag.Parse
	// stops at the first non-flag token, so without reordering --out after
	// positionals is misread as another input file.
	ordered, err := reorderMergeGraphFlags(args)
	if err != nil {
		return err
	}
	if err := fs.Parse(ordered); err != nil {
		return err
	}
	if fs.NArg() < 2 {
		return fmt.Errorf("merge-graphs: expected at least two input files, got %d", fs.NArg())
	}
	inputs := make([]export.GraphExport, 0, fs.NArg())
	for _, path := range fs.Args() {
		g, err := loadGraph(path)
		if err != nil {
			return fmt.Errorf("merge-graphs: %w", err)
		}
		inputs = append(inputs, g)
	}
	merged := merge.MergeAll(inputs)
	data, err := export.ExportJSON(merged)
	if err != nil {
		return fmt.Errorf("merge-graphs: marshal: %w", err)
	}
	if *out == "" {
		_, err = stdout.Write(append(data, '\n'))
		return err
	}
	return fsutil.WriteFileAtomic(*out, data, 0644)
}

// reorderMergeGraphFlags moves --out (or --out=<v>) before positional file
// args so flag.Parse picks it up regardless of where the user typed it.
func reorderMergeGraphFlags(args []string) ([]string, error) {
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--out", a == "-out":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("flag %s requires a value", a)
			}
			flags = append(flags, a, args[i+1])
			i++
		case strings.HasPrefix(a, "--out="), strings.HasPrefix(a, "-out="):
			flags = append(flags, a)
		case strings.HasPrefix(a, "-"):
			return nil, fmt.Errorf("unknown flag: %s", a)
		default:
			positional = append(positional, a)
		}
	}
	return append(flags, positional...), nil
}

// diffCommand backs `gogfy diff <old.json> <new.json>`. Loads two
// graph snapshots and prints a markdown summary of added / removed /
// changed nodes and added / removed edges. Useful for spotting what
// changed between two pipeline runs without diffing raw graph.json.
func diffCommand(args []string, stdout, stderr io.Writer) error {
	if len(args) != 2 {
		return fmt.Errorf("diff: expected <old.json> <new.json>, got %d args", len(args))
	}
	oldGraph, err := loadGraph(args[0])
	if err != nil {
		return fmt.Errorf("diff: load old: %w", err)
	}
	newGraph, err := loadGraph(args[1])
	if err != nil {
		return fmt.Errorf("diff: load new: %w", err)
	}
	d := graphdiff.Compute(oldGraph.Nodes, newGraph.Nodes, oldGraph.Edges, newGraph.Edges)
	renderDiff(stdout, d)
	return nil
}

// renderDiff writes the diff as a compact markdown summary. Empty
// sections are elided so a no-change diff produces just a header.
func renderDiff(w io.Writer, d graphdiff.Diff) {
	fmt.Fprintln(w, "# Graph diff")
	fmt.Fprintf(w, "\n- %d nodes added, %d removed, %d changed\n", len(d.NodesAdded), len(d.NodesRemoved), len(d.NodesChanged))
	fmt.Fprintf(w, "- %d edges added, %d removed\n", len(d.EdgesAdded), len(d.EdgesRemoved))
	if len(d.NodesAdded) > 0 {
		fmt.Fprintln(w, "\n## Nodes added")
		for _, n := range d.NodesAdded {
			fmt.Fprintf(w, "- %s (%s)\n", n.DisplayLabel(), n.ID)
		}
	}
	if len(d.NodesRemoved) > 0 {
		fmt.Fprintln(w, "\n## Nodes removed")
		for _, n := range d.NodesRemoved {
			fmt.Fprintf(w, "- %s (%s)\n", n.DisplayLabel(), n.ID)
		}
	}
	if len(d.NodesChanged) > 0 {
		fmt.Fprintln(w, "\n## Nodes changed")
		for _, c := range d.NodesChanged {
			fmt.Fprintf(w, "- %s: ", c.New.ID)
			if c.Old.Label != c.New.Label {
				fmt.Fprintf(w, "label %q→%q ", c.Old.Label, c.New.Label)
			}
			if c.Old.Community != c.New.Community {
				fmt.Fprintf(w, "community %q→%q ", c.Old.Community, c.New.Community)
			}
			if c.Old.FileType != c.New.FileType {
				fmt.Fprintf(w, "type %q→%q ", c.Old.FileType, c.New.FileType)
			}
			fmt.Fprintln(w)
		}
	}
	if len(d.EdgesAdded) > 0 {
		fmt.Fprintln(w, "\n## Edges added")
		for _, e := range d.EdgesAdded {
			fmt.Fprintf(w, "- %s --%s--> %s\n", e.Source, e.Relation, e.Target)
		}
	}
	if len(d.EdgesRemoved) > 0 {
		fmt.Fprintln(w, "\n## Edges removed")
		for _, e := range d.EdgesRemoved {
			fmt.Fprintf(w, "- %s --%s--> %s\n", e.Source, e.Relation, e.Target)
		}
	}
}

// ingestCommand fetches a URL through the SSRF-guarded safe_fetch
// and writes a markdown sidecar under --out (default "graphify-out").
// The sidecar is picked up by a subsequent `gogfy run` as an
// ordinary document — semantic extraction can then process it like
// any local markdown file.
func ingestCommand(args []string, stderr io.Writer) error {
	ordered, err := reorderFlags(args, []string{"out"}, nil)
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("ingest", flag.ContinueOnError)
	fs.SetOutput(stderr)
	out := fs.String("out", "graphify-out", "output directory (sidecar lands under <out>/ingested/)")
	if err := fs.Parse(ordered); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("ingest: expected one <url> argument, got %d", fs.NArg())
	}
	path, err := ingest.Ingest(context.Background(), fs.Arg(0), *out, safefetch.Options{})
	if err != nil {
		return fmt.Errorf("ingest: %w", err)
	}
	fmt.Fprintf(stderr, "ingest: wrote %s\n", path)
	return nil
}

// semanticJob bundles the file path + already-read bytes so the
// parallel pass doesn't re-read source from disk.
type semanticJob struct {
	path string
	src  []byte
}

// semanticConcurrency caps in-flight LLM requests. Anthropic's free
// tier rate-limit is loose enough at 4 concurrent that this won't
// 429 a typical run, while still giving ~3-4× throughput vs serial.
const semanticConcurrency = 4

// runSemanticJobs fans out semantic extraction with bounded concurrency.
// Returns results in input order so downstream merge produces
// deterministic graph IDs regardless of how Go scheduled workers.
// Errors are logged per-file (best-effort policy) so a single
// rate-limit hiccup doesn't fail the whole pipeline.
func runSemanticJobs(ctx context.Context, client llm.Client, jobs []semanticJob, limit int) ([]semantic.Result, error) {
	if limit <= 0 {
		limit = 1
	}
	out := make([]semantic.Result, len(jobs))
	sem := make(chan struct{}, limit)
	var wg sync.WaitGroup
	for i, j := range jobs {
		wg.Add(1)
		go func(i int, j semanticJob) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			r, err := semantic.Extract(ctx, client, j.path, j.src)
			if err != nil {
				fmt.Fprintf(os.Stderr, "gogfy: semantic skipped for %s: %v\n", j.path, err)
				return
			}
			out[i] = r
		}(i, j)
	}
	wg.Wait()
	return out, nil
}

// buildLLMClient picks an LLM backend by name. Empty string defaults
// to anthropic (most-likely user already has Claude access via the
// MCP integration). All backends share the llm.Client interface so
// the rest of the pipeline is provider-agnostic.
func buildLLMClient(backend string) (llm.Client, error) {
	if backend == "" {
		backend = "anthropic"
	}
	switch backend {
	case "anthropic":
		return anthropic.New()
	case "openai":
		return openai.New()
	case "ollama":
		return ollama.New()
	}
	return nil, fmt.Errorf("unknown LLM backend %q (supported: anthropic, openai, ollama)", backend)
}

// hookCommand backs `gogfy hook install` / `gogfy hook uninstall`. The
// hook structure (subcommand + verb) mirrors graphify's `graphify hook
// install` shape and keeps the top-level help readable.
func hookCommand(args []string, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("hook: missing verb (install|uninstall)")
	}
	verb, rest := args[0], args[1:]
	switch verb {
	case "status":
		return hookStatusCommand(rest, os.Stdout, stderr)
	case "install-merge-driver":
		return mergeDriverCommand(rest, false, stderr)
	case "uninstall-merge-driver":
		return mergeDriverCommand(rest, true, stderr)
	}
	if verb != "install" && verb != "uninstall" {
		return fmt.Errorf("hook: unknown verb %q (expected install, uninstall, status, install-merge-driver, or uninstall-merge-driver)", verb)
	}
	fs := flag.NewFlagSet("hook "+verb, flag.ContinueOnError)
	fs.SetOutput(stderr)
	repo := fs.String("repo", ".", "git repository root (defaults to cwd)")
	bin := fs.String("gogfy-bin", "", "path or name of the gogfy binary the hook should invoke (defaults to the absolute path of the running gogfy)")
	outDir := fs.String("out", "graphify-out", "graph output directory passed to gogfy run --update")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("hook %s: unexpected positional arguments: %v", verb, fs.Args())
	}
	abs, err := filepath.Abs(*repo)
	if err != nil {
		return fmt.Errorf("hook %s: resolve repo: %w", verb, err)
	}
	if verb == "uninstall" {
		if err := githook.Uninstall(abs); err != nil {
			return fmt.Errorf("hook uninstall: %w", err)
		}
		fmt.Fprintf(stderr, "gogfy: removed post-commit auto-rebuild from %s\n", abs)
		return nil
	}
	resolvedBin := *bin
	if resolvedBin == "" {
		// Default to the absolute path of the currently-running binary so
		// the hook works even when GUI git clients (Tower, SourceTree, the
		// VS Code embedded git) launch with a stripped PATH.
		if exe, err := os.Executable(); err == nil {
			resolvedBin = exe
		} else {
			resolvedBin = "gogfy"
		}
	}
	if err := githook.Install(abs, githook.Options{Bin: resolvedBin, OutDir: *outDir}); err != nil {
		return fmt.Errorf("hook install: %w", err)
	}
	fmt.Fprintf(stderr, "gogfy: post-commit auto-rebuild installed at %s\n", githook.HookPath(abs))
	return nil
}

// mergeDriverCommand backs `gogfy hook install-merge-driver` and the
// matching uninstall. Registers (or removes) a graph.json union merge
// driver in the repo's .git/config and a merge=gogfy rule in
// .gitattributes. Lets two devs commit parallel graphs without
// conflict-marker hell — git auto-unions via `gogfy merge-graphs`.
func mergeDriverCommand(args []string, remove bool, stderr io.Writer) error {
	op := "install-merge-driver"
	if remove {
		op = "uninstall-merge-driver"
	}
	fs := flag.NewFlagSet("hook "+op, flag.ContinueOnError)
	fs.SetOutput(stderr)
	repo := fs.String("repo", ".", "git repository root (defaults to cwd)")
	bin := fs.String("gogfy-bin", "", "gogfy binary path used inside the driver command (defaults to absolute path of running gogfy)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("hook %s: unexpected positional arguments: %v", op, fs.Args())
	}
	abs, err := filepath.Abs(*repo)
	if err != nil {
		return fmt.Errorf("hook %s: %w", op, err)
	}
	if remove {
		if err := githook.UninstallMergeDriver(abs); err != nil {
			return err
		}
		fmt.Fprintf(stderr, "gogfy: removed graph.json merge-driver from %s\n", abs)
		return nil
	}
	resolvedBin := *bin
	if resolvedBin == "" {
		if exe, err := os.Executable(); err == nil {
			resolvedBin = exe
		} else {
			resolvedBin = "gogfy"
		}
	}
	if err := githook.InstallMergeDriver(abs, githook.MergeDriverOptions{Bin: resolvedBin}); err != nil {
		return err
	}
	fmt.Fprintf(stderr, "gogfy: graph.json merge-driver installed (registered in .git/config + .gitattributes).\n")
	fmt.Fprintf(stderr, "      `git merge` on parallel graphify-out/graph.json branches will auto-union.\n")
	return nil
}

// hookStatusCommand reports per-hook install state.
func hookStatusCommand(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("hook status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repo := fs.String("repo", ".", "git repository root (defaults to cwd)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("hook status: unexpected positional arguments: %v", fs.Args())
	}
	abs, err := filepath.Abs(*repo)
	if err != nil {
		return err
	}
	for _, st := range githook.Status(abs) {
		state := "not installed"
		if st.Installed {
			state = "installed"
		}
		fmt.Fprintf(stdout, "%-15s %s    (%s)\n", st.Name, state, st.Path)
	}
	driverState := "not installed"
	if githook.MergeDriverInstalled(abs) {
		driverState = "installed"
	}
	fmt.Fprintf(stdout, "%-15s %s    (%s)\n", "merge-driver", driverState, filepath.Join(abs, ".git", "config"))
	return nil
}

// instructionsCommand backs `gogfy install-instructions` (and its
// uninstall counterpart). Writes a fenced gogfy block into a project-level
// docs file (CLAUDE.md, AGENTS.md, etc.) telling the agent to read
// GRAPH_REPORT.md before answering codebase questions.
func instructionsCommand(args []string, remove bool, stderr io.Writer) error {
	op := "install-instructions"
	if remove {
		op = "uninstall-instructions"
	}
	fs := flag.NewFlagSet(op, flag.ContinueOnError)
	fs.SetOutput(stderr)
	file := fs.String("file", "AGENTS.md", "target docs file (e.g. CLAUDE.md, AGENTS.md, .cursorrules)")
	report := fs.String("report", "graphify-out/GRAPH_REPORT.md", "workspace-relative path the snippet should reference")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("%s: unexpected positional arguments: %v", op, fs.Args())
	}
	abs, err := filepath.Abs(*file)
	if err != nil {
		return fmt.Errorf("%s: resolve file: %w", op, err)
	}
	if remove {
		if err := installer.UninstallSnippet(abs); err != nil {
			return fmt.Errorf("%s: %w", op, err)
		}
		fmt.Fprintf(stderr, "gogfy: removed instructions from %s\n", abs)
		return nil
	}
	if err := installer.InstallSnippet(abs, installer.SnippetOptions{ReportPath: *report}); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	fmt.Fprintf(stderr, "gogfy: wrote instructions to %s\n", abs)
	return nil
}

// installCommand handles both `gogfy install` and `gogfy uninstall` (the
// remove flag flips the operation). One subcommand pair lights up every
// MCP-capable platform we ship.
func installCommand(args []string, remove bool, stderr io.Writer) error {
	op := "install"
	if remove {
		op = "uninstall"
	}
	fs := flag.NewFlagSet(op, flag.ContinueOnError)
	fs.SetOutput(stderr)
	platform := fs.String("platform", "", "target platform: "+strings.Join(installer.SupportedPlatforms(), ", "))
	workspace := fs.String("workspace", ".", "workspace root (defaults to cwd)")
	bin := fs.String("gogfy-bin", "gogfy", "path or name of the gogfy binary the agent should launch")
	outDir := fs.String("out", "graphify-out", "directory containing graph.json / GRAPH_REPORT.md (must match `gogfy run --out`)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("%s: unexpected positional arguments: %v", op, fs.Args())
	}
	if *platform == "" {
		return fmt.Errorf("%s: --platform is required (one of: %s)", op, strings.Join(installer.SupportedPlatforms(), ", "))
	}
	inst, err := installer.For(*platform)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	abs, err := filepath.Abs(*workspace)
	if err != nil {
		return fmt.Errorf("%s: resolve workspace: %w", op, err)
	}
	if remove {
		if err := inst.Uninstall(abs); err != nil {
			return fmt.Errorf("uninstall %s: %w", *platform, err)
		}
		fmt.Fprintf(stderr, "gogfy: uninstalled from %s (%s)\n", *platform, inst.ConfigPath(abs))
		return nil
	}
	if err := inst.Install(abs, installer.Options{Bin: *bin, OutDir: *outDir}); err != nil {
		return fmt.Errorf("install %s: %w", *platform, err)
	}
	fmt.Fprintf(stderr, "gogfy: installed for %s at %s\n", *platform, inst.ConfigPath(abs))
	return nil
}

// serveCommand runs the MCP server over stdio. The server reads the graph
// snapshot and rendered report from disk once at startup; rebuilding is the
// caller's job (`gogfy run` or `gogfy watch`).
func serveCommand(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	graphPath := fs.String("graph", "graphify-out/graph.json", "path to graph.json")
	reportPath := fs.String("report", "graphify-out/GRAPH_REPORT.md", "path to GRAPH_REPORT.md")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("serve: unexpected positional arguments: %v", fs.Args())
	}
	g, err := loadGraph(*graphPath)
	if err != nil {
		return fmt.Errorf("serve: %w", err)
	}
	report, err := os.ReadFile(*reportPath)
	if err != nil {
		// Report is best-effort: a missing report should not kill the server,
		// since tools still work against the graph alone.
		fmt.Fprintf(stderr, "gogfy serve: report not loaded: %v\n", err)
		report = []byte("# Graph Report\n\n(report not available)\n")
	}
	srv := serve.New(g, report)
	return srv.Serve(context.Background(), stdin, stdout)
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
	".go":       extract.GoExtractor{},
	".py":       extract.PythonExtractor{},
	".js":       extract.JavaScriptExtractor{},
	".jsx":      extract.JavaScriptExtractor{},
	".mjs":      extract.JavaScriptExtractor{},
	".cjs":      extract.JavaScriptExtractor{},
	".ts":       extract.TypeScriptExtractor{},
	".tsx":      extract.TypeScriptExtractor{TSX: true},
	".java":     extract.JavaExtractor{},
	".c":        extract.CExtractor{},
	".h":        extract.CExtractor{},
	".cpp":      extract.CppExtractor{},
	".cc":       extract.CppExtractor{},
	".cxx":      extract.CppExtractor{},
	".hpp":      extract.CppExtractor{},
	".hxx":      extract.CppExtractor{},
	".hh":       extract.CppExtractor{},
	".rs":       extract.RustExtractor{},
	".rb":       extract.RubyExtractor{},
	".yaml":     extract.YAMLExtractor{},
	".yml":      extract.YAMLExtractor{},
	".toml":     extract.TOMLExtractor{},
	".kt":       extract.KotlinExtractor{},
	".kts":      extract.KotlinExtractor{},
	".scala":    extract.ScalaExtractor{},
	".sc":       extract.ScalaExtractor{},
	".php":      extract.PHPExtractor{},
	".lua":      extract.LuaExtractor{},
	".zig":      extract.ZigExtractor{},
	".jl":       extract.JuliaExtractor{},
	".sh":       extract.BashExtractor{},
	".bash":     extract.BashExtractor{},
	".cs":       extract.CSharpExtractor{},
	".hs":       extract.HaskellExtractor{},
	".ml":       extract.OCamlExtractor{},
	".mli":      extract.OCamlExtractor{},
	".svelte":   extract.SvelteExtractor{},
	".f":        extract.FortranExtractor{},
	".f90":      extract.FortranExtractor{},
	".f95":      extract.FortranExtractor{},
	".f03":      extract.FortranExtractor{},
	".f08":      extract.FortranExtractor{},
	".ex":       extract.ElixirExtractor{},
	".exs":      extract.ElixirExtractor{},
	".dart":     extract.DartExtractor{},
	".swift":    extract.SwiftExtractor{},
	".r":        extract.RExtractor{},
	".R":        extract.RExtractor{},
	".erl":      extract.ErlangExtractor{},
	".hrl":      extract.ErlangExtractor{},
	".md":       extract.MarkdownExtractor{},
	".mdx":      extract.MarkdownExtractor{},
	".markdown": extract.MarkdownExtractor{},
	".html":     extract.HTMLExtractor{},
	".htm":      extract.HTMLExtractor{},
	".txt":      extract.TextExtractor{},
	".rst":      extract.RSTExtractor{},
	".docx":     extract.DocxExtractor{},
	".xlsx":     extract.XlsxExtractor{},
	".pdf":      extract.PDFExtractor{},
	".pptx":     extract.PPTXExtractor{},
	".gdoc":     extract.GoogleWorkspaceExtractor{},
	".gsheet":   extract.GoogleWorkspaceExtractor{},
	".gslides":  extract.GoogleWorkspaceExtractor{},
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
	// ClusterOnly skips extraction and re-clusters an existing graph.json
	// from `out`. Useful when iterating on community-detection tuning.
	ClusterOnly bool
	// NoViz skips the graph.html artifact. Pure JSON/report runs are
	// faster and produce smaller artifact sets — useful in CI.
	NoViz bool
	// Wiki emits a Wikipedia-style markdown directory under <out>/wiki/
	// (index.md + one article per community + one per god node). Agent
	// skill prompts can direct the assistant to navigate the wiki
	// instead of grepping raw files.
	Wiki bool
	// Tree emits <out>/tree.html — a D3 collapsible filesystem-tree
	// view of the graph (complement to the force-directed graph.html).
	Tree bool
	// NoDedup skips entity deduplication. Faster but may produce
	// duplicate nodes for the same real-world concept.
	NoDedup bool
	// Semantic, when true, routes document files (.md/.txt/.rst) through
	// an LLM extractor in addition to the AST pass. Requires
	// ANTHROPIC_API_KEY (or another backend's env var when more
	// providers ship). Off by default — gogfy stays free + offline
	// unless the user explicitly opts in to LLM-token spend.
	Semantic bool
	// SemanticBackend names the LLM provider when Semantic is on.
	// Empty defaults to "anthropic". Future: openai, gemini, ollama.
	SemanticBackend string
}

// runClusterOnly reloads <out>/graph.json, re-runs clustering + analyze +
// rendering, and rewrites the artifact set without re-extracting any
// source files. Useful when iterating on community-detection tuning where
// extraction is the expensive step we want to skip.
func runClusterOnly(out string, directed bool, opts runOptions) error {
	g, err := loadGraph(filepath.Join(out, "graph.json"))
	if err != nil {
		return fmt.Errorf("cluster-only: %w", err)
	}
	clustered, err := cluster.NewLeidenClusterer().Cluster(g.Nodes, g.Edges)
	if err != nil {
		return fmt.Errorf("cluster-only: cluster: %w", err)
	}
	reportData := analyze.NewAnalyzer().Analyze(clustered, g.Edges)
	// The graph.json under <out>/ doesn't directly tell us the corpus
	// root — walk up from <out>'s parent (the corpus dir gogfy was
	// invoked against) to find .git/.
	commit, _ := gitmeta.HeadShortSHA(filepath.Dir(out))
	reportBytes, err := report.RenderWithOptions(reportData, report.Options{
		Nodes:         clustered,
		Edges:         g.Edges,
		BuiltAtCommit: commit,
	})
	if err != nil {
		return fmt.Errorf("cluster-only: report: %w", err)
	}
	exportGraph := export.GraphExport{
		Nodes:         clustered,
		Edges:         g.Edges,
		BuiltAtCommit: commit,
	}
	jsonBytes, err := export.ExportJSON(exportGraph)
	if err != nil {
		return fmt.Errorf("cluster-only: export json: %w", err)
	}
	artifacts := []struct {
		name string
		data []byte
	}{
		{"graph.json", jsonBytes},
		{"GRAPH_REPORT.md", reportBytes},
	}
	if !opts.NoViz {
		htmlBytes, err := export.ExportHTML(exportGraph, export.HTMLOptions{Directed: directed})
		if err != nil {
			return fmt.Errorf("cluster-only: export html: %w", err)
		}
		artifacts = append(artifacts, struct {
			name string
			data []byte
		}{"graph.html", htmlBytes})
	}
	if opts.GraphML {
		b, err := export.ExportGraphML(exportGraph)
		if err != nil {
			return fmt.Errorf("cluster-only: export graphml: %w", err)
		}
		artifacts = append(artifacts, struct {
			name string
			data []byte
		}{"graph.graphml", b})
	}
	if opts.Cypher {
		b, err := export.ExportCypher(exportGraph)
		if err != nil {
			return fmt.Errorf("cluster-only: export cypher: %w", err)
		}
		artifacts = append(artifacts, struct {
			name string
			data []byte
		}{"graph.cypher", b})
	}
	for _, a := range artifacts {
		if err := atomicWrite(filepath.Join(out, a.name), a.data); err != nil {
			return fmt.Errorf("cluster-only: write %s: %w", a.name, err)
		}
	}
	if opts.Wiki {
		if _, err := wiki.Generate(clustered, g.Edges, filepath.Join(out, "wiki"), wiki.Options{
			GodNodes: reportData.GodNodes,
		}); err != nil {
			return fmt.Errorf("cluster-only: wiki: %w", err)
		}
	}
	if opts.Tree {
		if err := writeTreeHTML(clustered, out); err != nil {
			return fmt.Errorf("cluster-only: tree: %w", err)
		}
	}
	return nil
}

func runPipeline(root, out string, update, directed bool, opts runOptions) error {
	if opts.ClusterOnly {
		return runClusterOnly(out, directed, opts)
	}
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
		if len(changed) == 0 && artifactsExist(out, opts.NoViz) {
			fmt.Println("No files changed, skipping extraction")
			// --wiki / --tree must still produce their artifacts even
			// when extraction was skipped — turning either on for a
			// subsequent --update run would otherwise silently no-op
			// until something changes. Both regenerate from the
			// existing graph.json.
			if opts.Wiki {
				if err := regenerateWikiFromDisk(out); err != nil {
					return fmt.Errorf("wiki: %w", err)
				}
			}
			if opts.Tree {
				if err := regenerateTreeFromDisk(out); err != nil {
					return fmt.Errorf("tree: %w", err)
				}
			}
			return nil
		}
		files = changed
	}

	builder := graph.NewBuilder()

	// Build the optional LLM client once, before the extract loop —
	// surfaces auth errors up front rather than per-file deep in the
	// loop. nil client = AST-only mode.
	var llmClient llm.Client
	if opts.Semantic {
		client, err := buildLLMClient(opts.SemanticBackend)
		if err != nil {
			return fmt.Errorf("semantic: %w", err)
		}
		llmClient = client
	}
	var semanticJobs []semanticJob
	semCost := struct {
		inputTokens, outputTokens int
		usd                       float64
	}{}

	for _, f := range files {
		ex, ok := supportedExtensions[filepath.Ext(f)]
		if !ok {
			continue
		}
		res, err := ex.Extract(f)
		if err != nil {
			return fmt.Errorf("extract %s: %w", f, err)
		}
		// Tag every node with its source-file classification at the
		// boundary so downstream packages (report, callflow, wiki)
		// don't need to re-derive it from SourceFile.
		ft := schema.ClassifyFile(f)
		for _, n := range res.Nodes {
			if n.FileType == "" {
				n.FileType = ft
			}
			if err := builder.AddNode(n); err != nil {
				return fmt.Errorf("add node: %w", err)
			}
		}
		// Rationale comments (NOTE/HACK/IMPORTANT/etc.) post-pass: read
		// the source bytes once and emit rationale_for edges into the
		// same file's module node. Best-effort — a read failure here
		// shouldn't fail the whole pipeline since the AST extraction
		// just succeeded, but a warning helps users notice when the
		// rationale section of their graph is empty.
		data, rerr := os.ReadFile(f)
		if rerr != nil {
			fmt.Fprintf(os.Stderr, "gogfy: rationale skipped for %s: %v\n", f, rerr)
		} else {
			rNodes, rEdges := rationale.Extract(f, data)
			for _, n := range rNodes {
				if err := builder.AddNode(n); err != nil {
					return fmt.Errorf("add rationale node: %w", err)
				}
			}
			for _, e := range rEdges {
				if err := builder.AddEdge(e); err != nil {
					return fmt.Errorf("add rationale edge: %w", err)
				}
			}
		}
		// Semantic extraction is deferred to a parallel pass below.
		// LLM calls are I/O-bound and dominate runtime; serializing
		// them here would turn a 100-file vault into a ~5-minute
		// wall-clock blocker.
		if llmClient != nil && schema.ClassifyFile(f) == schema.FileTypeDocument && data != nil {
			semanticJobs = append(semanticJobs, semanticJob{path: f, src: data})
		}
		for _, e := range res.Edges {
			if err := builder.AddEdge(e); err != nil {
				return fmt.Errorf("add edge: %w", err)
			}
		}
	}

	if llmClient != nil && len(semanticJobs) > 0 {
		results, err := runSemanticJobs(context.Background(), llmClient, semanticJobs, semanticConcurrency)
		if err != nil {
			return fmt.Errorf("semantic: %w", err)
		}
		// Merge sequentially — builder isn't goroutine-safe.
		for _, r := range results {
			semCost.inputTokens += r.InputTokens
			semCost.outputTokens += r.OutputTokens
			semCost.usd += r.EstimatedUSDCost
			for _, n := range r.Nodes {
				if err := builder.AddNode(n); err != nil {
					return fmt.Errorf("add semantic node: %w", err)
				}
			}
			for _, e := range r.Edges {
				if err := builder.AddEdge(e); err != nil {
					return fmt.Errorf("add semantic edge: %w", err)
				}
			}
		}
	}
	if llmClient != nil && (semCost.inputTokens+semCost.outputTokens) > 0 {
		fmt.Fprintf(os.Stderr, "gogfy: semantic extraction used %d input + %d output tokens (~$%.4f)\n",
			semCost.inputTokens, semCost.outputTokens, semCost.usd)
	}
	g := builder.Build()
	// JS/TS path-alias rewrite: tsconfig.json paths like "@app/*" → "src/*"
	// turn `ts:import:@app/foo` into `ts:import:<rootDir>/src/foo` so
	// downstream resolution / merge sees the resolved local module
	// instead of an external-looking specifier.
	aliasNodes, aliasEdges := g.Nodes(), g.Edges()
	if aliases, aerr := tsalias.Load(root); aerr == nil {
		aliasNodes, aliasEdges = aliases.Apply(aliasNodes, aliasEdges)
	}
	// Resolve `<lang>:call:<name>` synthetic targets into INFERRED edges
	// pointing at real function nodes (or AMBIGUOUS edges fanned out across
	// multiple candidates). Cross-file calls otherwise stay EXTRACTED with
	// the synthetic target preserved.
	nodes, edges := resolve.Calls(aliasNodes, aliasEdges)

	// Entity deduplication (three-pass: exact → fuzzy → LLM tiebreaker)
	if !opts.NoDedup {
		deduper := dedup.NewDeduplicator()
		// Build community map for pass 2 community boost
		commMap := make(map[string]string, len(nodes))
		for _, n := range nodes {
			commMap[n.ID] = n.Community
		}
		nodes, edges, _, err = deduper.Deduplicate(nodes, edges, commMap)
		if err != nil {
			return fmt.Errorf("dedup: %w", err)
		}
	}

	clusterer := cluster.NewLeidenClusterer()
	clusteredNodes, err := clusterer.Cluster(nodes, edges)
	if err != nil {
		return fmt.Errorf("cluster: %w", err)
	}

	analyzer := analyze.NewAnalyzer()
	reportData := analyzer.Analyze(clusteredNodes, edges)

	// Stamp the report with the corpus's HEAD SHA so a stale artifact
	// is visibly out-of-date next to a fresh repo. Missing-data is
	// non-fatal — the report just omits the freshness section.
	commit, _ := gitmeta.HeadShortSHA(root)
	var semCostReport *report.SemanticCost
	if llmClient != nil && len(semanticJobs) > 0 {
		semCostReport = &report.SemanticCost{
			Backend:          llmClient.Name(),
			FilesProcessed:   len(semanticJobs),
			InputTokens:      semCost.inputTokens,
			OutputTokens:     semCost.outputTokens,
			EstimatedUSDCost: semCost.usd,
		}
	}
	reportBytes, err := report.RenderWithOptions(reportData, report.Options{
		Nodes:         clusteredNodes,
		Edges:         edges,
		BuiltAtCommit: commit,
		SemanticCost:  semCostReport,
	})
	if err != nil {
		return fmt.Errorf("report: %w", err)
	}

	exportGraph := export.GraphExport{
		Nodes:         clusteredNodes,
		Edges:         edges,
		BuiltAtCommit: commit,
	}

	jsonBytes, err := export.ExportJSON(exportGraph)
	if err != nil {
		return fmt.Errorf("export json: %w", err)
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
	}
	if !opts.NoViz {
		htmlBytes, err := export.ExportHTML(exportGraph, export.HTMLOptions{Directed: directed})
		if err != nil {
			return fmt.Errorf("export html: %w", err)
		}
		artifacts = append(artifacts, struct {
			name string
			data []byte
		}{"graph.html", htmlBytes})
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

	if opts.Wiki {
		if _, err := wiki.Generate(clusteredNodes, edges, filepath.Join(out, "wiki"), wiki.Options{
			GodNodes: reportData.GodNodes,
		}); err != nil {
			return fmt.Errorf("wiki: %w", err)
		}
	}
	if opts.Tree {
		if err := writeTreeHTML(clusteredNodes, out); err != nil {
			return fmt.Errorf("tree: %w", err)
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
	commit, _ := gitmeta.HeadShortSHA(filepath.Dir(path))
	out, err := report.RenderWithOptions(r, report.Options{
		Nodes:         g.Nodes,
		Edges:         g.Edges,
		BuiltAtCommit: commit,
	})
	if err != nil {
		return fmt.Errorf("report: %w", err)
	}
	_, err = w.Write(out)
	return err
}

// writeTreeHTML builds and atomically writes <out>/tree.html.
func writeTreeHTML(nodes []schema.Node, outDir string) error {
	root := tree.Build(nodes, tree.Options{})
	html, err := tree.HTML(root, tree.HTMLOptions{})
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(outDir, "tree.html"), []byte(html))
}

// regenerateTreeFromDisk rebuilds <out>/tree.html from <out>/graph.json
// without re-extracting source. Used by the --update no-op path so a
// freshly-added --tree flag still produces output on unchanged repos.
func regenerateTreeFromDisk(out string) error {
	g, err := loadGraph(filepath.Join(out, "graph.json"))
	if err != nil {
		return err
	}
	return writeTreeHTML(g.Nodes, out)
}

// regenerateWikiFromDisk rebuilds <out>/wiki/ from <out>/graph.json
// without re-extracting source. Used by the --update no-op path so a
// freshly-added --wiki flag still produces output on unchanged repos.
func regenerateWikiFromDisk(out string) error {
	g, err := loadGraph(filepath.Join(out, "graph.json"))
	if err != nil {
		return err
	}
	r := analyze.NewAnalyzer().Analyze(g.Nodes, g.Edges)
	_, err = wiki.Generate(g.Nodes, g.Edges, filepath.Join(out, "wiki"), wiki.Options{GodNodes: r.GodNodes})
	return err
}

// treeCommand renders a tree.html view from an existing graph.json.
// Usage: gogfy tree <graph.json> [--out <path>]
func treeCommand(args []string, stderr io.Writer) error {
	ordered, err := groupWikiFlags(args) // same shape as wiki: one positional + --out
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("tree", flag.ContinueOnError)
	fs.SetOutput(stderr)
	outPath := fs.String("out", "", "output HTML path (defaults to <graph-dir>/tree.html)")
	if err := fs.Parse(ordered); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("tree: expected <graph.json>, got %d positional argument(s)", fs.NArg())
	}
	graphPath := fs.Arg(0)
	g, err := loadGraph(graphPath)
	if err != nil {
		return err
	}
	dst := *outPath
	if dst == "" {
		dst = filepath.Join(filepath.Dir(graphPath), "tree.html")
	}
	root := tree.Build(g.Nodes, tree.Options{})
	html, err := tree.HTML(root, tree.HTMLOptions{})
	if err != nil {
		return fmt.Errorf("tree: %w", err)
	}
	if err := atomicWrite(dst, []byte(html)); err != nil {
		return fmt.Errorf("tree: %w", err)
	}
	fmt.Fprintf(stderr, "tree: wrote %s\n", dst)
	return nil
}

// globalVerbs is used by both the missing-verb and unknown-verb error
// messages so the two listings never drift.
const globalVerbs = "add, remove, list, path"

// parseGlobalFlags handles the boilerplate shared by every `global`
// verb: reorder flags so positional args can precede them, register
// --dir, and return the parsed FlagSet plus a Store rooted at --dir.
// extraFlags registers verb-specific flags BEFORE parsing.
func parseGlobalFlags(name string, args []string, stderr io.Writer, valueFlags []string, extraFlags func(*flag.FlagSet)) (*flag.FlagSet, *globalgraph.Store, error) {
	ordered, err := reorderFlags(args, append([]string{"dir"}, valueFlags...), nil)
	if err != nil {
		return nil, nil, err
	}
	fs := flag.NewFlagSet("global "+name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("dir", "", "store directory (defaults to ~/.gogfy)")
	if extraFlags != nil {
		extraFlags(fs)
	}
	if err := fs.Parse(ordered); err != nil {
		return nil, nil, err
	}
	store, err := globalgraph.NewStore(*dir)
	if err != nil {
		return nil, nil, err
	}
	return fs, store, nil
}

// globalCommand dispatches the four `global` verbs against a
// directory-backed cross-repo graph store. Each verb accepts --dir
// to override the default ~/.gogfy location for test isolation.
func globalCommand(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("global: missing verb (one of: %s)", globalVerbs)
	}
	verb, rest := args[0], args[1:]
	switch verb {
	case "add":
		var asTag *string
		fs, s, err := parseGlobalFlags("add", rest, stderr, []string{"as"}, func(fs *flag.FlagSet) {
			asTag = fs.String("as", "", "repo tag (defaults to source-dir basename)")
		})
		if err != nil {
			return err
		}
		if fs.NArg() != 1 {
			return fmt.Errorf("global add: expected <graph.json>, got %d positional argument(s)", fs.NArg())
		}
		src := fs.Arg(0)
		tag := *asTag
		if tag == "" {
			tag = defaultRepoTag(src)
		}
		res, err := s.Add(src, tag)
		if err != nil {
			return fmt.Errorf("global add: %w", err)
		}
		if res.Skipped {
			fmt.Fprintf(stdout, "global: %s unchanged (skipped)\n", res.RepoTag)
		} else {
			fmt.Fprintf(stdout, "global: %s — added %d nodes, removed %d stale nodes\n",
				res.RepoTag, res.NodesAdded, res.NodesRemoved)
		}
		return nil

	case "remove":
		fs, s, err := parseGlobalFlags("remove", rest, stderr, nil, nil)
		if err != nil {
			return err
		}
		if fs.NArg() != 1 {
			return fmt.Errorf("global remove: expected <TAG>, got %d positional argument(s)", fs.NArg())
		}
		n, err := s.Remove(fs.Arg(0))
		if err != nil {
			return fmt.Errorf("global remove: %w", err)
		}
		fmt.Fprintf(stdout, "global: removed %s (%d nodes)\n", fs.Arg(0), n)
		return nil

	case "list":
		fs, s, err := parseGlobalFlags("list", rest, stderr, nil, nil)
		if err != nil {
			return err
		}
		if fs.NArg() > 0 {
			return fmt.Errorf("global list: unexpected positional argument(s): %v", fs.Args())
		}
		repos, err := s.List()
		if err != nil {
			return fmt.Errorf("global list: %w", err)
		}
		if len(repos) == 0 {
			fmt.Fprintln(stdout, "global: no repos added yet")
			return nil
		}
		for _, t := range slices.Sorted(maps.Keys(repos)) {
			e := repos[t]
			fmt.Fprintf(stdout, "%s\t%d nodes, %d edges\t%s\t%s\n",
				t, e.NodeCount, e.EdgeCount, e.AddedAt, e.SourcePath)
		}
		return nil

	case "path":
		fs, s, err := parseGlobalFlags("path", rest, stderr, nil, nil)
		if err != nil {
			return err
		}
		if fs.NArg() > 0 {
			return fmt.Errorf("global path: unexpected positional argument(s): %v", fs.Args())
		}
		fmt.Fprintln(stdout, s.Path())
		return nil

	default:
		return fmt.Errorf("global: unknown verb %q (want one of: %s)", verb, globalVerbs)
	}
}

// defaultRepoTag derives a repo tag from a graph.json path when the
// caller didn't supply --as. Graph paths are typically shaped like
// <project>/graphify-out/graph.json, so the parent's parent gives the
// project name; falls back to immediate parent if that's missing.
// Returns "" when no usable tag can be derived (Add's validateTag
// will surface the error with an actionable "pass --as" message).
func defaultRepoTag(src string) string {
	abs, err := filepath.Abs(src)
	if err != nil {
		return ""
	}
	bad := func(t string) bool {
		return t == "" || t == "." || t == ".." || t == "/" || t == "\\"
	}
	parent := filepath.Dir(abs)
	if tag := filepath.Base(filepath.Dir(parent)); !bad(tag) {
		return tag
	}
	if tag := filepath.Base(parent); !bad(tag) {
		return tag
	}
	return ""
}

// callflowCommand renders the call-flow architecture HTML from an
// existing graph.json.
func callflowCommand(args []string, stderr io.Writer) error {
	ordered, err := groupCallflowFlags(args)
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("callflow", flag.ContinueOnError)
	fs.SetOutput(stderr)
	outPath := fs.String("out", "", "output HTML path (defaults to <graph-dir>/callflow.html)")
	maxSections := fs.Int("max-sections", 0, "cap number of sections (default 15)")
	maxNodes := fs.Int("max-nodes", 0, "cap per-section diagram nodes (default 18)")
	maxEdges := fs.Int("max-edges", 0, "cap per-section diagram edges (default 24)")
	project := fs.String("project", "", "project name used in title (default 'Project')")
	if err := fs.Parse(ordered); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("callflow: expected <graph.json>, got %d positional argument(s)", fs.NArg())
	}
	graphPath := fs.Arg(0)
	g, err := loadGraph(graphPath)
	if err != nil {
		return err
	}
	html, err := callflow.Generate(g.Nodes, g.Edges, callflow.Options{
		MaxSections:        *maxSections,
		MaxNodesPerSection: *maxNodes,
		MaxEdgesPerSection: *maxEdges,
		ProjectName:        *project,
	})
	if err != nil {
		return fmt.Errorf("callflow: %w", err)
	}
	dst := *outPath
	if dst == "" {
		dst = filepath.Join(filepath.Dir(graphPath), "callflow.html")
	}
	if err := atomicWrite(dst, []byte(html)); err != nil {
		return fmt.Errorf("callflow: %w", err)
	}
	fmt.Fprintf(stderr, "callflow: wrote %s\n", dst)
	return nil
}

// benchmarkCommand measures token-reduction against an existing graph.json.
// Usage: gogfy benchmark <graph.json> [--corpus-words N] [--depth D] [--json]
func benchmarkCommand(args []string, stdout, stderr io.Writer) error {
	ordered, err := groupBenchmarkFlags(args)
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("benchmark", flag.ContinueOnError)
	fs.SetOutput(stderr)
	corpusWords := fs.Int("corpus-words", 0, "authoritative source-corpus word count (0 = estimate from node count)")
	depth := fs.Int("depth", 0, "BFS depth from question seeds (0 = default 3)")
	asJSON := fs.Bool("json", false, "emit the raw Result as JSON instead of the human-readable report")
	if err := fs.Parse(ordered); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("benchmark: expected <graph.json>, got %d positional argument(s)", fs.NArg())
	}
	g, err := loadGraph(fs.Arg(0))
	if err != nil {
		return err
	}
	res, err := benchmark.Run(g.Nodes, g.Edges, benchmark.Options{
		CorpusWords: *corpusWords,
		Depth:       *depth,
	})
	if err != nil {
		return fmt.Errorf("benchmark: %w", err)
	}
	if *asJSON {
		out, err := json.MarshalIndent(res, "", "  ")
		if err != nil {
			return fmt.Errorf("benchmark: marshal: %w", err)
		}
		if _, err := fmt.Fprintln(stdout, string(out)); err != nil {
			return fmt.Errorf("benchmark: write json: %w", err)
		}
		return nil
	}
	return benchmark.Render(res, stdout)
}

// wikiCommand turns an existing graph.json into a wiki directory.
// Usage: gogfy wiki <graph.json> [--out <dir>]
// Default output is <graph-dir>/wiki/.
func wikiCommand(args []string, stderr io.Writer) error {
	// Reorder so the positional <graph.json> can appear before --out
	// per the documented `gogfy wiki <graph.json> [--out <dir>]` form.
	// Without this, flag.Parse stops at the first non-flag arg and
	// silently ignores --out, writing to the default location.
	ordered, err := groupWikiFlags(args)
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("wiki", flag.ContinueOnError)
	fs.SetOutput(stderr)
	outDir := fs.String("out", "", "wiki output directory (defaults to <graph-dir>/wiki/)")
	if err := fs.Parse(ordered); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("wiki: expected <graph.json>, got %d positional argument(s)", fs.NArg())
	}
	graphPath := fs.Arg(0)
	g, err := loadGraph(graphPath)
	if err != nil {
		return err
	}
	dir := *outDir
	if dir == "" {
		dir = filepath.Join(filepath.Dir(graphPath), "wiki")
	}
	r := analyze.NewAnalyzer().Analyze(g.Nodes, g.Edges)
	// Missing labels file is non-fatal: wiki falls back to "Community <N>".
	labelsPath := filepath.Join(filepath.Dir(graphPath), labels.DefaultFilename)
	communityLabels, err := labels.Load(labelsPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	count, err := wiki.Generate(g.Nodes, g.Edges, dir, wiki.Options{
		GodNodes:        r.GodNodes,
		CommunityLabels: communityLabels,
	})
	if err != nil {
		return fmt.Errorf("wiki: %w", err)
	}
	fmt.Fprintf(stderr, "wiki: wrote %d articles + index.md to %s\n", count, dir)
	return nil
}

func labelsCommand(args []string, stderr io.Writer) error {
	ordered, err := reorderFlags(args, []string{"out"}, []string{"force"})
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("labels", flag.ContinueOnError)
	fs.SetOutput(stderr)
	outPath := fs.String("out", "", labels.DefaultFilename+" output path (defaults to <graph-dir>/"+labels.DefaultFilename+")")
	force := fs.Bool("force", false, "overwrite an existing labels file (default: refuse so hand-edits survive)")
	if err := fs.Parse(ordered); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("labels: expected <graph.json>, got %d positional argument(s)", fs.NArg())
	}
	graphPath := fs.Arg(0)
	g, err := loadGraph(graphPath)
	if err != nil {
		return err
	}
	path := *outPath
	if path == "" {
		path = filepath.Join(filepath.Dir(graphPath), labels.DefaultFilename)
	}
	if !*force {
		if _, statErr := os.Stat(path); statErr == nil {
			return fmt.Errorf("labels: %s already exists; pass --force to overwrite", path)
		}
	}
	out := labels.Generate(g.Nodes, g.Edges)
	if err := labels.Save(path, out); err != nil {
		return fmt.Errorf("labels: %w", err)
	}
	fmt.Fprintf(stderr, "labels: wrote %d community labels to %s\n", len(out), path)
	return nil
}

// groupWikiFlags reorders `gogfy wiki <graph.json> [--out <dir>]` args
// so the positional graph path can appear before --out without losing
// the flag to Go's stop-at-first-positional parser.
// obsidianCommand turns a clustered graph.json into an Obsidian vault.
// Auto-loads `.graphify_labels.json` adjacent to the graph so community
// notes use the same names as the wiki.
func obsidianCommand(args []string, stderr io.Writer) error {
	ordered, err := reorderFlags(args, []string{"out"}, nil)
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("obsidian", flag.ContinueOnError)
	fs.SetOutput(stderr)
	outDir := fs.String("out", "", "vault output directory (defaults to <graph-dir>/obsidian/)")
	if err := fs.Parse(ordered); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("obsidian: expected <graph.json>, got %d positional argument(s)", fs.NArg())
	}
	graphPath := fs.Arg(0)
	g, err := loadGraph(graphPath)
	if err != nil {
		return err
	}
	dir := *outDir
	if dir == "" {
		dir = filepath.Join(filepath.Dir(graphPath), "obsidian")
	}
	// Missing labels file is non-fatal: vault falls back to "Community N".
	communityLabels, err := labels.Load(filepath.Join(filepath.Dir(graphPath), labels.DefaultFilename))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	opts := obsidian.Options{
		OutDir:          dir,
		CommunityLabels: communityLabels,
	}
	count, err := obsidian.Generate(g.Nodes, g.Edges, opts)
	if err != nil {
		return fmt.Errorf("obsidian: %w", err)
	}
	// Companion .canvas file: opens in Obsidian as an infinite canvas
	// with community-colored groups. Written alongside the vault so a
	// single `obsidian` invocation produces both artifacts.
	canvas, err := obsidian.Canvas(g.Nodes, g.Edges, opts)
	if err != nil {
		return fmt.Errorf("obsidian canvas: %w", err)
	}
	canvasPath := filepath.Join(dir, "graph.canvas")
	if err := fsutil.WriteFileAtomic(canvasPath, canvas, 0644); err != nil {
		return fmt.Errorf("obsidian canvas write: %w", err)
	}
	fmt.Fprintf(stderr, "obsidian: wrote %d notes + graph.canvas to %s\n", count, dir)
	return nil
}

func groupWikiFlags(args []string) ([]string, error) {
	return reorderFlags(args, []string{"out"}, nil)
}

// reorderFlags moves any --name / --name=val / --name val pairs ahead of
// positional args so flag.Parse (which stops at the first non-flag) sees
// every flag the user supplied. valueFlags carries arguments; boolFlags
// stand alone. Unknown flags return an error rather than silently
// becoming positional args.
func reorderFlags(args, valueFlags, boolFlags []string) ([]string, error) {
	isValue := make(map[string]bool, len(valueFlags))
	for _, n := range valueFlags {
		isValue[n] = true
	}
	isBool := make(map[string]bool, len(boolFlags))
	for _, n := range boolFlags {
		isBool[n] = true
	}
	flagName := func(a string) (string, bool) {
		s := strings.TrimLeft(a, "-")
		if eq := strings.IndexByte(s, '='); eq >= 0 {
			s = s[:eq]
		}
		return s, isValue[s] || isBool[s]
	}

	var flags, positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			positional = append(positional, a)
			continue
		}
		name, known := flagName(a)
		if !known {
			return nil, fmt.Errorf("unknown flag: %s", a)
		}
		if strings.Contains(a, "=") || isBool[name] {
			flags = append(flags, a)
			continue
		}
		if i+1 >= len(args) {
			return nil, fmt.Errorf("flag %s requires a value", a)
		}
		// Reject a value that's itself a recognized flag — almost
		// certainly a typo (e.g. `--depth --json` would set
		// depth="--json" and surface as a cryptic int-parse error
		// later).
		if v := args[i+1]; strings.HasPrefix(v, "-") {
			if vn, vknown := flagName(v); vknown && (isValue[vn] || isBool[vn]) {
				return nil, fmt.Errorf("flag %s requires a value, got flag %q", a, v)
			}
		}
		flags = append(flags, a, args[i+1])
		i++
	}
	return append(flags, positional...), nil
}

// groupBenchmarkFlags reorders args for the benchmark subcommand
// (--corpus-words/--depth/--json).
func groupBenchmarkFlags(args []string) ([]string, error) {
	return reorderFlags(args, []string{"corpus-words", "depth"}, []string{"json"})
}

func groupCallflowFlags(args []string) ([]string, error) {
	return reorderFlags(args, []string{"out", "max-sections", "max-nodes", "max-edges", "project"}, nil)
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
			a == "--graphml", a == "-graphml", a == "--cypher", a == "-cypher",
			a == "--cluster-only", a == "-cluster-only", a == "--no-viz", a == "-no-viz",
			a == "--wiki", a == "-wiki",
			a == "--tree", a == "-tree":
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
			strings.HasPrefix(a, "--cypher="), strings.HasPrefix(a, "-cypher="),
			strings.HasPrefix(a, "--cluster-only="), strings.HasPrefix(a, "-cluster-only="),
			strings.HasPrefix(a, "--no-viz="), strings.HasPrefix(a, "-no-viz="),
			strings.HasPrefix(a, "--wiki="), strings.HasPrefix(a, "-wiki="),
			strings.HasPrefix(a, "--tree="), strings.HasPrefix(a, "-tree="):
			flags = append(flags, a)
		case strings.HasPrefix(a, "-"):
			return nil, fmt.Errorf("unknown flag: %s", a)
		default:
			positional = append(positional, a)
		}
	}
	return append(flags, positional...), nil
}

// artifactsExist reports whether the standard output set is already on
// disk. With noViz=true, graph.html is excluded from the check (it
// wasn't written and should not be required for no-op detection).
func artifactsExist(out string, noViz bool) bool {
	required := []string{"graph.json", "GRAPH_REPORT.md"}
	if !noViz {
		required = append(required, "graph.html")
	}
	for _, name := range required {
		if _, err := os.Stat(filepath.Join(out, name)); err != nil {
			return false
		}
	}
	return true
}

func loadGraph(path string) (export.GraphExport, error) {
	return export.LoadJSON(path)
}

// atomicWrite is a thin wrapper kept for callers that don't import fsutil
// directly; new code should use fsutil.WriteFileAtomic.
func atomicWrite(path string, data []byte) error {
	return fsutil.WriteFileAtomic(path, data, 0644)
}
