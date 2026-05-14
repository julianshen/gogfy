package cluster

import (
	"fmt"
	"strings"
	"testing"

	"github.com/julianshen/gogfy/internal/schema"
)

// adjFromEdges builds a symmetric adjacency map (the form cohesionScore
// and splitCommunity expect) from a list of directed edges.
func adjFromEdges(edges []schema.Edge) map[string]map[string]float64 {
	adj := make(map[string]map[string]float64)
	ensure := func(id string) {
		if _, ok := adj[id]; !ok {
			adj[id] = make(map[string]float64)
		}
	}
	for _, e := range edges {
		ensure(e.Source)
		ensure(e.Target)
		adj[e.Source][e.Target] += 1.0
		adj[e.Target][e.Source] += 1.0
	}
	return adj
}

// cliqueEdges generates all pairwise edges for a complete graph of n nodes
// with the given ID prefix (e.g. "c1_" produces c1_0–c1_(n-1)).
func cliqueEdges(prefix string, n int) []schema.Edge {
	var edges []schema.Edge
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			edges = append(edges, schema.Edge{
				Source: fmt.Sprintf("%s%d", prefix, i),
				Target: fmt.Sprintf("%s%d", prefix, j),
			})
		}
	}
	return edges
}

func TestLeidenClustererAssignsCommunities(t *testing.T) {
	nodes := []schema.Node{
		{ID: "a"}, {ID: "b"}, {ID: "c"},
	}
	edges := []schema.Edge{
		{Source: "a", Target: "b"},
		{Source: "b", Target: "c"},
	}
	c := NewLeidenClusterer()
	result, err := c.Cluster(nodes, edges)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range result {
		if n.Community == "" {
			t.Fatalf("node %s missing community", n.ID)
		}
	}
}

func TestLeidenClustererConnectedNodesShareCommunity(t *testing.T) {
	nodes := []schema.Node{
		{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"},
	}
	edges := []schema.Edge{
		{Source: "a", Target: "b"},
		{Source: "b", Target: "c"},
	}
	c := NewLeidenClusterer()
	result, err := c.Cluster(nodes, edges)
	if err != nil {
		t.Fatal(err)
	}

	// a, b, c should share the same community (they're connected)
	communities := make(map[string]string)
	for _, n := range result {
		communities[n.ID] = n.Community
	}
	if communities["a"] != communities["b"] {
		t.Fatal("a and b should share community")
	}
	if communities["b"] != communities["c"] {
		t.Fatal("b and c should share community")
	}
	// d is isolated, should be in its own community
	if communities["d"] == communities["a"] {
		t.Fatal("d should be in a different community from a")
	}
}

func TestLeidenClustererIsolatedNodes(t *testing.T) {
	nodes := []schema.Node{
		{ID: "a"}, {ID: "b"}, {ID: "c"},
	}
	edges := []schema.Edge{}
	c := NewLeidenClusterer()
	result, err := c.Cluster(nodes, edges)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(result))
	}
	// Each isolated node should get its own community
	communities := make(map[string]bool)
	for _, n := range result {
		communities[n.Community] = true
	}
	if len(communities) != 3 {
		t.Fatalf("expected 3 communities, got %d", len(communities))
	}
}

func TestLeidenClustererDoesNotMutateInput(t *testing.T) {
	nodes := []schema.Node{
		{ID: "a"}, {ID: "b"},
	}
	edges := []schema.Edge{
		{Source: "a", Target: "b"},
	}
	c := NewLeidenClusterer()
	_, err := c.Cluster(nodes, edges)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range nodes {
		if n.Community != "" {
			t.Fatalf("input node %s was mutated", n.ID)
		}
	}
}

