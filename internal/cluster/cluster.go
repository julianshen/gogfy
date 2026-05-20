// Package cluster groups graph nodes into communities using modularity-based clustering.
package cluster

import (
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"sort"
	"strconv"

	"github.com/julianshen/gogfy/internal/schema"
	leiden "github.com/vsuryav/leiden-go"
)

// stderrLogger receives non-fatal cluster diagnostics (e.g. the
// Leiden-to-Louvain fallback message). Tests override to capture and
// assert; production leaves the default (os.Stderr).
var stderrLogger io.Writer = os.Stderr

// Clusterer is the interface for algorithms that assign communities to nodes.
type Clusterer interface {
	Cluster(nodes []schema.Node, edges []schema.Edge) ([]schema.Node, error)
}

// ConnectedComponentsClusterer assigns community IDs based on connected components in the graph.
// It is deterministic and serves as a fallback when no modularity-based clustering is desired.
type ConnectedComponentsClusterer struct{}

// NewConnectedComponentsClusterer creates a new ConnectedComponentsClusterer.
func NewConnectedComponentsClusterer() *ConnectedComponentsClusterer {
	return &ConnectedComponentsClusterer{}
}

// Cluster assigns each node a community ID based on its connected component.
func (c *ConnectedComponentsClusterer) Cluster(nodes []schema.Node, edges []schema.Edge) ([]schema.Node, error) {
	// Build adjacency list
	adj := make(map[string][]string, len(nodes))
	for _, e := range edges {
		adj[e.Source] = append(adj[e.Source], e.Target)
		adj[e.Target] = append(adj[e.Target], e.Source)
	}

	// Build node index for O(1) lookup
	nodeIdx := make(map[string]int, len(nodes))
	result := make([]schema.Node, len(nodes))
	for i, n := range nodes {
		nodeIdx[n.ID] = i
		result[i] = n
	}

	visited := make(map[string]bool, len(nodes))
	var communityID int

	for _, n := range nodes {
		if visited[n.ID] {
			continue
		}

		queue := []string{n.ID}
		visited[n.ID] = true
		members := []string{n.ID}

		for len(queue) > 0 {
			curr := queue[0]
			queue = queue[1:]
			for _, neighbor := range adj[curr] {
				if !visited[neighbor] {
					visited[neighbor] = true
					queue = append(queue, neighbor)
					members = append(members, neighbor)
				}
			}
		}

		cid := strconv.Itoa(communityID)
		for _, id := range members {
			if idx, ok := nodeIdx[id]; ok {
				result[idx].Community = cid
			}
		}
		communityID++
	}

	// Sort by node ID for deterministic output
	sort.Slice(result, func(i, j int) bool {
		return result[i].ID < result[j].ID
	})

	return result, nil
}

// LeidenClusterer uses the Leiden algorithm for modularity-based community detection.
// It produces higher-quality communities than connected components by optimizing modularity.
// After the initial partition, oversized communities are split with a second Leiden pass
// on the subgraph, and low-cohesion communities are re-split to break apart doc-hub
// structures. Community IDs are assigned by size descending (0 = largest).
type LeidenClusterer struct {
	resolution           float64
	maxIterations        int
	minModularityGain    float64
	randomSeed           int64
	maxCommunityFraction float64
	minSplitSize         int
	cohesionThreshold    float64
	cohesionMinSize      int
}

// LeidenOption configures a LeidenClusterer.
type LeidenOption func(*LeidenClusterer)

// WithResolution sets the resolution parameter (higher = more, smaller communities).
func WithResolution(r float64) LeidenOption {
	return func(c *LeidenClusterer) { c.resolution = r }
}

// WithMaxIterations sets the maximum number of iterations.
func WithMaxIterations(n int) LeidenOption {
	return func(c *LeidenClusterer) { c.maxIterations = n }
}

// WithMinModularityGain sets the minimum modularity gain for convergence.
func WithMinModularityGain(g float64) LeidenOption {
	return func(c *LeidenClusterer) { c.minModularityGain = g }
}

// WithRandomSeed sets the random seed for deterministic output.
func WithRandomSeed(s int64) LeidenOption {
	return func(c *LeidenClusterer) { c.randomSeed = s }
}

