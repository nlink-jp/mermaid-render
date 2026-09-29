package raster

import (
	"fmt"
	"image/color"
	"math"
	"sort"
	"strconv"
	"unicode/utf8"

	mr "github.com/nlink-jp/mermaid-render"
)

// A pie chart follows pieRenderer (mermaid 12.0.0): slices in the order
// written, clockwise from twelve o'clock; an item under 1% of the whole
// gets no slice and the drawn slices share the circle; a slice's
// percentage is of the whole total, rounded as toFixed(0) rounds; the
// legend lists every item. What is this engine's: a percentage that does
// not fit inside its slice goes outside the circle, spaced so none overlap
// (mermaid lets them overlap), and the colours.

const (
	pieR        = 6.0  // em, the circle's radius
	pieText     = 0.75 // mermaid's textPosition: where a percentage sits, as a share of the radius
	pieOut      = 0.5  // em, from the circle to an outside label
	pieGap      = 0.2  // em, between stacked outside labels
	pieLegendGo = 1.6  // em, from the pie's right edge to the legend
	pieSwatch   = 0.9  // em, a legend swatch's side
	pieRowGap   = 0.45 // em, between legend rows
)

// piePalette is twelve colours for a white card, assigned in item order and
// repeated (pieRenderer's twelve pie1..pie12 are theme colours).
var piePalette = []color.RGBA{
	{0x4e, 0x79, 0xa7, 0xff}, {0xf2, 0x8e, 0x2b, 0xff}, {0x59, 0xa1, 0x4f, 0xff}, {0xe1, 0x57, 0x59, 0xff},
	{0x76, 0xb7, 0xb2, 0xff}, {0xed, 0xc9, 0x48, 0xff}, {0xb0, 0x7a, 0xa1, 0xff}, {0xff, 0x9d, 0xa7, 0xff},
	{0x9c, 0x75, 0x5f, 0xff}, {0xba, 0xb0, 0xac, 0xff}, {0x86, 0xbc, 0xb6, 0xff}, {0xd4, 0xa6, 0xc8, 0xff},
}

type pieWedge struct {
	item   int     // index in the diagram's slices
	a0, a1 float64 // radians clockwise from twelve o'clock
	pct    string  // "45%"
	box    rect    // the percentage's text box
	inside bool
	leader []pt // from the circle to an outside label
}

type pieRow struct {
	item   int
	swatch rect
	text   string
	box    rect
}

type pieLayout struct {
	W, H   float64
	c      pt
	wedges []pieWedge
	legend []pieRow
}

// pieShares is pieRenderer's arithmetic: which items get a slice, the
// share of the circle each gets, and the percentage it shows.
func pieShares(p *mr.Pie) (kept []int, angle []float64, pct []string) {
	sum := 0.0
	for _, s := range p.Slices {
		sum += s.Value
	}
	keptSum := 0.0
	for i, s := range p.Slices {
		if s.Value/sum*100 >= 1 { // createPieArcs: values under 1% are removed
			kept = append(kept, i)
			keptSum += s.Value
		}
	}
	a := 0.0
	for _, i := range kept {
		angle = append(angle, a)
		a += p.Slices[i].Value / keptSum * 2 * math.Pi
		pct = append(pct, toFixed0(p.Slices[i].Value/sum*100)+"%")
	}
	angle = append(angle, a)
	return kept, angle, pct
}

// toFixed0 is JavaScript's toFixed(0) for a non-negative finite x: the
// nearest integer, a tie going up, decided on x's exact value.
func toFixed0(x float64) string {
	n := math.Floor(x)
	if x-n >= 0.5 {
		n++
	}
	return strconv.FormatFloat(n, 'f', 0, 64)
}

// onCircle is the point at angle a (clockwise from twelve o'clock) and
// radius r around c.
func onCircle(c pt, r, a float64) pt {
	return pt{c.X + r*math.Sin(a), c.Y - r*math.Cos(a)}
}

