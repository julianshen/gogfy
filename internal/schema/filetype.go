package schema

import (
	"path/filepath"
	"strings"
)

// FileType classifies a node's source file into the coarse bucket the
// rest of the pipeline cares about (corpus stats, report sections,
// LLM-routing decisions when those features ship). String values match
// graphify upstream so cross-tool consumers reading graph.json don't
// need a translation layer.
type FileType string

const (
	FileTypeUnknown  FileType = ""
	FileTypeCode     FileType = "code"
	FileTypeDocument FileType = "document"
	FileTypePaper    FileType = "paper"
	FileTypeImage    FileType = "image"
	FileTypeVideo    FileType = "video"
)

// codeExtensions covers extensions classified as Code. Stored
// lowercase only — ClassifyFile lowercases the input — so the
// case-sensitive Fortran variants (.F vs .f) collapse to one entry.
// Includes extensions gogfy has dedicated extractors for plus the
// graphify-upstream set (Pascal, Vue, etc.) so cross-tool consumers
// reading graph.json see the same classification.
var codeExtensions = map[string]struct{}{
	// gogfy extractor coverage (cmd/gogfy/main.go supportedExtensions)
	".py": {}, ".ts": {}, ".js": {}, ".jsx": {}, ".tsx": {}, ".mjs": {}, ".cjs": {},
	".go": {}, ".rs": {}, ".java": {}, ".c": {}, ".h": {}, ".cpp": {}, ".cc": {},
	".cxx": {}, ".hpp": {}, ".hxx": {}, ".hh": {}, ".rb": {}, ".kt": {}, ".kts": {},
	".scala": {}, ".sc": {}, ".php": {}, ".lua": {}, ".zig": {}, ".jl": {},
	".sh": {}, ".bash": {}, ".cs": {}, ".hs": {}, ".ml": {}, ".mli": {},
	".svelte": {}, ".f": {}, ".f90": {}, ".f95": {}, ".f03": {}, ".f08": {},
	".ex": {}, ".exs": {}, ".dart": {}, ".swift": {}, ".r": {}, ".erl": {},
	// graphify-upstream extras (no gogfy extractor yet but still Code)
	".ejs": {}, ".groovy": {}, ".gradle": {}, ".luau": {}, ".ps1": {},
	".m": {}, ".mm": {}, ".vue": {}, ".v": {}, ".sv": {}, ".sql": {}, ".toc": {},
	".pas": {}, ".pp": {}, ".dpr": {}, ".dpk": {}, ".lpr": {}, ".inc": {},
	".dfm": {}, ".lfm": {}, ".lpk": {},
}

var docExtensions = map[string]struct{}{
	".md": {}, ".mdx": {}, ".qmd": {}, ".txt": {}, ".rst": {},
	".html": {}, ".yaml": {}, ".yml": {},
}

var paperExtensions = map[string]struct{}{".pdf": {}}

var imageExtensions = map[string]struct{}{
	".png": {}, ".jpg": {}, ".jpeg": {}, ".gif": {}, ".webp": {}, ".svg": {},
}

var videoExtensions = map[string]struct{}{
	".mp4": {}, ".mov": {}, ".webm": {}, ".mkv": {}, ".avi": {}, ".m4v": {},
	".mp3": {}, ".wav": {}, ".m4a": {}, ".ogg": {},
}

// ClassifyFile returns the FileType for path's extension (case-insensitive).
// Empty string is the unknown bucket — callers should not treat it as an
// error (build configs, lockfiles, etc. land here and are skipped later).
func ClassifyFile(path string) FileType {
	if path == "" {
		return FileTypeUnknown
	}
	ext := strings.ToLower(filepath.Ext(path))
	if _, ok := codeExtensions[ext]; ok {
		return FileTypeCode
	}
	if _, ok := docExtensions[ext]; ok {
		return FileTypeDocument
	}
	if _, ok := paperExtensions[ext]; ok {
		return FileTypePaper
	}
	if _, ok := imageExtensions[ext]; ok {
		return FileTypeImage
	}
	if _, ok := videoExtensions[ext]; ok {
		return FileTypeVideo
	}
	return FileTypeUnknown
}
