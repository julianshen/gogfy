package extract

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/julianshen/gogfy/internal/schema"
)

func TestGoogleWorkspaceExtractorEmitsDocumentNode(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "Quarterly Plan.gdoc")
	payload := `{"url":"https://docs.google.com/document/d/abc/edit","resource_id":"doc_abc","doc_id":"abc"}`
	if err := os.WriteFile(p, []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := GoogleWorkspaceExtractor{}.Extract(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(got.Nodes))
	}
	n := got.Nodes[0]
	if n.Label != "Quarterly Plan" {
		t.Errorf("label should strip .gdoc: got %q", n.Label)
	}
	if n.FileType != schema.FileTypeDocument {
		t.Errorf("FileType should be document: got %q", n.FileType)
	}
	if n.ExternalURL != "https://docs.google.com/document/d/abc/edit" {
		t.Errorf("linked URL not surfaced: got %q", n.ExternalURL)
	}
	if got.Edges != nil {
		t.Errorf("shortcut has no relationships to surface — no edges expected")
	}
}

func TestGoogleWorkspaceExtractorSheetAndSlides(t *testing.T) {
	dir := t.TempDir()
	// Each extension should get its own lang tag so consumers can filter.
	for ext, wantPrefix := range map[string]string{
		".gsheet":  "gsheet:module:",
		".gslides": "gslides:module:",
	} {
		p := filepath.Join(dir, "doc"+ext)
		if err := os.WriteFile(p, []byte(`{"url":"https://x"}`), 0o644); err != nil {
			t.Fatal(err)
		}
		got, err := GoogleWorkspaceExtractor{}.Extract(p)
		if err != nil {
			t.Fatal(err)
		}
		if got.Nodes[0].ID[:len(wantPrefix)] != wantPrefix {
			t.Errorf("%s: ID prefix got %q, want %q", ext, got.Nodes[0].ID, wantPrefix)
		}
	}
}

func TestGoogleWorkspaceExtractorSynthesizesURLFromDocID(t *testing.T) {
	// If the shortcut lacks a `url` field but has a doc_id (older
	// Drive versions), synthesize a Drive URL so the node still has
	// a clickable target.
	dir := t.TempDir()
	p := filepath.Join(dir, "old.gdoc")
	if err := os.WriteFile(p, []byte(`{"doc_id":"xyz"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, _ := GoogleWorkspaceExtractor{}.Extract(p)
	if got.Nodes[0].ExternalURL != "https://drive.google.com/open?id=xyz" {
		t.Errorf("URL synthesis missing: got %q", got.Nodes[0].ExternalURL)
	}
}

func TestGoogleWorkspaceExtractorMalformedJSON(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "bad.gdoc")
	if err := os.WriteFile(p, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := (GoogleWorkspaceExtractor{}).Extract(p); err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}
