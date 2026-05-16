package obsidian

import (
	"encoding/json"
	"math"
	"sort"
	"strconv"

	"github.com/julianshen/gogfy/internal/schema"
)

// Canvas emits an Obsidian .canvas JSON file: communities laid out as
// colored groups in a √N×√N grid, member nodes as file-card cards
// inside each group, and up to maxCanvasEdges weighted edges between
// them. Opens in Obsidian as an infinite canvas — sister artifact to
// the .md vault written by Generate.
func Canvas(nodes []schema.Node, edges []schema.Edge, opts Options) ([]byte, error) {
	filenames := buildFilenames(nodes)
	communityMembers := groupByCommunity(nodes)

	cids := make([]string, 0, len(communityMembers))
	for cid := range communityMembers {
		cids = append(cids, cid)
	}
	sort.Strings(cids)

	// Square-ish grid of communities. cols=ceil(√n), rows derived.
	num := len(cids)
	cols := 1
	if num > 0 {
		cols = int(math.Ceil(math.Sqrt(float64(num))))
	}
	rows := 1
	if num > 0 {
		rows = int(math.Ceil(float64(num) / float64(cols)))
	}

	// Pre-size each group based on member count: ~3 cards per row.
	groupW := make([]int, num)
	groupH := make([]int, num)
	for i, cid := range cids {
		n := len(communityMembers[cid])
		w := 600
		if n > 0 {
			if cand := 220 * int(math.Ceil(math.Sqrt(float64(n)))); cand > w {
				w = cand
			}
		}
		h := 400
		if n > 0 {
			if cand := 100*int(math.Ceil(float64(n)/3)) + 120; cand > h {
				h = cand
			}
		}
		groupW[i] = w
		groupH[i] = h
	}

	// Column widths / row heights are the max in each grid line so
	// neighboring groups don't overlap when sizes vary.
	colWidths := make([]int, cols)
	rowHeights := make([]int, rows)
	for c := 0; c < cols; c++ {
		max := 0
		for r := 0; r < rows; r++ {
			i := r*cols + c
			if i < num && groupW[i] > max {
				max = groupW[i]
			}
		}
		colWidths[c] = max
	}
	for r := 0; r < rows; r++ {
		max := 0
		for c := 0; c < cols; c++ {
			i := r*cols + c
			if i < num && groupH[i] > max {
				max = groupH[i]
			}
		}
		rowHeights[r] = max
	}

	const gap = 80
	type rect struct{ x, y, w, h int }
	groupRect := make(map[string]rect, num)
	for i, cid := range cids {
		col := i % cols
		row := i / cols
		gx, gy := col*gap, row*gap
		for k := 0; k < col; k++ {
			gx += colWidths[k]
		}
		for k := 0; k < row; k++ {
			gy += rowHeights[k]
		}
		groupRect[cid] = rect{gx, gy, groupW[i], groupH[i]}
	}

	// Cycle through Obsidian's six built-in canvas colors.
	canvasColors := []string{"1", "2", "3", "4", "5", "6"}

	var cnvNodes []canvasNode
	var cnvEdges []canvasEdge
	inCanvas := make(map[string]struct{}, len(nodes))

	for i, cid := range cids {
		gr := groupRect[cid]
		cnvNodes = append(cnvNodes, canvasNode{
			ID:     "g" + cid,
			Type:   "group",
			Label:  communityName(cid, opts.CommunityLabels),
			X:      gr.x,
			Y:      gr.y,
			Width:  gr.w,
			Height: gr.h,
			Color:  canvasColors[i%len(canvasColors)],
		})
		// Members sorted by filename for stable card placement.
		members := append([]schema.Node(nil), communityMembers[cid]...)
		sort.Slice(members, func(i, j int) bool {
			return filenames[members[i].ID] < filenames[members[j].ID]
		})
		for mi, m := range members {
			col := mi % 3
			row := mi / 3
			cnvNodes = append(cnvNodes, canvasNode{
				ID:     "n_" + m.ID,
				Type:   "file",
				File:   filenames[m.ID] + ".md",
				X:      gr.x + 20 + col*200, // 180 card + 20 gap
				Y:      gr.y + 80 + row*80,  // 60 card + 20 gap
				Width:  180,
				Height: 60,
			})
			inCanvas[m.ID] = struct{}{}
		}
	}

	// Edges: only between cards both on the canvas (skip dangling
	// targets, e.g. unassigned-community nodes that didn't make it
	// into a group). Cap at maxCanvasEdges so a dense graph doesn't
	// turn the canvas into a wireball.
	const maxCanvasEdges = 200
	type weighted struct {
		from, to, label string
	}
	cands := make([]weighted, 0, len(edges))
	for _, e := range edges {
		if _, fok := inCanvas[e.Source]; !fok {
			continue
		}
		if _, tok := inCanvas[e.Target]; !tok {
			continue
		}
		lbl := "[" + e.Confidence.String() + "]"
		if e.Relation != "" {
			lbl = e.Relation + " " + lbl
		}
		cands = append(cands, weighted{from: e.Source, to: e.Target, label: lbl})
	}
	// Deterministic order: lexicographic (from, to, label).
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].from != cands[j].from {
			return cands[i].from < cands[j].from
		}
		if cands[i].to != cands[j].to {
			return cands[i].to < cands[j].to
		}
		return cands[i].label < cands[j].label
	})
	if len(cands) > maxCanvasEdges {
		cands = cands[:maxCanvasEdges]
	}
	// Index suffix disambiguates multi-edges between the same pair
	// (different relations) so Obsidian doesn't silently drop edges
	// that share an id.
	seenEdgeID := map[string]int{}
	for _, c := range cands {
		base := "e_" + c.from + "_" + c.to
		id := base
		if n := seenEdgeID[base]; n > 0 {
			id = base + "_" + strconv.Itoa(n)
		}
		seenEdgeID[base]++
		cnvEdges = append(cnvEdges, canvasEdge{
			ID:       id,
			FromNode: "n_" + c.from,
			ToNode:   "n_" + c.to,
			Label:    c.label,
		})
	}

	// Obsidian rejects empty arrays as undefined; emit them explicitly
	// to avoid a "Cannot read properties of undefined" on load.
	if cnvNodes == nil {
		cnvNodes = []canvasNode{}
	}
	if cnvEdges == nil {
		cnvEdges = []canvasEdge{}
	}
	return json.MarshalIndent(canvasFile{Nodes: cnvNodes, Edges: cnvEdges}, "", "  ")
}

func groupByCommunity(nodes []schema.Node) map[string][]schema.Node {
	out := map[string][]schema.Node{}
	for _, n := range nodes {
		if n.Community == "" {
			continue
		}
		out[n.Community] = append(out[n.Community], n)
	}
	return out
}

// Canvas file schema (Obsidian's JSONCanvas spec — keys are documented
// at https://jsoncanvas.org/spec/1.0/ and lowercase-camelCase).
type canvasFile struct {
	Nodes []canvasNode `json:"nodes"`
	Edges []canvasEdge `json:"edges"`
}

type canvasNode struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	Label  string `json:"label,omitempty"`
	File   string `json:"file,omitempty"`
	X      int    `json:"x"`
	Y      int    `json:"y"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Color  string `json:"color,omitempty"`
}

type canvasEdge struct {
	ID       string `json:"id"`
	FromNode string `json:"fromNode"`
	ToNode   string `json:"toNode"`
	Label    string `json:"label,omitempty"`
}
