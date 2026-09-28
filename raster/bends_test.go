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
// nodes up with their trunk costs two bends elsewhere.
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
	if total > 116 || small > 5 {
		for k, v := range per {
			t.Logf("  %s bends=%d small=%d", k, v[0], v[1])
		}
		t.Errorf("bends %d (baseline 116), small steps %d (baseline 5)", total, small)
	}
}
