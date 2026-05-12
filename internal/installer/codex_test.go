package installer

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCodexRegisteredAsPlatform — codex must show up alongside the JSON
// platforms so `gogfy install --platform codex` works. The full platform
// list is checked in TestRegistryListsAllSupportedPlatforms.
func TestCodexRegisteredAsPlatform(t *testing.T) {
	if _, err := For("codex"); err != nil {
		t.Fatalf("codex platform not registered: %v", err)
	}
	for _, name := range SupportedPlatforms() {
		if name == "codex" {
			return
		}
	}
	t.Fatalf("codex missing from SupportedPlatforms(): %v", SupportedPlatforms())
}

// TestCodexConfigPath — ~/.codex/config.toml is user-level on graphify, but
// we use workspace-local for parity with the other platforms.
func TestCodexConfigPath(t *testing.T) {
	inst, _ := For("codex")
	got := inst.ConfigPath("/ws")
	want := filepath.Join("/ws", ".codex", "config.toml")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestCodexInstallWritesBlock — install on an empty workspace must create
// `.codex/config.toml` with a `[mcp_servers.gogfy]` block.
func TestCodexInstallWritesBlock(t *testing.T) {
	ws := t.TempDir()
	inst, _ := For("codex")
	if err := inst.Install(ws, Options{}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(inst.ConfigPath(ws))
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, "[mcp_servers.gogfy]") {
		t.Fatalf("missing [mcp_servers.gogfy] header:\n%s", s)
	}
	if !strings.Contains(s, `command = "gogfy"`) {
		t.Fatalf("missing command line:\n%s", s)
	}
	wantGraph := filepath.Join("graphify-out", "graph.json")
	if !strings.Contains(s, wantGraph) {
		t.Fatalf("missing relative graph path %q:\n%s", wantGraph, s)
	}
}

// TestCodexInstallPreservesExistingTOML — adding the gogfy block must not
// disturb other tables, comments, or top-level keys in the user's config.
func TestCodexInstallPreservesExistingTOML(t *testing.T) {
	ws := t.TempDir()
	inst, _ := For("codex")
	path := inst.ConfigPath(ws)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	existing := `# user-managed config — do not lose this comment

[features]
multi_agent = true

[mcp_servers.other]
command = "other-mcp"
args = ["--foo"]
`
	if err := os.WriteFile(path, []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}
	if err := inst.Install(ws, Options{}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	s := string(data)
	for _, must := range []string{
		"# user-managed config — do not lose this comment",
		"[features]",
		"multi_agent = true",
		"[mcp_servers.other]",
		`command = "other-mcp"`,
		"[mcp_servers.gogfy]",
	} {
		if !strings.Contains(s, must) {
			t.Fatalf("missing %q in result:\n%s", must, s)
		}
	}
}

// TestCodexInstallReplacesExistingGogfyBlock — re-running install must
// refresh the gogfy block in place, not append a duplicate.
func TestCodexInstallReplacesExistingGogfyBlock(t *testing.T) {
	ws := t.TempDir()
	inst, _ := For("codex")
	if err := inst.Install(ws, Options{}); err != nil {
		t.Fatal(err)
	}
	if err := inst.Install(ws, Options{Bin: "/opt/bin/gogfy"}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(inst.ConfigPath(ws))
	s := string(data)
	if got := strings.Count(s, "[mcp_servers.gogfy]"); got != 1 {
		t.Fatalf("expected exactly one [mcp_servers.gogfy] block, got %d:\n%s", got, s)
	}
	if !strings.Contains(s, `command = "/opt/bin/gogfy"`) {
		t.Fatalf("custom Bin not persisted on re-install:\n%s", s)
	}
}

// TestCodexUninstallRemovesOnlyGogfyBlock — uninstall must drop the gogfy
// block and leave everything else intact.
func TestCodexUninstallRemovesOnlyGogfyBlock(t *testing.T) {
	ws := t.TempDir()
	inst, _ := For("codex")
	if err := inst.Install(ws, Options{}); err != nil {
		t.Fatal(err)
	}
	// Add an unrelated block by hand.
	path := inst.ConfigPath(ws)
	data, _ := os.ReadFile(path)
	extra := "\n[mcp_servers.other]\ncommand = \"other\"\n"
	if err := os.WriteFile(path, append(data, []byte(extra)...), 0644); err != nil {
		t.Fatal(err)
	}
	if err := inst.Uninstall(ws); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(path)
	s := string(out)
	if strings.Contains(s, "[mcp_servers.gogfy]") {
		t.Fatalf("gogfy block still present:\n%s", s)
	}
	if !strings.Contains(s, "[mcp_servers.other]") {
		t.Fatalf("other block erased:\n%s", s)
	}
}

// TestCodexUninstallNoOpWhenMissing — symmetric with the JSON installers:
// uninstalling without a config must not error.
func TestCodexUninstallNoOpWhenMissing(t *testing.T) {
	ws := t.TempDir()
	inst, _ := For("codex")
	if err := inst.Uninstall(ws); err != nil {
		t.Fatalf("expected no-op, got %v", err)
	}
}

// TestCodexUninstallWhenNoGogfyBlock — file exists but has no gogfy block:
// must leave the file byte-identical (no rewrite, no churn).
func TestCodexUninstallWhenNoGogfyBlock(t *testing.T) {
	ws := t.TempDir()
	inst, _ := For("codex")
	path := inst.ConfigPath(ws)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	original := []byte("[features]\nmulti_agent = true\n")
	if err := os.WriteFile(path, original, 0644); err != nil {
		t.Fatal(err)
	}
	if err := inst.Uninstall(ws); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, original) {
		t.Fatalf("uninstall rewrote file when no gogfy block to remove:\nbefore: %s\nafter:  %s", original, got)
	}
}

// TestCodexInstallFailsWhenParentIsAFile — MkdirAll under writeBytes must
// surface its error rather than silently no-op.
func TestCodexInstallFailsWhenParentIsAFile(t *testing.T) {
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, ".codex"), []byte("blocker"), 0644); err != nil {
		t.Fatal(err)
	}
	inst, _ := For("codex")
	if err := inst.Install(ws, Options{}); err == nil {
		t.Fatal("expected MkdirAll error when .codex is a file")
	}
}

