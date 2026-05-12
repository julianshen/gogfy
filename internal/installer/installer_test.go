package installer

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// readJSON parses a JSON file into a generic map for assertion.
func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal %s: %v", path, err)
	}
	return m
}

func TestRegistryListsAllSupportedPlatforms(t *testing.T) {
	got := SupportedPlatforms()
	want := []string{
		"aider", "antigravity", "claude", "claw", "codex", "copilot",
		"cursor", "droid", "gemini", "hermes", "kilocode", "kimi",
		"kiro", "opencode", "pi", "qwen", "trae", "trae-cn", "vscode",
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d platforms, got %d (%v)", len(want), len(got), got)
	}
	for i, name := range want {
		if got[i] != name {
			t.Fatalf("platform[%d]: expected %q, got %q", i, name, got[i])
		}
	}
}

func TestForReturnsErrorForUnknownPlatform(t *testing.T) {
	if _, err := For("notarealthing"); err == nil {
		t.Fatal("expected error for unknown platform")
	}
}

// expectedServersKey returns the platform-native top-level key for an
// `mcpServers`-shaped object.
func expectedServersKey(platform string) string {
	if platform == "vscode" {
		return "servers"
	}
	return "mcpServers"
}

func assertGogfyServerEntry(t *testing.T, m map[string]any, platform string) {
	t.Helper()
	key := expectedServersKey(platform)
	servers, ok := m[key].(map[string]any)
	if !ok {
		t.Fatalf("missing %s: %v", key, m)
	}
	gogfy, ok := servers["gogfy"].(map[string]any)
	if !ok {
		t.Fatalf("missing %s.gogfy: %v", key, servers)
	}
	if gogfy["command"] != "gogfy" {
		t.Fatalf("expected command=gogfy, got %v", gogfy["command"])
	}
	args, ok := gogfy["args"].([]any)
	if !ok || len(args) == 0 || args[0] != "serve" {
		t.Fatalf("expected args[0]=serve, got %v", gogfy["args"])
	}
	wantGraph := filepath.Join("graphify-out", "graph.json") // relative
	found := false
	for _, a := range args {
		if a == wantGraph {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected relative graph path %q in args, got %v", wantGraph, args)
	}
}

// TestInstallEachPlatformWritesValidConfig verifies every JSON-config
// platform produces a config with the platform-native servers key + a
// gogfy entry pointing at the workspace's graph artifacts. Codex (TOML)
// is exercised by codex_test.go.
func TestInstallEachPlatformWritesValidConfig(t *testing.T) {
	for _, name := range SupportedPlatforms() {
		// codex uses TOML (codex_test.go); opencode has its own
		// flattened shape (TestOpenCodeInstallUsesMcpKeyAndFlattenedCommand).
		if name == "codex" || name == "opencode" {
			continue
		}
		t.Run(name, func(t *testing.T) {
			ws := t.TempDir()
			inst, err := For(name)
			if err != nil {
				t.Fatalf("For(%q): %v", name, err)
			}
			if err := inst.Install(ws, Options{}); err != nil {
				t.Fatalf("Install: %v", err)
			}
			path := inst.ConfigPath(ws)
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("config not written at %s: %v", path, err)
			}
			assertGogfyServerEntry(t, readJSON(t, path), name)
		})
	}
}

// TestInstallIsIdempotent — running install twice should not duplicate or
// corrupt the entry.
func TestInstallIsIdempotent(t *testing.T) {
	ws := t.TempDir()
	inst, _ := For("claude")
	if err := inst.Install(ws, Options{}); err != nil {
		t.Fatal(err)
	}
	if err := inst.Install(ws, Options{}); err != nil {
		t.Fatal(err)
	}
	m := readJSON(t, inst.ConfigPath(ws))
	servers := m["mcpServers"].(map[string]any)
	if len(servers) != 1 {
		t.Fatalf("expected one server entry, got %d", len(servers))
	}
}

