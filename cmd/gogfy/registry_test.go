package main

import (
	"testing"

	"github.com/julianshen/gogfy/internal/installer"
)

// TestSupportedExtensionsRegistered guards against the silent-break
// failure mode where a new extractor is added in internal/extract but
// the CLI dispatch table is forgotten. Without this test, an .xlsx
// (or .pptx, .epub, …) file shows up in a repo, the CLI skips it, and
// nobody notices because there's no error — the file just isn't in the
// graph. Reached production once already (PR #44).
func TestSupportedExtensionsRegistered(t *testing.T) {
	required := []string{
		".go", ".py", ".js", ".ts", ".java", ".c", ".cpp", ".rs", ".rb",
		".md", ".html", ".txt", ".rst", ".docx", ".xlsx", ".pdf", ".pptx",
		".r", ".R", ".erl", ".hrl",
	}
	for _, ext := range required {
		if _, ok := supportedExtensions[ext]; !ok {
			t.Errorf("supportedExtensions missing %q", ext)
		}
	}
}

// TestComboPlatformDocsCoversEveryInstallerPlatform pins that every
// platform registered in internal/installer has a corresponding case
// in comboPlatformDocs. Without this, adding platform #20 to the
// installer registry and forgetting to update comboPlatformDocs would
// silently make `gogfy <newplatform> install` skip the docs-snippet
// write — install would appear to succeed but the agent would never
// learn about the graph.
func TestComboPlatformDocsCoversEveryInstallerPlatform(t *testing.T) {
	for _, p := range installer.SupportedPlatforms() {
		if got := comboPlatformDocs(p); got == "" {
			t.Errorf("comboPlatformDocs(%q) returned \"\" — combo install would skip the docs snippet", p)
		}
	}
}

func TestComboPlatformDocsUnknownReturnsEmpty(t *testing.T) {
	// The empty return is the recognition mechanism in dispatch() that
	// distinguishes a real platform from a typo / unknown subcommand.
	// Pin it so the "unknown subcommand" error path doesn't regress.
	if got := comboPlatformDocs("notarealplatform"); got != "" {
		t.Fatalf("expected empty for unknown platform, got %q", got)
	}
}
