package raster

import (
	"fmt"
	"image/color"
	"math"
	"sort"
	"strings"

	mr "github.com/nlink-jp/mermaid-render"
)

// A gantt chart is laid out as ganttRenderer lays it out, except where the
// RFP decides otherwise: section titles in a column on the left, a row per
// task on its section's stripe, bars on the time scale d3 builds, the
// axis's ticks and labels as d3 gives them, a task's text inside its bar or
// to its right. The time scale's width is chosen so the axis labels stand
// apart (mermaid fills its container).

const (
	gtRow      = 1.9   // em, a row's pitch
	gtBar      = 1.3   // em, a bar's height
	gtTextPad  = 0.4   // em, text to a bar's edge
	gtSecPad   = 0.6   // em, around a section title
	gtMinScale = 36.0  // em, the time scale's least width
	gtMaxScale = 120.0 // em, past which axis labels are thinned
	gtTickGap  = 0.8   // em, between two axis labels
	gtAxisGap  = 0.35  // em, rows to the axis labels
	gtLaneGap  = 0.3   // em, between vert label lanes
	gtMaxTicks = 10000
	// gtTickLimit: an estimate of 10,000 can make 10,001 ticks (both ends),
	// and months are estimated at 30.4 days; twice the estimate is a
	// resource bound, not a rule mermaid has.
	gtTickLimit = 2 * gtMaxTicks
	// gtMaxWeekStep bounds a week tick interval's step: d3 filters weeks by
	// their count from the epoch, walking a week at a time.
	gtMaxWeekStep = 10000
)

var (
	gtStripes = []color.RGBA{
		{0xe7, 0xec, 0xfb, 0xff}, {0xff, 0xff, 0xff, 0xff}, {0xfb, 0xf4, 0xd9, 0xff}, {0xff, 0xff, 0xff, 0xff},
	}
	gtExcluded = color.RGBA{0xe6, 0xe6, 0xe6, 0xff}
	gtGrid     = color.RGBA{0xdd, 0xe1, 0xe8, 0xff}
	gtVertCol  = color.RGBA{0xc0, 0x39, 0x2b, 0xff}
)

type gtBarBox struct {
	task      *mr.GanttTask
	box       rect // the bar, or the milestone's diamond bounds
	text      rect
	inside    bool
	fill, rim color.RGBA
}

type gtSection struct {
	lines []string
	run   rect // the run of rows it titles
	text  rect
}

type gtTick struct {
	t     float64 // its time
	x     float64
	label string
	box   rect
}

type gtVert struct {
	y0, y1 float64 // its line, across the rows
	task   *mr.GanttTask
	x      float64
	label  rect
}

type ganttLayout struct {
	W, H       float64
	x0, T      float64 // the time scale: [x0, x0+T]
	lo, hi     float64 // its domain, ms
	rowsTop    float64
	rowsBottom float64
	stripes    []rect
	stripeCol  []int
	bars       []gtBarBox
	sections   []gtSection
	excluded   []rect
	ticks      []gtTick
	verts      []gtVert
}

func (gl *ganttLayout) x(t float64) float64 {
	if gl.hi == gl.lo {
		return gl.x0 + gl.T/2 // d3: a degenerate domain maps to the middle
	}
	return gl.x0 + (t-gl.lo)/(gl.hi-gl.lo)*gl.T
}

func gtColors(t *mr.GanttTask) (fill, rim color.RGBA) {
	task := color.RGBA{0x8a, 0x90, 0xdd, 0xff}
	border := color.RGBA{0x53, 0x4f, 0xbc, 0xff}
	red := color.RGBA{0xe0, 0x1b, 0x1b, 0xff}
	switch {
	case t.Active && t.Crit:
		return color.RGBA{0xff, 0x88, 0x88, 0xff}, red
	case t.Active:
		return color.RGBA{0xbf, 0xc7, 0xff, 0xff}, border
	case t.Done && t.Crit:
		return color.RGBA{0xd3, 0xd3, 0xd3, 0xff}, red
	case t.Done:
		return color.RGBA{0xd3, 0xd3, 0xd3, 0xff}, color.RGBA{0x80, 0x80, 0x80, 0xff}
	case t.Crit:
		return color.RGBA{0xff, 0x88, 0x88, 0xff}, red
	}
	return task, border
}

