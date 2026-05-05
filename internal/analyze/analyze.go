package analyze

import (
	"sort"

	"github.com/julianshen/gogfy/internal/schema"
)

type Report struct {
	GodNodes             []schema.Node
	SurprisingLinks      []schema.Edge
	ExplorationQuestions []string
}

type nodeDegree struct {
	node   schema.Node
	degree int
}

type Analyzer struct{}

func NewAnalyzer() *Analyzer {
	return &Analyzer{}
}

func (a *Analyzer) Analyze(nodes []schema.Node, edges []schema.Edge) Report {
	nodeMap := make(map[string]schema.Node, len(nodes))
	for _, n := range nodes {
		nodeMap[n.ID] = n
	}

	degree := make(map[string]int, len(nodes))
	for _, e := range edges {
		degree[e.Source]++
		degree[e.Target]++
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

	// God nodes: top 20% or nodes with degree >= 2x median, whichever is more inclusive
	godNodes := filterGodNodes(nd)

	var surprising []schema.Edge
	for _, e := range edges {
		src := nodeMap[e.Source]
		dst := nodeMap[e.Target]
		if src.Community != "" && dst.Community != "" && src.Community != dst.Community {
			surprising = append(surprising, e)
		}
	}

	questions := []string{}
	for _, gn := range godNodes {
		label := gn.Label
		if label == "" {
			label = gn.ID
		}
		questions = append(questions, "What is the role of "+label+"?")
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

	return Report{
		GodNodes:             godNodes,
		SurprisingLinks:      surprising,
		ExplorationQuestions: questions,
	}
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
