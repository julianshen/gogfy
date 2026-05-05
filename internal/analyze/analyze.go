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

	type nodeDegree struct {
		node   schema.Node
		degree int
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

	godNodes := make([]schema.Node, 0, len(nd))
	for _, item := range nd {
		if item.degree > 0 {
			godNodes = append(godNodes, item.node)
		}
	}

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