// TestCodexInstallReadsExistingFile — readBytesOrEmpty must return the
// file's actual bytes (not nil) when it exists. Verified indirectly through
// TestCodexInstallPreservesExistingTOML, but a direct read-fail path is
// still uncovered. Trigger it with an unreadable file.
func TestCodexInstallFailsOnUnreadableConfig(t *testing.T) {
	ws := t.TempDir()
	inst, _ := For("codex")
	path := inst.ConfigPath(ws)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("[a]\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0); err != nil {
		t.Skipf("chmod 0 not honored: %v", err)
	}
	defer os.Chmod(path, 0644)
	if err := inst.Install(ws, Options{}); err == nil {
		t.Fatal("expected read error on unreadable config")
	}
}

// TestCodexUninstallFailsOnUnreadableConfig — symmetric path through
// Uninstall's os.ReadFile.
func TestCodexUninstallFailsOnUnreadableConfig(t *testing.T) {
	ws := t.TempDir()
	inst, _ := For("codex")
	path := inst.ConfigPath(ws)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("[mcp_servers.gogfy]\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0); err != nil {
		t.Skipf("chmod 0 not honored: %v", err)
	}
	defer os.Chmod(path, 0644)
	if err := inst.Uninstall(ws); err == nil {
		t.Fatal("expected read error on unreadable config")
	}
}

// TestCodexInstallNReinstallsAreFixedPoint — N consecutive installs must
// produce identical bytes. Catches the "consume one trailing newline" /
// "blank-line separator" branches accumulating whitespace silently.
func TestCodexInstallNReinstallsAreFixedPoint(t *testing.T) {
	ws := t.TempDir()
	inst, _ := For("codex")
	if err := inst.Install(ws, Options{}); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(inst.ConfigPath(ws))
	for i := 0; i < 5; i++ {
		if err := inst.Install(ws, Options{}); err != nil {
			t.Fatalf("reinstall #%d: %v", i, err)
		}
	}
	got, _ := os.ReadFile(inst.ConfigPath(ws))
	if !bytes.Equal(first, got) {
		t.Fatalf("5 reinstalls drifted from 1:\nfirst (%d bytes): %q\nafter5 (%d bytes): %q",
			len(first), first, len(got), got)
	}
}

