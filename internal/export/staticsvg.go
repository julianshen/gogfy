package export

import (
	"bytes"
	"fmt"
	"math"
	"math/rand"
	"sort"

	"github.com/julianshen/gogfy/internal/schema"
)

// StaticSVG renders graph as a self-contained .svg with positions
// computed via a small Fruchterman-Reingold force-directed pass.
// Output is a single document — no JavaScript, no D3 — so it embeds
// in docs / PRs / markdown without runtime cost. Compared to the
// interactive viewer this loses search and click-to-inspect, but the
// trade is "renders in any SVG viewer" vs. "needs a browser".
//
// Determinism: layout uses a seeded RNG keyed on node-id sort order
// so two runs over the same graph produce byte-identical output. CI
// snapshots stay stable.
//
// Performance: layout cost is O(iters · N²). For 10K-node corpora
// the layout becomes the bottleneck; the caller is expected to
// pre-aggregate to community-level (see internal/agg) before
// calling for very large graphs.
func StaticSVG(graph GraphExport, opts StaticSVGOptions) []byte {
	if opts.Width == 0 {
		opts.Width = 1200
	}
	if opts.Height == 0 {
		opts.Height = 1200
	}
	if opts.Iterations == 0 {
		opts.Iterations = 120
	}

	// Sort nodes by ID so layout order is deterministic. The PRNG is
	// seeded with a constant — randomness drives initial placement
	// but the final positions are reproducible across runs.
	nodes := make([]schema.Node, len(graph.Nodes))
	copy(nodes, graph.Nodes)
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })

	idx := make(map[string]int, len(nodes))
	for i, n := range nodes {
		idx[n.ID] = i
	}

	// Initial random positions inside a unit square; the layout
	// projects to viewbox after settling.
	rng := rand.New(rand.NewSource(1))
	xs := make([]float64, len(nodes))
	ys := make([]float64, len(nodes))
	for i := range nodes {
		xs[i] = rng.Float64()
		ys[i] = rng.Float64()
	}

	// Edge list as index pairs so the inner loop avoids string keys.
	pairs := make([]struct{ a, b int }, 0, len(graph.Edges))
	for _, e := range graph.Edges {
		ai, aok := idx[e.Source]
		bi, bok := idx[e.Target]
		if aok && bok && ai != bi {
			pairs = append(pairs, struct{ a, b int }{ai, bi})
		}
	}

	// Fruchterman-Reingold: repulsion between every pair, attraction
	// along each edge, temperature cooling per iteration.
	n := float64(len(nodes))
	if n < 2 {
		return renderSVG(nodes, pairs, xs, ys, opts)
	}
	k := math.Sqrt(1.0 / n) // ideal edge length in unit-square coords
	temp := 0.1
	cool := temp / float64(opts.Iterations+1)
	dx := make([]float64, len(nodes))
	dy := make([]float64, len(nodes))
	for it := 0; it < opts.Iterations; it++ {
		for i := range dx {
			dx[i] = 0
			dy[i] = 0
		}
		// Repulsion (O(N²) — the dominant cost). For each pair the
		// force is k²/d in the direction of i from j.
		for i := 0; i < len(nodes); i++ {
			for j := i + 1; j < len(nodes); j++ {
				ddx := xs[i] - xs[j]
				ddy := ys[i] - ys[j]
				dist := math.Sqrt(ddx*ddx+ddy*ddy) + 1e-9
				force := (k * k) / dist
				ux := ddx / dist
				uy := ddy / dist
				dx[i] += ux * force
				dy[i] += uy * force
				dx[j] -= ux * force
				dy[j] -= uy * force
			}
		}
		// Attraction along edges: d²/k pulls endpoints together.
		for _, p := range pairs {
			ddx := xs[p.a] - xs[p.b]
			ddy := ys[p.a] - ys[p.b]
			dist := math.Sqrt(ddx*ddx+ddy*ddy) + 1e-9
			force := (dist * dist) / k
			ux := ddx / dist
			uy := ddy / dist
			dx[p.a] -= ux * force
			dy[p.a] -= uy * force
			dx[p.b] += ux * force
			dy[p.b] += uy * force
		}
		// Apply displacement clamped to current temperature.
		for i := range nodes {
			d := math.Sqrt(dx[i]*dx[i]+dy[i]*dy[i]) + 1e-9
			scale := math.Min(d, temp) / d
			xs[i] += dx[i] * scale
			ys[i] += dy[i] * scale
			// Hold inside the unit square.
			if xs[i] < 0 {
				xs[i] = 0
			} else if xs[i] > 1 {
				xs[i] = 1
			}
			if ys[i] < 0 {
				ys[i] = 0
			} else if ys[i] > 1 {
				ys[i] = 1
			}
		}
		temp -= cool
	}
	return renderSVG(nodes, pairs, xs, ys, opts)
}