// gtTickInterval is the tickInterval as a d3 interval, or nil for the
// default ticks (none given, or more than 10,000 estimated).
func gtTickInterval(g *mr.Gantt, lo, hi float64) (*d3Interval, error) {
	if g.TickEvery == 0 {
		return nil, nil
	}
	unitMs := map[string]float64{"millisecond": 1, "second": durSecond, "minute": durMinute, "hour": durHour,
		"day": durDay, "week": durWeek, "month": 2628000000}[g.TickUnit]
	if est := math.Ceil((hi - lo) / (unitMs * float64(g.TickEvery))); est > gtMaxTicks {
		return nil, nil
	}
	var iv *d3Interval
	switch g.TickUnit {
	case "millisecond":
		// d3's millisecond.every floors to multiples, not a filter.
		return d3MillisecondEvery(float64(g.TickEvery)), nil
	case "second":
		iv = d3Second
	case "minute":
		iv = d3Minute
	case "hour":
		iv = d3Hour
	case "day":
		iv = d3Day
	case "week":
		if g.TickEvery > gtMaxWeekStep {
			return nil, &mr.Error{Kind: mr.UnsupportedConstruct, Msg: fmt.Sprintf("a tick interval of more than %d weeks", gtMaxWeekStep)}
		}
		days := map[string]int{"sunday": 0, "monday": 1, "tuesday": 2, "wednesday": 3, "thursday": 4, "friday": 5, "saturday": 6}
		iv = weekdayInterval(days[g.Weekday])
	case "month":
		iv = d3Month
	}
	return iv.every(float64(g.TickEvery)), nil
}

