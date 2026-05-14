// Package benchmark measures token-reduction: how many fewer tokens an
// LLM needs to read to answer a question against the graph compared to
// reading the whole corpus.
//
// Mirrors upstream graphify's benchmark.py: BFS from best-label-match
// nodes to depth N, format the subgraph as NODE/EDGE lines, then
// compare estimated tokens against a corpus-tokens baseline derived
// from word count.
package benchmark

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/julianshen/gogfy/internal/schema"
)

// defaultCharsPerToken is the standard approximation: ~4 ASCII chars per token.
const defaultCharsPerToken = 4

// defaultDepth controls how far BFS expands from the question's seed nodes.
const defaultDepth = 3

// wordsPerNodeEstimate is the upstream heuristic: each node is ~3 words
// of identifier + ~47 words of source context. Used only when the
// caller didn't supply an authoritative CorpusWords (from detect()).
const wordsPerNodeEstimate = 50

// defaultQuestions are the five sample prompts the report uses to
// produce a representative reduction-ratio. Mirrors upstream exactly so
// gogfy and graphify benchmark outputs are comparable on the same repo.
var defaultQuestions = []string{
	"how does authentication work",
	"what is the main entry point",
	"how are errors handled",
	"what connects the data layer to the api",
	"what are the core abstractions",
}

// Options tunes a benchmark run.
type Options struct {
	// CorpusWords is the authoritative word count for the source
	// corpus (e.g. from `detect` output). When 0, estimated from the
	// node count: nodes * wordsPerNodeEstimate.
	CorpusWords int

	// Questions overrides the default sample questions entirely. When
	// nil or empty, defaultQuestions is used.
	Questions []string

	// Depth bounds the BFS expansion from each question's seeds. When
	// 0, defaultDepth is used. Negative values are treated as 0.
	Depth int

	// CharsPerToken is the chars→tokens approximation. When 0,
	// defaultCharsPerToken is used.
	CharsPerToken int
}

// PerQuestion captures the per-prompt cost and savings.
type PerQuestion struct {
	Question    string  `json:"question"`
	QueryTokens int     `json:"query_tokens"`
	Reduction   float64 `json:"reduction"`
}

// Result is the full benchmark report.
type Result struct {
	CorpusTokens   int           `json:"corpus_tokens"`
	CorpusWords    int           `json:"corpus_words"`
	Nodes          int           `json:"nodes"`
	Edges          int           `json:"edges"`
	AvgQueryTokens int           `json:"avg_query_tokens"`
	ReductionRatio float64       `json:"reduction_ratio"`
	PerQuestion    []PerQuestion `json:"per_question"`
}

// Run executes the benchmark against the given graph and options.
//
// Returns an error when *no* configured question matches any node — a
// silent zero-ratio result would be misleading (the user likely forgot
// to build the graph, or supplied prompts unrelated to the corpus).
func Run(nodes []schema.Node, edges []schema.Edge, opts Options) (Result, error) {
	cpt := opts.CharsPerToken
	if cpt <= 0 {
		cpt = defaultCharsPerToken
	}
	depth := opts.Depth
	if depth < 0 {
		depth = 0
	}
	if depth == 0 && opts.Depth == 0 {
		depth = defaultDepth
	}
	questions := opts.Questions
	if len(questions) == 0 {
		questions = defaultQuestions
	}

	corpusWords := opts.CorpusWords
	if corpusWords <= 0 {
		corpusWords = len(nodes) * wordsPerNodeEstimate
	}
	// Upstream conversion: 100 words ≈ 133 tokens (words * 100 / 75).
	corpusTokens := corpusWords * 100 / 75

	adj, nodeByID := buildAdjacency(nodes, edges)

	per := make([]PerQuestion, 0, len(questions))
	for _, q := range questions {
		qt := queryTokens(q, nodes, edges, adj, nodeByID, depth, cpt)
		if qt <= 0 {
			continue
		}
		reduction := 0.0
		if corpusTokens > 0 {
			reduction = roundOne(float64(corpusTokens) / float64(qt))
		}
		per = append(per, PerQuestion{Question: q, QueryTokens: qt, Reduction: reduction})
	}

	if len(per) == 0 {
		return Result{}, errors.New("benchmark: no matching nodes found for any sample question (is the graph built? are your prompts on-topic?)")
	}

	totalQT := 0
	for _, p := range per {
		totalQT += p.QueryTokens
	}
	avg := totalQT / len(per)
	ratio := 0.0
	if avg > 0 {
		ratio = roundOne(float64(corpusTokens) / float64(avg))
	}

	return Result{
		CorpusTokens:   corpusTokens,
		CorpusWords:    corpusWords,
		Nodes:          len(nodes),
		Edges:          len(edges),
		AvgQueryTokens: avg,
		ReductionRatio: ratio,
		PerQuestion:    per,
	}, nil
}

