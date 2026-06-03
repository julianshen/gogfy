package centrality

import (
	"math"
	"testing"

	"github.com/julianshen/gogfy/internal/schema"
)

func TestBetweennessLineGraphMiddleIsBridge(t *testing.T) {
	// In a 5-node line A-B-C-D-E, node C lies on every shortest path
	// between a pair on opposite sides. Brandes' algorithm should rank
	// C highest, B and D below, A and E at zero.
	nodes := []schema.Node{
		{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"}, {ID: "e"},
	}
	edges := []schema.Edge{
		{Source: "a", Target: "b"},
		{Source: "b", Target: "c"},
		{Source: "c", Target: "d"},
		{Source: "d", Target: "e"},
	}
	cb := Betweenness(nodes, edges)
	if cb["c"] <= cb["b"] || cb["c"] <= cb["d"] {
		t.Fatalf("middle of line should beat its neighbors: %+v", cb)
	}
	if cb["a"] != 0 || cb["e"] != 0 {
		t.Fatalf("endpoints should have zero betweenness: %+v", cb)
	}
}

func TestBetweennessSymmetryAcrossLineGraph(t *testing.T) {
	// A-B-C-D-E is symmetric around C: B and D must have identical
	// scores, A and E must both be 0.
	nodes := []schema.Node{{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"}, {ID: "e"}}
	edges := []schema.Edge{
		{Source: "a", Target: "b"},
		{Source: "b", Target: "c"},
		{Source: "c", Target: "d"},
		{Source: "d", Target: "e"},
	}
	cb := Betweenness(nodes, edges)
	if math.Abs(cb["b"]-cb["d"]) > 1e-9 {
		t.Fatalf("symmetric nodes should have equal betweenness: b=%f d=%f", cb["b"], cb["d"])
	}
}

func TestBetweennessIsolatedNodeIsZero(t *testing.T) {
	nodes := []schema.Node{{ID: "iso"}, {ID: "a"}, {ID: "b"}}
	edges := []schema.Edge{{Source: "a", Target: "b"}}
	cb := Betweenness(nodes, edges)
	if cb["iso"] != 0 {
		t.Fatalf("isolated node should have zero betweenness, got %f", cb["iso"])
	}
}

func TestBetweennessTriangleAllZero(t *testing.T) {
	// K3: every pair has two shortest paths of equal length, and the
	// "third" node lies on no shortest path of length 1. All zero.
	nodes := []schema.Node{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	edges := []schema.Edge{
		{Source: "a", Target: "b"},
		{Source: "b", Target: "c"},
		{Source: "c", Target: "a"},
	}
	cb := Betweenness(nodes, edges)
	for _, v := range cb {
		if math.Abs(v) > 1e-9 {
			t.Fatalf("triangle: all betweenness must be 0, got %+v", cb)
		}
	}
}

func TestBetweennessDuplicateEdgesDoNotInflate(t *testing.T) {
	// Multigraph: two A-B edges should be treated as one (the graph
	// is conceptually undirected with a single shortest path).
	nodes := []schema.Node{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	edges := []schema.Edge{
		{Source: "a", Target: "b"},
		{Source: "a", Target: "b"}, // duplicate
		{Source: "b", Target: "c"},
	}
	cb := Betweenness(nodes, edges)
	// B is on the only path between A and C → score = 1 (un-normalized).
	if math.Abs(cb["b"]-1) > 1e-9 {
		t.Fatalf("duplicate edges must not inflate: cb[b]=%f, want 1", cb["b"])
	}
}

func TestBetweennessSelfLoopIgnored(t *testing.T) {
	nodes := []schema.Node{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	edges := []schema.Edge{
		{Source: "a", Target: "a"}, // self-loop
		{Source: "a", Target: "b"},
		{Source: "b", Target: "c"},
	}
	cb := Betweenness(nodes, edges)
	if math.Abs(cb["b"]-1) > 1e-9 {
		t.Fatalf("self-loop must not affect scoring: cb[b]=%f, want 1", cb["b"])
	}
}

func TestBetweennessHandlesEmptyGraph(t *testing.T) {
	cb := Betweenness(nil, nil)
	if len(cb) != 0 {
		t.Fatalf("empty graph should produce empty result, got %+v", cb)
	}
}

func TestBetweennessDanglingEdgeIgnored(t *testing.T) {
	// Edge references a node not in the node list — should be skipped
	// without crashing.
	nodes := []schema.Node{{ID: "a"}, {ID: "b"}}
	edges := []schema.Edge{
		{Source: "a", Target: "b"},
		{Source: "a", Target: "ghost"}, // dangling
	}
	cb := Betweenness(nodes, edges)
	if _, ok := cb["ghost"]; ok {
		t.Fatalf("ghost node should not appear in result: %+v", cb)
	}
}

func TestBetweennessLineGraphExactScores(t *testing.T) {
	// Pin the exact (halved) scores on a known shape. A-B-C-D-E:
	// node C is on the shortest path for {A,B}↔{D,E} pairs.
	// Halved un-normalized: cb[C] = (2·2)/2 = 2; analytically 3 for B.
	// Actually for a 5-node line the standard result is:
	//   cb[A]=cb[E]=0, cb[B]=cb[D]=3.0, cb[C]=4.0 (un-halved)
	//   halved:  cb[B]=cb[D]=1.5, cb[C]=2.0
	// Computing on paper: paths through B: {A,C}, {A,D}, {A,E} → 3.
	// Halved: 1.5. Pin that exactly so the /2 step can't silently drop.
	nodes := []schema.Node{{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"}, {ID: "e"}}
	edges := []schema.Edge{
		{Source: "a", Target: "b"},
		{Source: "b", Target: "c"},
		{Source: "c", Target: "d"},
		{Source: "d", Target: "e"},
	}
	cb := Betweenness(nodes, edges)
	if math.Abs(cb["b"]-3.0) > 1e-9 {
		t.Errorf("cb[b] = %f, want 3.0 (halved)", cb["b"])
	}
	if math.Abs(cb["c"]-4.0) > 1e-9 {
		t.Errorf("cb[c] = %f, want 4.0 (halved)", cb["c"])
	}
}

func TestPageRankHubAccumulatesMostRank(t *testing.T) {
	// a→hub, b→hub, c→hub: everyone references hub, so hub should rank
	// highest. Edges are directed (rank flows source→target).
	nodes := []schema.Node{{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "hub"}}
	edges := []schema.Edge{
		{Source: "a", Target: "hub"},
		{Source: "b", Target: "hub"},
		{Source: "c", Target: "hub"},
	}
	pr := PageRank(nodes, edges, nil)
	for _, leaf := range []string{"a", "b", "c"} {
		if pr["hub"] <= pr[leaf] {
			t.Fatalf("hub (%g) should outrank leaf %s (%g)", pr["hub"], leaf, pr[leaf])
		}
	}
	// Ranks should form a probability distribution summing to ~1.
	var sum float64
	for _, v := range pr {
		sum += v
	}
	if math.Abs(sum-1.0) > 1e-6 {
		t.Fatalf("PageRank should sum to 1, got %g", sum)
	}
}

func TestPageRankPersonalizationBiasesRanking(t *testing.T) {
	// Two disconnected pairs: a→b and c→d. With uniform teleport, b and d
	// are symmetric. Personalizing toward {a} should pull rank into the
	// a/b component, lifting b above d.
	nodes := []schema.Node{{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"}}
	edges := []schema.Edge{
		{Source: "a", Target: "b"},
		{Source: "c", Target: "d"},
	}
	uniform := PageRank(nodes, edges, nil)
	if math.Abs(uniform["b"]-uniform["d"]) > 1e-6 {
		t.Fatalf("symmetric components should tie under uniform: b=%g d=%g", uniform["b"], uniform["d"])
	}
	personalized := PageRank(nodes, edges, map[string]float64{"a": 1})
	if personalized["b"] <= personalized["d"] {
		t.Fatalf("personalizing toward a should lift b above d: b=%g d=%g", personalized["b"], personalized["d"])
	}
}

func TestPageRankUnknownPersonalizationFallsBackToUniform(t *testing.T) {
	nodes := []schema.Node{{ID: "a"}, {ID: "b"}}
	edges := []schema.Edge{{Source: "a", Target: "b"}}
	// Personalization referencing only unknown IDs must not divide by zero
	// or produce NaN — it falls back to uniform teleport.
	pr := PageRank(nodes, edges, map[string]float64{"ghost": 1})
	for id, v := range pr {
		if math.IsNaN(v) || v <= 0 {
			t.Fatalf("expected positive finite rank for %s, got %g", id, v)
		}
	}
}

func TestPageRankEmptyGraph(t *testing.T) {
	if pr := PageRank(nil, nil, nil); len(pr) != 0 {
		t.Fatalf("expected empty result, got %v", pr)
	}
}
