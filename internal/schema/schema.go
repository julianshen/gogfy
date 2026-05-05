package schema

import (
	"errors"
	"fmt"
)

type Confidence string

const (
	Extracted Confidence = "EXTRACTED"
	Inferred  Confidence = "INFERRED"
	Ambiguous Confidence = "AMBIGUOUS"
)

func (c Confidence) Validate() error {
	switch c {
	case Extracted, Inferred, Ambiguous:
		return nil
	default:
		return fmt.Errorf("invalid confidence: %q", c)
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
