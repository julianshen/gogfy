package fence

import (
	"bytes"
	"strings"
	"testing"
)

const (
	startMarker = "<!-- start -->"
	endMarker   = "<!-- end -->"
)

func TestIndexAllNoMatch(t *testing.T) {
	if got := IndexAll([]byte("hello"), []byte("xyz")); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestIndexAllMultipleMatches(t *testing.T) {
	got := IndexAll([]byte("ababab"), []byte("ab"))
	want := []int{0, 2, 4}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestReplaceNoMarkersReturnsNotFound(t *testing.T) {
	existing := []byte("hello world")
	got, replaced, err := Replace(existing, []byte(startMarker), []byte(endMarker), []byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	if replaced {
		t.Fatal("expected replaced=false when markers absent")
	}
	if !bytes.Equal(got, existing) {
		t.Fatalf("expected unchanged input, got %q", got)
	}
}

func TestReplaceSingleMatchedPair(t *testing.T) {
	existing := []byte("before " + startMarker + "old" + endMarker + " after")
	rendered := []byte(startMarker + "new" + endMarker)
	got, replaced, err := Replace(existing, []byte(startMarker), []byte(endMarker), rendered)
	if err != nil {
		t.Fatal(err)
	}
	if !replaced {
		t.Fatal("expected replaced=true")
	}
	if string(got) != "before "+startMarker+"new"+endMarker+" after" {
		t.Fatalf("unexpected: %q", got)
	}
}

func TestReplaceConsumesTrailingNewline(t *testing.T) {
	// Trailing newline after the end marker is consumed so repeated
	// replacements don't accumulate blank lines.
	existing := []byte("# top\n" + startMarker + "\nold\n" + endMarker + "\n## next\n")
	rendered := []byte(startMarker + "\nnew\n" + endMarker + "\n")
	got, replaced, _ := Replace(existing, []byte(startMarker), []byte(endMarker), rendered)
	if !replaced {
		t.Fatal("expected replaced=true")
	}
	if !strings.Contains(string(got), "## next") {
		t.Fatalf("trailing content lost: %q", got)
	}
	if strings.Contains(string(got), "\n\n## next") {
		t.Fatalf("blank line accumulated before next section: %q", got)
	}
}

func TestReplaceRefusesMultipleStarts(t *testing.T) {
	existing := []byte(startMarker + "a" + endMarker + " " + startMarker + "b" + endMarker)
	_, _, err := Replace(existing, []byte(startMarker), []byte(endMarker), []byte("x"))
	if err == nil {
		t.Fatal("expected error on multiple start markers")
	}
}

func TestReplaceRefusesEndBeforeStart(t *testing.T) {
	existing := []byte(endMarker + " stuff " + startMarker)
	_, _, err := Replace(existing, []byte(startMarker), []byte(endMarker), []byte("x"))
	if err == nil {
		t.Fatal("expected error on end-before-start")
	}
}

func TestStripNoMarkersReturnsNotFound(t *testing.T) {
	existing := []byte("hello")
	got, removed, err := Strip(existing, []byte(startMarker), []byte(endMarker))
	if err != nil {
		t.Fatal(err)
	}
	if removed {
		t.Fatal("expected removed=false")
	}
	if !bytes.Equal(got, existing) {
		t.Fatalf("expected unchanged, got %q", got)
	}
}

func TestStripSingleMatchedPair(t *testing.T) {
	existing := []byte("# top\n\n" + startMarker + "\nfenced\n" + endMarker + "\n## next\n")
	got, removed, err := Strip(existing, []byte(startMarker), []byte(endMarker))
	if err != nil {
		t.Fatal(err)
	}
	if !removed {
		t.Fatal("expected removed=true")
	}
	s := string(got)
	if strings.Contains(s, "fenced") || strings.Contains(s, startMarker) {
		t.Fatalf("fenced content not removed: %q", s)
	}
	if !strings.Contains(s, "# top") || !strings.Contains(s, "## next") {
		t.Fatalf("surrounding content erased: %q", s)
	}
	// Must not leave a double-blank gap at the seam.
	if strings.Contains(s, "\n\n\n") {
		t.Fatalf("triple-newline at seam: %q", s)
	}
}

func TestStripRefusesMismatchedMarkers(t *testing.T) {
	existing := []byte(startMarker + " orphan with no end")
	_, _, err := Strip(existing, []byte(startMarker), []byte(endMarker))
	if err == nil {
		t.Fatal("expected error on orphan start")
	}
}

func TestReplaceWithDifferentMarkerKinds(t *testing.T) {
	// The package must work with shell-style markers too (used by githook).
	const shStart = "# go-start"
	const shEnd = "# go-end"
	existing := []byte("#!/bin/sh\nfoo\n" + shStart + "\nold\n" + shEnd + "\n")
	rendered := []byte(shStart + "\nnew\n" + shEnd + "\n")
	got, replaced, _ := Replace(existing, []byte(shStart), []byte(shEnd), rendered)
	if !replaced {
		t.Fatal("expected replaced=true")
	}
	if !strings.Contains(string(got), "new") || strings.Contains(string(got), "old") {
		t.Fatalf("replacement failed: %q", got)
	}
}

func TestNReplacementsAreFixedPoint(t *testing.T) {
	existing := []byte("# top\n\n" + startMarker + "\nv1\n" + endMarker + "\n# bottom\n")
	rendered := []byte(startMarker + "\nv2\n" + endMarker + "\n")
	first, _, _ := Replace(existing, []byte(startMarker), []byte(endMarker), rendered)
	got := first
	for i := 0; i < 5; i++ {
		got, _, _ = Replace(got, []byte(startMarker), []byte(endMarker), rendered)
	}
	if !bytes.Equal(first, got) {
		t.Fatalf("5 reinstalls drifted from 1:\nfirst:  %q\nafter5: %q", first, got)
	}
}