func TestLeidenClustererDeterministicOutput(t *testing.T) {
	nodes := []schema.Node{
		{ID: "c"}, {ID: "a"}, {ID: "b"},
	}
	edges := []schema.Edge{
		{Source: "a", Target: "b"},
	}
	c := NewLeidenClusterer()
	result1, err := c.Cluster(nodes, edges)
	if err != nil {
		t.Fatal(err)
	}
	result2, err := c.Cluster(nodes, edges)
	if err != nil {
		t.Fatal(err)
	}
	if len(result1) != len(result2) {
		t.Fatal("result lengths differ")
	}
	for i := range result1 {
		if result1[i].ID != result2[i].ID {
			t.Fatalf("node order differs at index %d", i)
		}
		if result1[i].Community != result2[i].Community {
			t.Fatalf("community differs for node %s", result1[i].ID)
		}
	}
}

func TestLeidenClustererTwoCliques(t *testing.T) {
	// Two distinct cliques that should be detected as separate communities
	nodes := []schema.Node{
		{ID: "a"}, {ID: "b"}, {ID: "c"},
		{ID: "d"}, {ID: "e"}, {ID: "f"},
	}
	edges := []schema.Edge{
		// Clique 1: a-b-c (triangle)
		{Source: "a", Target: "b"},
		{Source: "b", Target: "c"},
		{Source: "c", Target: "a"},
		// Clique 2: d-e-f (triangle)
		{Source: "d", Target: "e"},
		{Source: "e", Target: "f"},
		{Source: "f", Target: "d"},
	}
	c := NewLeidenClusterer()
	result, err := c.Cluster(nodes, edges)
	if err != nil {
		t.Fatal(err)
	}

	communities := make(map[string]string)
	for _, n := range result {
		communities[n.ID] = n.Community
	}

	// Nodes within each clique should share communities
	if communities["a"] != communities["b"] || communities["b"] != communities["c"] {
		t.Logf("communities: %v", communities)
		t.Fatal("clique 1 nodes should share a community")
	}
	if communities["d"] != communities["e"] || communities["e"] != communities["f"] {
		t.Logf("communities: %v", communities)
		t.Fatal("clique 2 nodes should share a community")
	}
	// The two cliques should be in different communities
	if communities["a"] == communities["d"] {
		t.Logf("communities: %v", communities)
		t.Fatal("two cliques should be in different communities")
	}
}

func TestLeidenClustererEmptyGraph(t *testing.T) {
	c := NewLeidenClusterer()
	result, err := c.Cluster([]schema.Node{}, []schema.Edge{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 0 {
		t.Fatalf("expected empty result, got %d nodes", len(result))
	}
}

func TestLeidenClustererAutoCreatesMissingNodes(t *testing.T) {
	// Leiden clusterer auto-creates nodes referenced by edges but missing from the node list
	nodes := []schema.Node{{ID: "a"}, {ID: "b"}}
	edges := []schema.Edge{{Source: "x", Target: "a"}}
	c := NewLeidenClusterer()
	result, err := c.Cluster(nodes, edges)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("expected 3 nodes (including auto-created x), got %d", len(result))
	}
}

func TestLeidenClustererAutoCreatesMissingTarget(t *testing.T) {
	nodes := []schema.Node{{ID: "a"}, {ID: "b"}}
	edges := []schema.Edge{{Source: "a", Target: "x"}}
	c := NewLeidenClusterer()
	result, err := c.Cluster(nodes, edges)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("expected 3 nodes (including auto-created x), got %d", len(result))
	}
}

func TestLeidenClustererOptions(t *testing.T) {
	c := NewLeidenClusterer(
		WithResolution(0.5),
		WithMaxIterations(50),
		WithMinModularityGain(0.001),
		WithRandomSeed(123),
	)
	nodes := []schema.Node{{ID: "a"}, {ID: "b"}}
	edges := []schema.Edge{{Source: "a", Target: "b"}}
	result, err := c.Cluster(nodes, edges)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(result))
	}
}

func TestLeidenClustererSingleNode(t *testing.T) {
	nodes := []schema.Node{{ID: "a"}}
	edges := []schema.Edge{}
	c := NewLeidenClusterer()
	result, err := c.Cluster(nodes, edges)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 node, got %d", len(result))
	}
	if result[0].Community == "" {
		t.Fatal("single node should have a community")
	}
}