func layoutPie(p *mr.Pie, measure measurer) (*pieLayout, error) {
	m := func(s string, bold bool) (float64, float64, error) {
		if utf8.RuneCountInString(s) > MaxLabel {
			return 0, 0, &mr.Error{Kind: mr.UnsupportedConstruct, Msg: fmt.Sprintf("a label longer than %d characters", MaxLabel)}
		}
		return measure(s, bold)
	}
	pl := &pieLayout{c: pt{0, 0}}
	kept, angle, pct := pieShares(p)
	var left, right []int // outside labels by side, as indexes into wedges
	for k, i := range kept {
		w := pieWedge{item: i, a0: angle[k], a1: angle[k+1], pct: pct[k]}
		tw, th, err := m(w.pct, false)
		if err != nil {
			return nil, glyphErr(err, p.Slices[i].Line)
		}
		mid := (w.a0 + w.a1) / 2
		at := onCircle(pl.c, pieR*pieText, mid)
		w.box = rect{at.X - tw/2, at.Y - th/2, at.X + tw/2, at.Y + th/2}
		if len(kept) == 1 || inWedge(w.box, pl.c, pieR, w.a0, w.a1) {
			w.inside = true
		} else {
			edge := onCircle(pl.c, pieR+pieOut, mid)
			if math.Sin(mid) >= 0 {
				w.box = rect{edge.X, edge.Y - th/2, edge.X + tw, edge.Y + th/2}
				right = append(right, len(pl.wedges))
			} else {
				w.box = rect{edge.X - tw, edge.Y - th/2, edge.X, edge.Y + th/2}
				left = append(left, len(pl.wedges))
			}
		}
		pl.wedges = append(pl.wedges, w)
	}
	// Outside labels on each side stack top to bottom without touching:
	// each is pushed down below the one above it, then the run is pulled
	// up as a whole if it hangs below its last anchor.
	for _, side := range [][]int{left, right} {
		sort.SliceStable(side, func(a, b int) bool { return pl.wedges[side[a]].box.Y0 < pl.wedges[side[b]].box.Y0 })
		for j := 1; j < len(side); j++ {
			prev, cur := &pl.wedges[side[j-1]], &pl.wedges[side[j]]
			if d := prev.box.Y1 + pieGap - cur.box.Y0; d > 0 {
				cur.box.Y0 += d
				cur.box.Y1 += d
			}
		}
		for _, j := range side {
			w := &pl.wedges[j]
			mid := (w.a0 + w.a1) / 2
			// Near twelve and six o'clock, and once pushed down, a box
			// beside its anchor can reach into the circle: move it out
			// sideways until it keeps pieOut from the circle.
			dir := 1.0
			if math.Sin(mid) < 0 {
				dir = -1
			}
			for rectMeetsCircle(w.box, pl.c, pieR+pieOut) {
				w.box.X0 += dir * 0.1
				w.box.X1 += dir * 0.1
			}
			from := onCircle(pl.c, pieR, mid)
			to := pt{w.box.X0, w.box.Center().Y}
			if math.Sin(mid) < 0 {
				to.X = w.box.X1
			}
			w.leader = []pt{from, to}
		}
	}
	// The legend: right of the circle and of any label outside it.
	x0 := pl.c.X + pieR + pieLegendGo
	for _, w := range pl.wedges {
		if !w.inside {
			x0 = math.Max(x0, w.box.X1+pieLegendGo)
		}
	}
	type row struct {
		text   string
		tw, th float64
		item   int
	}
	var rows []row
	total := 0.0
	for i, s := range p.Slices {
		text := s.Label
		if p.ShowData {
			text = fmt.Sprintf("%s [%s]", s.Label, s.Text)
		}
		tw, th, err := m(text, false)
		if err != nil {
			return nil, glyphErr(err, s.Line)
		}
		rows = append(rows, row{text, tw, th, i})
		total += math.Max(th, pieSwatch) + pieRowGap
	}
	y := pl.c.Y - total/2 + pieRowGap/2
	for _, r := range rows {
		h := math.Max(r.th, pieSwatch)
		cy := y + h/2
		sw := rect{x0, cy - pieSwatch/2, x0 + pieSwatch, cy + pieSwatch/2}
		tx := sw.X1 + 0.4
		pl.legend = append(pl.legend, pieRow{item: r.item, swatch: sw, text: r.text, box: rect{tx, cy - r.th/2, tx + r.tw, cy + r.th/2}})
		y += h + pieRowGap
	}
	// Bounds: shift everything to start at 0.
	minX, minY, maxX, maxY := pl.c.X-pieR, pl.c.Y-pieR, pl.c.X+pieR, pl.c.Y+pieR
	grow := func(r rect) {
		minX, minY, maxX, maxY = math.Min(minX, r.X0), math.Min(minY, r.Y0), math.Max(maxX, r.X1), math.Max(maxY, r.Y1)
	}
	for _, w := range pl.wedges {
		grow(w.box)
	}
	for _, r := range pl.legend {
		grow(r.swatch)
		grow(r.box)
	}
	pl.shift(-minX, -minY)
	pl.W, pl.H = maxX-minX, maxY-minY
	return pl, nil
}

