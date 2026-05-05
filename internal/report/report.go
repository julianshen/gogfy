package report

import (
	"bytes"
	"fmt"
	"github.com/julianshen/gogfy/internal/analyze"
)

func Render(r analyze.Report) ([]byte, error) {
	var b bytes.Buffer
	fmt.Fprintf(&b, "# Graph Report\n\n")
	fmt.Fprintf(&b, "## God Nodes\n")
	for _, n := range r.GodNodes {
		fmt.Fprintf(&b, "- %s\n", n.Label)
	}
	fmt.Fprintf(&b, "\n## Surprising Links\n")
	for _, e := range r.SurprisingLinks {
		fmt.Fprintf(&b, "- %s -> %s (%s)\n", e.Source, e.Target, e.Relation)
	}
	fmt.Fprintf(&b, "\n## Exploration Questions\n")
	for _, q := range r.ExplorationQuestions {
		fmt.Fprintf(&b, "- %s\n", q)
	}
	return b.Bytes(), nil
}