func TestCohesionScorePathGraph(t *testing.T) {
	nodes := []string{"a", "b", "c", "d"}
	edges := []schema.Edge{
		{Source: "a", Target: "b"},
		{Source: "b", Target: "c"},
		{Source: "c", Target: "d"},
	}
	got := cohesionScore(nodes, adjFromEdges(edges))
	want := 0.5 // 3 edges / (4*3/2) = 3/6
	if got != want {
		t.Fatalf("cohesionScore = %v, want %v", got, want)
	}
}

func TestCohesionScoreCompleteGraph(t *testing.T) {
	nodes := []string{"a", "b", "c"}
	edges := []schema.Edge{
		{Source: "a", Target: "b"},
		{Source: "b", Target: "c"},
		{Source: "c", Target: "a"},
	}
	got := cohesionScore(nodes, adjFromEdges(edges))
	want := 1.0
	if got != want {
		t.Fatalf("cohesionScore = %v, want %v", got, want)
	}
}

func TestCohesionScoreSingleNode(t *testing.T) {
	got := cohesionScore([]string{"a"}, map[string]map[string]float64{})
	want := 1.0
	if got != want {
		t.Fatalf("cohesionScore = %v, want %v", got, want)
	}
}

func TestCohesionScoreEmpty(t *testing.T) {
	got := cohesionScore(nil, map[string]map[string]float64{})
	want := 0.0
	if got != want {
		t.Fatalf("cohesionScore = %v, want %v", got, want)
	}
}

func TestCohesionScoreNoInternalEdges(t *testing.T) {
	nodes := []string{"a", "b", "c", "d"}
	edges := []schema.Edge{
		{Source: "a", Target: "x"},
		{Source: "b", Target: "y"},
	}
	got := cohesionScore(nodes, adjFromEdges(edges))
	want := 0.0
	if got != want {
		t.Fatalf("cohesionScore = %v, want %v", got, want)
	}
}

func TestCohesionScoreIgnoresDirection(t *testing.T) {
	// Both a->b and b->a should count as one undirected edge.
	nodes := []string{"a", "b"}
	edges := []schema.Edge{
		{Source: "a", Target: "b"},
		{Source: "b", Target: "a"},
	}
	got := cohesionScore(nodes, adjFromEdges(edges))
	want := 1.0 // 1 unique undirected pair / (2*1/2) = 1
	if got != want {
		t.Fatalf("cohesionScore = %v, want %v", got, want)
	}
}

func TestLeidenClustererSplitsOversizedCommunity(t *testing.T) {
	// Hub + 14 rim nodes arranged in two cliques of 7, plus 10 isolates.
	// With minSplitSize=5 and maxFraction=0.2 (max_size=5 for 25 nodes),
	// an initial community of 15 should be split.
	nodes := make([]schema.Node, 25)
	var edges []schema.Edge

	nodes[0] = schema.Node{ID: "hub"}
	for i := 0; i < 7; i++ {
		nodes[i+1] = schema.Node{ID: fmt.Sprintf("c1_%d", i)}
	}
	for i := 0; i < 7; i++ {
		nodes[i+8] = schema.Node{ID: fmt.Sprintf("c2_%d", i)}
	}
	for i := 0; i < 10; i++ {
		nodes[i+15] = schema.Node{ID: fmt.Sprintf("iso_%d", i)}
	}

	// Hub connects to all clique nodes.
	for i := 0; i < 7; i++ {
		edges = append(edges, schema.Edge{Source: "hub", Target: fmt.Sprintf("c1_%d", i)})
		edges = append(edges, schema.Edge{Source: "hub", Target: fmt.Sprintf("c2_%d", i)})
	}
	edges = append(edges, cliqueEdges("c1_", 7)...)
	edges = append(edges, cliqueEdges("c2_", 7)...)

	c := NewLeidenClusterer(
		WithMinSplitSize(5),
		WithMaxCommunityFraction(0.2),
	)
	result, err := c.Cluster(nodes, edges)
	if err != nil {
		t.Fatal(err)
	}

	// All nodes must have communities.
	for _, n := range result {
		if n.Community == "" {
			t.Fatalf("node %s missing community", n.ID)
		}
	}

	// Count community sizes.
	commSizes := make(map[string]int)
	for _, n := range result {
		commSizes[n.Community]++
	}

	// Isolates should each be in their own community (10 singletons).
	singletons := 0
	for _, size := range commSizes {
		if size == 1 {
			singletons++
		}
	}
	if singletons < 10 {
		t.Fatalf("expected at least 10 singleton communities for isolates, got %d", singletons)
	}
}