// TestCodexInstallRefusesArrayOfTablesGogfy — pre-existing
// `[[mcp_servers.gogfy]]` is an array-of-tables shape our line editor
// can't safely modify; appending a `[mcp_servers.gogfy]` block would
// produce duplicate-key TOML. Surface as an error.
func TestCodexInstallRefusesArrayOfTablesGogfy(t *testing.T) {
	ws := t.TempDir()
	inst, _ := For("codex")
	path := inst.ConfigPath(ws)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	hostile := []byte("[[mcp_servers.gogfy]]\ncommand = \"old\"\n")
	if err := os.WriteFile(path, hostile, 0644); err != nil {
		t.Fatal(err)
	}
	if err := inst.Install(ws, Options{}); err == nil {
		t.Fatal("expected error on pre-existing [[mcp_servers.gogfy]]")
	}
}

// TestCodexInstallRefusesInlineTableGogfy — pre-existing inline-table
// definition like `mcp_servers = { gogfy = ... }` is the other hazard
// shape; refuse rather than silently produce duplicate-key TOML.
func TestCodexInstallRefusesInlineTableGogfy(t *testing.T) {
	ws := t.TempDir()
	inst, _ := For("codex")
	path := inst.ConfigPath(ws)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	hostile := []byte(`mcp_servers = { gogfy = { command = "old" } }
`)
	if err := os.WriteFile(path, hostile, 0644); err != nil {
		t.Fatal(err)
	}
	if err := inst.Install(ws, Options{}); err == nil {
		t.Fatal("expected error on inline-table mcp_servers.gogfy")
	}
}