func layoutGantt(g *mr.Gantt, m measurer) (*ganttLayout, error) {
	gl := &ganttLayout{}
	var rows []*mr.GanttTask
	first := true
	for _, t := range g.Tasks {
		if first || float64(t.Start) < gl.lo {
			gl.lo = float64(t.Start)
		}
		if first || float64(t.End) > gl.hi {
			gl.hi = float64(t.End)
		}
		first = false
		if !t.Vert {
			rows = append(rows, t)
		}
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Row < rows[j].Row })
	// Sections: each run of rows of one section, titled; the stripe colour
	// by the section's order of first appearance.
	secIndex := map[string]int{}
	for _, t := range rows {
		if _, ok := secIndex[t.Section]; !ok {
			secIndex[t.Section] = len(secIndex)
		}
	}
	type run struct {
		name       string
		start, end int
		lines      []string
		w, h       float64
	}
	var runs []run
	for i, t := range rows {
		if len(runs) == 0 || runs[len(runs)-1].name != t.Section {
			runs = append(runs, run{name: t.Section, start: i})
		}
		runs[len(runs)-1].end = i + 1
	}
	colW := 0.0
	for k := range runs {
		r := &runs[k]
		title := rows[r.start].SectionTitle
		if title == "" {
			continue
		}
		r.lines = strings.Split(title, "\n")
		w, h, err := m(title, false)
		if err != nil {
			return nil, glyphErr(err, rows[r.start].Line)
		}
		r.w, r.h = w, h
		colW = math.Max(colW, w+2*gtSecPad)
	}
	// Half a milestone's diamond past the column, so one at the earliest
	// time stays clear of the section titles.
	gl.x0 = math.Max(colW, gtSecPad) + gtBar/2
	// Rows: a run grows its rows when its title is taller than they are.
	y := 0.0
	gl.rowsTop = y
	rowTop := make([]float64, len(rows))
	pitch := make([]float64, len(rows))
	for _, r := range runs {
		n := float64(r.end - r.start)
		p := gtRow
		if need := r.h + 2*gtSecPad; n*p < need {
			p = need / n
		}
		runTop := y
		for i := r.start; i < r.end; i++ {
			rowTop[i], pitch[i] = y, p
			gl.stripes = append(gl.stripes, rect{0, y, 0, y + p}) // widths set below
			gl.stripeCol = append(gl.stripeCol, secIndex[rows[i].Section]%len(gtStripes))
			y += p
		}
		if len(r.lines) > 0 {
			cy := (runTop + y) / 2
			gl.sections = append(gl.sections, gtSection{lines: r.lines,
				run: rect{0, runTop, gl.x0, y},
				// Centred across the column, as every title is (the operator's
				// check of 2026-09-29).
				text: rect{colW/2 - r.w/2, cy - r.h/2, colW/2 + r.w/2, cy + r.h/2}})
		}
	}
	gl.rowsBottom = y
	// Ticks and their labels decide the scale's width.
	iv, err := gtTickInterval(g, gl.lo, gl.hi)
	if err != nil {
		return nil, err
	}
	var times []float64
	if gl.hi != gl.lo || iv != nil {
		times = d3Ticks(gl.lo, gl.hi, iv, gtTickLimit)
	}
	if len(times) > gtTickLimit {
		return nil, &mr.Error{Kind: mr.UnsupportedConstruct, Msg: fmt.Sprintf("more than %d axis ticks", gtTickLimit)}
	}
	type lab struct {
		text string
		w, h float64
	}
	labs := make([]lab, len(times))
	for i, t := range times {
		s := d3Format(g.AxisFormat, t)
		if err := long(s, 0); err != nil {
			return nil, err
		}
		w, h, err := m(s, false)
		if err != nil {
			return nil, glyphErr(err, 0)
		}
		labs[i] = lab{s, w, h}
	}
	// The scale is as wide as every k-th label needs, the smallest k whose
	// width is at most gtMaxScale (k = 1 unless the ticks are dense).
	need := func(k int) float64 {
		t := gtMinScale
		for i := k; i < len(times); i += k {
			if frac := (times[i] - times[i-k]) / (gl.hi - gl.lo); frac > 0 {
				t = math.Max(t, ((labs[i].w+labs[i-k].w)/2+gtTickGap)/frac)
			}
		}
		return t
	}
	every := 1
	for gl.T = need(1); gl.T > gtMaxScale && every < len(times); {
		every++
		gl.T = need(every)
	}
	// Bars and their texts.
	right := gl.x0 + gl.T
	for i, t := range rows {
		fill, rim := gtColors(t)
		b := gtBarBox{task: t, fill: fill, rim: rim}
		cy := rowTop[i] + pitch[i]/2
		x0, x1 := gl.x(float64(t.Start)), gl.x(float64(t.Bar))
		if t.Milestone {
			c := x0 + (gl.x(float64(t.End))-x0)/2
			b.box = rect{c - gtBar/2, cy - gtBar/2, c + gtBar/2, cy + gtBar/2}
		} else {
			b.box = rect{x0, cy - gtBar/2, math.Max(x1, x0), cy + gtBar/2}
		}
		if err := long(t.Text, t.Line); err != nil {
			return nil, err
		}
		tw, th, err := m(t.Text, false)
		if err != nil {
			return nil, glyphErr(err, t.Line)
		}
		if !t.Milestone && tw+2*gtTextPad <= b.box.W() {
			b.inside = true
			c := b.box.Center()
			b.text = rect{c.X - tw/2, cy - th/2, c.X + tw/2, cy + th/2}
		} else if tw > 0 {
			b.text = rect{b.box.X1 + gtTextPad, cy - th/2, b.box.X1 + gtTextPad + tw, cy + th/2}
			right = math.Max(right, b.text.X1)
		}
		gl.bars = append(gl.bars, b)
	}
	for k := range gl.stripes {
		gl.stripes[k].X1 = right
	}
	for k := 0; k+1 < len(g.Excluded); k += 2 {
		a := math.Max(gl.x(float64(g.Excluded[k])), gl.x0)
		b := math.Min(gl.x(float64(g.Excluded[k+1])), gl.x0+gl.T)
		if b > a {
			gl.excluded = append(gl.excluded, rect{a, gl.rowsTop, b, gl.rowsBottom})
		}
	}
	// The axis below the rows.
	axisY := gl.rowsBottom + gtAxisGap
	bottom := axisY
	for i, t := range times {
		x := gl.x(t)
		if i%every != 0 {
			gl.ticks = append(gl.ticks, gtTick{t: t, x: x}) // a grid line without a label
			continue
		}
		l := labs[i]
		box := rect{x - l.w/2, axisY, x + l.w/2, axisY + l.h}
		gl.ticks = append(gl.ticks, gtTick{t: t, x: x, label: l.text, box: box})
		bottom = math.Max(bottom, box.Y1)
	}
	// vert markers: a line across the rows, the text below the axis in
	// lanes so none overlaps another.
	var lanes [][]rect
	top := bottom + gtAxisGap
	for _, t := range g.Tasks {
		if !t.Vert {
			continue
		}
		if err := long(t.Text, t.Line); err != nil {
			return nil, err
		}
		tw, th, err := m(t.Text, false)
		if err != nil {
			return nil, glyphErr(err, t.Line)
		}
		x := gl.x(float64(t.Start))
		lane := 0
		for ; lane < len(lanes); lane++ {
			free := true
			for _, o := range lanes[lane] {
				if x-tw/2 < o.X1+gtTickGap && o.X0-gtTickGap < x+tw/2 {
					free = false
					break
				}
			}
			if free {
				break
			}
		}
		if lane == len(lanes) {
			lanes = append(lanes, nil)
		}
		ly := top + float64(lane)*(th+gtLaneGap)
		box := rect{x - tw/2, ly, x + tw/2, ly + th}
		lanes[lane] = append(lanes[lane], box)
		gl.verts = append(gl.verts, gtVert{task: t, x: x, y0: gl.rowsTop, y1: gl.rowsBottom, label: box})
		bottom = math.Max(bottom, box.Y1)
	}
	// Bounds: shift so that everything starts at 0.
	minX, maxX := 0.0, right
	for _, b := range gl.bars {
		// A milestone's diamond reaches half a bar past its time.
		minX, maxX = math.Min(minX, b.box.X0), math.Max(maxX, b.box.X1)
	}
	for k := range gl.stripes {
		gl.stripes[k].X1 = maxX
	}
	for _, tk := range gl.ticks {
		if tk.label != "" {
			minX, maxX = math.Min(minX, tk.box.X0), math.Max(maxX, tk.box.X1)
		}
	}
	for _, v := range gl.verts {
		minX, maxX = math.Min(minX, v.label.X0), math.Max(maxX, v.label.X1)
	}
	gl.shift(-minX)
	gl.W, gl.H = maxX-minX, bottom
	return gl, nil
}

