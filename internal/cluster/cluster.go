// Package cluster groups graph nodes into communities using modularity-based clustering.
package cluster

import (
	"fmt"
	"hash/fnv"
	"sort"
	"strconv"

	"github.com/julianshen/gogfy/internal/schema"
	leiden "github.com/vsuryav/leiden-go"
)

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

// WithMaxCommunityFraction sets the fraction of total nodes above which a
// community is considered oversized and subject to splitting.
func WithMaxCommunityFraction(f float64) LeidenOption {
	return func(c *LeidenClusterer) { c.maxCommunityFraction = f }
}

// WithMinSplitSize sets the minimum size a community must have before it can
// be split for being oversized.
func WithMinSplitSize(n int) LeidenOption {
	return func(c *LeidenClusterer) { c.minSplitSize = n }
}

// WithCohesionThreshold sets the cohesion score below which a large community
// is re-split in a second pass.
func WithCohesionThreshold(t float64) LeidenOption {
	return func(c *LeidenClusterer) { c.cohesionThreshold = t }
}

// WithCohesionMinSize sets the minimum size for a community to be considered
// for cohesion-based re-splitting.
func WithCohesionMinSize(n int) LeidenOption {
	return func(c *LeidenClusterer) { c.cohesionMinSize = n }
}

// NewLeidenClusterer creates a new LeidenClusterer with the given options.
func NewLeidenClusterer(opts ...LeidenOption) *LeidenClusterer {
	c := &LeidenClusterer{
		resolution:           1.0,
		maxIterations:        100,
		minModularityGain:    0.0001,
		randomSeed:           42,
		maxCommunityFraction: 0.25,
		minSplitSize:         10,
		cohesionThreshold:    0.05,
		cohesionMinSize:      50,
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
		return nil, fmt.Errorf("leiden clustering failed: %w", err)
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

	// Compute max community size threshold.
	maxSize := max(c.minSplitSize, int(float64(len(nodes))*c.maxCommunityFraction))

	// Split oversized communities.
	var split [][]string
	for _, members := range memberLists {
		if len(members) > maxSize {
			subs, err := c.splitCommunity(members, adj)
			if err != nil {
				return nil, fmt.Errorf("splitCommunity oversized: %w", err)
			}
			split = append(split, subs...)
		} else {
			split = append(split, members)
		}
	}

	// Re-split low-cohesion communities.
	var secondPass [][]string
	for _, members := range split {
		if len(members) >= c.cohesionMinSize && cohesionScore(members, edges) < c.cohesionThreshold {
			subs, err := c.splitCommunity(members, adj)
			if err != nil {
				return nil, fmt.Errorf("splitCommunity cohesion: %w", err)
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

func cohesionScore(members []string, edges []schema.Edge) float64 {
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

	seen := make(map[[2]string]bool)
	actual := 0
	for _, e := range edges {
		if memberSet[e.Source] && memberSet[e.Target] {
			pair := [2]string{e.Source, e.Target}
			if pair[0] > pair[1] {
				pair[0], pair[1] = pair[1], pair[0]
			}
			if !seen[pair] {
				seen[pair] = true
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