// WithMaxCommunityFraction enables oversized-community splitting: any
// community larger than f * |V| (and ≥ MinSplitSize) gets a second Leiden
// pass on its subgraph. Zero or negative disables the feature; values
// above 1.0 are clamped to 1.0 (which makes splitting impossible). The
// raison d'être: Louvain/Leiden occasionally returns one community
// holding 60%+ of nodes in graphs with strong hub structure, which
// downstream collapses the GRAPH_REPORT's surprising-connections.
func WithMaxCommunityFraction(f float64) LeidenOption {
	return func(c *LeidenClusterer) {
		if f < 0 {
			f = 0
		}
		if f > 1.0 {
			f = 1.0
		}
		c.maxCommunityFraction = f
	}
}

// WithMinSplitSize sets the floor for oversized-community splitting:
// communities below this size are left alone even if they exceed the
// MaxCommunityFraction threshold. Negative values clamp to 0.
func WithMinSplitSize(n int) LeidenOption {
	return func(c *LeidenClusterer) {
		if n < 0 {
			n = 0
		}
		c.minSplitSize = n
	}
}

// WithCohesionThreshold enables cohesion-based re-splitting: communities
// at or above CohesionMinSize whose intra/possible-edges ratio is below
// t get a second Leiden pass. Catches doc-hub structures the first pass
// missed (the hub holds many low-cohesion neighbors together). Zero or
// negative disables; values above 1.0 are clamped (cohesion is bounded
// at 1.0, so values >1.0 would re-split every community).
func WithCohesionThreshold(t float64) LeidenOption {
	return func(c *LeidenClusterer) {
		if t < 0 {
			t = 0
		}
		if t > 1.0 {
			t = 1.0
		}
		c.cohesionThreshold = t
	}
}

// WithCohesionMinSize sets the minimum size for a community to be
// considered for cohesion-based re-splitting. Negative values clamp to 0.
func WithCohesionMinSize(n int) LeidenOption {
	return func(c *LeidenClusterer) {
		if n < 0 {
			n = 0
		}
		c.cohesionMinSize = n
	}
}

