// Package analyze provides graph analysis to identify notable nodes and cross-community edges.
package analyze

import (
	"math"
	"sort"

	"github.com/julianshen/gogfy/internal/schema"
)

// MaxGodNodes / MaxSurprisingLinks / MaxExplorationQuestions cap the
// per-section sizes so reports stay digestible for AI assistants whose
// context windows we don't want to flood. Ranking decides what survives.
const (
	MaxGodNodes             = 10
	MaxSurprisingLinks      = 10
	MaxExplorationQuestions = 5
)

// Report contains the findings of a graph analysis.
type Report struct {
	GodNodes             []schema.Node
	SurprisingLinks      []schema.Edge
	ExplorationQuestions []string
	// ConfidenceSummary counts edges per confidence level so the rendered
	// report can surface how much of the graph was directly extracted vs.
	// inferred or guessed.
	ConfidenceSummary map[schema.Confidence]int
}

type nodeDegree struct {
	node   schema.Node
	degree int
}

// Analyzer performs analysis on a graph to produce a Report.
type Analyzer struct{}

// NewAnalyzer creates a new Analyzer.
func NewAnalyzer() *Analyzer {
	return &Analyzer{}
}

// Analyze examines the provided nodes and edges and returns a Report with insights.
func (a *Analyzer) Analyze(nodes []schema.Node, edges []schema.Edge) Report {
	nodeMap := make(map[string]schema.Node, len(nodes))
	for _, n := range nodes {
		nodeMap[n.ID] = n
	}

	degree := make(map[string]int, len(nodes))
	confidence := map[schema.Confidence]int{}
	for _, e := range edges {
		degree[e.Source]++
		degree[e.Target]++
		confidence[e.Confidence]++
	}

	nd := make([]nodeDegree, 0, len(nodes))
	for _, n := range nodes {
		d := degree[n.ID]
		nd = append(nd, nodeDegree{node: n, degree: d})
	}
	sort.Slice(nd, func(i, j int) bool {
		if nd[i].degree != nd[j].degree {
			return nd[i].degree > nd[j].degree
		}
		return nd[i].node.ID < nd[j].node.ID
	})

	// God nodes: top 20% by degree, capped at MaxGodNodes.
	godNodes := filterGodNodes(nd)
	if len(godNodes) > MaxGodNodes {
		godNodes = godNodes[:MaxGodNodes]
	}

	surprising := rankSurprising(edges, nodeMap, degree)
	if len(surprising) > MaxSurprisingLinks {
		surprising = surprising[:MaxSurprisingLinks]
	}

	questions := []string{}
	for _, gn := range godNodes {
		label := gn.Label
		if label == "" {
			label = gn.ID
		}
		if label != "" {
			questions = append(questions, "What is the role of "+label+"?")
		}
	}
	communityPairs := make(map[[2]string]struct{})
	for _, e := range surprising {
		src := nodeMap[e.Source]
		dst := nodeMap[e.Target]
		pair := [2]string{src.Community, dst.Community}
		if pair[0] > pair[1] {
			pair[0], pair[1] = pair[1], pair[0]
		}
		if _, ok := communityPairs[pair]; !ok {
			communityPairs[pair] = struct{}{}
			questions = append(questions, "Why does "+pair[0]+" connect to "+pair[1]+"?")
		}
	}

	if len(questions) > MaxExplorationQuestions {
		questions = questions[:MaxExplorationQuestions]
	}

	return Report{
		GodNodes:             godNodes,
		SurprisingLinks:      surprising,
		ExplorationQuestions: questions,
		ConfidenceSummary:    confidence,
	}
}

// rankSurprising returns cross-community edges ordered by descending
// "surprise score" — the inverse of the product of the endpoints' log-degrees.
// Edges between low-degree nodes (leaves) outrank edges involving hubs, so a
// capped section retains the most genuinely unexpected connections.
func rankSurprising(edges []schema.Edge, nodeMap map[string]schema.Node, degree map[string]int) []schema.Edge {
	type scored struct {
		edge  schema.Edge
		score float64
		idx   int
	}
	ranked := make([]scored, 0, len(edges))
	for i, e := range edges {
		src := nodeMap[e.Source]
		dst := nodeMap[e.Target]
		if src.Community == "" || dst.Community == "" || src.Community == dst.Community {
			continue
		}
		ds := math.Log2(float64(degree[e.Source]) + 2)
		dt := math.Log2(float64(degree[e.Target]) + 2)
		ranked = append(ranked, scored{edge: e, score: 1.0 / (ds * dt), idx: i})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		// Stable secondary order: original edge order, so output is deterministic.
		return ranked[i].idx < ranked[j].idx
	})
	out := make([]schema.Edge, len(ranked))
	for i, r := range ranked {
		out[i] = r.edge
	}
	return out
}

func filterGodNodes(nd []nodeDegree) []schema.Node {
	if len(nd) == 0 {
		return nil
	}

	// Count connected nodes
	connected := 0
	for _, item := range nd {
		if item.degree > 0 {
			connected++
		}
	}
	if connected == 0 {
		return nil
	}

	// Top 20% of connected nodes, at least 1
	topN := connected / 5
	if topN < 1 {
		topN = 1
	}

	var result []schema.Node
	for i, item := range nd {
		if item.degree == 0 {
			continue
		}
		if i < topN {
			result = append(result, item.node)
		}
	}
	return result
}