// Render prints a human-readable report (mirrors upstream layout).
func Render(r Result, w io.Writer) error {
	if r.Nodes == 0 && r.CorpusTokens == 0 && len(r.PerQuestion) == 0 {
		return errors.New("benchmark: empty Result — call Run first")
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	fmt.Fprintln(w, "gogfy token reduction benchmark")
	fmt.Fprintln(w, strings.Repeat("-", 50))
	fmt.Fprintf(w, "  Corpus:          %s words -> ~%s tokens (naive)\n",
		commas(r.CorpusWords), commas(r.CorpusTokens))
	fmt.Fprintf(w, "  Graph:           %s nodes, %s edges\n", commas(r.Nodes), commas(r.Edges))
	fmt.Fprintf(w, "  Avg query cost:  ~%s tokens\n", commas(r.AvgQueryTokens))
	fmt.Fprintf(w, "  Reduction:       %sx fewer tokens per query\n", trimFloat(r.ReductionRatio))
	fmt.Fprintln(w)
	fmt.Fprintln(w, "  Per question:")
	for _, p := range r.PerQuestion {
		fmt.Fprintf(w, "    [%sx] %s\n", trimFloat(p.Reduction), truncate(p.Question, 55))
	}
	fmt.Fprintln(w)
	return nil
}

// estimateTokens approximates LLM token count via chars/cpt with a
// floor of 1 (matches upstream `max(1, len // 4)`).
func estimateTokens(text string, charsPerToken int) int {
	if charsPerToken <= 0 {
		charsPerToken = defaultCharsPerToken
	}
	if n := len(text) / charsPerToken; n > 0 {
		return n
	}
	return 1
}

// buildAdjacency returns an undirected adjacency map keyed by node ID
// plus an id→node lookup. Neighbors are slices (sorted lazily on first
// use in BFS for determinism).
func buildAdjacency(nodes []schema.Node, edges []schema.Edge) (map[string][]string, map[string]schema.Node) {
	adj := make(map[string][]string, len(nodes))
	nodeByID := make(map[string]schema.Node, len(nodes))
	for _, n := range nodes {
		nodeByID[n.ID] = n
		// Initialize so isolates appear in adj with an empty slice.
		if _, ok := adj[n.ID]; !ok {
			adj[n.ID] = nil
		}
	}
	for _, e := range edges {
		adj[e.Source] = append(adj[e.Source], e.Target)
		adj[e.Target] = append(adj[e.Target], e.Source)
	}
	// Sort neighbor lists once so BFS expansion is deterministic.
	for id := range adj {
		sort.Strings(adj[id])
	}
	return adj, nodeByID
}

// queryTokens runs BFS from the top-scoring label-match nodes for one
// question and returns the estimated token cost of the
// NODE/EDGE-formatted subgraph context.
func queryTokens(
	question string,
	nodes []schema.Node,
	edges []schema.Edge,
	adj map[string][]string,
	nodeByID map[string]schema.Node,
	depth, charsPerToken int,
) int {
	seeds := bestMatches(question, nodes)
	if len(seeds) == 0 {
		return 0
	}

	visited := make(map[string]bool, len(seeds))
	for _, id := range seeds {
		visited[id] = true
	}
	frontier := append([]string(nil), seeds...)

	type edgeSeen struct{ u, v string }
	var edgesSeen []edgeSeen

	for i := 0; i < depth; i++ {
		var next []string
		for _, u := range frontier {
			// adj[u] is pre-sorted in buildAdjacency.
			for _, v := range adj[u] {
				if !visited[v] {
					visited[v] = true
					next = append(next, v)
					edgesSeen = append(edgesSeen, edgeSeen{u, v})
				}
			}
		}
		frontier = next
	}

	// Build the NODE/EDGE block. Iterate visited in sorted order so
	// the token estimate is independent of map iteration order.
	visIDs := make([]string, 0, len(visited))
	for id := range visited {
		visIDs = append(visIDs, id)
	}
	sort.Strings(visIDs)

	var b strings.Builder
	for _, id := range visIDs {
		n := nodeByID[id]
		label := n.Label
		if label == "" {
			label = id
		}
		fmt.Fprintf(&b, "NODE %s src=%s loc=%s\n", label, n.SourceFile, n.SourceLocation)
	}
	// Resolve edge relation by looking up the first matching edge in
	// the original list (cheap; edge counts are small per question).
	relation := func(u, v string) string {
		for _, e := range edges {
			if (e.Source == u && e.Target == v) || (e.Source == v && e.Target == u) {
				return e.Relation
			}
		}
		return ""
	}
	for _, e := range edgesSeen {
		if visited[e.u] && visited[e.v] {
			fmt.Fprintf(&b, "EDGE %s --%s--> %s\n",
				nodeByID[e.u].Label, relation(e.u, e.v), nodeByID[e.v].Label)
		}
	}

	// Drop the trailing newline so length matches upstream's
	// "\n".join(lines).
	s := strings.TrimRight(b.String(), "\n")
	return estimateTokens(s, charsPerToken)
}

// bestMatches scores nodes by how many >2-char question terms appear
// in their label (case-insensitive substring), and returns the top 3
// node IDs with score > 0. Ties are broken by node ID (sorted) so the
// result is deterministic.
func bestMatches(question string, nodes []schema.Node) []string {
	terms := termsOf(question)
	if len(terms) == 0 {
		return nil
	}
	type scored struct {
		id    string
		score int
	}
	var matches []scored
	for _, n := range nodes {
		label := strings.ToLower(n.Label)
		s := 0
		for _, t := range terms {
			if strings.Contains(label, t) {
				s++
			}
		}
		if s > 0 {
			matches = append(matches, scored{id: n.ID, score: s})
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].score != matches[j].score {
			return matches[i].score > matches[j].score
		}
		return matches[i].id < matches[j].id
	})
	if len(matches) > 3 {
		matches = matches[:3]
	}
	out := make([]string, len(matches))
	for i, m := range matches {
		out[i] = m.id
	}
	return out
}