func TestLeidenClustererDoesNotSplitSmallCommunity(t *testing.T) {
	// 12 nodes: 8 in a clique, 4 isolates.
	// With minSplitSize=10, maxFraction=0.25 (max_size=10), the 8-node community
	// should NOT be split.
	nodes := make([]schema.Node, 12)
	var edges []schema.Edge

	for i := 0; i < 8; i++ {
		nodes[i] = schema.Node{ID: fmt.Sprintf("c_%d", i)}
	}
	for i := 8; i < 12; i++ {
		nodes[i] = schema.Node{ID: fmt.Sprintf("iso_%d", i)}
	}
	edges = append(edges, cliqueEdges("c_", 8)...)

	c := NewLeidenClusterer(
		WithMinSplitSize(10),
		WithMaxCommunityFraction(0.25),
	)
	result, err := c.Cluster(nodes, edges)
	if err != nil {
		t.Fatal(err)
	}

	// The 8 clique nodes should remain in one community.
	commIDs := make(map[string]string)
	for _, n := range result {
		commIDs[n.ID] = n.Community
	}
	cliqueComm := commIDs["c_0"]
	for i := 1; i < 8; i++ {
		if commIDs[fmt.Sprintf("c_%d", i)] != cliqueComm {
			t.Fatal("8-node clique was split when it should not have been")
		}
	}
}

func TestLeidenClustererSplitsLowCohesionCommunity(t *testing.T) {
	// Hub + 4 subsystems of 5 clique nodes each, connected only through hub.
	// Cohesion ~0.138, so with threshold=0.15 the cohesion re-split triggers.
	nodes := make([]schema.Node, 36)
	var edges []schema.Edge

	nodes[0] = schema.Node{ID: "hub"}
	for s := 0; s < 4; s++ {
		for i := 0; i < 5; i++ {
			nodes[1+s*5+i] = schema.Node{ID: fmt.Sprintf("s%d_%d", s, i)}
		}
	}
	for i := 21; i < 36; i++ {
		nodes[i] = schema.Node{ID: fmt.Sprintf("iso_%d", i)}
	}

	// Hub connects to all subsystem nodes.
	for s := 0; s < 4; s++ {
		for i := 0; i < 5; i++ {
			edges = append(edges, schema.Edge{Source: "hub", Target: fmt.Sprintf("s%d_%d", s, i)})
		}
	}
	// Subsystem internal clique edges.
	for s := 0; s < 4; s++ {
		edges = append(edges, cliqueEdges(fmt.Sprintf("s%d_", s), 5)...)
	}

	c := NewLeidenClusterer(
		WithMinSplitSize(5),
		WithMaxCommunityFraction(0.2),     // max(5, 7) = 7 for 36 nodes
		WithCohesionThreshold(0.15),        // cohesion ~0.138 < 0.15, triggers
		WithCohesionMinSize(5),
	)
	result, err := c.Cluster(nodes, edges)
	if err != nil {
		t.Fatal(err)
	}

	// Verify all nodes have communities.
	for _, n := range result {
		if n.Community == "" {
			t.Fatalf("node %s missing community", n.ID)
		}
	}

	// The 21 non-isolate nodes should be in multiple communities (split occurred).
	// At minimum, the 4 subsystems should be in separate communities.
	nonIsolateCommunities := make(map[string]int)
	for _, n := range result {
		if strings.HasPrefix(n.ID, "iso") {
			continue
		}
		nonIsolateCommunities[n.Community]++
	}
	if len(nonIsolateCommunities) < 2 {
		t.Fatalf("expected at least 2 communities for non-isolate nodes, got %d", len(nonIsolateCommunities))
	}
}

