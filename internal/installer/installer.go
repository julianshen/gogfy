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
// stores its MCP config as JSON with a top-level `mcpServers` object. The
// only platform-specific bit is the relative path inside the workspace.
type jsonInstaller struct {
	relativePath string // path relative to the workspace root, e.g. ".mcp.json"
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
	servers := ensureServersMap(cfg)
	servers["gogfy"] = gogfyServerEntry(workspace)
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
	servers, _ := cfg["mcpServers"].(map[string]any)
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

// ensureServersMap returns cfg["mcpServers"] as a map[string]any, creating
// it if missing. The returned map is the same one stored in cfg, so callers
// can mutate it directly.
func ensureServersMap(cfg map[string]any) map[string]any {
	if existing, ok := cfg["mcpServers"].(map[string]any); ok {
		return existing
	}
	m := map[string]any{}
	cfg["mcpServers"] = m
	return m
}

// gogfyServerEntry is the canonical mcpServers.gogfy value shape: command +
// args wired to the workspace's graph artifacts.
func gogfyServerEntry(workspace string) map[string]any {
	graph := filepath.Join(workspace, "graphify-out", "graph.json")
	report := filepath.Join(workspace, "graphify-out", "GRAPH_REPORT.md")
	return map[string]any{
		"command": "gogfy",
		"args":    []any{"serve", "--graph", graph, "--report", report},
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
var registry = map[string]Installer{
	"claude": jsonInstaller{relativePath: ".mcp.json"},
	"cursor": jsonInstaller{relativePath: filepath.Join(".cursor", "mcp.json")},
	"vscode": jsonInstaller{relativePath: filepath.Join(".vscode", "mcp.json")},
	"gemini": jsonInstaller{relativePath: filepath.Join(".gemini", "settings.json")},
}