func (gl *ganttLayout) shift(dx float64) {
	if dx == 0 {
		return
	}
	mv := func(r rect) rect { return rect{r.X0 + dx, r.Y0, r.X1 + dx, r.Y1} }
	gl.x0 += dx
	for i := range gl.stripes {
		gl.stripes[i] = mv(gl.stripes[i])
	}
	for i := range gl.bars {
		gl.bars[i].box, gl.bars[i].text = mv(gl.bars[i].box), mv(gl.bars[i].text)
	}
	for i := range gl.sections {
		gl.sections[i].run, gl.sections[i].text = mv(gl.sections[i].run), mv(gl.sections[i].text)
	}
	for i := range gl.excluded {
		gl.excluded[i] = mv(gl.excluded[i])
	}
	for i := range gl.ticks {
		gl.ticks[i].x += dx
		gl.ticks[i].box = mv(gl.ticks[i].box)
	}
	for i := range gl.verts {
		gl.verts[i].x += dx
		gl.verts[i].label = mv(gl.verts[i].label)
	}
}

func (c *canvas) drawGantt(gl *ganttLayout, fn *Font) error {
	em := c.em
	text := func(r rect, s string, col color.Color, line int) error {
		ctr := r.Center()
		if err := fn.drawText(c.img, (ctr.X+c.offX)*em, (ctr.Y+c.offY)*em, s, false, em, col); err != nil {
			return glyphErr(err, line)
		}
		return nil
	}
	box := func(r rect) []pt { return []pt{{r.X0, r.Y0}, {r.X1, r.Y0}, {r.X1, r.Y1}, {r.X0, r.Y1}} }
	for i, s := range gl.stripes {
		c.fill(box(s), gtStripes[gl.stripeCol[i]])
	}
	for _, e := range gl.excluded {
		c.fill(box(e), gtExcluded)
	}
	for _, tk := range gl.ticks {
		c.segment(pt{tk.x, gl.rowsTop}, pt{tk.x, gl.rowsBottom + gtAxisGap/2}, lineW*0.6, gtGrid)
	}
	c.segment(pt{gl.x0, gl.rowsBottom}, pt{gl.x0 + gl.T, gl.rowsBottom}, lineW*0.8, colStroke)
	for _, b := range gl.bars {
		if b.task.Milestone {
			ctr := b.box.Center()
			h := b.box.W() / 2
			c.outlineShape([]pt{{ctr.X, ctr.Y - h}, {ctr.X + h, ctr.Y}, {ctr.X, ctr.Y + h}, {ctr.X - h, ctr.Y}}, b.fill, b.rim, lineW, false)
		} else {
			c.outlineShape(roundRect(b.box, math.Min(0.2, b.box.W()/2)), b.fill, b.rim, lineW, false)
		}
		col := colText
		if b.inside {
			col = textOn(b.fill)
		}
		if b.task.Text != "" {
			if err := text(b.text, b.task.Text, col, b.task.Line); err != nil {
				return err
			}
		}
		c.tracef("task %s row %d inside %v", b.task.ID, b.task.Row, b.inside)
	}
	for _, v := range gl.verts {
		c.segment(pt{v.x, v.y0}, pt{v.x, v.y1}, lineW*1.4, gtVertCol)
		if v.task.Text != "" {
			if err := text(v.label, v.task.Text, gtVertCol, v.task.Line); err != nil {
				return err
			}
		}
		c.tracef("vert %s", v.task.ID)
	}
	for _, s := range gl.sections {
		if err := text(s.text, strings.Join(s.lines, "\n"), colText, 0); err != nil {
			return err
		}
		c.tracef("section %s", strings.Join(s.lines, "|"))
	}
	for _, tk := range gl.ticks {
		if tk.label == "" {
			continue
		}
		if err := text(tk.box, tk.label, colText, 0); err != nil {
			return err
		}
		c.tracef("tick %s", tk.label)
	}
	return nil
}
