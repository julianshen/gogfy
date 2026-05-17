package schema

import (
	"encoding/json"
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

func TestEdgeMarshalJSONIncludesConfidenceScore(t *testing.T) {
	cases := map[Confidence]float64{
		Extracted: 1.0,
		Inferred:  0.5,
		Ambiguous: 0.25,
	}
	for c, want := range cases {
		e := Edge{Source: "a", Target: "b", Relation: "calls", Confidence: c}
		data, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		var decoded struct {
			ConfidenceScore float64 `json:"confidence_score"`
			Confidence      int     `json:"Confidence"`
		}
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded.ConfidenceScore != want {
			t.Errorf("%v: confidence_score = %v, want %v (raw: %s)", c, decoded.ConfidenceScore, want, data)
		}
		if Confidence(decoded.Confidence) != c {
			t.Errorf("%v: Confidence int field lost: %d", c, decoded.Confidence)
		}
	}
}

func TestEdgeJSONRoundTripPreservesConfidence(t *testing.T) {
	// Marshal then unmarshal must still produce the original enum
	// value — the new confidence_score field is additive metadata, the
	// int Confidence stays authoritative.
	original := Edge{Source: "a", Target: "b", Relation: "calls", Confidence: Ambiguous}
	data, _ := json.Marshal(original)
	var decoded Edge
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Confidence != Ambiguous {
		t.Fatalf("round-trip: got %v, want Ambiguous", decoded.Confidence)
	}
}

func TestIndexNodesReturnsAllNodes(t *testing.T) {
	nodes := []Node{
		{ID: "a", Label: "A"},
		{ID: "b", Label: "B"},
	}
	got := IndexNodes(nodes)
	if len(got) != 2 || got["a"].Label != "A" || got["b"].Label != "B" {
		t.Fatalf("IndexNodes: got %+v", got)
	}
}

func TestNodeDisplayLabelFallsBackToID(t *testing.T) {
	withLabel := Node{ID: "x", Label: "Pretty"}
	withoutLabel := Node{ID: "raw-id"}
	if withLabel.DisplayLabel() != "Pretty" {
		t.Errorf("with-label: got %q want Pretty", withLabel.DisplayLabel())
	}
	if withoutLabel.DisplayLabel() != "raw-id" {
		t.Errorf("without-label: got %q want raw-id", withoutLabel.DisplayLabel())
	}
}
