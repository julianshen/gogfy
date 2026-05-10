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
	want := []string{"claude", "cursor", "gemini", "vscode"}
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

// TestInstallEachPlatformWritesValidConfig verifies every supported platform
// produces a JSON config with an `mcpServers.gogfy` entry pointing at the
// workspace's graph artifacts.
func TestInstallEachPlatformWritesValidConfig(t *testing.T) {
	for _, name := range SupportedPlatforms() {
		t.Run(name, func(t *testing.T) {
			ws := t.TempDir()
			inst, err := For(name)
			if err != nil {
				t.Fatalf("For(%q): %v", name, err)
			}
			if err := inst.Install(ws); err != nil {
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
	if err := inst.Install(ws); err != nil {
		t.Fatal(err)
	}
	if err := inst.Install(ws); err != nil {
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
	if err := inst.Install(ws); err != nil {
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
	if err := inst.Install(ws); err != nil {
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
	if err := inst.Install(ws); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(inst.ConfigPath(ws))
	if bytes.Contains(data, []byte(ws)) {
		t.Fatalf("config baked workspace path into args: %s", data)
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
	if err := inst.Install(ws); err == nil {
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
	if err := inst.Install(ws); err != nil {
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
	if err := inst.Install(ws); err == nil {
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
	if err := inst.Install(ws); err == nil {
		t.Fatal("expected MkdirAll error when .cursor is a file")
	}
}

// TestPlatformConfigPaths — each platform writes to the expected location
// (so installers documentation can rely on these paths).
func TestPlatformConfigPaths(t *testing.T) {
	cases := map[string]string{
		"claude": ".mcp.json",
		"cursor": ".cursor/mcp.json",
		"vscode": ".vscode/mcp.json",
		"gemini": ".gemini/settings.json",
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
