package dedup

import (
	"fmt"
	"runtime"
	"sort"
	"sync"

	"github.com/dgryski/go-minhash"
	"github.com/dgryski/go-spooky"
	"github.com/julianshen/gogfy/internal/schema"
	"github.com/xrash/smetrics"
)

func sortStringSlice(s []string) { sort.Strings(s) }

const (
	entropyThreshold = 2.5
	lshThreshold     = 0.7
	mergeThreshold   = 92.0
	communityBoost   = 5.0
	numPerm          = 128
	shingleSize      = 3
)

// pass2Fuzzy performs MinHash/LSH + Jaro-Winkler fuzzy deduplication.
// It takes the current union-find (from pass 1), nodes, and community mapping.
func pass2Fuzzy(nodes []schema.Node, uf *UnionFind, communities map[string]string) error {
	// Filter candidates by entropy
	candidates := make([]schema.Node, 0, len(nodes))
	for _, n := range nodes {
		label := n.Label
		if label == "" {
			label = n.ID
		}
		if entropy(label) >= entropyThreshold {
			candidates = append(candidates, n)
		}
	}
	if len(candidates) < 2 {
		return nil
	}

	// Precompute per-candidate invariants ONCE rather than recomputing
	// them inside the O(n²) comparison loop. The original recomputed
	// mergeBucket (→ ParseLangID) and normalize for both endpoints of
	// every pair — ~30M redundant string operations on a few-thousand-node
	// graph. Hoisting them out is the dominant speedup here.
	n := len(candidates)
	sketch := make([]*minhash.BottomK, n)  // MinHash sketch
	jwLabel := make([]string, n)           // label used for Jaro-Winkler
	bucket := make([]string, n)            // kind bucket (mergeBucket)
	comm := make([]string, n)              // community id
	root := make([]string, n)              // pass-1 union-find root snapshot
	for i, c := range candidates {
		label := c.Label
		if label == "" {
			label = c.ID
		}
		nl := normalize(label)
		m := minhash.NewBottomK(spooky.Hash64, numPerm)
		for _, s := range shingles(nl, shingleSize) {
			m.Push([]byte(s))
		}
		sketch[i] = m
		// Jaro-Winkler used normalize(Label) with a fallback to
		// normalize(ID) — preserve that exact precedence.
		jl := normalize(c.Label)
		if jl == "" {
			jl = normalize(c.ID)
		}
		jwLabel[i] = jl
		bucket[i] = mergeBucket(c.ID)
		comm[i] = communities[c.ID]
		root[i] = uf.Find(c.ID)
	}

	// LSH neighbor search. We lack a banded LSH index, so this is still a
	// pairwise O(n²) scan — but it's now parallel and allocation-free in
	// the hot loop. The per-source rows are independent: each worker
	// collects the index pairs that pass the merge threshold, and the
	// union-find mutations are applied serially afterward. The final
	// connected components (and thus the dedup result) are independent of
	// union order, so deferring the unions is exact. The root[] snapshot
	// reproduces pass-1's already-merged early-skip without racing on
	// union-find's path-compressing Find.
	type idxPair struct{ i, j int }
	workers := runtime.GOMAXPROCS(0)
	if workers > n {
		workers = n
	}
	if workers < 1 {
		workers = 1
	}
	partials := make([][]idxPair, workers)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			var local []idxPair
			for i := w; i < n; i += workers {
				for j := i + 1; j < n; j++ {
					if root[i] == root[j] || bucket[i] != bucket[j] {
						continue
					}
					if sketch[i].Similarity(sketch[j]) < lshThreshold {
						continue
					}
					jw := smetrics.JaroWinkler(jwLabel[i], jwLabel[j], 0.7, 4) * 100
					if comm[i] == comm[j] {
						jw += communityBoost
					}
					if jw >= mergeThreshold {
						local = append(local, idxPair{i, j})
					}
				}
			}
			partials[w] = local
		}(w)
	}
	wg.Wait()

	for _, pl := range partials {
		for _, p := range pl {
			a, b := candidates[p.i], candidates[p.j]
			winner := pickWinner([]schema.Node{a, b})
			uf.Union(winner.ID, a.ID)
			uf.Union(winner.ID, b.ID)
		}
	}

	return nil
}

// pass3LLM performs LLM-based tiebreaking for ambiguous pairs.
// This is a placeholder for the optional LLM tiebreaker.
type LLMBackend interface {
	// AreSameConcept returns true if the two labels refer to the same real-world concept.
	AreSameConcept(labelA, labelB string) (bool, error)
}

