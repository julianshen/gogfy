package schema

import (
	"bytes"
	"encoding/json"
	"testing"
)

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
		// Case-insensitive on common extensions across all buckets.
		{"App.JSX", FileTypeCode},
		{"logo.PNG", FileTypeImage},
		{"clip.MP4", FileTypeVideo},
		{"DOC.MD", FileTypeDocument},
		{"Paper.PDF", FileTypePaper},
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

func TestClassifyFileFortranBothForms(t *testing.T) {
	// Fortran uses .F vs .f for free-form vs fixed-form. ClassifyFile
	// is case-insensitive (lowercases upfront) so both forms resolve
	// to the same map entry without needing duplicate keys.
	if got := ClassifyFile("module.F90"); got != FileTypeCode {
		t.Errorf(".F90 should classify as code, got %q", got)
	}
	if got := ClassifyFile("module.f90"); got != FileTypeCode {
		t.Errorf(".f90 should classify as code, got %q", got)
	}
}

func TestNodeFileTypeJSONRoundTrip(t *testing.T) {
	// FileType must round-trip cleanly; omitempty must omit the empty
	// value (so pre-existing graph.json files without the field can
	// deserialize without losing data).
	type wrap struct {
		Node Node `json:"node"`
	}
	in := wrap{Node: Node{ID: "x", Label: "X", FileType: FileTypeCode}}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out wrap
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out.Node.FileType != FileTypeCode {
		t.Fatalf("FileType lost in round-trip: %q", out.Node.FileType)
	}

	// Empty FileType must be omitted from the JSON output so existing
	// readers don't fail on a `"FileType":""` key they don't expect.
	emp := wrap{Node: Node{ID: "y", Label: "Y"}}
	data2, _ := json.Marshal(emp)
	if bytes.Contains(data2, []byte(`"FileType"`)) {
		t.Fatalf("empty FileType must omitempty: %s", data2)
	}
}
