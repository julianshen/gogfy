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
	n := Node{ID: "", Label: "test"}
	if err := n.Validate(); err == nil {
		t.Fatal("expected error for empty ID")
	}
}