// TestCodexInstallPreservesBlankLineBeforeNextTable — when re-installing
// over a config where the gogfy block is followed by another table, the
// idiomatic blank-line separator before that next table must survive.
func TestCodexInstallPreservesBlankLineBeforeNextTable(t *testing.T) {
	ws := t.TempDir()
	inst, _ := For("codex")
	path := inst.ConfigPath(ws)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	// Existing config has gogfy block then blank line then another table.
	existing := []byte("[mcp_servers.gogfy]\ncommand = \"old\"\n\n[mcp_servers.other]\ncommand = \"other\"\n")
	if err := os.WriteFile(path, existing, 0644); err != nil {
		t.Fatal(err)
	}
	if err := inst.Install(ws, Options{}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if !bytes.Contains(got, []byte("\n\n[mcp_servers.other]")) {
		t.Fatalf("blank line separator before next table was lost:\n%s", got)
	}
}

// TestCodexInstallMatchesHeaderWithInlineComment — `[mcp_servers.gogfy] # foo`
// is the same logical block as `[mcp_servers.gogfy]`. Failing to match it
// would cause us to append a duplicate header → duplicate-key TOML.
func TestCodexInstallMatchesHeaderWithInlineComment(t *testing.T) {
	ws := t.TempDir()
	inst, _ := For("codex")
	path := inst.ConfigPath(ws)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	existing := []byte("[mcp_servers.gogfy] # legacy entry\ncommand = \"old\"\n")
	if err := os.WriteFile(path, existing, 0644); err != nil {
		t.Fatal(err)
	}
	if err := inst.Install(ws, Options{}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if c := bytes.Count(got, []byte("[mcp_servers.gogfy]")); c != 1 {
		t.Fatalf("expected exactly one [mcp_servers.gogfy] header, got %d:\n%s", c, got)
	}
	if bytes.Contains(got, []byte(`command = "old"`)) {
		t.Fatalf("old block not replaced:\n%s", got)
	}
}

// TestCodexInstallSkipsRewriteWhenContentUnchanged — a no-op install (same
// args, same Options) must not touch mtime; mirrors the snippet behavior.
func TestCodexInstallSkipsRewriteWhenContentUnchanged(t *testing.T) {
	ws := t.TempDir()
	inst, _ := For("codex")
	if err := inst.Install(ws, Options{}); err != nil {
		t.Fatal(err)
	}
	preInfo, _ := os.Stat(inst.ConfigPath(ws))
	if err := inst.Install(ws, Options{}); err != nil {
		t.Fatal(err)
	}
	postInfo, _ := os.Stat(inst.ConfigPath(ws))
	if !preInfo.ModTime().Equal(postInfo.ModTime()) {
		t.Fatalf("idempotent codex install touched mtime")
	}
}

// TestCodexInstallToleratesUnrelatedMentionOfBothWords — a string value
// that happens to mention both "mcp_servers" and "gogfy" must not trigger
// the alternate-form refusal (the guard previously matched any non-header
// line containing both substrings).
func TestCodexInstallToleratesUnrelatedMentionOfBothWords(t *testing.T) {
	ws := t.TempDir()
	inst, _ := For("codex")
	path := inst.ConfigPath(ws)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	// Description string mentions both words — false positive bait.
	existing := []byte("[features]\ndescription = \"covers mcp_servers and gogfy together\"\n")
	if err := os.WriteFile(path, existing, 0644); err != nil {
		t.Fatal(err)
	}
	if err := inst.Install(ws, Options{}); err != nil {
		t.Fatalf("install rejected unrelated string mentioning both words: %v", err)
	}
	got, _ := os.ReadFile(path)
	if !bytes.Contains(got, []byte(`description = "covers mcp_servers and gogfy together"`)) {
		t.Fatalf("description value mangled:\n%s", got)
	}
	if !bytes.Contains(got, []byte("[mcp_servers.gogfy]")) {
		t.Fatalf("gogfy block not appended:\n%s", got)
	}
}

// TestCodexInstallToleratesWhitespaceInHeader — `[ mcp_servers.gogfy ]`
// and `[mcp_servers . gogfy]` are TOML-equivalent to the canonical form.
// Reinstall must match them in place rather than appending a duplicate
// header (which would yield duplicate-key TOML).
func TestCodexInstallToleratesWhitespaceInHeader(t *testing.T) {
	cases := map[string]string{
		"spaces inside brackets": "[ mcp_servers.gogfy ]\ncommand = \"old\"\n",
		"spaces around dots":     "[mcp_servers . gogfy]\ncommand = \"old\"\n",
	}
	for name, existing := range cases {
		t.Run(name, func(t *testing.T) {
			ws := t.TempDir()
			inst, _ := For("codex")
			path := inst.ConfigPath(ws)
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(existing), 0644); err != nil {
				t.Fatal(err)
			}
			if err := inst.Install(ws, Options{}); err != nil {
				t.Fatalf("install: %v", err)
			}
			got, _ := os.ReadFile(path)
			// Should have exactly one header now (the canonical replacement),
			// and the old `command = "old"` must be gone.
			if c := bytes.Count(got, []byte("[mcp_servers.gogfy]")); c != 1 {
				t.Fatalf("expected one canonical header, got %d:\n%s", c, got)
			}
			if bytes.Contains(got, []byte(`command = "old"`)) {
				t.Fatalf("old block not replaced:\n%s", got)
			}
		})
	}
}

// TestCodexInstallToleratesGogfySubstringInOtherServerName — a server
// named `mygogfyproxy` (containing the substring `gogfy`) inside an
// inline-table mcp_servers definition must NOT trip the alternate-form
// guard. Only an actual `gogfy = ` key should.
func TestCodexInstallToleratesGogfySubstringInOtherServerName(t *testing.T) {
	ws := t.TempDir()
	inst, _ := For("codex")
	path := inst.ConfigPath(ws)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	existing := []byte("mcp_servers = { mygogfyproxy = { command = \"x\" } }\n")
	if err := os.WriteFile(path, existing, 0644); err != nil {
		t.Fatal(err)
	}
	if err := inst.Install(ws, Options{}); err != nil {
		t.Fatalf("install rejected mygogfyproxy as if it defined gogfy: %v", err)
	}
}

// TestCodexInstallCustomOutDir — Options.OutDir must flow into the args
// list inside the TOML block (not just the JSON installers).
func TestCodexInstallCustomOutDir(t *testing.T) {
	ws := t.TempDir()
	inst, _ := For("codex")
	if err := inst.Install(ws, Options{OutDir: "custom-out"}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(inst.ConfigPath(ws))
	if !strings.Contains(string(data), filepath.Join("custom-out", "graph.json")) {
		t.Fatalf("custom OutDir not propagated:\n%s", data)
	}
}
