package raster

import (
	"fmt"
	"image/color"
	"math"
	"strconv"
	"unicode/utf8"

	mr "github.com/nlink-jp/mermaid-render"
)

// A pie chart follows pieRenderer (mermaid 12.0.0): slices in the order
// written, clockwise from twelve o'clock; an item under 1% of the whole
// gets no slice and the drawn slices share the circle; a slice's
// percentage is of the whole total, rounded as toFixed(0) rounds; the
// legend lists every item. What is this engine's (the operator's decision
// of 2026-09-29): every item's percentage also stands in a column at the
// legend's right — "<1%" for an item with no slice — and a slice carries
// its percentage only when the text fits inside it. mermaid writes every
// percentage on its slice, where thin slices' labels overlap until none
// can be read; leader lines to labels outside the circle, tried first,
// crossed and ran through the circle once slices were many.

const (
	pieR        = 6.0  // em, the circle's radius
	pieText     = 0.75 // mermaid's textPosition: where a percentage sits, as a share of the radius
	pieLegendGo = 1.6  // em, from the circle to the legend
	pieSwatch   = 0.9  // em, a legend swatch's side
	pieRowGap   = 0.45 // em, between legend rows
	piePctGap   = 1.2  // em, from the longest legend label to the percentage column
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
	box    rect    // the percentage's text box, when inside
	inside bool    // the percentage is written on the slice
}

