// Package analyze provides graph analysis to identify notable nodes and cross-community edges.
package analyze

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/julianshen/gogfy/internal/centrality"
	"github.com/julianshen/gogfy/internal/schema"
)

// MaxGodNodes / MaxSurprisingLinks / MaxExplorationQuestions cap the
// per-section sizes so reports stay digestible for AI assistants whose
// context windows we don't want to flood. Ranking decides what survives.
const (
	MaxGodNodes             = 10
	MaxSurprisingLinks      = 10
	// Sized for 7 question categories with a per-category budget; a
	// tighter cap silently starves non-god types when several fire.
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
	// BridgeNodes are the top-betweenness nodes — the ones whose
	// removal would partition the graph into otherwise-disconnected
	// components. Distinct from GodNodes (which uses degree): a bridge
	// can be low-degree yet sit on every cross-cluster path.
	BridgeNodes []schema.Node
}

// MaxBridgeNodes caps the BridgeNodes list. Three is enough to spotlight
// the cross-cutting concerns without burying the rest of the report.
const MaxBridgeNodes = 3

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

	bridgeNodes := pickBridgeNodes(nodes, edges, nodeMap)

	return Report{
		GodNodes:             godNodes,
		SurprisingLinks:      surprising,
		ExplorationQuestions: questions,
		ConfidenceSummary:    confidence,
		BridgeNodes:          bridgeNodes,
	}
}

// rankSurprising returns cross-community edges ordered by descending
// composite surprise score. Components (graphify-parity):
//
//   - Confidence bonus: AMBIGUOUS=3, INFERRED=2, EXTRACTED=1. Cross-
//     language INFERRED `calls` is zeroed — usually resolver pollution,
//     not real surprise.
//   - Cross file-type: +2 (code↔doc/paper/image is non-obvious).
//   - Cross-repo: +2 (top-level dir differs).
//   - Cross-community: +1 (Leiden says these are structurally distant).
//   - Semantic similarity (relation == "semantically_similar_to"):
//     score *= 1.5 (truncated).
//   - Peripheral→hub: +1 when min(degree) ≤ 2 and max(degree) ≥ 5.
//
// Ties broken by the legacy inverse-log-degree score (so within a
// composite tier, leaf-to-leaf edges still outrank hub-touching ones)
// then by input-edge index for byte-stable output.
func rankSurprising(edges []schema.Edge, nodeMap map[string]schema.Node, degree map[string]int) []schema.Edge {
	type scored struct {
		edge      schema.Edge
		composite int
		tieScore  float64
		idx       int
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
		du, dv := degree[e.Source], degree[e.Target]
		ranked = append(ranked, scored{
			edge:      e,
			composite: surpriseScore(e, src, dst, du, dv),
			tieScore:  1.0 / (ds * dt),
			idx:       i,
		})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].composite != ranked[j].composite {
			return ranked[i].composite > ranked[j].composite
		}
		if ranked[i].tieScore != ranked[j].tieScore {
			return ranked[i].tieScore > ranked[j].tieScore
		}
		return ranked[i].idx < ranked[j].idx
	})
	out := make([]schema.Edge, len(ranked))
	for i, r := range ranked {
		out[i] = r.edge
	}
	return out
}

// surpriseScore computes the integer composite. Caller has already
// verified src.Community != dst.Community, so a constant cross-community
// offset isn't included — it'd shift every score equally and not affect
// ranking.
func surpriseScore(e schema.Edge, src, dst schema.Node, du, dv int) int {
	score := 0
	confBonus := 1
	switch e.Confidence {
	case schema.Ambiguous:
		confBonus = 3
	case schema.Inferred:
		confBonus = 2
	}
	// Cross-language INFERRED `calls` are usually resolver pollution
	// — don't promote likely false positives.
	if e.Confidence == schema.Inferred && e.Relation == "calls" && crossLanguage(src.ID, dst.ID) {
		confBonus = 0
	}
	score += confBonus

	if src.FileType != "" && dst.FileType != "" && src.FileType != dst.FileType {
		score += 2
	}
	// Only award cross-repo when BOTH paths are known — otherwise an
	// extractor that left SourceFile blank on one side would
	// systematically inflate scores against nodes that happen to have
	// classification gaps.
	if src.SourceFile != "" && dst.SourceFile != "" && topLevelDir(src.SourceFile) != topLevelDir(dst.SourceFile) {
		score += 2
	}

	if e.Relation == "semantically_similar_to" {
		score = (score * 3) / 2
	}

	if min(du, dv) <= 2 && max(du, dv) >= 5 {
		score++
	}
	return score
}

