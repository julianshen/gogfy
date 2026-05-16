package schema

import "testing"

func TestClassifyFile(t *testing.T) {
	cases := []struct {
		path string
		want FileType
	}{
		// Code
		{"src/main.go", FileTypeCode},
		{"app.py", FileTypeCode},
		{"lib/foo.rs", FileTypeCode},
		{"Component.tsx", FileTypeCode},
		// Case-insensitive on common extensions.
		{"App.JSX", FileTypeCode},
		// Documents
		{"README.md", FileTypeDocument},
		{"docs/spec.rst", FileTypeDocument},
		{"config.yaml", FileTypeDocument},
		// Paper
		{"paper.pdf", FileTypePaper},
		// Image
		{"logo.png", FileTypeImage},
		{"cover.JPEG", FileTypeImage},
		// Video / audio (graphify lumps both under video)
		{"clip.mp4", FileTypeVideo},
		{"voice.mp3", FileTypeVideo},
		// Unknown
		{"go.sum", FileTypeUnknown},
		{"Dockerfile", FileTypeUnknown},
		{"", FileTypeUnknown},
		{"package-lock.json", FileTypeUnknown},
	}
	for _, c := range cases {
		got := ClassifyFile(c.path)
		if got != c.want {
			t.Errorf("ClassifyFile(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

func TestClassifyFileFortranCaseSensitive(t *testing.T) {
	// Fortran uses .F vs .f for free-form vs fixed-form. Both are code.
	// The case-insensitive lookup already handles .F → .f, so this
	// mostly pins that the case-sensitive fallback branch doesn't break
	// the lowercase path.
	if got := ClassifyFile("module.F90"); got != FileTypeCode {
		t.Errorf(".F90 should classify as code, got %q", got)
	}
	if got := ClassifyFile("module.f90"); got != FileTypeCode {
		t.Errorf(".f90 should classify as code, got %q", got)
	}
}
