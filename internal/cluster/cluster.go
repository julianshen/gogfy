// Package cluster groups graph nodes into communities using modularity-based clustering.
package cluster

import (
	"fmt"
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
type LeidenClusterer struct {
	resolution        float64
	maxIterations     int
	minModularityGain float64
	randomSeed        int64
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

// NewLeidenClusterer creates a new LeidenClusterer with the given options.
func NewLeidenClusterer(opts ...LeidenOption) *LeidenClusterer {
	c := &LeidenClusterer{
		resolution:        1.0,
		maxIterations:     100,
		minModularityGain: 0.0001,
		randomSeed:        42,
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

	// Build node -> community mapping.
	// Leiden community IDs are non-deterministic even with a fixed seed,
	// so we remap them to stable IDs derived from sorted member lists.
	communities := result.Partition.Communities()
	type commEntry struct {
		id      int
		members []string
	}
	entries := make([]commEntry, 0, len(communities))
	for commID, members := range communities {
		m := make([]string, len(members))
		copy(m, members)
		sort.Strings(m)
		entries = append(entries, commEntry{id: commID, members: m})
	}
	// Sort communities by their first member for stable ordering.
	sort.Slice(entries, func(i, j int) bool {
		if len(entries[i].members) == 0 {
			return true
		}
		if len(entries[j].members) == 0 {
			return false
		}
		return entries[i].members[0] < entries[j].members[0]
	})
	stableCommID := make(map[int]string, len(entries))
	for i, e := range entries {
		stableCommID[e.id] = strconv.Itoa(i)
	}

	nodeToComm := make(map[string]string, len(nodes))
	for commID, members := range communities {
		cid := stableCommID[commID]
		for _, nodeID := range members {
			nodeToComm[nodeID] = cid
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
			// Isolated nodes not in any community get their own stable ID.
			clone.Community = strconv.Itoa(len(entries) + len(out))
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
			comm = strconv.Itoa(len(entries) + len(out))
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
