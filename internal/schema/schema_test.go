package schema

import (
	"strings"
	"testing"
)

func TestConfidenceEnum(t *testing.T) {
	cases := []struct {
		name     string
		value    Confidence
		expected string
	}{
		{"Extracted", Extracted, "EXTRACTED"},
		{"Inferred", Inferred, "INFERRED"},
		{"Ambiguous", Ambiguous, "AMBIGUOUS"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.value.String() != tc.expected {
				t.Fatalf("expected %q, got %q", tc.expected, tc.value.String())
			}
		})
	}
}

func TestConfidenceValidate(t *testing.T) {
	cases := []struct {
		name    string
		value   Confidence
		wantErr bool
	}{
		{"Extracted", Extracted, false},
		{"Inferred", Inferred, false},
		{"Ambiguous", Ambiguous, false},
		{"invalid", Confidence(999), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.value.Validate()
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				if !strings.Contains(err.Error(), "invalid confidence") {
					t.Fatalf("unexpected error message: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestNodeValidation(t *testing.T) {
	cases := []struct {
		name    string
		node    Node
		wantErr string
	}{
		{"empty ID", Node{ID: "", Label: "test"}, "node ID required"},
		{"empty label", Node{ID: "pkg:main", Label: ""}, "node label required"},
		{"valid", Node{ID: "pkg:main", Label: "main"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.node.Validate()
			if tc.wantErr != "" {
				if err == nil {
					t.Fatal("expected error")
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestEdgeValidation(t *testing.T) {
	cases := []struct {
		name    string
		edge    Edge
		wantErr string
	}{
		{"empty source", Edge{Source: "", Target: "b", Relation: "imports", Confidence: Extracted}, "edge source required"},
		{"empty target", Edge{Source: "a", Target: "", Relation: "imports", Confidence: Extracted}, "edge target required"},
		{"empty relation", Edge{Source: "a", Target: "b", Relation: "", Confidence: Extracted}, "edge relation required"},
		{"invalid confidence", Edge{Source: "a", Target: "b", Relation: "imports", Confidence: Confidence(999)}, "invalid confidence"},
		{"valid", Edge{Source: "a", Target: "b", Relation: "imports", Confidence: Extracted}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.edge.Validate()
			if tc.wantErr != "" {
				if err == nil {
					t.Fatal("expected error")
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
