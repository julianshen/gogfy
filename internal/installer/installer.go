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
)

// Installer writes/removes the gogfy MCP entry in one platform's config.
type Installer interface {
	// ConfigPath returns the absolute path the installer writes to, given
	// the workspace root. Pure: never touches disk.
	ConfigPath(workspace string) string
	// Install adds (or refreshes) the gogfy server entry, preserving any
	// other entries and top-level keys already in the file.
	Install(workspace string) error
	// Uninstall removes only the gogfy server entry. No-op when the file
	// does not exist; preserves other servers and top-level keys.
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

func (j jsonInstaller) Install(workspace string) error {
	path := j.ConfigPath(workspace)
	cfg, err := readOrEmpty(path)
	if err != nil {
		return err
	}
	servers := ensureServersMap(cfg, j.serversKey)
	servers["gogfy"] = gogfyServerEntry()
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
	servers, _ := cfg[j.serversKey].(map[string]any)
	if servers == nil {
		// Nothing to remove; leave the file alone.
		return nil
	}
	delete(servers, "gogfy")
	return writeJSON(path, cfg)
}

// readOrEmpty parses the JSON config at path, or returns an empty map if the
// file does not exist. Any other error (parse failure, IO error) propagates.
func readOrEmpty(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]any{}, nil
	}
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
// missing. The returned map is the same one stored in cfg, so callers can
// mutate it directly.
func ensureServersMap(cfg map[string]any, key string) map[string]any {
	if existing, ok := cfg[key].(map[string]any); ok {
		return existing
	}
	m := map[string]any{}
	cfg[key] = m
	return m
}

// gogfyServerEntry is the canonical gogfy server value shape: command + args
// wired to the *workspace-relative* graph artifacts. Relative paths survive
// workspace moves and git checkouts on different machines; every MCP-capable
// agent we support launches the server with the workspace as cwd.
func gogfyServerEntry() map[string]any {
	graph := filepath.Join("graphify-out", "graph.json")
	report := filepath.Join("graphify-out", "GRAPH_REPORT.md")
	return map[string]any{
		"command": "gogfy",
		"args":    []string{"serve", "--graph", graph, "--report", report},
	}
}

// writeJSON marshals cfg to indented JSON and writes it atomically (.tmp +
// rename), creating parent directories as needed.
func writeJSON(path string, cfg map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
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
}
