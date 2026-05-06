package schema

import (
	"errors"
	"fmt"
)

type Confidence int

const (
	Extracted Confidence = iota
	Inferred
	Ambiguous
)

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

func (c Confidence) Validate() error {
	switch c {
	case Extracted, Inferred, Ambiguous:
		return nil
	default:
		return fmt.Errorf("invalid confidence: %s", c)
	}
}

type Node struct {
	ID             string
	Label          string
	SourceFile     string
	SourceLocation string
	Community      string
}

func (n Node) Validate() error {
	if n.ID == "" {
		return errors.New("node ID required")
	}
	if n.Label == "" {
		return errors.New("node label required")
	}
	return nil
}

type Edge struct {
	Source     string
	Target     string
	Relation   string
	Confidence Confidence
}

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
