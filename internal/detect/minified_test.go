package detect

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsMinifiedBySuffix(t *testing.T) {
	// .min.js / .min.css are the universal minified-asset convention.
	// Caught by suffix without reading the file at all (the path
	// doesn't even need to exist for the suffix fast-path).
	for _, p := range []string{
		"/x/d3.v7.min.js",
		"/x/bundle.min.css",
		"/x/JQUERY.MIN.JS", // case-insensitive
	} {
		if !IsMinified(p) {
			t.Errorf("expected %q to be detected as minified by suffix", p)
		}
	}
}

func TestIsMinifiedByLineLength(t *testing.T) {
	// A bundled file without the .min. marker (webpack output,
	// inlined vendored lib) is caught by the overlong-line heuristic.
	dir := t.TempDir()
	p := filepath.Join(dir, "bundle.js")
	// One giant line, well past the 5000-char ceiling.
	giant := "var x=" + strings.Repeat("a", 6000) + ";"
	if err := os.WriteFile(p, []byte(giant), 0644); err != nil {
		t.Fatal(err)
	}
	if !IsMinified(p) {
		t.Errorf("expected overlong-line file to be detected as minified")
	}
}

func TestIsMinifiedAcceptsRealSource(t *testing.T) {
	// Real source — even dense, with some long lines — must NOT be
	// flagged. This is the false-positive guard: dropping real code
	// would be far worse than keeping a minified file.
	dir := t.TempDir()
	p := filepath.Join(dir, "main.go")
	var b strings.Builder
	for i := 0; i < 500; i++ {
		// ~120-char lines: typical dense Go.
		b.WriteString("func handler" + string(rune('A'+i%26)) + "(ctx context.Context, req *Request) (*Response, error) { return process(ctx, req) }\n")
	}
	if err := os.WriteFile(p, []byte(b.String()), 0644); err != nil {
		t.Fatal(err)
	}
	if IsMinified(p) {
		t.Errorf("real Go source falsely flagged as minified")
	}
}

func TestIsMinifiedAcceptsLongButReasonableLine(t *testing.T) {
	// A single long line — e.g. an embedded base64 data URI or a long
	// string literal — should pass as long as it's under the 5000-char
	// ceiling. Verifies the threshold has real headroom.
	dir := t.TempDir()
	p := filepath.Join(dir, "config.go")
	line := `const dataURI = "data:image/png;base64,` + strings.Repeat("Q", 3000) + `"`
	if err := os.WriteFile(p, []byte(line+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if IsMinified(p) {
		t.Errorf("3000-char line should be under the 5000 ceiling, not flagged")
	}
}

func TestIsMinifiedMissingFileNotFlagged(t *testing.T) {
	// A path that can't be opened is treated as not-minified —
	// detection is a quality filter, not a gate, so a transient read
	// failure shouldn't silently drop the file. (The .min. suffix
	// fast-path is checked first, so use a plain .js name.)
	if IsMinified("/nonexistent/path/file.js") {
		t.Errorf("unopenable file should not be flagged minified")
	}
}

func TestHasOverlongLineNoNewlineFile(t *testing.T) {
	// A file with no newline at all but under the ceiling shouldn't
	// trip; one over the ceiling should. Exercises the
	// no-trailing-newline streaming path directly.
	if hasOverlongLine(strings.NewReader(strings.Repeat("x", 4000))) {
		t.Errorf("4000-char no-newline content should be under ceiling")
	}
	if !hasOverlongLine(strings.NewReader(strings.Repeat("x", 6000))) {
		t.Errorf("6000-char no-newline content should exceed ceiling")
	}
}

func TestCollectFilesSkipsMinified(t *testing.T) {
	// End-to-end: a .min.js in the corpus dir is dropped by
	// CollectFiles even though its extension (.js) is in the
	// allowlist.
	dir := t.TempDir()
	mustWrite(t, dir+"/app.js", "function f() { return 1; }\n")
	mustWrite(t, dir+"/vendor.min.js", "function g(){return 2}\n")
	got, err := CollectFiles(dir, []string{".js"})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range got {
		if strings.Contains(p, "vendor.min.js") {
			t.Errorf("minified file leaked into corpus: %v", got)
		}
	}
	var sawApp bool
	for _, p := range got {
		if strings.HasSuffix(p, "app.js") {
			sawApp = true
		}
	}
	if !sawApp {
		t.Errorf("real app.js should still be collected: %v", got)
	}
}