func termsOf(question string) []string {
	var out []string
	for _, t := range strings.Fields(question) {
		if len(t) > 2 {
			out = append(out, strings.ToLower(t))
		}
	}
	return out
}

// roundOne rounds to 1 decimal place (matches Python `round(x, 1)`).
func roundOne(x float64) float64 {
	scaled := x * 10
	if scaled >= 0 {
		scaled += 0.5
	} else {
		scaled -= 0.5
	}
	return float64(int64(scaled)) / 10
}

// trimFloat formats a float that's been rounded to one decimal, dropping
// the ".0" when whole so "10.0x" prints as "10x" (matches upstream).
func trimFloat(f float64) string {
	s := fmt.Sprintf("%.1f", f)
	s = strings.TrimSuffix(s, ".0")
	return s
}

func commas(n int) string {
	s := fmt.Sprintf("%d", n)
	if n < 0 {
		return "-" + commas(-n)
	}
	if len(s) <= 3 {
		return s
	}
	var out strings.Builder
	pre := len(s) % 3
	if pre > 0 {
		out.WriteString(s[:pre])
		if len(s) > pre {
			out.WriteByte(',')
		}
	}
	for i := pre; i < len(s); i += 3 {
		out.WriteString(s[i : i+3])
		if i+3 < len(s) {
			out.WriteByte(',')
		}
	}
	return out.String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
