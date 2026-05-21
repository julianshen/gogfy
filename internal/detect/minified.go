package detect

import (
	"bytes"
	"io"
	"os"
	"strings"
)

// maxReasonableLineLength is the line-length ceiling above which a
// file is treated as minified / generated / bundled and dropped from
// the corpus. Real hand-written source — even dense, deeply-indented
// code — rarely exceeds a few hundred characters per line; gofmt caps
// nothing but practical lines stay under ~250. Minified bundles
// (d3.v7.min.js: a single 279,645-char line) blow past this by two
// orders of magnitude. 5000 leaves enormous headroom for legitimate
// long lines (embedded base64 data URIs, long string literals) while
// still catching every real minified file.
const maxReasonableLineLength = 5000

// minifiedScanBytes bounds how much of a file we read to decide if
// it's minified. Minified files put their giant line at the very
// start, so the first chunk is decisive. Reading the whole file would
// defeat the purpose — a 280KB minified blob shouldn't cost a full
// read just to reject it.
const minifiedScanBytes = 64 * 1024

// IsMinified reports whether the file at path looks machine-generated
// /minified and should be skipped. Two signals, cheapest first:
//
//  1. A `.min.js` / `.min.css` filename suffix — the universal
//     convention for minified web assets. Fast path, no read.
//  2. A line longer than maxReasonableLineLength within the first
//     minifiedScanBytes. Catches bundled vendored blobs that don't
//     carry the `.min.` marker (webpack output, inlined libraries,
//     generated parsers).
//
// On any read error the file is treated as NOT minified — detection
// is a quality filter, not a security boundary, so a transient read
// failure shouldn't silently drop a real source file. The eventual
// extractor read will surface the error properly if it persists.
func IsMinified(path string) bool {
	low := strings.ToLower(path)
	if strings.HasSuffix(low, ".min.js") || strings.HasSuffix(low, ".min.css") {
		return true
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	return hasOverlongLine(io.LimitReader(f, minifiedScanBytes))
}

// hasOverlongLine reports whether r contains a line (run of bytes
// between newlines) longer than maxReasonableLineLength. Streams in
// chunks so a pathological no-newline file doesn't buffer unbounded —
// we only ever track the length since the last newline, not the
// content.
func hasOverlongLine(r io.Reader) bool {
	buf := make([]byte, 8192)
	sinceNewline := 0
	for {
		n, err := r.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			for {
				nl := bytes.IndexByte(chunk, '\n')
				if nl < 0 {
					sinceNewline += len(chunk)
					if sinceNewline > maxReasonableLineLength {
						return true
					}
					break
				}
				sinceNewline += nl
				if sinceNewline > maxReasonableLineLength {
					return true
				}
				sinceNewline = 0
				chunk = chunk[nl+1:]
			}
		}
		if err != nil {
			// io.EOF or any read error ends the scan. We've already
			// returned true above if an overlong line was seen; here
			// the (possibly final, newline-free) line was within bounds.
			return false
		}
	}
}