// crossLanguage compares only the colon-separated lang prefix of two
// LangIDs. Returns false when either side isn't a LangID — external
// nodes (no lang prefix) are conservatively not flagged.
func crossLanguage(a, b string) bool {
	ai := strings.IndexByte(a, ':')
	bi := strings.IndexByte(b, ':')
	if ai < 0 || bi < 0 {
		return false
	}
	return a[:ai] != b[:bi]
}

// topLevelDir returns the first path component, used as the cross-repo
// proxy (a monorepo with `a/` and `b/` top-levels treats those as
// distinct subrepos). Empty inputs return "" so two nodes both lacking
// SourceFile aren't credited a cross-repo bonus.
func topLevelDir(path string) string {
	head, _, _ := strings.Cut(path, "/")
	return head
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

	// God-node questions first: wiki/report tests assert this position.
	for i, gn := range godNodes {
		if i >= perCategoryBudget {
			break
		}
		label := labelOrID(gn)
		if label != "" {
			qs = append(qs, "What is the role of "+label+"?")
		}
	}

	// Sort before truncation: input edges may arrive in non-deterministic
	// map-iteration order, so the picked-2 would otherwise drift run-to-run.
	ambig := sortedEdgeQuestions(edges, nodeMap, schema.Ambiguous, perCategoryBudget,
		func(s, t, r string) string { return "Is the ambiguous edge " + s + " " + r + " " + t + " accurate?" })
	qs = append(qs, ambig...)
	infer := sortedEdgeQuestions(edges, nodeMap, schema.Inferred, perCategoryBudget,
		func(s, t, r string) string { return "Verify inferred edge: does " + s + " " + r + " " + t + "?" })
	qs = append(qs, infer...)

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

// sortedEdgeQuestions filters edges by confidence and emits up to budget
// questions in (source-label, target-label, relation) order so the output
// is independent of input edge ordering.
func sortedEdgeQuestions(edges []schema.Edge, nodeMap map[string]schema.Node, want schema.Confidence, budget int, render func(s, t, rel string) string) []string {
	type cand struct{ s, t, r string }
	var cs []cand
	for _, e := range edges {
		if e.Confidence != want {
			continue
		}
		s, t := labelOrID(nodeMap[e.Source]), labelOrID(nodeMap[e.Target])
		if s == "" || t == "" {
			continue
		}
		cs = append(cs, cand{s, t, relationOrDefault(e)})
	}
	sort.Slice(cs, func(i, j int) bool {
		if cs[i].s != cs[j].s {
			return cs[i].s < cs[j].s
		}
		if cs[i].t != cs[j].t {
			return cs[i].t < cs[j].t
		}
		return cs[i].r < cs[j].r
	})
	if len(cs) > budget {
		cs = cs[:budget]
	}
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = render(c.s, c.t, c.r)
	}
	return out
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
	// Dedupe by unordered endpoint pair so reciprocal extractions
	// (A→B + B→A) and parallel edges don't inflate intra-counts past
	// the undirected n*(n-1)/2 max, which would skew density and let
	// genuinely sparse communities slip past the threshold.
	type pair struct{ a, b string }
	seen := map[pair]struct{}{}
	intra := map[string]int{}
	for _, e := range edges {
		if e.Source == e.Target {
			continue
		}
		sc := nodeMap[e.Source].Community
		if sc == "" || sc != nodeMap[e.Target].Community {
			continue
		}
		a, b := e.Source, e.Target
		if a > b {
			a, b = b, a
		}
		key := pair{a, b}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		intra[sc]++
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
	return fmt.Sprintf("No relationships detected despite %d parsed nodes — did extraction fail?", nodeCount)
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

// pickBridgeNodes computes betweenness centrality and returns the
// top-MaxBridgeNodes nodes that connect otherwise-distant parts of the
// graph. Filters out endpoints (zero score) so a graph with no
// structurally interesting bridges produces an empty list rather than
// arbitrary fillers. Ties broken by node ID so output is deterministic.
func pickBridgeNodes(nodes []schema.Node, edges []schema.Edge, nodeMap map[string]schema.Node) []schema.Node {
	if len(edges) == 0 {
		return nil
	}
	scores := centrality.Betweenness(nodes, edges)
	type scored struct {
		id    string
		score float64
	}
	cands := make([]scored, 0, len(scores))
	for id, s := range scores {
		if s <= 0 {
			continue
		}
		cands = append(cands, scored{id, s})
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].score != cands[j].score {
			return cands[i].score > cands[j].score
		}
		return cands[i].id < cands[j].id
	})
	if len(cands) > MaxBridgeNodes {
		cands = cands[:MaxBridgeNodes]
	}
	out := make([]schema.Node, 0, len(cands))
	for _, c := range cands {
		out = append(out, nodeMap[c.id])
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
