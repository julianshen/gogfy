package schema

import "errors"

type Confidence string

const (
	Extracted Confidence = "EXTRACTED"
	Inferred  Confidence = "INFERRED"
	Ambiguous Confidence = "AMBIGUOUS"
)

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
	return nil
}

type Edge struct {
	Source     string
	Target     string
	Relation   string
	Confidence Confidence
}

func (e Edge) Validate() error {
	if e.Source == "" || e.Target == "" {
		return errors.New("edge source and target required")
	}
	return nil
}