func TestLeidenClustererDeterministicAfterSplitting(t *testing.T) {
	// leiden-go can produce different community memberships between runs for
	// graphs with ambiguous structure (e.g. a hub between two equal cliques),
	// even with a fixed seed. We therefore verify structural invariants
	// rather than exact community IDs.
	nodes := make([]schema.Node, 20)
	var edges []schema.Edge

	nodes[0] = schema.Node{ID: "hub"}
	for i := 0; i < 7; i++ {
		nodes[i+1] = schema.Node{ID: fmt.Sprintf("c1_%d", i)}
	}
	for i := 0; i < 7; i++ {
		nodes[i+8] = schema.Node{ID: fmt.Sprintf("c2_%d", i)}
	}
	for i := 0; i < 5; i++ {
		nodes[i+15] = schema.Node{ID: fmt.Sprintf("iso_%d", i)}
	}

	for i := 0; i < 7; i++ {
		edges = append(edges, schema.Edge{Source: "hub", Target: fmt.Sprintf("c1_%d", i)})
		edges = append(edges, schema.Edge{Source: "hub", Target: fmt.Sprintf("c2_%d", i)})
	}
	edges = append(edges, cliqueEdges("c1_", 7)...)
	edges = append(edges, cliqueEdges("c2_", 7)...)

	c := NewLeidenClusterer(
		WithMinSplitSize(5),
		WithMaxCommunityFraction(0.2),
	)

	result1, err := c.Cluster(nodes, edges)
	if err != nil {
		t.Fatal(err)
	}
	result2, err := c.Cluster(nodes, edges)
	if err != nil {
		t.Fatal(err)
	}

	if len(result1) != len(result2) {
		t.Fatal("result lengths differ")
	}
	for i := range result1 {
		if result1[i].ID != result2[i].ID {
			t.Fatalf("node order differs at index %d", i)
		}
	}

	// Verify structural invariants on both runs.
	checkInvariants := func(result []schema.Node) {
		nodeToComm := make(map[string]string, len(result))
		commMembers := make(map[string]map[string]bool)
		for _, n := range result {
			nodeToComm[n.ID] = n.Community
			if commMembers[n.Community] == nil {
				commMembers[n.Community] = make(map[string]bool)
			}
			commMembers[n.Community][n.ID] = true
		}

		// All c1 nodes must share a community.
		c1Comm := nodeToComm["c1_0"]
		for i := 1; i < 7; i++ {
			if nodeToComm[fmt.Sprintf("c1_%d", i)] != c1Comm {
				t.Fatal("c1 nodes split across communities")
			}
		}

		// All c2 nodes must share a community.
		c2Comm := nodeToComm["c2_0"]
		for i := 1; i < 7; i++ {
			if nodeToComm[fmt.Sprintf("c2_%d", i)] != c2Comm {
				t.Fatal("c2 nodes split across communities")
			}
		}

		// c1 and c2 must be in different communities.
		if c1Comm == c2Comm {
			t.Fatal("c1 and c2 should be in different communities")
		}

		// Isolates should each be in their own singleton community.
		for i := 0; i < 5; i++ {
			id := fmt.Sprintf("iso_%d", i)
			if len(commMembers[nodeToComm[id]]) != 1 {
				t.Fatalf("isolate %s should be singleton, got community of %d", id, len(commMembers[nodeToComm[id]]))
			}
		}
	}

	checkInvariants(result1)
	checkInvariants(result2)

	// Both runs should have the same community-size distribution.
	sizeHist1 := make(map[int]int)
	commMap1 := make(map[string]int)
	for _, n := range result1 {
		commMap1[n.Community]++
	}
	for _, size := range commMap1 {
		sizeHist1[size]++
	}

	sizeHist2 := make(map[int]int)
	commMap2 := make(map[string]int)
	for _, n := range result2 {
		commMap2[n.Community]++
	}
	for _, size := range commMap2 {
		sizeHist2[size]++
	}

	if len(sizeHist1) != len(sizeHist2) {
		t.Fatalf("community size histograms differ: %v vs %v", sizeHist1, sizeHist2)
	}
	for size, count1 := range sizeHist1 {
		if sizeHist2[size] != count1 {
			t.Fatalf("community size histograms differ: %v vs %v", sizeHist1, sizeHist2)
		}
	}
}