// TestInstallPreservesExistingMcpServers — adding gogfy to an existing config
// must not erase other entries.
func TestInstallPreservesExistingMcpServers(t *testing.T) {
	ws := t.TempDir()
	inst, _ := For("claude")
	path := inst.ConfigPath(ws)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	existing := []byte(`{"mcpServers":{"otherserver":{"command":"other","args":[]}},"otherKey":42}`)
	if err := os.WriteFile(path, existing, 0644); err != nil {
		t.Fatal(err)
	}
	if err := inst.Install(ws, Options{}); err != nil {
		t.Fatal(err)
	}
	m := readJSON(t, path)
	servers := m["mcpServers"].(map[string]any)
	if _, ok := servers["otherserver"]; !ok {
		t.Fatalf("existing otherserver was erased: %v", servers)
	}
	if _, ok := servers["gogfy"]; !ok {
		t.Fatalf("gogfy not added: %v", servers)
	}
	if v, _ := m["otherKey"].(float64); v != 42 {
		t.Fatalf("top-level otherKey lost: %v", m["otherKey"])
	}
}

// TestUninstallRemovesGogfyOnly — uninstall must drop only the gogfy entry,
// keeping other servers and top-level keys intact.
func TestUninstallRemovesGogfyOnly(t *testing.T) {
	ws := t.TempDir()
	inst, _ := For("claude")
	path := inst.ConfigPath(ws)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	existing := []byte(`{"mcpServers":{"otherserver":{"command":"other"},"gogfy":{"command":"gogfy"}}}`)
	if err := os.WriteFile(path, existing, 0644); err != nil {
		t.Fatal(err)
	}
	if err := inst.Uninstall(ws); err != nil {
		t.Fatal(err)
	}
	m := readJSON(t, path)
	servers := m["mcpServers"].(map[string]any)
	if _, ok := servers["gogfy"]; ok {
		t.Fatalf("gogfy still present after uninstall: %v", servers)
	}
	if _, ok := servers["otherserver"]; !ok {
		t.Fatalf("otherserver erased by uninstall: %v", servers)
	}
}

// TestUninstallNoOpWhenConfigMissing — uninstalling from a workspace that
// never had a config must not error.
func TestUninstallNoOpWhenConfigMissing(t *testing.T) {
	ws := t.TempDir()
	inst, _ := For("claude")
	if err := inst.Uninstall(ws); err != nil {
		t.Fatalf("uninstall on missing config should be no-op, got %v", err)
	}
}

// TestVSCodeUsesServersKey — VS Code's native MCP config keys servers under
// "servers" (not "mcpServers" like Claude/Cursor/Gemini). Lock that down so
// a future shared-helper refactor can't silently regress to mcpServers and
// make the entry invisible to VS Code.
func TestVSCodeUsesServersKey(t *testing.T) {
	ws := t.TempDir()
	inst, _ := For("vscode")
	if err := inst.Install(ws, Options{}); err != nil {
		t.Fatal(err)
	}
	cfg := readJSON(t, inst.ConfigPath(ws))
	if _, ok := cfg["servers"]; !ok {
		t.Fatalf("VS Code config missing 'servers' key: %v", cfg)
	}
	if _, ok := cfg["mcpServers"]; ok {
		t.Fatalf("VS Code config must not use 'mcpServers' key: %v", cfg)
	}
}

