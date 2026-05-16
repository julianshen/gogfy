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
	// Bumped from 5 to 10: gogfy now generates 7 question types
	// (god, ambiguous, verify-inferred, isolated, low-cohesion,
	// no-signal, community-bridge). At 5 the cap silently dropped
	// most non-god types in graphs that triggered several at once.
	MaxExplorationQuestions = 10

	// lowCohesionThreshold mirrors the cluster-splitter's threshold
	// (internal/cluster) so the "should we split?" question only
	// fires for communities the splitter would itself target.
	lowCohesionThreshold = 0.05
	// minLowCohesionMembers avoids noise for tiny communities where
	// cohesion math is dominated by integer rounding.
	minLowCohesionMembers = 5
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

	// No-signal short-circuits: if we have nothing to talk about, the
	// only useful prompt is "did extraction fail?". Skipping the other
	// generators here avoids emitting questions that just look broken.
	if len(edges) == 0 {
		return Report{
			GodNodes:             godNodes,
			SurprisingLinks:      surprising,
			ExplorationQuestions: []string{noSignalQuestion(len(nodes))},
			ConfidenceSummary:    confidence,
		}
	}

	questions := buildQuestions(nodes, edges, nodeMap, degree, godNodes, surprising)
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

// buildQuestions emits up to a small per-category budget so a graph with
// many ambiguous edges (or one with many god nodes) can't crowd out the
// other types. The order here is intentional: higher-signal prompts come
// first so callers that truncate retain the most useful ones.
func buildQuestions(
	nodes []schema.Node,
	edges []schema.Edge,
	nodeMap map[string]schema.Node,
	degree map[string]int,
	godNodes []schema.Node,
	surprising []schema.Edge,
) []string {
	const perCategoryBudget = 2

	var qs []string

	// God-node role questions — kept first for behavioral compatibility
	// with the prior shape (existing wiki/report tests pin this order).
	for i, gn := range godNodes {
		if i >= perCategoryBudget {
			break
		}
		label := labelOrID(gn)
		if label != "" {
			qs = append(qs, "What is the role of "+label+"?")
		}
	}

	// Ambiguous (pinned uncertainty) and Inferred (verify) edges share
	// the same lookup; one pass with two counters keeps them separable
	// but avoids walking the edge list twice.
	var ambigCount, inferCount int
	for _, e := range edges {
		if ambigCount >= perCategoryBudget && inferCount >= perCategoryBudget {
			break
		}
		s, t := labelOrID(nodeMap[e.Source]), labelOrID(nodeMap[e.Target])
		if s == "" || t == "" {
			continue
		}
		switch e.Confidence {
		case schema.Ambiguous:
			if ambigCount < perCategoryBudget {
				qs = append(qs, "Is the ambiguous edge "+s+" "+relationOrDefault(e)+" "+t+" accurate?")
				ambigCount++
			}
		case schema.Inferred:
			if inferCount < perCategoryBudget {
				qs = append(qs, "Verify inferred edge: does "+s+" "+relationOrDefault(e)+" "+t+"?")
				inferCount++
			}
		}
	}

	// Isolated nodes — degree-0 in the parsed graph. Sorted by label so
	// output is deterministic across runs.
	var isolated []schema.Node
	for _, n := range nodes {
		if degree[n.ID] == 0 {
			isolated = append(isolated, n)
		}
	}
	sort.Slice(isolated, func(i, j int) bool {
		return labelOrID(isolated[i]) < labelOrID(isolated[j])
	})
	for i, n := range isolated {
		if i >= perCategoryBudget {
			break
		}
		label := labelOrID(n)
		if label != "" {
			qs = append(qs, "Is the isolated node "+label+" actually unconnected, or did extraction miss its edges?")
		}
	}

	// Low-cohesion communities — same heuristic the cluster splitter
	// uses, so the prompt aligns with that tool's recommendation.
	for _, cid := range lowCohesionCommunities(nodes, edges, nodeMap) {
		if len(qs) >= MaxExplorationQuestions {
			break
		}
		qs = append(qs, "Community "+cid+" has low cohesion — should it be split?")
	}

	// Community-bridge questions — least specific (label often a number),
	// so they go last and only fill remaining slots.
	communityPairs := make(map[[2]string]struct{})
	for _, e := range surprising {
		if len(qs) >= MaxExplorationQuestions {
			break
		}
		src := nodeMap[e.Source]
		dst := nodeMap[e.Target]
		pair := [2]string{src.Community, dst.Community}
		if pair[0] > pair[1] {
			pair[0], pair[1] = pair[1], pair[0]
		}
		if _, seen := communityPairs[pair]; seen {
			continue
		}
		communityPairs[pair] = struct{}{}
		qs = append(qs, "Why does "+pair[0]+" connect to "+pair[1]+"?")
	}

	return qs
}

// lowCohesionCommunities returns community IDs whose intra-edge density
// falls below lowCohesionThreshold. Density = intra-edges / max-possible.
// Communities under minLowCohesionMembers are skipped — at that size the
// metric is dominated by integer rounding noise.
func lowCohesionCommunities(nodes []schema.Node, edges []schema.Edge, nodeMap map[string]schema.Node) []string {
	community := map[string]int{}
	for _, n := range nodes {
		if n.Community == "" {
			continue
		}
		community[n.Community]++
	}
	intra := map[string]int{}
	for _, e := range edges {
		if e.Source == e.Target {
			continue
		}
		sc := nodeMap[e.Source].Community
		if sc != "" && sc == nodeMap[e.Target].Community {
			intra[sc]++
		}
	}
	var out []string
	for cid, n := range community {
		if n < minLowCohesionMembers {
			continue
		}
		maxEdges := n * (n - 1) / 2
		if maxEdges == 0 {
			continue
		}
		density := float64(intra[cid]) / float64(maxEdges)
		if density < lowCohesionThreshold {
			out = append(out, cid)
		}
	}
	sort.Strings(out)
	return out
}

func noSignalQuestion(nodeCount int) string {
	if nodeCount == 0 {
		return "No relationships detected — is the corpus indexed correctly?"
	}
	return "No relationships detected despite parsed nodes — did extraction fail?"
}

// relationOrDefault gives unrelated relations a stable placeholder so the
// generated questions read naturally even when extraction left Relation
// blank (extractors are allowed to omit it for ambiguous matches).
func relationOrDefault(e schema.Edge) string {
	if e.Relation != "" {
		return e.Relation
	}
	return "relates_to"
}

func labelOrID(n schema.Node) string {
	if n.Label != "" {
		return n.Label
	}
	return n.ID
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
