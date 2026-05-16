package extract

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/julianshen/gogfy/internal/schema"
)

// GoogleWorkspaceExtractor handles .gdoc / .gsheet / .gslides shortcut
// files that Google Drive for desktop drops into a synced folder.
// Those are small JSON pointers, not the document content; gogfy
// can't fetch the body without an OAuth flow, but emitting a node
// with the linked URL still surfaces "there's a Google Doc lived
// here" in the graph and lets the wiki/Obsidian artifacts link out.
type GoogleWorkspaceExtractor struct{}

// shortcutSchema mirrors the fields google-drive writes to the JSON
// payload. All optional — different drive versions populate different
// subsets.
type shortcutSchema struct {
	URL        string `json:"url"`
	ResourceID string `json:"resource_id"`
	DocID      string `json:"doc_id"`
}

func (GoogleWorkspaceExtractor) Extract(path string) (Result, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Result{}, fmt.Errorf("gworkspace: read %s: %w", path, err)
	}
	var s shortcutSchema
	if err := json.Unmarshal(data, &s); err != nil {
		return Result{}, fmt.Errorf("gworkspace: parse %s: %w", path, err)
	}
	label := filepath.Base(path)
	// Strip the .g* extension from the label so wiki/obsidian don't
	// render the shortcut suffix as part of the user-facing name.
	if i := strings.LastIndexByte(label, '.'); i >= 0 {
		label = label[:i]
	}
	node := schema.Node{
		ID:          schema.LangID(workspaceLang(path), "module", path),
		Label:       label,
		SourceFile:  path,
		FileType:    schema.FileTypeDocument,
		ExternalURL: linkedURL(s),
	}
	return Result{Nodes: []schema.Node{node}}, nil
}

// workspaceLang maps each Google shortcut extension to a stable lang tag.
// Distinct tags so cross-tool consumers (label, callflow) can filter by
// document type if they want; keeps graph.json self-describing.
func workspaceLang(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".gdoc":
		return "gdoc"
	case ".gsheet":
		return "gsheet"
	case ".gslides":
		return "gslides"
	}
	return "gworkspace"
}

// linkedURL prefers the explicit url field, then synthesizes a Drive
// URL from the resource/doc IDs. Returns "" when neither is present so
// callers can detect a malformed shortcut.
func linkedURL(s shortcutSchema) string {
	if s.URL != "" {
		return s.URL
	}
	id := s.DocID
	if id == "" {
		id = s.ResourceID
	}
	if id == "" {
		return ""
	}
	return "https://drive.google.com/open?id=" + id
}
