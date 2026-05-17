// Package schema defines the core data types for the gogfy graph model.
package schema

import (
	"encoding/json"
	"errors"
	"fmt"
)

// jsonMarshalEdge is a package-local indirection so Edge.MarshalJSON
// can encode the shadow struct without recursing into itself.
var jsonMarshalEdge = func(e edgeJSON) ([]byte, error) { return json.Marshal(e) }

// Confidence indicates how a relationship was determined.
type Confidence int

const (
	// Extracted means the relationship was directly extracted from source code.
	Extracted Confidence = iota
	// Inferred means the relationship was inferred by analysis.
	Inferred
	// Ambiguous means the relationship could not be clearly determined.
	Ambiguous
)

// String returns the string representation of the Confidence value.
func (c Confidence) String() string {
	switch c {
	case Extracted:
		return "EXTRACTED"
	case Inferred:
		return "INFERRED"
	case Ambiguous:
		return "AMBIGUOUS"
	default:
		return fmt.Sprintf("Confidence(%d)", c)
	}
}

// Validate checks that the Confidence value is valid.
func (c Confidence) Validate() error {
	switch c {
	case Extracted, Inferred, Ambiguous:
		return nil
	default:
		return fmt.Errorf("invalid confidence: %s", c)
	}
}

// Node represents an entity in the graph (e.g., a package or function).
type Node struct {
	ID             string
	Label          string
	SourceFile     string
	SourceLocation string
	Community      string
	// FileType, when set, is derived from SourceFile's extension via
	// ClassifyFile. Stored on the node so cross-tool consumers
	// (callflow/wiki/report) can group/filter without re-running the
	// classifier and so a manually-overridden value can survive.
	FileType FileType `json:",omitempty"`
	// ExternalURL points at the actual content for nodes whose
	// SourceFile is just a pointer (Google Workspace shortcuts today;
	// could later carry arXiv URLs, GitHub permalinks, etc.).
	// SourceLocation is reserved for source-position strings like
	// "L12" so a URL would be misleading there.
	ExternalURL string `json:",omitempty"`
}

// Validate checks that the Node has the required fields populated.
func (n Node) Validate() error {
	if n.ID == "" {
		return errors.New("node ID required")
	}
	if n.Label == "" {
		return errors.New("node label required")
	}
	return nil
}

// Edge represents a relationship between two nodes in the graph.
type Edge struct {
	Source     string
	Target     string
	Relation   string
	Confidence Confidence
}

// Score returns a normalized [0..1] weight derived from the confidence
// tier. Mirrors graphify's `confidence_score` so cross-tool consumers
// reading graph.json can sort edges numerically without having to
// translate the enum themselves.
func (c Confidence) Score() float64 {
	switch c {
	case Extracted:
		return 1.0
	case Inferred:
		return 0.5
	case Ambiguous:
		return 0.25
	}
	return 0
}

// MarshalJSON adds a derived `confidence_score` field next to the
// existing `Confidence` int so external consumers get the numeric
// weight without re-implementing the tier→score map. Unmarshaling
// stays unchanged — the field is informational, the int is canonical.
//
// edgeJSON is a separate type to avoid infinite recursion through
// MarshalJSON when Go encodes the struct.
type edgeJSON struct {
	Source          string     `json:"Source"`
	Target          string     `json:"Target"`
	Relation        string     `json:"Relation"`
	Confidence      Confidence `json:"Confidence"`
	ConfidenceScore float64    `json:"confidence_score"`
}

// MarshalJSON implements json.Marshaler. The shadow `edgeJSON` carries
// the same fields plus the derived score; Edge's own JSON shape stays
// stable for existing readers (the original int Confidence is still
// present and authoritative).
func (e Edge) MarshalJSON() ([]byte, error) {
	return jsonMarshalEdge(edgeJSON{
		Source:          e.Source,
		Target:          e.Target,
		Relation:        e.Relation,
		Confidence:      e.Confidence,
		ConfidenceScore: e.Confidence.Score(),
	})
}

// Validate checks that the Edge has the required fields populated and valid confidence.
func (e Edge) Validate() error {
	if e.Source == "" {
		return errors.New("edge source required")
	}
	if e.Target == "" {
		return errors.New("edge target required")
	}
	if e.Relation == "" {
		return errors.New("edge relation required")
	}
	if err := e.Confidence.Validate(); err != nil {
		return err
	}
	return nil
}