// NewLeidenClusterer creates a new LeidenClusterer with the given options.
func NewLeidenClusterer(opts ...LeidenOption) *LeidenClusterer {
	// Splitting (oversized + cohesion-based) is OPT-IN. Pre-existing
	// callers of NewLeidenClusterer() must keep getting a pure Leiden
	// partition — otherwise stable community IDs across releases break
	// and downstream graph.json snapshots compare-fail.
	c := &LeidenClusterer{
		resolution:        1.0,
		maxIterations:     100,
		minModularityGain: 0.0001,
		randomSeed:        42,
		// maxCommunityFraction, minSplitSize, cohesionThreshold,
		// cohesionMinSize all default to 0 — splitting disabled.
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Cluster assigns each node a community ID using the Leiden algorithm.
// The output is deterministic when the same random seed is used.
func (c *LeidenClusterer) Cluster(nodes []schema.Node, edges []schema.Edge) ([]schema.Node, error) {
	if len(nodes) == 0 {
		return []schema.Node{}, nil
	}

	// Build adjacency map for leiden-go
	adj := make(map[string]map[string]float64, len(nodes))
	for _, n := range nodes {
		adj[n.ID] = make(map[string]float64)
	}
	for _, e := range edges {
		// Auto-create nodes referenced by edges but missing from the node list
		if _, ok := adj[e.Source]; !ok {
			adj[e.Source] = make(map[string]float64)
		}
		if _, ok := adj[e.Target]; !ok {
			adj[e.Target] = make(map[string]float64)
		}
		// Use uniform weight 1.0; sum weights for duplicate edges
		adj[e.Source][e.Target] += 1.0
		adj[e.Target][e.Source] += 1.0
	}

	// Check if graph has any edges - leiden-go fails on edgeless graphs
	hasEdges := false
	for _, neighbors := range adj {
		if len(neighbors) > 0 {
			hasEdges = true
			break
		}
	}

	if !hasEdges {
		// Fallback to connected-components behavior for edgeless graphs
		out := make([]schema.Node, len(nodes))
		for i, n := range nodes {
			out[i] = n
			out[i].Community = strconv.Itoa(i)
		}
		sort.Slice(out, func(i, j int) bool {
			return out[i].ID < out[j].ID
		})
		return out, nil
	}

	graph := leiden.NewGraph(adj)
	config := &leiden.Config{
		Resolution:        c.resolution,
		MaxIterations:     c.maxIterations,
		MinModularityGain: c.minModularityGain,
		RandomSeed:        c.randomSeed,
	}

	result, err := leiden.Leiden(graph, config)
	if err != nil {
		// Fall back to Louvain rather than aborting. Leiden errors on
		// pathological inputs (certain disconnected-component shapes,
		// edge-weight underflow) where Louvain still produces a
		// reasonable partition. Better to ship slightly-lower-quality
		// communities than to fail the whole run.
		fmt.Fprintf(stderrLogger, "cluster: leiden failed (%v), falling back to Louvain\n", err)
		return NewLouvainClusterer().Cluster(nodes, edges)
	}

	// Build initial communities from Leiden result.
	communities := result.Partition.Communities()
	var memberLists [][]string
	assigned := make(map[string]bool)
	for _, members := range communities {
		m := make([]string, len(members))
		copy(m, members)
		memberLists = append(memberLists, m)
		for _, id := range members {
			assigned[id] = true
		}
	}

	// Isolates (nodes with degree 0) are not assigned by Leiden.
	for _, n := range nodes {
		if !assigned[n.ID] {
			memberLists = append(memberLists, []string{n.ID})
		}
	}

	// Oversized-community splitting (opt-in: disabled when
	// maxCommunityFraction <= 0). Iterate until either no community
	// exceeds the threshold or the iteration cap is hit — the iteration
	// cap guards against pathological subgraphs that Leiden refuses to
	// split (always returns a single community), which would otherwise
	// loop forever.
	split := memberLists
	if c.maxCommunityFraction > 0 {
		const maxSplitPasses = 5
		maxSize := max(c.minSplitSize, int(float64(len(nodes))*c.maxCommunityFraction))
		for pass := 0; pass < maxSplitPasses; pass++ {
			var next [][]string
			changed := false
			for _, members := range split {
				if len(members) > maxSize {
					subs, err := c.splitCommunity(members, adj)
					if err != nil {
						// Best-effort: log and keep the oversized
						// community rather than aborting the whole
						// cluster pass. Partial-progress preservation
						// matters because Cluster() is called late in
						// the pipeline.
						fmt.Printf("cluster: oversized splitCommunity failed (size=%d): %v\n", len(members), err)
						next = append(next, members)
						continue
					}
					if len(subs) > 1 {
						changed = true
						next = append(next, subs...)
					} else {
						next = append(next, members)
					}
				} else {
					next = append(next, members)
				}
			}
			split = next
			if !changed {
				break
			}
		}
	}

	// Cohesion-based re-splitting (opt-in: disabled when
	// cohesionThreshold <= 0). Single pass — re-splitting low-cohesion
	// fragments recursively risks shattering a sparse but meaningful
	// community into singletons.
	secondPass := split
	if c.cohesionThreshold > 0 {
		secondPass = nil
		for _, members := range split {
			if len(members) >= c.cohesionMinSize && cohesionScore(members, adj) < c.cohesionThreshold {
				subs, err := c.splitCommunity(members, adj)
				if err != nil {
					fmt.Printf("cluster: cohesion splitCommunity failed (size=%d): %v\n", len(members), err)
					secondPass = append(secondPass, members)
					continue
				}
				if len(subs) > 1 {
					secondPass = append(secondPass, subs...)
				} else {
					secondPass = append(secondPass, members)
				}
			} else {
				secondPass = append(secondPass, members)
			}
		}
	}

	// Re-index by size descending for deterministic ordering.
	// Upstream convention: 0 = largest community after splitting.
	sort.Slice(secondPass, func(i, j int) bool {
		if li, lj := len(secondPass[i]), len(secondPass[j]); li != lj {
			return li > lj
		}
		return secondPass[i][0] < secondPass[j][0]
	})

	nodeToComm := make(map[string]string, len(nodes))
	for i, members := range secondPass {
		cid := strconv.Itoa(i)
		for _, id := range members {
			nodeToComm[id] = cid
		}
	}

	// Collect all node IDs from the input plus any auto-created ones.
	allNodeIDs := make(map[string]struct{}, len(adj))
	for id := range adj {
		allNodeIDs[id] = struct{}{}
	}

	// Assign communities to nodes (copy to avoid mutating input).
	out := make([]schema.Node, 0, len(allNodeIDs))
	for _, n := range nodes {
		clone := n
		if comm, ok := nodeToComm[n.ID]; ok {
			clone.Community = comm
		} else {
			clone.Community = strconv.Itoa(len(secondPass) + len(out))
		}
		out = append(out, clone)
		delete(allNodeIDs, n.ID)
	}
	// Append auto-created nodes (from edges referencing missing nodes).
	for id := range allNodeIDs {
		comm := ""
		if c, ok := nodeToComm[id]; ok {
			comm = c
		} else {
			comm = strconv.Itoa(len(secondPass) + len(out))
		}
		out = append(out, schema.Node{
			ID:        id,
			Community: comm,
		})
	}

	// Sort by node ID for deterministic output.
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})

	return out, nil
}