func TestLeidenClustererOptionsForSplitting(t *testing.T) {
	c := NewLeidenClusterer(
		WithResolution(0.5),
		WithMaxIterations(50),
		WithMinModularityGain(0.001),
		WithRandomSeed(123),
		WithMaxCommunityFraction(0.3),
		WithMinSplitSize(8),
		WithCohesionThreshold(0.1),
		WithCohesionMinSize(20),
	)
	nodes := []schema.Node{{ID: "a"}, {ID: "b"}}
	edges := []schema.Edge{{Source: "a", Target: "b"}}
	result, err := c.Cluster(nodes, edges)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(result))
	}
}

func TestSplitCommunityEdgelessSubgraph(t *testing.T) {
	// A community with no internal edges should be split into singletons.
	c := NewLeidenClusterer()
	members := []string{"a", "b", "c"}
	adj := map[string]map[string]float64{
		"a": {},
		"b": {},
		"c": {},
	}
	result, err := c.splitCommunity(members, adj)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 3 {
		t.Fatalf("expected 3 singleton communities, got %d", len(result))
	}
	for i, comm := range result {
		if len(comm) != 1 {
			t.Fatalf("community %d has %d members, expected 1", i, len(comm))
		}
	}
}

func TestSplitCommunityUnsplittableClique(t *testing.T) {
	// A clique cannot be split by Leiden, so the original community is kept.
	c := NewLeidenClusterer()
	members := []string{"a", "b", "c"}
	adj := map[string]map[string]float64{
		"a": {"b": 1, "c": 1},
		"b": {"a": 1, "c": 1},
		"c": {"a": 1, "b": 1},
	}
	result, err := c.splitCommunity(members, adj)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 community (unsplittable clique), got %d", len(result))
	}
	if len(result[0]) != 3 {
		t.Fatalf("expected 3 members, got %d", len(result[0]))
	}
}

func TestSplitCommunityConnectedPair(t *testing.T) {
	// A 2-node connected graph: Leiden returns 1 community of 2.
	c := NewLeidenClusterer()
	members := []string{"a", "b"}
	adj := map[string]map[string]float64{
		"a": {"b": 1},
		"b": {"a": 1},
	}
	result, err := c.splitCommunity(members, adj)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 community, got %d", len(result))
	}
}

func TestSplitCommunitySuccessfulSplit(t *testing.T) {
	// Two disconnected cliques should be split into two communities.
	c := NewLeidenClusterer()
	members := []string{"a0", "a1", "a2", "b0", "b1", "b2"}
	adj := map[string]map[string]float64{
		"a0": {"a1": 1, "a2": 1},
		"a1": {"a0": 1, "a2": 1},
		"a2": {"a0": 1, "a1": 1},
		"b0": {"b1": 1, "b2": 1},
		"b1": {"b0": 1, "b2": 1},
		"b2": {"b0": 1, "b1": 1},
	}
	result, err := c.splitCommunity(members, adj)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 communities, got %d", len(result))
	}
	// Verify all 6 nodes are assigned.
	assigned := make(map[string]bool)
	for _, comm := range result {
		for _, id := range comm {
			assigned[id] = true
		}
	}
	if len(assigned) != 6 {
		t.Fatalf("expected 6 assigned nodes, got %d", len(assigned))
	}
}

func TestDeriveSeedDeterministic(t *testing.T) {
	members := []string{"c", "a", "b"}
	got1 := deriveSeed(members)
	got2 := deriveSeed(members)
	if got1 != got2 {
		t.Fatalf("deriveSeed not deterministic: %d != %d", got1, got2)
	}
}

func TestDeriveSeedOrderIndependent(t *testing.T) {
	members1 := []string{"a", "b", "c"}
	members2 := []string{"c", "a", "b"}
	got1 := deriveSeed(members1)
	got2 := deriveSeed(members2)
	if got1 != got2 {
		t.Fatalf("deriveSeed not order-independent: %d != %d", got1, got2)
	}
}