func (pl *pieLayout) shift(dx, dy float64) {
	mv := func(r rect) rect { return rect{r.X0 + dx, r.Y0 + dy, r.X1 + dx, r.Y1 + dy} }
	pl.c = pt{pl.c.X + dx, pl.c.Y + dy}
	for i := range pl.wedges {
		w := &pl.wedges[i]
		w.box = mv(w.box)
		for j := range w.leader {
			w.leader[j] = pt{w.leader[j].X + dx, w.leader[j].Y + dy}
		}
	}
	for i := range pl.legend {
		pl.legend[i].swatch = mv(pl.legend[i].swatch)
		pl.legend[i].box = mv(pl.legend[i].box)
	}
}

// inWedge reports whether r lies inside the circle of radius rad around c
// and between the angles a0 and a1, with a little room from each edge.
func inWedge(r rect, c pt, rad, a0, a1 float64) bool {
	const margin = 0.15
	for _, p := range []pt{{r.X0, r.Y0}, {r.X1, r.Y0}, {r.X1, r.Y1}, {r.X0, r.Y1}} {
		dx, dy := p.X-c.X, p.Y-c.Y
		d := math.Hypot(dx, dy)
		if d > rad-margin || d < margin {
			return false
		}
		a := math.Atan2(dx, -dy) // clockwise from twelve o'clock
		if a < 0 {
			a += 2 * math.Pi
		}
		// The corner keeps margin em from each straight edge: its distance
		// to an edge's ray is d·sin of the angle between them, or d once
		// that angle passes a right angle and the nearest point is the centre.
		edge := func(da float64) float64 {
			if da > math.Pi/2 {
				return d
			}
			return d * math.Sin(da)
		}
		if a < a0 || a > a1 || edge(a-a0) < margin || edge(a1-a) < margin {
			return false
		}
	}
	return true
}

// arcPoints is the outline of a slice: the centre, then the arc.
func arcPoints(c pt, r, a0, a1 float64) []pt {
	pts := []pt{c}
	n := int(math.Ceil((a1-a0)/(math.Pi/90))) + 1
	for i := 0; i <= n; i++ {
		pts = append(pts, onCircle(c, r, a0+(a1-a0)*float64(i)/float64(n)))
	}
	return pts
}

// textOn picks dark or white text for a fill, by its luminance.
func textOn(col color.RGBA) color.RGBA {
	l := 0.2126*float64(col.R) + 0.7152*float64(col.G) + 0.0722*float64(col.B)
	if l > 150 {
		return colText
	}
	return colBG
}

func (c *canvas) drawPie(p *mr.Pie, pl *pieLayout, fn *Font) error {
	colorOf := func(item int) color.RGBA { return piePalette[item%len(piePalette)] }
	for _, w := range pl.wedges {
		if w.a1-w.a0 >= 2*math.Pi-1e-9 {
			c.fill(arcPoints(pl.c, pieR, 0, 2*math.Pi)[1:], colorOf(w.item))
		} else {
			c.fill(arcPoints(pl.c, pieR, w.a0, w.a1), colorOf(w.item))
		}
		c.tracef("slice %d %.4f %.4f", w.item, w.a0, w.a1)
	}
	if len(pl.wedges) > 1 {
		for _, w := range pl.wedges {
			c.segment(pl.c, onCircle(pl.c, pieR, w.a0), lineW*1.5, colBG)
		}
	}
	circle := arcPoints(pl.c, pieR, 0, 2*math.Pi)[1:]
	c.polyline(append(circle, circle[0]), lineW, false, colEdge)
	for _, w := range pl.wedges {
		col := colText
		if w.inside {
			col = textOn(colorOf(w.item))
		} else {
			c.polyline(w.leader, lineW, false, colEdge)
		}
		ctr := w.box.Center()
		if err := fn.drawText(c.img, (ctr.X+c.offX)*c.em, (ctr.Y+c.offY)*c.em, w.pct, false, c.em, col); err != nil {
			return glyphErr(err, p.Slices[w.item].Line)
		}
	}
	for _, r := range pl.legend {
		c.outlineShape([]pt{{r.swatch.X0, r.swatch.Y0}, {r.swatch.X1, r.swatch.Y0}, {r.swatch.X1, r.swatch.Y1}, {r.swatch.X0, r.swatch.Y1}}, colorOf(r.item), colEdge, lineW*0.6, false)
		ctr := r.box.Center()
		if err := fn.drawText(c.img, (ctr.X+c.offX)*c.em, (ctr.Y+c.offY)*c.em, r.text, false, c.em, colText); err != nil {
			return glyphErr(err, p.Slices[r.item].Line)
		}
		c.tracef("legend %d %s", r.item, r.text)
	}
	return nil
}