type pieRow struct {
	item   int
	swatch rect
	text   string
	box    rect
	pct    string // "48%", "<1%", or "" when the total is zero
	pctBox rect
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
	sum := pieSum(p)
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

func pieSum(p *mr.Pie) float64 {
	sum := 0.0
	for _, s := range p.Slices {
		sum += s.Value
	}
	return sum
}

// legendPct is an item's percentage in the legend column: the slice's own,
// "<1%" for an item under 1% (it has no slice), nothing when the total is
// zero and no share exists.
func legendPct(p *mr.Pie, i int) string {
	sum := pieSum(p)
	if !(sum > 0) {
		return ""
	}
	x := p.Slices[i].Value / sum * 100
	if x >= 1 {
		return toFixed0(x) + "%"
	}
	return "<1%"
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
	for k, i := range kept {
		w := pieWedge{item: i, a0: angle[k], a1: angle[k+1], pct: pct[k]}
		tw, th, err := m(w.pct, false)
		if err != nil {
			return nil, glyphErr(err, p.Slices[i].Line)
		}
		at := onCircle(pl.c, pieR*pieText, (w.a0+w.a1)/2)
		if len(kept) == 1 {
			at = pl.c // a full circle: its percentage at the centre
		}
		w.box = rect{at.X - tw/2, at.Y - th/2, at.X + tw/2, at.Y + th/2}
		w.inside = len(kept) == 1 || inWedge(w.box, pl.c, pieR, w.a0, w.a1)
		pl.wedges = append(pl.wedges, w)
	}
	// The legend, right of the circle: swatch, label, and the percentage
	// column right-aligned beyond the longest label.
	x0 := pl.c.X + pieR + pieLegendGo
	type row struct {
		text, pct      string
		tw, th, pw, ph float64
	}
	rows := make([]row, len(p.Slices))
	total, textMax, pctMax := 0.0, 0.0, 0.0
	for i, s := range p.Slices {
		text := s.Label
		if p.ShowData {
			text = fmt.Sprintf("%s [%s]", s.Label, s.Text)
		}
		tw, th, err := m(text, false)
		if err != nil {
			return nil, glyphErr(err, s.Line)
		}
		r := row{text: text, pct: legendPct(p, i), tw: tw, th: th}
		if r.pct != "" {
			if r.pw, r.ph, err = m(r.pct, false); err != nil {
				return nil, glyphErr(err, s.Line)
			}
		}
		rows[i] = r
		textMax, pctMax = math.Max(textMax, tw), math.Max(pctMax, r.pw)
		total += math.Max(math.Max(th, r.ph), pieSwatch) + pieRowGap
	}
	tx := x0 + pieSwatch + 0.4
	pctRight := tx + textMax + piePctGap + pctMax
	y := pl.c.Y - total/2 + pieRowGap/2
	for i, r := range rows {
		h := math.Max(math.Max(r.th, r.ph), pieSwatch)
		cy := y + h/2
		row := pieRow{item: i, text: r.text, pct: r.pct,
			swatch: rect{x0, cy - pieSwatch/2, x0 + pieSwatch, cy + pieSwatch/2},
			box:    rect{tx, cy - r.th/2, tx + r.tw, cy + r.th/2}}
		if r.pct != "" {
			row.pctBox = rect{pctRight - r.pw, cy - r.ph/2, pctRight, cy + r.ph/2}
		}
		pl.legend = append(pl.legend, row)
		y += h + pieRowGap
	}
	// Bounds: shift everything to start at 0.
	minX, minY, maxX, maxY := pl.c.X-pieR, pl.c.Y-pieR, pl.c.X+pieR, pl.c.Y+pieR
	grow := func(r rect) {
		minX, minY, maxX, maxY = math.Min(minX, r.X0), math.Min(minY, r.Y0), math.Max(maxX, r.X1), math.Max(maxY, r.Y1)
	}
	for _, r := range pl.legend {
		grow(r.swatch)
		grow(r.box)
		if r.pct != "" {
			grow(r.pctBox)
		}
	}
	pl.shift(-minX, -minY)
	pl.W, pl.H = maxX-minX, maxY-minY
	return pl, nil
}

func (pl *pieLayout) shift(dx, dy float64) {
	mv := func(r rect) rect { return rect{r.X0 + dx, r.Y0 + dy, r.X1 + dx, r.Y1 + dy} }
	pl.c = pt{pl.c.X + dx, pl.c.Y + dy}
	for i := range pl.wedges {
		pl.wedges[i].box = mv(pl.wedges[i].box)
	}
	for i := range pl.legend {
		r := &pl.legend[i]
		r.swatch, r.box, r.pctBox = mv(r.swatch), mv(r.box), mv(r.pctBox)
	}
}

// inWedge reports whether r lies inside the circle of radius rad around c
// and between the angles a0 and a1, with a little room from each edge. It
// tests the corners, which is enough for a wedge up to half the circle; a
// wider one's notch could fall between corners, and a box there is not
// asked about: a percentage sits at 0.75 R on its wedge's middle, opposite
// the notch.
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
	text := func(r rect, s string, col color.RGBA, line int) error {
		ctr := r.Center()
		if err := fn.drawText(c.img, (ctr.X+c.offX)*c.em, (ctr.Y+c.offY)*c.em, s, false, c.em, col); err != nil {
			return glyphErr(err, line)
		}
		return nil
	}
	for _, w := range pl.wedges {
		if !w.inside {
			continue
		}
		if err := text(w.box, w.pct, textOn(colorOf(w.item)), p.Slices[w.item].Line); err != nil {
			return err
		}
		c.tracef("pct %d %s", w.item, w.pct)
	}
	for _, r := range pl.legend {
		c.outlineShape([]pt{{r.swatch.X0, r.swatch.Y0}, {r.swatch.X1, r.swatch.Y0}, {r.swatch.X1, r.swatch.Y1}, {r.swatch.X0, r.swatch.Y1}}, colorOf(r.item), colEdge, lineW*0.6, false)
		if err := text(r.box, r.text, colText, p.Slices[r.item].Line); err != nil {
			return err
		}
		drawn := ""
		if r.pct != "" {
			if err := text(r.pctBox, r.pct, colText, p.Slices[r.item].Line); err != nil {
				return err
			}
			drawn = r.pct
		}
		c.tracef("legend %d %s | %s", r.item, r.text, drawn)
	}
	return nil
}