func TestLeidenClustererCohesionReSplitTriggers(t *testing.T) {
	// A path graph of 10 nodes: low cohesion (9/45 = 0.2).
	// With minSplitSize=25 the oversized split does not trigger (10 < 25).
	// With cohesionThreshold=0.25 the cohesion re-split triggers (0.2 < 0.25).
	nodes := make([]schema.Node, 10)
	var edges []schema.Edge
	for i := 0; i < 10; i++ {
		nodes[i] = schema.Node{ID: fmt.Sprintf("n%d", i)}
	}
	for i := 0; i < 9; i++ {
		edges = append(edges, schema.Edge{
			Source: fmt.Sprintf("n%d", i),
			Target: fmt.Sprintf("n%d", i+1),
		})
	}

	c := NewLeidenClusterer(
		WithMinSplitSize(25),         // maxSize = 25, 10 < 25: no oversized split
		WithMaxCommunityFraction(0.2),
		WithCohesionThreshold(0.25),   // 0.2 < 0.25: cohesion re-split triggers
		WithCohesionMinSize(5),
	)
	result, err := c.Cluster(nodes, edges)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 10 {
		t.Fatalf("expected 10 nodes, got %d", len(result))
	}
	for _, n := range result {
		if n.Community == "" {
			t.Fatalf("node %s missing community", n.ID)
		}
	}
}

func TestSplitCommunityUnassignedFallback(t *testing.T) {
	// Defensive: verify the unassigned-member fallback in splitCommunity
	// handles synthetic partitions that drop members.
	c := NewLeidenClusterer()
	members := []string{"a", "b", "c"}
	adj := map[string]map[string]float64{
		"a": {"b": 1},
		"b": {"a": 1, "c": 1},
		"c": {"b": 1},
	}
	result, err := c.splitCommunity(members, adj)
	if err != nil {
		t.Fatal(err)
	}
	assigned := make(map[string]bool)
	for _, comm := range result {
		for _, id := range comm {
			assigned[id] = true
		}
	}
	for _, id := range members {
		if !assigned[id] {
			t.Fatalf("member %s not assigned to any community", id)
		}
	}
}

func TestLeidenClustererAutoCreatedNodesWithSplitting(t *testing.T) {
	// Auto-created nodes from edges should still get communities after splitting.
	nodes := []schema.Node{{ID: "a"}, {ID: "b"}}
	edges := []schema.Edge{
		{Source: "x", Target: "a"},
		{Source: "a", Target: "y"},
	}
	c := NewLeidenClusterer(
		WithMinSplitSize(1),
		WithMaxCommunityFraction(0.1),
	)
	result, err := c.Cluster(nodes, edges)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 4 {
		t.Fatalf("expected 4 nodes (2 input + 2 auto-created), got %d", len(result))
	}
	for _, n := range result {
		if n.Community == "" {
			t.Fatalf("auto-created node %s missing community", n.ID)
		}
	}
}

func TestLeidenClustererFallbackCommunityAssignment(t *testing.T) {
	// A node not in nodeToComm should get a fallback community ID.
	// This is defensive; we force it by using an edgeless graph where
	// Leiden assigns nothing and our isolate handling should cover it.
	nodes := []schema.Node{{ID: "a"}, {ID: "b"}}
	edges := []schema.Edge{}
	c := NewLeidenClusterer()
	result, err := c.Cluster(nodes, edges)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(result))
	}
	for _, n := range result {
		if n.Community == "" {
			t.Fatalf("node %s missing community", n.ID)
		}
	}
}

