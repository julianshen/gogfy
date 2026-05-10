package installer

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallSnippetCreatesFileWhenMissing(t *testing.T) {
	ws := t.TempDir()
	path := filepath.Join(ws, "AGENTS.md")
	if err := InstallSnippet(path, SnippetOptions{}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("snippet file not created: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, snippetStartMarker) || !strings.Contains(s, snippetEndMarker) {
		t.Fatalf("missing fenced markers:\n%s", s)
	}
	if !strings.Contains(s, "GRAPH_REPORT.md") {
		t.Fatalf("snippet must reference GRAPH_REPORT.md:\n%s", s)
	}
}

func TestInstallSnippetAppendsToExistingFile(t *testing.T) {
	ws := t.TempDir()
	path := filepath.Join(ws, "CLAUDE.md")
	original := []byte("# Existing Project Doc\n\nDo not lose this.\n")
	if err := os.WriteFile(path, original, 0644); err != nil {
		t.Fatal(err)
	}
	if err := InstallSnippet(path, SnippetOptions{}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	s := string(data)
	if !strings.Contains(s, "Existing Project Doc") {
		t.Fatalf("existing content erased:\n%s", s)
	}
	if !strings.Contains(s, snippetStartMarker) {
		t.Fatalf("snippet not appended:\n%s", s)
	}
}

func TestInstallSnippetReplacesExistingFencedBlock(t *testing.T) {
	ws := t.TempDir()
	path := filepath.Join(ws, "CLAUDE.md")
	original := "# Project\n\n" + snippetStartMarker + "\nold instructions\n" + snippetEndMarker + "\n\n## More content\n"
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}
	if err := InstallSnippet(path, SnippetOptions{}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	s := string(data)
	if strings.Contains(s, "old instructions") {
		t.Fatalf("old fenced content not replaced:\n%s", s)
	}
	if !strings.Contains(s, "## More content") {
		t.Fatalf("trailing content erased:\n%s", s)
	}
	if got := strings.Count(s, snippetStartMarker); got != 1 {
		t.Fatalf("expected exactly one fenced block, got %d:\n%s", got, s)
	}
}

func TestInstallSnippetIsIdempotent(t *testing.T) {
	ws := t.TempDir()
	path := filepath.Join(ws, "AGENTS.md")
	if err := InstallSnippet(path, SnippetOptions{}); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(path)
	if err := InstallSnippet(path, SnippetOptions{}); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(path)
	if !bytes.Equal(first, second) {
		t.Fatalf("snippet not idempotent:\nfirst:  %s\nsecond: %s", first, second)
	}
}

func TestInstallSnippetHonorsCustomReportPath(t *testing.T) {
	ws := t.TempDir()
	path := filepath.Join(ws, "AGENTS.md")
	if err := InstallSnippet(path, SnippetOptions{ReportPath: "custom-out/MY_REPORT.md"}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "custom-out/MY_REPORT.md") {
		t.Fatalf("custom ReportPath not honored:\n%s", data)
	}
}

func TestUninstallSnippetRemovesFencedBlockOnly(t *testing.T) {
	ws := t.TempDir()
	path := filepath.Join(ws, "CLAUDE.md")
	if err := os.WriteFile(path, []byte("# Project\n\nKeep me.\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := InstallSnippet(path, SnippetOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := UninstallSnippet(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	s := string(data)
	if strings.Contains(s, snippetStartMarker) || strings.Contains(s, snippetEndMarker) {
		t.Fatalf("markers still present after uninstall:\n%s", s)
	}
	if !strings.Contains(s, "Keep me.") {
		t.Fatalf("pre-existing content erased:\n%s", s)
	}
}

func TestUninstallSnippetNoOpWhenFileMissing(t *testing.T) {
	ws := t.TempDir()
	path := filepath.Join(ws, "AGENTS.md")
	if err := UninstallSnippet(path); err != nil {
		t.Fatalf("expected no-op on missing file, got %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("uninstall created a file when none existed")
	}
}

func TestUninstallSnippetNoOpWhenNoFencedBlock(t *testing.T) {
	ws := t.TempDir()
	path := filepath.Join(ws, "AGENTS.md")
	original := []byte("# Just a doc\n")
	if err := os.WriteFile(path, original, 0644); err != nil {
		t.Fatal(err)
	}
	preInfo, _ := os.Stat(path)
	if err := UninstallSnippet(path); err != nil {
		t.Fatal(err)
	}
	postInfo, _ := os.Stat(path)
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, original) {
		t.Fatalf("uninstall rewrote file when no fenced block to remove:\nbefore: %s\nafter:  %s", original, got)
	}
	if !preInfo.ModTime().Equal(postInfo.ModTime()) {
		t.Fatalf("uninstall touched mtime when nothing to remove")
	}
}

func TestInstallSnippetIsByteIdenticalOnReinstall(t *testing.T) {
	// When the snippet content didn't change, InstallSnippet must skip the
	// rewrite (no mtime churn).
	ws := t.TempDir()
	path := filepath.Join(ws, "AGENTS.md")
	if err := InstallSnippet(path, SnippetOptions{}); err != nil {
		t.Fatal(err)
	}
	preInfo, _ := os.Stat(path)
	if err := InstallSnippet(path, SnippetOptions{}); err != nil {
		t.Fatal(err)
	}
	postInfo, _ := os.Stat(path)
	if !preInfo.ModTime().Equal(postInfo.ModTime()) {
		t.Fatalf("idempotent install touched mtime")
	}
}

func TestInstallSnippetFailsOnUnreadableFile(t *testing.T) {
	ws := t.TempDir()
	path := filepath.Join(ws, "AGENTS.md")
	if err := os.WriteFile(path, []byte("# x\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0); err != nil {
		t.Skipf("chmod 0 not honored: %v", err)
	}
	defer os.Chmod(path, 0644)
	if err := InstallSnippet(path, SnippetOptions{}); err == nil {
		t.Fatal("expected read error on unreadable file")
	}
}

func TestUninstallSnippetRefusesOrphanStartMarker(t *testing.T) {
	// Orphan start marker (no matching end) is mismatched state; refuse to
	// guess where the block ends, surface an error, and leave the file
	// byte-identical.
	ws := t.TempDir()
	path := filepath.Join(ws, "AGENTS.md")
	original := []byte("# Project\n\n" + snippetStartMarker + "\nbroken — no end marker\n")
	if err := os.WriteFile(path, original, 0644); err != nil {
		t.Fatal(err)
	}
	preInfo, _ := os.Stat(path)
	if err := UninstallSnippet(path); err == nil {
		t.Fatal("expected error on orphan start marker")
	}
	got, _ := os.ReadFile(path)
	postInfo, _ := os.Stat(path)
	if !bytes.Equal(got, original) {
		t.Fatalf("file modified despite returned error:\nbefore: %s\nafter:  %s", original, got)
	}
	if !preInfo.ModTime().Equal(postInfo.ModTime()) {
		t.Fatalf("mtime touched despite returned error")
	}
}

// TestInstallSnippetRefusesMultipleStartMarkers — the silent-data-loss
// hazard: an orphan `<!-- ...:start -->` plus a later valid block would
// previously be spanned by replacement, deleting user content between.
// Now we surface an error and refuse to touch the file.
func TestInstallSnippetRefusesMultipleStartMarkers(t *testing.T) {
	ws := t.TempDir()
	path := filepath.Join(ws, "AGENTS.md")
	hostile := []byte("# Project\n\n" + snippetStartMarker + "\n" +
		"USER WROTE THIS BY HAND AND NEVER CLOSED IT\n\n" +
		snippetStartMarker + "\nold gogfy content\n" + snippetEndMarker + "\n")
	if err := os.WriteFile(path, hostile, 0644); err != nil {
		t.Fatal(err)
	}
	if err := InstallSnippet(path, SnippetOptions{}); err == nil {
		t.Fatal("expected error on multiple start markers")
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, hostile) {
		t.Fatalf("file modified despite returned error — would have lost user content")
	}
}

// TestInstallSnippetRefusesEndBeforeStart — pathological ordering must
// also surface as an error, not be guessed at.
func TestInstallSnippetRefusesEndBeforeStart(t *testing.T) {
	ws := t.TempDir()
	path := filepath.Join(ws, "AGENTS.md")
	hostile := []byte("# Project\n" + snippetEndMarker + "\nstuff\n" + snippetStartMarker + "\n")
	if err := os.WriteFile(path, hostile, 0644); err != nil {
		t.Fatal(err)
	}
	if err := InstallSnippet(path, SnippetOptions{}); err == nil {
		t.Fatal("expected error on end-before-start markers")
	}
}

// TestInstallSnippetNReinstallsAreFixedPoint — protect the "consume one
// trailing newline" branch in replaceFencedSnippet from silently
// accumulating blank lines on repeated install.
func TestInstallSnippetNReinstallsAreFixedPoint(t *testing.T) {
	ws := t.TempDir()
	path := filepath.Join(ws, "AGENTS.md")
	if err := os.WriteFile(path, []byte("# Project\n\nKeep me.\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := InstallSnippet(path, SnippetOptions{}); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(path)
	for i := 0; i < 5; i++ {
		if err := InstallSnippet(path, SnippetOptions{}); err != nil {
			t.Fatalf("reinstall #%d: %v", i, err)
		}
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(first, got) {
		t.Fatalf("5 reinstalls drifted from 1:\nfirst:  %q\nafter5: %q", first, got)
	}
}

func TestUninstallSnippetDeletesEmptiedFile(t *testing.T) {
	// Edge case: the file was created entirely by InstallSnippet (no prior
	// content). After uninstall, it should be removed rather than left as
	// an empty/whitespace-only stub.
	ws := t.TempDir()
	path := filepath.Join(ws, "AGENTS.md")
	if err := InstallSnippet(path, SnippetOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := UninstallSnippet(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("expected file to be removed after fenced-only uninstall")
	}
}
