// Package schema defines the core data types for the gogfy graph model.
package schema

import (
	"errors"
	"fmt"
)

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
