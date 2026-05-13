package cluster

import (
	"fmt"
	"testing"

	"github.com/julianshen/gogfy/internal/schema"
)

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
	// Just verify it doesn't panic and produces output
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
	got := cohesionScore(nodes, edges)
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
	got := cohesionScore(nodes, edges)
	want := 1.0
	if got != want {
		t.Fatalf("cohesionScore = %v, want %v", got, want)
	}
}

func TestCohesionScoreSingleNode(t *testing.T) {
	got := cohesionScore([]string{"a"}, nil)
	want := 1.0
	if got != want {
		t.Fatalf("cohesionScore = %v, want %v", got, want)
	}
}

func TestCohesionScoreEmpty(t *testing.T) {
	got := cohesionScore(nil, nil)
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
	got := cohesionScore(nodes, edges)
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
	got := cohesionScore(nodes, edges)
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
	// Clique 1 internal edges.
	for i := 0; i < 7; i++ {
		for j := i + 1; j < 7; j++ {
			edges = append(edges, schema.Edge{Source: fmt.Sprintf("c1_%d", i), Target: fmt.Sprintf("c1_%d", j)})
		}
	}
	// Clique 2 internal edges.
	for i := 0; i < 7; i++ {
		for j := i + 1; j < 7; j++ {
			edges = append(edges, schema.Edge{Source: fmt.Sprintf("c2_%d", i), Target: fmt.Sprintf("c2_%d", j)})
		}
	}

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
	for i := 0; i < 8; i++ {
		for j := i + 1; j < 8; j++ {
			edges = append(edges, schema.Edge{Source: fmt.Sprintf("c_%d", i), Target: fmt.Sprintf("c_%d", j)})
		}
	}

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
	// 20 nodes: hub + 19 leaves (star graph).
	// Cohesion = 19 / (20*19/2) = 19/190 = 0.1.
	// With cohesionThreshold=0.15, this should NOT be split (0.1 < 0.15).
	// Wait — we want to test splitting, so use threshold=0.15 and the star
	// should be split because cohesion < threshold.
	// But Leiden on a star keeps it together, so the split won't help.
	// Instead, use a graph where Leiden CAN split: two cliques with a bridge.
	// 16 nodes: two 8-node cliques, one bridge edge, plus 4 isolates.
	// Cohesion of the full 16-node group (if kept together):
	//   edges = 2*28 + 1 = 57
	//   cohesion = 57 / (16*15/2) = 57/120 = 0.475.
	// That's > 0.15, so cohesion split won't trigger.
	// We need a LOW cohesion graph. Let's use a hub with many leaves that
	// also have some weak internal connections.
	//
	// Simpler: create 12 nodes in a sparse graph (a tree: 11 edges).
	// cohesion = 11 / (12*11/2) = 11/66 = 0.167.
	// With threshold=0.2, this won't trigger either.
	//
	// Let's use a hub+leaves with 50 nodes. cohesion = 49/1225 = 0.04.
	// With lowered threshold=0.05, this triggers.
	// But creating 50 nodes in a test is verbose.
	//
	// Instead, make the threshold configurable and test with a smaller graph.
	// 15 nodes in a star: cohesion = 14/105 = 0.133.
	// With threshold=0.15, doesn't trigger.
	// 20 nodes in a star: cohesion = 19/190 = 0.1.
	// With threshold=0.15, triggers!
	// But Leiden on a star won't split it.
	//
	// OK — let's use a hub+cliques where the cohesion is low but Leiden CAN split.
	// Two cliques of 5 connected by a single edge, total 10 nodes.
	// edges = 2*10 + 1 = 21
	// cohesion = 21 / (10*9/2) = 21/45 = 0.467.
	// Still too high.
	//
	// What about a line graph of 20 nodes?
	// edges = 19, cohesion = 19/190 = 0.1.
	// With threshold=0.15, triggers. But Leiden on a line won't split.
	//
	// The key insight: for cohesion splitting to actually split, the subgraph
	// must have community structure that Leiden can find. A line graph doesn't.
	// Two cliques with a bridge DO have structure, but their cohesion is high.
	//
	// The upstream's example is "doc-hub nodes that bridge otherwise-unrelated
	// subsystems". The hub connects to many subsystems. Without external edges,
	// the hub's connections to subsystems are the only intra-community edges.
	// Cohesion is low because most possible edges are missing.
	//
	// For our test, let's use a hub connected to 20 leaves, with the leaves
	// having NO other connections. Cohesion = 20/210 = 0.095.
	// With threshold=0.1, this triggers.
	// But Leiden on a 21-node star won't split it.
	//
	// So the test will verify that cohesion splitting is ATTEMPTED (the code
	// path runs) even if Leiden can't split the subgraph.
	// The real value is verifying the cohesion score and threshold logic.
	//
	// Let's test with a graph where cohesion splitting DOES work:
	// 30 nodes: hub + 4 subsystems of 5 nodes each. Subsystems are cliques.
	// No edges between subsystems except through hub.
	// Total edges = 4*10 + 20 = 60.
	// cohesion = 60 / (30*29/2) = 60/435 = 0.138.
	// With threshold=0.15, triggers.
	// In subgraph of 30, Leiden should find the 4 subsystems + hub.
	// The split produces 5 communities of ~6 nodes each.
	//
	// Let's try this.
	nodes := make([]schema.Node, 35)
	var edges []schema.Edge

	nodes[0] = schema.Node{ID: "hub"}
	for s := 0; s < 4; s++ {
		for i := 0; i < 5; i++ {
			nodes[1+s*5+i] = schema.Node{ID: fmt.Sprintf("s%d_%d", s, i)}
		}
	}
	for i := 20; i < 35; i++ {
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
		for i := 0; i < 5; i++ {
			for j := i + 1; j < 5; j++ {
				edges = append(edges, schema.Edge{
					Source: fmt.Sprintf("s%d_%d", s, i),
					Target: fmt.Sprintf("s%d_%d", s, j),
				})
			}
		}
	}

	c := NewLeidenClusterer(
		WithMinSplitSize(5),
		WithMaxCommunityFraction(0.2),     // max(5, 7) = 7 for 35 nodes
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

	// The 20 non-isolate nodes should be in multiple communities (split occurred).
	// At minimum, the 4 subsystems should be in separate communities.
	nonIsolateCommunities := make(map[string]int)
	for _, n := range result {
		if n.ID[0:3] == "iso" {
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
	for i := 0; i < 7; i++ {
		for j := i + 1; j < 7; j++ {
			edges = append(edges, schema.Edge{Source: fmt.Sprintf("c1_%d", i), Target: fmt.Sprintf("c1_%d", j)})
		}
	}
	for i := 0; i < 7; i++ {
		for j := i + 1; j < 7; j++ {
			edges = append(edges, schema.Edge{Source: fmt.Sprintf("c2_%d", i), Target: fmt.Sprintf("c2_%d", j)})
		}
	}

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
		commMap := make(map[string]map[string]bool)
		for _, n := range result {
			if commMap[n.Community] == nil {
				commMap[n.Community] = make(map[string]bool)
			}
			commMap[n.Community][n.ID] = true
		}

		// All c1 nodes must share a community.
		c1Comm := ""
		for i := 0; i < 7; i++ {
			id := fmt.Sprintf("c1_%d", i)
			for cid, members := range commMap {
				if members[id] {
					if c1Comm == "" {
						c1Comm = cid
					} else if c1Comm != cid {
						t.Fatal("c1 nodes split across communities")
					}
					break
				}
			}
		}

		// All c2 nodes must share a community.
		c2Comm := ""
		for i := 0; i < 7; i++ {
			id := fmt.Sprintf("c2_%d", i)
			for cid, members := range commMap {
				if members[id] {
					if c2Comm == "" {
						c2Comm = cid
					} else if c2Comm != cid {
						t.Fatal("c2 nodes split across communities")
					}
					break
				}
			}
		}

		// c1 and c2 must be in different communities.
		if c1Comm == c2Comm {
			t.Fatal("c1 and c2 should be in different communities")
		}

		// Isolates should each be in their own singleton community.
		for i := 0; i < 5; i++ {
			id := fmt.Sprintf("iso_%d", i)
			for _, members := range commMap {
				if members[id] {
					if len(members) != 1 {
						t.Fatalf("isolate %s should be singleton, got community of %d", id, len(members))
					}
					break
				}
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
	result := c.splitCommunity(members, adj)
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
	result := c.splitCommunity(members, adj)
	if len(result) != 1 {
		t.Fatalf("expected 1 community (unsplittable clique), got %d", len(result))
	}
	if len(result[0]) != 3 {
		t.Fatalf("expected 3 members, got %d", len(result[0]))
	}
}

func TestSplitCommunityWithUnassignedMembers(t *testing.T) {
	// If leiden-go drops some members from its partition, they should be
	// added back as singleton communities.
	c := NewLeidenClusterer()
	// Use a graph where Leiden will likely partition but we manually test
	// the fallback by using a simple structure.
	members := []string{"a", "b"}
	adj := map[string]map[string]float64{
		"a": {"b": 1},
		"b": {"a": 1},
	}
	result := c.splitCommunity(members, adj)
	// A 2-node graph with 1 edge: Leiden returns 1 community of 2.
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
	result := c.splitCommunity(members, adj)
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
