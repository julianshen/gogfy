package schema

import "testing"

func TestConfidenceEnum(t *testing.T) {
	if Extracted != "EXTRACTED" {
		t.Fatal("Extracted confidence mismatch")
	}
	if Inferred != "INFERRED" {
		t.Fatal("Inferred confidence mismatch")
	}
	if Ambiguous != "AMBIGUOUS" {
		t.Fatal("Ambiguous confidence mismatch")
	}
}

func TestNodeValidation(t *testing.T) {
	t.Run("empty ID returns error", func(t *testing.T) {
		n := Node{ID: "", Label: "test"}
		if err := n.Validate(); err == nil {
			t.Fatal("expected error for empty ID")
		}
	})
	t.Run("valid node returns no error", func(t *testing.T) {
		n := Node{ID: "pkg:main", Label: "main"}
		if err := n.Validate(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestEdgeValidation(t *testing.T) {
	t.Run("empty source returns error", func(t *testing.T) {
		e := Edge{Source: "", Target: "b", Relation: "imports"}
		if err := e.Validate(); err == nil {
			t.Fatal("expected error for empty source")
		}
	})
	t.Run("empty target returns error", func(t *testing.T) {
		e := Edge{Source: "a", Target: "", Relation: "imports"}
		if err := e.Validate(); err == nil {
			t.Fatal("expected error for empty target")
		}
	})
	t.Run("both empty returns error", func(t *testing.T) {
		e := Edge{Source: "", Target: "", Relation: "imports"}
		if err := e.Validate(); err == nil {
			t.Fatal("expected error for empty source and target")
		}
	})
	t.Run("valid edge returns no error", func(t *testing.T) {
		e := Edge{Source: "a", Target: "b", Relation: "imports"}
		if err := e.Validate(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}
