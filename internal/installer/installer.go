// Package installer writes per-agent-platform MCP configuration so coding
// assistants can launch `gogfy serve` and use the gogfy graph as a tool.
//
// Supported platforms (all JSON-config, MCP-capable):
//
//   - claude → <workspace>/.mcp.json
//   - cursor → <workspace>/.cursor/mcp.json
//   - vscode → <workspace>/.vscode/mcp.json
//   - gemini → <workspace>/.gemini/settings.json
//
// All four use the same shape: a top-level `mcpServers` object keyed by
// server name. Installs merge into existing configs without disturbing
// unrelated entries; uninstalls remove only the gogfy entry.
package installer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/julianshen/gogfy/internal/fsutil"
)

// Options tunes the gogfy server entry the installer writes. Zero-value is
// graphify-parity defaults: Bin="gogfy" (resolved via $PATH at agent
// launch), OutDir="graphify-out".
type Options struct {
	// Bin is the executable name or path the agent should launch. Defaults
	// to "gogfy" so it matches the upstream graphify CLI shape; users can
	// override with an absolute path when the binary isn't on $PATH.
	Bin string
	// OutDir is the workspace-relative directory containing graph.json and
	// GRAPH_REPORT.md. Must match what `gogfy run --out` produced.
	OutDir string
}

func (o Options) bin() string {
	if o.Bin == "" {
		return "gogfy"
	}
	return o.Bin
}

func (o Options) outDir() string {
	if o.OutDir == "" {
		return "graphify-out"
	}
	return o.OutDir
}

// Installer writes/removes the gogfy MCP entry in one platform's config.
type Installer interface {
	// ConfigPath returns the absolute path the installer writes to, given
	// the workspace root. Pure: never touches disk.
	ConfigPath(workspace string) string
	// Install adds (or refreshes) the gogfy server entry, preserving any
	// other entries and top-level keys already in the file.
	Install(workspace string, opts Options) error
	// Uninstall removes only the gogfy server entry. No-op when the file
	// does not exist or has no gogfy entry; preserves other servers and
	// top-level keys.
	Uninstall(workspace string) error
}

// For returns the installer for the named platform. Use SupportedPlatforms
// to enumerate the valid names.
func For(name string) (Installer, error) {
	if inst, ok := registry[name]; ok {
		return inst, nil
	}
	return nil, fmt.Errorf("unknown platform %q (supported: %v)", name, SupportedPlatforms())
}

// SupportedPlatforms lists known platform names sorted alphabetically.
func SupportedPlatforms() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// jsonInstaller implements the install/uninstall flow for any platform that
// stores its MCP config as JSON keyed by a top-level servers object. The
// platform-specific bits are: (1) the path inside the workspace, and
// (2) the name of the top-level key — most use `mcpServers` but VS Code's
// native MCP config uses `servers`.
type jsonInstaller struct {
	relativePath string // path relative to the workspace root, e.g. ".mcp.json"
	serversKey   string // top-level key, e.g. "mcpServers" or "servers"
}

func (j jsonInstaller) ConfigPath(workspace string) string {
	return filepath.Join(workspace, j.relativePath)
}

func (j jsonInstaller) Install(workspace string, opts Options) error {
	path := j.ConfigPath(workspace)
	cfg, err := readOrEmpty(path)
	if err != nil {
		return err
	}
	servers, err := ensureServersMap(cfg, j.serversKey)
	if err != nil {
		return err
	}
	servers["gogfy"] = gogfyServerEntry(opts)
	return writeJSON(path, cfg)
}

func (j jsonInstaller) Uninstall(workspace string) error {
	path := j.ConfigPath(workspace)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}
	cfg, err := readOrEmpty(path)
	if err != nil {
		return err
	}
	raw, present := cfg[j.serversKey]
	if !present {
		// No servers section at all — nothing to remove, skip the rewrite.
		return nil
	}
	servers, ok := raw.(map[string]any)
	if !ok {
		return fmt.Errorf("uninstall: %s exists in %s but is not a JSON object (%T) — refusing to clobber unknown data", j.serversKey, path, raw)
	}
	if _, has := servers["gogfy"]; !has {
		// Nothing to remove; skip rewrite to preserve mtime/whitespace.
		return nil
	}
	delete(servers, "gogfy")
	return writeJSON(path, cfg)
}

// readOrEmpty parses the JSON config at path, or returns an empty map if the
// file does not exist or is empty. Any other error (parse failure, IO error)
// propagates.
func readOrEmpty(path string) (map[string]any, error) {
	data, err := fsutil.ReadFileOrEmpty(path)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return map[string]any{}, nil
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if cfg == nil {
		cfg = map[string]any{}
	}
	return cfg, nil
}

// ensureServersMap returns cfg[key] as a map[string]any, creating it if
// missing. Returns an error if cfg[key] exists but isn't a JSON object —
// silently overwriting unknown data (e.g. a misconfigured string) would
// destroy user state.
func ensureServersMap(cfg map[string]any, key string) (map[string]any, error) {
	raw, present := cfg[key]
	if !present {
		m := map[string]any{}
		cfg[key] = m
		return m, nil
	}
	if existing, ok := raw.(map[string]any); ok {
		return existing, nil
	}
	return nil, fmt.Errorf("install: %s already exists but is not a JSON object (%T) — refusing to clobber unknown data", key, raw)
}

// gogfyServerEntry is the canonical gogfy server value shape: command + args
// wired to the *workspace-relative* graph artifacts. Relative paths survive
// workspace moves and git checkouts on different machines; every MCP-capable
// agent we support launches the server with the workspace as cwd.
func gogfyServerEntry(opts Options) map[string]any {
	graph := filepath.Join(opts.outDir(), "graph.json")
	report := filepath.Join(opts.outDir(), "GRAPH_REPORT.md")
	return map[string]any{
		"command": opts.bin(),
		"args":    []string{"serve", "--graph", graph, "--report", report},
	}
}

// writeJSON marshals cfg to indented JSON and writes it via fsutil.WriteFileAtomic.
func writeJSON(path string, cfg map[string]any) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(path, data, 0644)
}

// registry maps platform name → installer. Adding a new JSON-config
// platform is one entry here.
//
// VS Code's native MCP config (`.vscode/mcp.json`) keys servers under
// "servers" rather than "mcpServers" — match each platform's actual
// expected schema rather than a single conventional shape.
var registry = map[string]Installer{
	"claude": jsonInstaller{relativePath: ".mcp.json", serversKey: "mcpServers"},
	"cursor": jsonInstaller{relativePath: filepath.Join(".cursor", "mcp.json"), serversKey: "mcpServers"},
	"vscode": jsonInstaller{relativePath: filepath.Join(".vscode", "mcp.json"), serversKey: "servers"},
	"gemini": jsonInstaller{relativePath: filepath.Join(".gemini", "settings.json"), serversKey: "mcpServers"},
	"codex":  codexInstaller{relativePath: filepath.Join(".codex", "config.toml")},
}
