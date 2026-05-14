// Package benchmark measures token-reduction: how many fewer tokens an
// LLM needs to read to answer a question against the graph compared to
// reading the whole corpus. Modeled after graphify's benchmark.py;
// formulae (words×100/75, len/4-with-floor-1) are kept aligned so
// per-repo numbers stay comparable across the two tools.
package benchmark

import (
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"

	"github.com/julianshen/gogfy/internal/schema"
)

const (
	defaultCharsPerToken = 4
	defaultDepth         = 3
	// wordsPerNodeEstimate is the fallback when CorpusWords is unset;
	// rough average for source-code corpora (50 ≈ identifier + a few
	// lines of surrounding context per node).
	wordsPerNodeEstimate = 50
)

// defaultQuestions is kept aligned with upstream graphify so per-repo
// numbers can be cross-compared without renormalizing.
var defaultQuestions = []string{
	"how does authentication work",
	"what is the main entry point",
	"how are errors handled",
	"what connects the data layer to the api",
	"what are the core abstractions",
}

// Options tunes a benchmark run. Zero/negative on any field selects
// the package default. CorpusWords carries the authoritative count
// from detect() when available.
type Options struct {
	CorpusWords   int
	Questions     []string
	Depth         int
	CharsPerToken int
}

type PerQuestion struct {
	Question    string  `json:"question"`
	QueryTokens int     `json:"query_tokens"`
	Reduction   float64 `json:"reduction"`
}

// Result is the full benchmark report. Treat as immutable after Run;
// fields are derivable from each other (CorpusTokens from CorpusWords,
// ReductionRatio from CorpusTokens/AvgQueryTokens) and mutating one in
// isolation produces an inconsistent record.
type Result struct {
	CorpusTokens   int           `json:"corpus_tokens"`
	CorpusWords    int           `json:"corpus_words"`
	Nodes          int           `json:"nodes"`
	Edges          int           `json:"edges"`
	AvgQueryTokens int           `json:"avg_query_tokens"`
	ReductionRatio float64       `json:"reduction_ratio"`
	PerQuestion    []PerQuestion `json:"per_question"`
	// Skipped lists questions that produced zero seed-matches. Empty
	// for the default question set on a typical repo; non-empty
	// signals the caller that some prompts in a custom list were
	// off-topic (the aggregate average ignored them).
	Skipped []string `json:"skipped,omitempty"`
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
	var skipped []string
	for _, q := range questions {
		qt := ctx.queryTokens(q)
		if qt <= 0 {
			skipped = append(skipped, q)
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
	// When the caller supplied a custom question list, missing more
	// than half is a strong signal of misuse (wrong corpus, typos)
	// rather than expected sparseness. Default questions are
	// best-effort by design.
	if len(opts.Questions) > 0 && len(skipped)*2 > len(opts.Questions) {
		return Result{}, fmt.Errorf("benchmark: %d of %d supplied questions matched no nodes (skipped: %v)",
			len(skipped), len(opts.Questions), skipped)
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
		Skipped:        skipped,
	}, nil
}

// Render writes the human-readable report to w. Returns an error on
// an empty Result (called before Run) or if the final write fails —
// the last write is the one most likely to surface a broken-pipe
// state (`gogfy benchmark ... | head -1`).
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
	if len(r.Skipped) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "  Skipped %d question(s) with no matching nodes:\n", len(r.Skipped))
		for _, q := range r.Skipped {
			fmt.Fprintf(w, "    - %s\n", truncate(q, 55))
		}
	}
	_, err := fmt.Fprintln(w)
	return err
}

// estimateTokens uses chars/cpt with a floor of 1 — the floor ensures
// an empty subgraph still consumes "some" tokens so reduction ratios
// never divide by zero (matches upstream's `max(1, len // 4)`).
func estimateTokens(text string, charsPerToken int) int {
	if charsPerToken <= 0 {
		charsPerToken = defaultCharsPerToken
	}
	if n := len(text) / charsPerToken; n > 0 {
		return n
	}
	return 1
}

// queryContext is per-Run-invariant; building once (vs per-question)
// makes question-scaling O(Q+N) instead of O(Q·N).
type queryContext struct {
	nodes         []schema.Node
	adj           map[string][]string
	nodeByID      map[string]schema.Node
	relation      map[edgeKey]string
	lowerLabels   []string // parallel to nodes
	depth         int
	charsPerToken int
}

// edgeKey is a normalized (a ≤ b) struct key for undirected edges.
// Struct chosen over string concatenation to avoid separator-collision
// risk on node IDs that contain arbitrary bytes (file paths, etc.).
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
		// Skip edges with endpoints absent from the node list — they
		// would cause BFS to traverse phantom nodes and emit malformed
		// NODE lines with empty labels. Silently dropping is safer
		// than the alternative because a partial graph.json is a
		// common state during incremental builds.
		if _, ok := nodeByID[e.Source]; !ok {
			continue
		}
		if _, ok := nodeByID[e.Target]; !ok {
			continue
		}
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

// bestMatches returns at most 3 seed node IDs ranked by label-substring
// hits on the question's >2-char terms (cap mirrors upstream). Ties
// break on ID so BFS expansion is deterministic.
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

// roundOne rounds to one decimal place using IEEE 754 banker's
// rounding (ties-to-even at the binary float level). Aligns with
// Python's `round(x, 1)` for the genuine-tie cases — values exactly
// representable as binary float, like 2.25 (→ 2.2 in both, vs 2.3
// under half-up). Strict decimal-aware Python ties (e.g. 0.05) will
// still diverge because that requires a decimal library, not a
// float64 algorithm.
func roundOne(x float64) float64 {
	return math.RoundToEven(x*10) / 10
}

// trimFloat formats a 1-decimal float, dropping ".0" when whole so
// "10.0x" prints as "10x".
func trimFloat(f float64) string {
	return strings.TrimSuffix(fmt.Sprintf("%.1f", f), ".0")
}

func commas(n int) string {
	// Sign-strip via the formatted string instead of `-n` to dodge
	// the math.MinInt64 trap (`-math.MinInt64` overflows back to
	// negative and would infinite-recurse).
	s := fmt.Sprintf("%d", n)
	sign := ""
	if strings.HasPrefix(s, "-") {
		sign = "-"
		s = s[1:]
	}
	if len(s) <= 3 {
		return sign + s
	}
	var out strings.Builder
	out.WriteString(sign)
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

// truncate cuts s to at most n runes. Rune-aware to avoid splitting a
// multi-byte codepoint when custom questions contain unicode.
func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
