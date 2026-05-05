package report

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/julianshen/gogfy/internal/analyze"
)

func Render(r analyze.Report) ([]byte, error) {
	var b bytes.Buffer
	fmt.Fprintf(&b, "# Graph Report\n\n")

	fmt.Fprintf(&b, "## God Nodes\n")
	if len(r.GodNodes) == 0 {
		fmt.Fprintf(&b, "_None found_\n")
	} else {
		for _, n := range r.GodNodes {
			fmt.Fprintf(&b, "- %s\n", escapeMarkdown(n.Label))
		}
	}

	fmt.Fprintf(&b, "\n## Surprising Links\n")
	if len(r.SurprisingLinks) == 0 {
		fmt.Fprintf(&b, "_None found_\n")
	} else {
		for _, e := range r.SurprisingLinks {
			fmt.Fprintf(&b, "- %s -> %s (%s)\n", escapeMarkdown(e.Source), escapeMarkdown(e.Target), escapeMarkdown(e.Relation))
		}
	}

	fmt.Fprintf(&b, "\n## Exploration Questions\n")
	if len(r.ExplorationQuestions) == 0 {
		fmt.Fprintf(&b, "_None found_\n")
	} else {
		for _, q := range r.ExplorationQuestions {
			fmt.Fprintf(&b, "- %s\n", q)
		}
	}

	return b.Bytes(), nil
}

func escapeMarkdown(s string) string {
	s = strings.ReplaceAll(s, "*", "\\*")
	s = strings.ReplaceAll(s, "_", "\\_")
	return s
}