// TestArgsArePathRelative — args must use workspace-relative graph/report
// paths so the config survives `git clone` to a different directory or a
// workspace move. Absolute paths from install-time would break both.
func TestArgsArePathRelative(t *testing.T) {
	ws := t.TempDir()
	inst, _ := For("claude")
	if err := inst.Install(ws, Options{}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(inst.ConfigPath(ws))
	if bytes.Contains(data, []byte(ws)) {
		t.Fatalf("config baked workspace path into args: %s", data)
	}
}

// TestInstallHonorsOptionsBinAndOutDir — custom `--gogfy-bin` and `--out`
// must flow through to the written config.
func TestInstallHonorsOptionsBinAndOutDir(t *testing.T) {
	ws := t.TempDir()
	inst, _ := For("claude")
	if err := inst.Install(ws, Options{Bin: "/opt/bin/gogfy", OutDir: "custom-out"}); err != nil {
		t.Fatal(err)
	}
	m := readJSON(t, inst.ConfigPath(ws))
	gogfy := m["mcpServers"].(map[string]any)["gogfy"].(map[string]any)
	if gogfy["command"] != "/opt/bin/gogfy" {
		t.Fatalf("custom Bin not honored: %v", gogfy["command"])
	}
	args := gogfy["args"].([]any)
	wantGraph := filepath.Join("custom-out", "graph.json")
	found := false
	for _, a := range args {
		if a == wantGraph {
			found = true
		}
	}
	if !found {
		t.Fatalf("custom OutDir not honored: %v", args)
	}
}

// TestInstallRejectsNonObjectServersKey — a misconfigured config (where
// mcpServers is a string or array) must surface an error rather than being
// silently overwritten.
func TestInstallRejectsNonObjectServersKey(t *testing.T) {
	ws := t.TempDir()
	inst, _ := For("claude")
	path := inst.ConfigPath(ws)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"mcpServers":"not an object"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := inst.Install(ws, Options{}); err == nil {
		t.Fatal("expected error when mcpServers is not an object")
	}
}

// TestUninstallSkipsWriteWhenNoGogfyEntry — if the config has no gogfy entry
// to remove, Uninstall must leave the file byte-identical.
func TestUninstallSkipsWriteWhenNoGogfyEntry(t *testing.T) {
	ws := t.TempDir()
	inst, _ := For("claude")
	path := inst.ConfigPath(ws)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	original := []byte(`{"mcpServers":{"otherserver":{"command":"other"}}}`)
	if err := os.WriteFile(path, original, 0644); err != nil {
		t.Fatal(err)
	}
	preInfo, _ := os.Stat(path)
	if err := inst.Uninstall(ws); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, original) {
		t.Fatalf("uninstall rewrote config when no gogfy entry to remove:\nbefore: %s\nafter:  %s", original, got)
	}
	postInfo, _ := os.Stat(path)
	if !preInfo.ModTime().Equal(postInfo.ModTime()) {
		t.Fatalf("uninstall touched mtime when no gogfy entry to remove")
	}
}

// TestUninstallRejectsNonObjectServersKey — like Install, refuse to silently
// overwrite a misconfigured mcpServers value.
func TestUninstallRejectsNonObjectServersKey(t *testing.T) {
	ws := t.TempDir()
	inst, _ := For("claude")
	path := inst.ConfigPath(ws)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"mcpServers":42}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := inst.Uninstall(ws); err == nil {
		t.Fatal("expected error when mcpServers is not an object")
	}
}

// TestInstallRejectsCorruptExistingConfig — a malformed JSON file should
// surface as an error, not silently overwrite existing state.
func TestInstallRejectsCorruptExistingConfig(t *testing.T) {
	ws := t.TempDir()
	inst, _ := For("claude")
	path := inst.ConfigPath(ws)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("not json"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := inst.Install(ws, Options{}); err == nil {
		t.Fatal("expected parse error on corrupt config")
	}
}

// TestInstallTreatsEmptyFileAsEmptyConfig — a zero-byte config file (left
// over from a touch or a botched edit) should be treated like a missing
// file rather than failing JSON parse.
func TestInstallTreatsEmptyFileAsEmptyConfig(t *testing.T) {
	ws := t.TempDir()
	inst, _ := For("claude")
	path := inst.ConfigPath(ws)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0644); err != nil {
		t.Fatal(err)
	}
	if err := inst.Install(ws, Options{}); err != nil {
		t.Fatalf("install on empty file: %v", err)
	}
	assertGogfyServerEntry(t, readJSON(t, path), "claude")
}

