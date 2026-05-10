package main

import "testing"

// TestSupportedExtensionsRegistered guards against the silent-break
// failure mode where a new extractor is added in internal/extract but
// the CLI dispatch table is forgotten. Without this test, an .xlsx
// (or .pptx, .epub, …) file shows up in a repo, the CLI skips it, and
// nobody notices because there's no error — the file just isn't in the
// graph. Reached production once already (PR #44).
func TestSupportedExtensionsRegistered(t *testing.T) {
	required := []string{
		".go", ".py", ".js", ".ts", ".java", ".c", ".cpp", ".rs", ".rb",
		".md", ".html", ".txt", ".rst", ".docx", ".xlsx",
	}
	for _, ext := range required {
		if _, ok := supportedExtensions[ext]; !ok {
			t.Errorf("supportedExtensions missing %q", ext)
		}
	}
}
