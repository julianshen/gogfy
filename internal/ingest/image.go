package ingest

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/julianshen/gogfy/internal/fsutil"
)

// imageKind reports whether the response should be treated as an
// image and, if so, the file extension to use for the sidecar
// (".png", ".jpg", ".gif", ".webp"). Magic bytes win over the URL
// suffix when both are present, so an attacker can't fool the
// classifier by lying in either direction alone.
//
// SVG is intentionally excluded — it's XML, not binary, and the
// existing markdown path handles it more usefully (text inside
// `<svg>` elements gets indexed).
func imageKind(body []byte, finalURL string) (string, bool) {
	switch {
	case len(body) >= 8 && string(body[:8]) == "\x89PNG\r\n\x1a\n":
		return ".png", true
	case len(body) >= 3 && string(body[:3]) == "\xFF\xD8\xFF":
		return ".jpg", true
	case len(body) >= 6 && (string(body[:6]) == "GIF87a" || string(body[:6]) == "GIF89a"):
		return ".gif", true
	case len(body) >= 12 && string(body[:4]) == "RIFF" && string(body[8:12]) == "WEBP":
		return ".webp", true
	}
	low := strings.ToLower(finalURL)
	// Strip query/fragment so `?v=2` after `.png` still classifies.
	if i := strings.IndexAny(low, "?#"); i >= 0 {
		low = low[:i]
	}
	for _, ext := range []string{".png", ".jpg", ".jpeg", ".gif", ".webp"} {
		if strings.HasSuffix(low, ext) {
			if ext == ".jpeg" {
				ext = ".jpg"
			}
			return ext, true
		}
	}
	return "", false
}

// writeImageSidecar lands `body` at the image sidecar path for
// `rawURL`+`ext` and returns the path. Lives next to imageKind so
// the image branch in Ingest stays a two-line if.
func writeImageSidecar(outDir, rawURL, ext string, body []byte) (string, error) {
	path := imageSidecarPath(outDir, rawURL, ext)
	if err := fsutil.WriteFileAtomic(path, body, 0644); err != nil {
		return "", fmt.Errorf("ingest: write %s: %w", path, err)
	}
	return path, nil
}

// imageSidecarPath mirrors pdfSidecarPath but with a caller-supplied
// extension so the sidecar lands at the right MIME for downstream
// schema.ClassifyFile + semantic.ExtractImage.
func imageSidecarPath(outDir, rawURL, ext string) string {
	h := sha1.Sum([]byte(rawURL))
	hx := hex.EncodeToString(h[:8])
	stem := urlStem(rawURL)
	if stem == "" {
		stem = "url"
	}
	return filepath.Join(outDir, "ingested", stem+"-"+hx+ext)
}