// TestInstallFailsWhenWorkspaceIsReadOnly — write failures must propagate.
func TestInstallFailsWhenWorkspaceIsReadOnly(t *testing.T) {
	ws := t.TempDir()
	if err := os.Chmod(ws, 0500); err != nil {
		t.Skipf("chmod not honored on this filesystem: %v", err)
	}
	defer os.Chmod(ws, 0755)
	inst, _ := For("claude")
	if err := inst.Install(ws, Options{}); err == nil {
		t.Fatal("expected error writing to read-only workspace")
	}
}

// TestUninstallTolerantOfConfigWithoutMcpServers — file exists but has no
// mcpServers section: nothing to remove, must not error.
func TestUninstallTolerantOfConfigWithoutMcpServers(t *testing.T) {
	ws := t.TempDir()
	inst, _ := For("claude")
	path := inst.ConfigPath(ws)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"otherKey":1}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := inst.Uninstall(ws); err != nil {
		t.Fatalf("uninstall on config without mcpServers: %v", err)
	}
}

// TestUninstallPropagatesParseError — corrupt JSON must surface, not be
// silently rewritten.
func TestUninstallPropagatesParseError(t *testing.T) {
	ws := t.TempDir()
	inst, _ := For("claude")
	path := inst.ConfigPath(ws)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("garbage"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := inst.Uninstall(ws); err == nil {
		t.Fatal("expected parse error on uninstall over corrupt config")
	}
}

// TestInstallFailsWhenParentPathIsAFile — the cursor installer must create
// `.cursor/mcp.json`. If `.cursor` already exists as a file, MkdirAll fails.
func TestInstallFailsWhenParentPathIsAFile(t *testing.T) {
	ws := t.TempDir()
	// Pre-create `.cursor` as a regular file so MkdirAll fails.
	if err := os.WriteFile(filepath.Join(ws, ".cursor"), []byte("blocker"), 0644); err != nil {
		t.Fatal(err)
	}
	inst, _ := For("cursor")
	if err := inst.Install(ws, Options{}); err == nil {
		t.Fatal("expected MkdirAll error when .cursor is a file")
	}
}

// TestPlatformConfigPaths — each platform writes to the expected location
// (so installers documentation can rely on these paths).
func TestPlatformConfigPaths(t *testing.T) {
	cases := map[string]string{
		"claude":      ".mcp.json",
		"cursor":      ".cursor/mcp.json",
		"vscode":      ".vscode/mcp.json",
		"gemini":      ".gemini/settings.json",
		"opencode":    "opencode.json",
		"kilocode":    ".kilocode/mcp.json",
		"qwen":        ".qwen/settings.json",
		"kimi":        ".kimi/settings.json",
		"aider":       ".aider/mcp.json",
		"claw":        ".openclaw/mcp.json",
		"copilot":     ".github/mcp.json",
		"droid":       ".factory/mcp.json",
		"trae":        ".trae/mcp.json",
		"trae-cn":     ".trae-cn/mcp.json",
		"hermes":      ".hermes/mcp.json",
		"kiro":        ".kiro/mcp.json",
		"pi":          ".pi/mcp.json",
		"antigravity": ".antigravity/mcp.json",
	}
	for name, suffix := range cases {
		t.Run(name, func(t *testing.T) {
			inst, _ := For(name)
			got := inst.ConfigPath("/ws")
			want := filepath.Join("/ws", suffix)
			if got != want {
				t.Fatalf("%s ConfigPath: got %q, want %q", name, got, want)
			}
		})
	}
}

func TestOpenCodeInstallUsesMcpKeyAndFlattenedCommand(t *testing.T) {
	// OpenCode's MCP schema differs from the standard mcpServers shape:
	// the top-level key is `mcp`, and each server entry uses
	// `{type: "local", command: [cmd, args...]}` (single array) instead
	// of the split `{command, args: []}`. Without this test, a refactor
	// of jsonInstaller could quietly regress OpenCode to the standard
	// shape and silently break every OpenCode install.
	ws := t.TempDir()
	inst, err := For("opencode")
	if err != nil {
		t.Fatal(err)
	}
	if err := inst.Install(ws, Options{Bin: "gogfy", OutDir: "graphify-out"}); err != nil {
		t.Fatal(err)
	}
	m := readJSON(t, filepath.Join(ws, "opencode.json"))
	mcp, ok := m["mcp"].(map[string]any)
	if !ok {
		t.Fatalf("missing mcp key (got %v)", m)
	}
	if _, hasStandard := m["mcpServers"]; hasStandard {
		t.Fatalf("opencode should not write mcpServers key: %v", m)
	}
	gogfy, ok := mcp["gogfy"].(map[string]any)
	if !ok {
		t.Fatalf("missing gogfy entry: %v", mcp)
	}
	if gogfy["type"] != "local" {
		t.Fatalf("expected type=local, got %v", gogfy["type"])
	}
	cmd, ok := gogfy["command"].([]any)
	if !ok {
		t.Fatalf("command should be a single array, got %T", gogfy["command"])
	}
	if len(cmd) < 4 || cmd[0] != "gogfy" || cmd[1] != "serve" {
		t.Fatalf("command should start with [gogfy, serve, ...], got %v", cmd)
	}
	// Args must NOT be present (OpenCode folds them into command).
	if _, hasArgs := gogfy["args"]; hasArgs {
		t.Fatalf("opencode entry should not have separate args key: %v", gogfy)
	}
}

// TestOpenCodeUninstallUsesMcpKey pins that jsonInstaller.Uninstall
// looks up the platform's own serversKey rather than hardcoding
// "mcpServers". A refactor that hardcoded the standard key would
// silently make every opencode uninstall a no-op — `gogfy opencode
// uninstall` would report success while leaving the entry in place.
func TestOpenCodeUninstallUsesMcpKey(t *testing.T) {
	ws := t.TempDir()
	inst, err := For("opencode")
	if err != nil {
		t.Fatal(err)
	}
	if err := inst.Install(ws, Options{Bin: "gogfy"}); err != nil {
		t.Fatal(err)
	}
	if err := inst.Uninstall(ws); err != nil {
		t.Fatal(err)
	}
	m := readJSON(t, inst.ConfigPath(ws))
	mcp, ok := m["mcp"].(map[string]any)
	if !ok {
		// Acceptable for the mcp key to be entirely absent after uninstall.
		return
	}
	if _, present := mcp["gogfy"]; present {
		t.Fatalf("opencode uninstall left gogfy entry under mcp key: %v", mcp)
	}
}

// TestEveryPlatformRoundTripsClean exercises Install → Uninstall on
// every registered platform. Catches bugs where a platform-specific
// servers key (opencode's `mcp`, vscode's `servers`) gets hardcoded
// to `mcpServers` somewhere in the uninstall path. Idempotency of
// the install side is exercised by TestInstallIsIdempotent for
// claude, but this loop catches cross-shape regressions cheaply.
func TestEveryPlatformRoundTripsClean(t *testing.T) {
	for _, p := range SupportedPlatforms() {
		if p == "codex" {
			continue // TOML; codex_test.go covers it
		}
		t.Run(p, func(t *testing.T) {
			ws := t.TempDir()
			inst, err := For(p)
			if err != nil {
				t.Fatal(err)
			}
			if err := inst.Install(ws, Options{Bin: "gogfy"}); err != nil {
				t.Fatalf("install: %v", err)
			}
			if err := inst.Uninstall(ws); err != nil {
				t.Fatalf("uninstall: %v", err)
			}
		})
	}
}