// pass3LLM resolves ambiguous pairs with Jaro-Winkler score in [75.0, 92.0).
func pass3LLM(nodes []schema.Node, uf *UnionFind, communities map[string]string, backend LLMBackend) error {
	if backend == nil {
		return nil
	}

	// Find ambiguous pairs
	var pairs [][2]schema.Node
	for i := 0; i < len(nodes); i++ {
		for j := i + 1; j < len(nodes); j++ {
			a, b := nodes[i], nodes[j]
			if uf.Find(a.ID) == uf.Find(b.ID) {
				continue
			}
			// Kind-scoped: don't ask the LLM whether a type and a
			// module with the same name are the same concept.
			if mergeBucket(a.ID) != mergeBucket(b.ID) {
				continue
			}

			labelA := normalize(a.Label)
			if labelA == "" {
				labelA = normalize(a.ID)
			}
			labelB := normalize(b.Label)
			if labelB == "" {
				labelB = normalize(b.ID)
			}

			jw := smetrics.JaroWinkler(labelA, labelB, 0.7, 4) * 100
			if communities[a.ID] == communities[b.ID] {
				jw += communityBoost
			}

			if jw >= 75.0 && jw < mergeThreshold {
				pairs = append(pairs, [2]schema.Node{a, b})
			}
		}
	}

	// Process in batches of 30
	batchSize := 30
	for i := 0; i < len(pairs); i += batchSize {
		end := i + batchSize
		if end > len(pairs) {
			end = len(pairs)
		}
		batch := pairs[i:end]
		for _, pair := range batch {
			same, err := backend.AreSameConcept(pair[0].Label, pair[1].Label)
			if err != nil {
				// Skip on error
				continue
			}
			if same {
				winner := pickWinner([]schema.Node{pair[0], pair[1]})
				uf.Union(winner.ID, pair[0].ID)
				uf.Union(winner.ID, pair[1].ID)
			}
		}
	}

	return nil
}

// Deduplicator performs three-pass entity deduplication.
type Deduplicator struct {
	LLMBackend LLMBackend
}

// NewDeduplicator creates a new Deduplicator.
func NewDeduplicator() *Deduplicator {
	return &Deduplicator{}
}

// Deduplicate performs three-pass deduplication on the given nodes and edges.
// It returns the deduplicated nodes, edges, and a remap from old IDs to winner IDs.
func (d *Deduplicator) Deduplicate(nodes []schema.Node, edges []schema.Edge, communities map[string]string) ([]schema.Node, []schema.Edge, map[string]string, error) {
	if len(nodes) == 0 {
		return []schema.Node{}, []schema.Edge{}, map[string]string{}, nil
	}

	// Pass 1: Exact normalization
	uf := pass1Exact(nodes)

	// Pass 2: MinHash/LSH + Jaro-Winkler
	if err := pass2Fuzzy(nodes, uf, communities); err != nil {
		return nil, nil, nil, fmt.Errorf("pass 2 fuzzy dedup: %w", err)
	}

	// Pass 3: LLM tiebreaker
	if d.LLMBackend != nil {
		if err := pass3LLM(nodes, uf, communities, d.LLMBackend); err != nil {
			return nil, nil, nil, fmt.Errorf("pass 3 LLM dedup: %w", err)
		}
	}

	// Build remap. Sort component members for determinism — Components()
	// iterates a map under the hood, so without this sort tie-broken
	// winner selection (pickWinner sees equal-score candidates in
	// different orders) yields different winners across runs.
	remap := make(map[string]string)
	comps := uf.Components()
	for _, members := range comps {
		sortStringSlice(members)
		if len(members) < 2 {
			continue
		}
		// Find winner node
		var memberNodes []schema.Node
		for _, id := range members {
			for _, n := range nodes {
				if n.ID == id {
					memberNodes = append(memberNodes, n)
					break
				}
			}
		}
		winner := pickWinner(memberNodes)
		for _, id := range members {
			if id != winner.ID {
				remap[id] = winner.ID
			}
		}
	}

	// Build deduplicated nodes (unique by ID, keep winner)
	nodeMap := make(map[string]schema.Node, len(nodes))
	for _, n := range nodes {
		if winnerID, ok := remap[n.ID]; ok {
			nodeMap[winnerID] = nodeMap[winnerID]
		} else {
			nodeMap[n.ID] = n
		}
	}

	// Build deduplicated edges with remapped endpoints
	edgeMap := make(map[[3]string]schema.Edge)
	for _, e := range edges {
		src := remap[e.Source]
		if src == "" {
			src = e.Source
		}
		tgt := remap[e.Target]
		if tgt == "" {
			tgt = e.Target
		}
		if src == tgt {
			continue // drop self-loops
		}
		key := [3]string{src, tgt, e.Relation}
		edgeMap[key] = schema.Edge{
			Source:     src,
			Target:     tgt,
			Relation:   e.Relation,
			Confidence: e.Confidence,
		}
	}

	// Convert maps to slices
	outNodes := make([]schema.Node, 0, len(nodeMap))
	for _, n := range nodeMap {
		outNodes = append(outNodes, n)
	}
	schema.SortNodesByID(outNodes)

	outEdges := make([]schema.Edge, 0, len(edgeMap))
	for _, e := range edgeMap {
		outEdges = append(outEdges, e)
	}

	return outNodes, outEdges, remap, nil
}