// deriveSeed computes a deterministic int64 seed from a slice of strings.
// The members are sorted before hashing so the result is independent of
// input order.
func deriveSeed(members []string) int64 {
	sorted := make([]string, len(members))
	copy(sorted, members)
	sort.Strings(sorted)
	h := fnv.New64a()
	for _, s := range sorted {
		h.Write([]byte(s))
		h.Write([]byte{0})
	}
	return int64(h.Sum64())
}

// cohesionScore = (intra-community edges) / (possible pairs). Self-loops
// are skipped so the score stays bounded in [0, 1]. Uses adj instead of
// scanning the full edge list — for the typical case of many small
// communities the cost drops from O(|C|·|E|) to O(Σ degree within C).
func cohesionScore(members []string, adj map[string]map[string]float64) float64 {
	n := len(members)
	if n == 0 {
		return 0.0
	}
	if n == 1 {
		return 1.0
	}

	memberSet := make(map[string]bool, n)
	for _, id := range members {
		memberSet[id] = true
	}

	// adj is symmetric; visit each undirected pair once by requiring src<dst.
	actual := 0
	for _, src := range members {
		for dst := range adj[src] {
			if src == dst {
				continue
			}
			if src < dst && memberSet[dst] {
				actual++
			}
		}
	}

	possible := float64(n) * float64(n-1) / 2
	return float64(actual) / possible
}

// splitCommunity runs a second Leiden pass on the subgraph induced by members.
// If the subgraph is edgeless, each member becomes its own singleton community.
// If Leiden cannot split the subgraph (returns a single community), the original
// members are returned unchanged.
func (c *LeidenClusterer) splitCommunity(members []string, adj map[string]map[string]float64) ([][]string, error) {
	memberSet := make(map[string]bool, len(members))
	for _, id := range members {
		memberSet[id] = true
	}

	subAdj := make(map[string]map[string]float64, len(members))
	for _, id := range members {
		subAdj[id] = make(map[string]float64)
	}

	hasEdges := false
	for _, id := range members {
		// Iterate over neighbors in sorted order for determinism.
		neighbors := make([]string, 0, len(adj[id]))
		for n := range adj[id] {
			neighbors = append(neighbors, n)
		}
		sort.Strings(neighbors)
		for _, neighbor := range neighbors {
			if memberSet[neighbor] {
				subAdj[id][neighbor] = adj[id][neighbor]
				hasEdges = true
			}
		}
	}

	if !hasEdges {
		result := make([][]string, len(members))
		for i, id := range members {
			result[i] = []string{id}
		}
		return result, nil
	}

	subgraph := leiden.NewGraph(subAdj)
	// Derive a deterministic seed from the sorted member list so that
	// splitCommunity is deterministic regardless of call order or any
	// global RNG state inside leiden-go.
	seed := deriveSeed(members)
	config := &leiden.Config{
		Resolution:        c.resolution,
		MaxIterations:     c.maxIterations,
		MinModularityGain: c.minModularityGain,
		RandomSeed:        seed,
	}

	result, err := leiden.Leiden(subgraph, config)
	if err != nil {
		return nil, fmt.Errorf("leiden subgraph failed: %w", err)
	}

	communities := result.Partition.Communities()
	if len(communities) <= 1 {
		return [][]string{members}, nil
	}

	var out [][]string
	assigned := make(map[string]bool)
	for _, commMembers := range communities {
		m := make([]string, len(commMembers))
		copy(m, commMembers)
		sort.Strings(m)
		out = append(out, m)
		for _, id := range m {
			assigned[id] = true
		}
	}

	for _, id := range members {
		if !assigned[id] {
			out = append(out, []string{id})
		}
	}

	return out, nil
}
