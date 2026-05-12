// Package installer writes per-agent-platform MCP configuration so coding
// assistants can launch `gogfy serve` and use the gogfy graph as a tool.
//
// The registry at the bottom of this file is the authoritative platform
// table; the package supports 19 platforms covering Claude Code, Codex,
// Cursor, VS Code, Gemini CLI, OpenCode, and the broader graphify-aligned
// set (Aider, OpenClaw, Copilot, Droid, Trae, Hermes, Kiro, Pi, Antigravity,
// Kimi, Qwen, Kilo Code).
//
// Platform-name + config-path conventions match safishamsi/graphify's
// per-platform install paths (see graphify/__main__.py:_PLATFORM_CONFIG).
// graphify installs a SKILL.md under each platform's user-home config dir;
// gogfy registers an MCP server in each platform's workspace-relative
// config — different architectures, same naming convention.
//
// Best-effort note: some platforms in the registry don't yet have official
// workspace-relative MCP config support documented (e.g., Aider primarily
// configures via .aider.conf.yml; GitHub Copilot CLI uses user-level
// config). The install writes the file at the conventional location each
// platform's config-dir suggests; consult the upstream platform's docs
// to confirm whether your version reads it. Future PRs can refine the
// paths as conventions stabilize.
//
// Installs merge into existing configs without disturbing unrelated
// entries; uninstalls remove only the gogfy entry.
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
// platform-specific bits are: (1) the path inside the workspace,
// (2) the name of the top-level key — most use `mcpServers` but VS Code's
// native MCP config uses `servers` and OpenCode uses `mcp`, and (3) the
// shape of each server entry — standard is split `{command, args}` but
// OpenCode flattens to `{type: "local", command: [...]}`.
type jsonInstaller struct {
	relativePath string                       // path relative to the workspace root, e.g. ".mcp.json"
	serversKey   string                       // top-level key, e.g. "mcpServers" or "servers"
	entry        func(Options) map[string]any // server-entry builder; nil = the standard {command,args} shape
}

func (j jsonInstaller) entryFor(opts Options) map[string]any {
	if j.entry != nil {
		return j.entry(opts)
	}
	return gogfyServerEntry(opts)
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
	servers["gogfy"] = j.entryFor(opts)
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

// gogfyServeArgs returns the canonical argv tail for the gogfy serve
// command, pointing at workspace-relative graph artifacts. Relative paths
// survive workspace moves and git checkouts on different machines; every
// MCP-capable agent we support launches the server with the workspace as
// cwd. Centralized so a future flag addition can't drift between the
// standard {command, args} shape and OpenCode's flattened [command, ...]
// shape.
func gogfyServeArgs(opts Options) []string {
	graph := filepath.Join(opts.outDir(), "graph.json")
	report := filepath.Join(opts.outDir(), "GRAPH_REPORT.md")
	return []string{"serve", "--graph", graph, "--report", report}
}

// gogfyServerEntry is the canonical {command, args[]} shape that
// mcpServers-style configs expect (Claude/Cursor/Gemini/etc.).
func gogfyServerEntry(opts Options) map[string]any {
	return map[string]any{
		"command": opts.bin(),
		"args":    gogfyServeArgs(opts),
	}
}

// writeJSON marshals cfg to indented JSON and writes it atomically.
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
	"claude":      jsonInstaller{relativePath: ".mcp.json", serversKey: "mcpServers"},
	"cursor":      jsonInstaller{relativePath: filepath.Join(".cursor", "mcp.json"), serversKey: "mcpServers"},
	"vscode":      jsonInstaller{relativePath: filepath.Join(".vscode", "mcp.json"), serversKey: "servers"},
	"gemini":      jsonInstaller{relativePath: filepath.Join(".gemini", "settings.json"), serversKey: "mcpServers"},
	"codex":       codexInstaller{relativePath: filepath.Join(".codex", "config.toml")},
	"opencode":    jsonInstaller{relativePath: "opencode.json", serversKey: "mcp", entry: opencodeServerEntry},
	"kilocode":    jsonInstaller{relativePath: filepath.Join(".kilocode", "mcp.json"), serversKey: "mcpServers"},
	"qwen":        jsonInstaller{relativePath: filepath.Join(".qwen", "settings.json"), serversKey: "mcpServers"},
	"kimi":        jsonInstaller{relativePath: filepath.Join(".kimi", "settings.json"), serversKey: "mcpServers"},
	"aider":       jsonInstaller{relativePath: filepath.Join(".aider", "mcp.json"), serversKey: "mcpServers"},
	"claw":        jsonInstaller{relativePath: filepath.Join(".openclaw", "mcp.json"), serversKey: "mcpServers"},
	"copilot":     jsonInstaller{relativePath: filepath.Join(".github", "mcp.json"), serversKey: "mcpServers"},
	"droid":       jsonInstaller{relativePath: filepath.Join(".factory", "mcp.json"), serversKey: "mcpServers"},
	"trae":        jsonInstaller{relativePath: filepath.Join(".trae", "mcp.json"), serversKey: "mcpServers"},
	"trae-cn":     jsonInstaller{relativePath: filepath.Join(".trae-cn", "mcp.json"), serversKey: "mcpServers"},
	"hermes":      jsonInstaller{relativePath: filepath.Join(".hermes", "mcp.json"), serversKey: "mcpServers"},
	"kiro":        jsonInstaller{relativePath: filepath.Join(".kiro", "mcp.json"), serversKey: "mcpServers"},
	"pi":          jsonInstaller{relativePath: filepath.Join(".pi", "mcp.json"), serversKey: "mcpServers"},
	"antigravity": jsonInstaller{relativePath: filepath.Join(".antigravity", "mcp.json"), serversKey: "mcpServers"},
}

// opencodeServerEntry builds the OpenCode-specific server entry shape.
// OpenCode flattens `command` and `args` into a single array under
// `command`, and tags local commands with `type: "local"`.
//
// See: https://opencode-tutorial.com/en/docs/mcp-servers for the schema.
func opencodeServerEntry(opts Options) map[string]any {
	return map[string]any{
		"type":    "local",
		"command": append([]string{opts.bin()}, gogfyServeArgs(opts)...),
	}
}
