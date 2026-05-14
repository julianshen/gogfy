// Package benchmark measures token-reduction: how many fewer tokens an
// LLM needs to read to answer a question against the graph compared to
// reading the whole corpus.
//
// Mirrors upstream graphify's benchmark.py — BFS from best-label-match
// nodes to depth N, format the subgraph as NODE/EDGE lines, then
// compare estimated tokens against a corpus-tokens baseline derived
// from word count (words * 100 / 75 ≈ tokens).
package benchmark

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/julianshen/gogfy/internal/schema"
)

const (
	defaultCharsPerToken = 4
	defaultDepth         = 3
	// wordsPerNodeEstimate: ~3 words of identifier + ~47 words of
	// source context per node. Only used when CorpusWords is unset.
	wordsPerNodeEstimate = 50
)

// defaultQuestions mirrors upstream so gogfy and graphify benchmarks
// on the same repo produce directly comparable numbers.
var defaultQuestions = []string{
	"how does authentication work",
	"what is the main entry point",
	"how are errors handled",
	"what connects the data layer to the api",
	"what are the core abstractions",
}

// Options tunes a benchmark run.
type Options struct {
	// CorpusWords is the authoritative word count from detect(). When
	// 0, estimated from node count * wordsPerNodeEstimate.
	CorpusWords int

	// Questions overrides the default sample questions entirely. When
	// nil or empty, defaultQuestions is used.
	Questions []string

	// Depth bounds BFS expansion from each question's seeds. Zero or
	// negative selects defaultDepth.
	Depth int

	// CharsPerToken is the chars→tokens approximation. Zero selects
	// defaultCharsPerToken.
	CharsPerToken int
}

// PerQuestion captures one prompt's cost and savings ratio.
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

// Run executes the benchmark.
//
// Returns an error when no configured question matches any node — a
// silent zero-ratio result would mislead the user (most common cause:
// graph not built, or prompts unrelated to the corpus).
func Run(nodes []schema.Node, edges []schema.Edge, opts Options) (Result, error) {
	cpt := opts.CharsPerToken
	if cpt <= 0 {
		cpt = defaultCharsPerToken
	}
	depth := opts.Depth
	if depth <= 0 {
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
	// Upstream conversion: 100 words ≈ 133 tokens.
	corpusTokens := corpusWords * 100 / 75

	ctx := newQueryContext(nodes, edges, depth, cpt)

	per := make([]PerQuestion, 0, len(questions))
	for _, q := range questions {
		qt := ctx.queryTokens(q)
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

// Render prints a human-readable report mirroring upstream's layout.
func Render(r Result, w io.Writer) error {
	if r.Nodes == 0 && len(r.PerQuestion) == 0 {
		return errors.New("benchmark: empty Result — call Run first")
	}
	fmt.Fprintln(w)
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

// estimateTokens approximates LLM tokens via chars/cpt with a floor of
// 1 (matches upstream's `max(1, len // 4)`).
func estimateTokens(text string, charsPerToken int) int {
	if charsPerToken <= 0 {
		charsPerToken = defaultCharsPerToken
	}
	if n := len(text) / charsPerToken; n > 0 {
		return n
	}
	return 1
}

// queryContext holds the per-Run-invariant indexes. Building these
// once (instead of per-question) makes question-scaling O(Q) instead
// of O(Q·N).
type queryContext struct {
	nodes         []schema.Node
	adj           map[string][]string
	nodeByID      map[string]schema.Node
	relation      map[edgeKey]string
	lowerLabels   []string // parallel to nodes
	depth         int
	charsPerToken int
}

type edgeKey struct{ a, b string }

func makeEdgeKey(u, v string) edgeKey {
	if u <= v {
		return edgeKey{u, v}
	}
	return edgeKey{v, u}
}

func newQueryContext(nodes []schema.Node, edges []schema.Edge, depth, cpt int) *queryContext {
	adj := make(map[string][]string, len(nodes))
	nodeByID := make(map[string]schema.Node, len(nodes))
	for _, n := range nodes {
		nodeByID[n.ID] = n
	}
	relation := make(map[edgeKey]string, len(edges))
	for _, e := range edges {
		adj[e.Source] = append(adj[e.Source], e.Target)
		adj[e.Target] = append(adj[e.Target], e.Source)
		k := makeEdgeKey(e.Source, e.Target)
		if _, ok := relation[k]; !ok {
			relation[k] = e.Relation
		}
	}
	for id := range adj {
		sort.Strings(adj[id])
	}
	lowerLabels := make([]string, len(nodes))
	for i, n := range nodes {
		lowerLabels[i] = strings.ToLower(n.Label)
	}
	return &queryContext{
		nodes:         nodes,
		adj:           adj,
		nodeByID:      nodeByID,
		relation:      relation,
		lowerLabels:   lowerLabels,
		depth:         depth,
		charsPerToken: cpt,
	}
}

// queryTokens runs BFS from the top label-match seeds and returns the
// estimated token cost of the NODE/EDGE-formatted subgraph.
func (q *queryContext) queryTokens(question string) int {
	seeds := q.bestMatches(question)
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

	for i := 0; i < q.depth && len(frontier) > 0; i++ {
		var next []string
		for _, u := range frontier {
			for _, v := range q.adj[u] {
				if !visited[v] {
					visited[v] = true
					next = append(next, v)
					edgesSeen = append(edgesSeen, edgeSeen{u, v})
				}
			}
		}
		frontier = next
	}

	visIDs := make([]string, 0, len(visited))
	for id := range visited {
		visIDs = append(visIDs, id)
	}
	sort.Strings(visIDs)

	var b strings.Builder
	for _, id := range visIDs {
		n := q.nodeByID[id]
		label := n.Label
		if label == "" {
			label = id
		}
		fmt.Fprintf(&b, "NODE %s src=%s loc=%s\n", label, n.SourceFile, n.SourceLocation)
	}
	for _, e := range edgesSeen {
		fmt.Fprintf(&b, "EDGE %s --%s--> %s\n",
			q.nodeByID[e.u].Label, q.relation[makeEdgeKey(e.u, e.v)], q.nodeByID[e.v].Label)
	}

	return estimateTokens(strings.TrimRight(b.String(), "\n"), q.charsPerToken)
}

// bestMatches returns the top-3 node IDs by lowercase label-substring
// hit count against the question's >2-char terms. Ties break on ID for
// determinism.
func (q *queryContext) bestMatches(question string) []string {
	terms := termsOf(question)
	if len(terms) == 0 {
		return nil
	}
	type scored struct {
		id    string
		score int
	}
	var matches []scored
	for i, n := range q.nodes {
		label := q.lowerLabels[i]
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

// roundOne rounds to one decimal (matches Python `round(x, 1)`).
func roundOne(x float64) float64 {
	scaled := x*10 + 0.5
	return float64(int64(scaled)) / 10
}

// trimFloat formats a 1-decimal float, dropping ".0" when whole so
// "10.0x" prints as "10x" (matches upstream).
func trimFloat(f float64) string {
	return strings.TrimSuffix(fmt.Sprintf("%.1f", f), ".0")
}

// commas formats a non-negative int with comma thousands separators.
func commas(n int) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var out strings.Builder
	pre := len(s) % 3
	if pre > 0 {
		out.WriteString(s[:pre])
		out.WriteByte(',')
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