// StaticSVGOptions tunes the renderer. Zero-value means use defaults.
type StaticSVGOptions struct {
	Width, Height int
	// Iterations controls layout quality vs. cost. 120 is a sensible
	// default for sub-2K-node graphs; larger graphs benefit from more.
	Iterations int
	// ShowLabels prints each node's Label next to its dot. Off by
	// default for large graphs — text crowds the picture above a few
	// hundred nodes. Caller decides.
	ShowLabels bool
}

// renderSVG emits the final SVG document. Positions in xs/ys are
// unit-square coordinates; we scale to (margin, width-margin) here.
func renderSVG(nodes []schema.Node, pairs []struct{ a, b int }, xs, ys []float64, opts StaticSVGOptions) []byte {
	const margin = 40.0
	w := float64(opts.Width)
	h := float64(opts.Height)
	scaleX := func(x float64) float64 { return margin + x*(w-2*margin) }
	scaleY := func(y float64) float64 { return margin + y*(h-2*margin) }

	var buf bytes.Buffer
	fmt.Fprintf(&buf, `<?xml version="1.0" encoding="UTF-8"?>`+"\n")
	fmt.Fprintf(&buf, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" width="%d" height="%d">`+"\n", opts.Width, opts.Height, opts.Width, opts.Height)
	fmt.Fprintf(&buf, `<rect width="%d" height="%d" fill="#fafafa"/>`+"\n", opts.Width, opts.Height)

	// Edges first so dots overlay them.
	fmt.Fprintf(&buf, `<g stroke="#bbbbbb" stroke-width="0.7" fill="none">`+"\n")
	for _, p := range pairs {
		fmt.Fprintf(&buf, `<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f"/>`+"\n",
			scaleX(xs[p.a]), scaleY(ys[p.a]), scaleX(xs[p.b]), scaleY(ys[p.b]))
	}
	fmt.Fprintln(&buf, `</g>`)

	// Nodes colored by Community to give the picture some visual
	// structure even without interaction.
	fmt.Fprintf(&buf, `<g>`+"\n")
	for i, n := range nodes {
		color := communityColor(n.Community)
		fmt.Fprintf(&buf, `<circle cx="%.1f" cy="%.1f" r="3" fill="%s" stroke="#333" stroke-width="0.4"/>`+"\n",
			scaleX(xs[i]), scaleY(ys[i]), color)
	}
	fmt.Fprintln(&buf, `</g>`)

	if opts.ShowLabels {
		fmt.Fprintf(&buf, `<g font-family="sans-serif" font-size="9" fill="#111">`+"\n")
		for i, n := range nodes {
			fmt.Fprintf(&buf, `<text x="%.1f" y="%.1f">%s</text>`+"\n",
				scaleX(xs[i])+5, scaleY(ys[i])-3, escapeXML(n.Label))
		}
		fmt.Fprintln(&buf, `</g>`)
	}
	fmt.Fprintln(&buf, `</svg>`)
	return buf.Bytes()
}

// communityColor maps a community ID to a stable color. Hash → HSL
// keeps the palette consistent across runs even for unseen IDs and
// avoids the rainbow-by-index bias where adjacent IDs look related.
func communityColor(community string) string {
	if community == "" {
		return "#888888"
	}
	var h uint32
	for i := 0; i < len(community); i++ {
		h = h*131 + uint32(community[i])
	}
	hue := int(h % 360)
	return fmt.Sprintf("hsl(%d, 60%%, 60%%)", hue)
}

// escapeXML handles the four reserved chars we might encounter in
// node labels. Faster than html.EscapeString because we don't need
// the broader entity table for this usage.
func escapeXML(s string) string {
	if !needsEscape(s) {
		return s
	}
	var b bytes.Buffer
	for _, r := range s {
		switch r {
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '&':
			b.WriteString("&amp;")
		case '"':
			b.WriteString("&quot;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func needsEscape(s string) bool {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '<', '>', '&', '"':
			return true
		}
	}
	return false
}
