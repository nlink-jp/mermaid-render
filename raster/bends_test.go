package raster

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	mr "github.com/nlink-jp/mermaid-render"
)

// TestRealBends counts direction changes along each link, and the across
// runs shorter than 1 em (small steps), over the 22 real flowcharts. The
// operator's check of the step-2 drawings (2026-09-28) marked 16 of 22 for
// "needless bends": 254 bends and 61 small steps then. This is a baseline
// against regressions, not a gate on output: lower the numbers when a
// change improves them, and never raise them without saying why.
//
// Raised once, 114/4 -> 116/5, on purpose: the second check asked for boxes
// centred on their links and links meeting a rhombus at its vertex; lining
// nodes up with their trunk costs two bends elsewhere. Then 110/2: members
// carry their frame's edges, and a tie between one link in and one out
// takes the one in (no staircases). Then 106/2: runs of one-to-one links
// move onto one column together where every item of the run reaches it.
func TestRealBends(t *testing.T) {
	fn, err := DefaultFont()
	if err != nil {
		t.Skipf("no system font: %v", err)
	}
	files, _ := filepath.Glob("../testdata/real/*/*.mmd")
	total, small := 0, 0
	per := map[string][2]int{}
	for _, file := range files {
		b, _ := os.ReadFile(file)
		d, err := mr.Parse(string(b))
		if err != nil {
			continue
		}
		f := d.(*mr.Flowchart)
		lay, err := layoutFlowchart(f, fn.measureEm)
		if err != nil {
			continue
		}
		horiz := f.Direction == mr.LR || f.Direction == mr.RL
		bt, sm := 0, 0
		for _, e := range lay.Edges {
			if e.Link.From == e.Link.To {
				continue
			}
			p := e.Points
			for i := 1; i+1 < len(p); i++ {
				ax, ay := p[i].X-p[i-1].X, p[i].Y-p[i-1].Y
				bx, by := p[i+1].X-p[i].X, p[i+1].Y-p[i].Y
				if math.Abs(ax*by-ay*bx) > 1e-6 {
					bt++
				}
			}
			for i := 0; i+1 < len(p); i++ {
				across := math.Abs(p[i+1].X - p[i].X)
				along := math.Abs(p[i+1].Y - p[i].Y)
				if horiz {
					across, along = along, across
				}
				if along < 1e-6 && across > 1e-6 && across < 1 {
					sm++
				}
			}
		}
		total += bt
		small += sm
		per[filepath.Base(file)] = [2]int{bt, sm}
	}
	t.Logf("bends %d, small steps %d", total, small)
	if total > 106 || small > 2 {
		for k, v := range per {
			t.Logf("  %s bends=%d small=%d", k, v[0], v[1])
		}
		t.Errorf("bends %d (baseline 106), small steps %d (baseline 2)", total, small)
	}
}

// TestRealSiblingCrossings counts crossings between links that share an
// end node (a fan leaving one node, or links converging on one), over the 22
// real flowcharts. The second check marked fans whose lines crossed each
// other's verticals for want of a track order: 8 such crossings then (6 in
// 266825ab94, 2 in f4214ee0f3). Like TestRealBends, a baseline.
func TestRealSiblingCrossings(t *testing.T) {
	fn, err := DefaultFont()
	if err != nil {
		t.Skipf("no system font: %v", err)
	}
	files, _ := filepath.Glob("../testdata/real/*/*.mmd")
	total := 0
	per := map[string]int{}
	for _, file := range files {
		b, _ := os.ReadFile(file)
		d, err := mr.Parse(string(b))
		if err != nil {
			continue
		}
		f := d.(*mr.Flowchart)
		lay, err := layoutFlowchart(f, fn.measureEm)
		if err != nil {
			continue
		}
		n := 0
		for i, a := range lay.Edges {
			for _, e := range lay.Edges[i+1:] {
				if a.Link.From.ID != e.Link.From.ID && a.Link.To.ID != e.Link.To.ID {
					continue
				}
				n += properCrossings(a.Points, e.Points)
			}
		}
		total += n
		per[filepath.Base(file)] = n
	}
	t.Logf("sibling crossings %d", total)
	if total > 0 {
		for k, v := range per {
			if v > 0 {
				t.Logf("  %s %d", k, v)
			}
		}
		t.Errorf("sibling crossings %d (baseline 0)", total)
	}
}

// properCrossings counts where a horizontal segment of one path crosses a
// vertical segment of the other strictly inside both.
func properCrossings(p, q []Pt) int {
	n := 0
	count := func(p, q []Pt) {
		for i := 0; i+1 < len(p); i++ {
			if math.Abs(p[i].Y-p[i+1].Y) > 1e-6 {
				continue
			}
			y, x0, x1 := p[i].Y, math.Min(p[i].X, p[i+1].X), math.Max(p[i].X, p[i+1].X)
			for j := 0; j+1 < len(q); j++ {
				if math.Abs(q[j].X-q[j+1].X) > 1e-6 {
					continue
				}
				x, y0, y1 := q[j].X, math.Min(q[j].Y, q[j+1].Y), math.Max(q[j].Y, q[j+1].Y)
				if x > x0+1e-6 && x < x1-1e-6 && y > y0+1e-6 && y < y1-1e-6 {
					n++
				}
			}
		}
	}
	count(p, q)
	count(q, p)
	return n
}