func TestLeidenClustererWeightedEdges(t *testing.T) {
	// Multiple edges between same nodes should be treated as higher weight
	nodes := []schema.Node{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	edges := []schema.Edge{
		{Source: "a", Target: "b"},
		{Source: "a", Target: "b"},
		{Source: "b", Target: "c"},
	}
	c := NewLeidenClusterer()
	result, err := c.Cluster(nodes, edges)
	if err != nil {
		t.Fatal(err)
	}
	// All connected nodes should share a community
	communities := make(map[string]string)
	for _, n := range result {
		communities[n.ID] = n.Community
	}
	if communities["a"] != communities["b"] || communities["b"] != communities["c"] {
		t.Logf("communities: %v", communities)
		t.Fatal("all connected nodes should share a community")
	}
}

// Pins deriveSeed to its current numeric output. If FNV-1a is swapped or
// the trailing zero-byte separator is removed, this test fails — alerting
// us that previously-frozen graph.json snapshots will shift their
// community IDs after a Leiden subgraph re-pass.
func TestDeriveSeedPinned(t *testing.T) {
	got := deriveSeed([]string{"alpha", "beta", "gamma"})
	want := int64(3768274256728616250)
	if got != want {
		t.Fatalf("deriveSeed drift: got %d, want %d (regenerate frozen seeds intentionally)", got, want)
	}
}

// Self-loops in adj must not inflate cohesionScore past 1.0. A self-loop
// on every member would otherwise contribute n to actual while possible
// only counts unordered pairs (n*(n-1)/2).
func TestCohesionScoreIgnoresSelfLoops(t *testing.T) {
	adj := map[string]map[string]float64{
		"a": {"a": 1, "b": 1},
		"b": {"a": 1, "b": 1},
	}
	got := cohesionScore([]string{"a", "b"}, adj)
	if got != 1.0 {
		t.Fatalf("self-loops drove cohesion past 1.0: got %v", got)
	}
}

// Splitting is opt-in: NewLeidenClusterer() with no options must NOT
// invoke the splitCommunity path, even on a graph where it could trigger.
// Locks in backward compatibility for downstream graph.json snapshots
// frozen before splitting was introduced.
func TestLeidenClustererDefaultsDoNotSplit(t *testing.T) {
	// Two cliques of 10 nodes, fully connected internally, bridged by
	// one cross-edge. A single large pre-splitting community is exactly
	// what would trigger oversized-splitting if defaults were on.
	var nodes []schema.Node
	var edges []schema.Edge
	for i := 0; i < 10; i++ {
		nodes = append(nodes, schema.Node{ID: fmt.Sprintf("c1_%d", i)})
		nodes = append(nodes, schema.Node{ID: fmt.Sprintf("c2_%d", i)})
	}
	edges = append(edges, cliqueEdges("c1_", 10)...)
	edges = append(edges, cliqueEdges("c2_", 10)...)
	edges = append(edges, schema.Edge{Source: "c1_0", Target: "c2_0"})

	defaults := NewLeidenClusterer()
	if defaults.maxCommunityFraction != 0 ||
		defaults.minSplitSize != 0 ||
		defaults.cohesionThreshold != 0 ||
		defaults.cohesionMinSize != 0 {
		t.Fatalf("splitting fields must default to zero (opt-in): %+v", defaults)
	}
	// Sanity: still produces communities; we just don't assert split shape.
	got, err := defaults.Cluster(nodes, edges)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range got {
		if n.Community == "" {
			t.Fatalf("node %s missing community on default-config Cluster", n.ID)
		}
	}
}

// Negative / out-of-range option inputs must clamp rather than poison
// the partition (e.g. WithMaxCommunityFraction(2.0) shouldn't actually
// force max_size = 2*|V|).
func TestLeidenClustererOptionClamping(t *testing.T) {
	c := NewLeidenClusterer(
		WithMaxCommunityFraction(2.0),
		WithCohesionThreshold(-0.5),
		WithMinSplitSize(-3),
		WithCohesionMinSize(-1),
	)
	if c.maxCommunityFraction != 1.0 {
		t.Fatalf("WithMaxCommunityFraction(2.0) not clamped to 1.0: %v", c.maxCommunityFraction)
	}
	if c.cohesionThreshold != 0 {
		t.Fatalf("WithCohesionThreshold(-0.5) not clamped to 0: %v", c.cohesionThreshold)
	}
	if c.minSplitSize != 0 || c.cohesionMinSize != 0 {
		t.Fatalf("negative size options not clamped: minSplit=%d cohesionMin=%d", c.minSplitSize, c.cohesionMinSize)
	}
}
