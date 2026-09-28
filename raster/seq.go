package raster

import (
	"fmt"
	"image/color"
	"math"
	"unicode/utf8"

	mr "github.com/nlink-jp/mermaid-render"
)

// A sequence diagram has a layout of its own: participants in columns, in
// order of first mention; events stacked down the page in source order.
// Columns are placed left to right, each as far from every column before
// it as the headers, message texts, self-message loops and notes between
// them need. Events are then laid down one after another; blocks frame
// what they hold, activations are bars on the lifelines, and the headers
// are repeated at the bottom (mermaid's mirrorActors default).

// Sequence spacing, in em.
const (
	sqPartSep   = 1.6  // between neighbouring header boxes
	sqMinPartW  = 4.0  // a header box's least width
	sqMsgPad    = 1.2  // a message's text to its arrow's ends
	sqMsgMinLen = 3.0  // the shortest message arrow
	sqTextGap   = 0.25 // a message's text to its arrow
	sqRowGap    = 0.9  // between events
	sqSelfW     = 2.4  // how far a message to itself loops out (1.8 looked cramped)
	sqSelfH     = 1.1  // and down
	sqSelfText  = 0.4  // its text's offset from the lifeline
	sqNotePad   = 0.45 // note text to its box
	sqNoteGap   = 0.5  // a note to a lifeline beside it
	sqNoteOver  = 0.8  // how far a note over lifelines reaches past them
	sqMinNoteW  = 2.4
	sqActW      = 0.7  // an activation bar's width
	sqActStep   = 0.35 // a nested bar's offset
	sqFramePad  = 0.7  // a block's frame to what it holds
	sqTabPad    = 0.3  // a block's kind label to its tab
	sqBoxPad    = 0.7  // a box to its headers
	sqNumR      = 0.45 // an autonumber's circle
	sqMinBar    = 0.8  // an activation with nothing in it
	sqFigH      = 2.2  // an actor's figure
)

var (
	colNote   = color.RGBA{0xff, 0xf8, 0xd6, 0xff}
	colNoteSt = color.RGBA{0xc9, 0xb4, 0x58, 0xff}
)

type sqHead struct {
	box   rect // the header box (an actor's: its figure and label)
	label string
	actor bool
	lw    float64 // label width and height
	lh    float64
}

type sqMsg struct {
	pts    []pt
	dotted bool
	head   mr.ArrowHead
	both   bool
	text   string
	tbox   rect // zero when there is no text
	number string
	numAt  pt
	line   int
}

type sqNote struct {
	box  rect
	text string
	line int
}

type sqSection struct {
	y    float64
	text string
	tbox rect
}

type sqFrame struct {
	box      rect
	kind     string
	tab      rect // the kind label's tab
	cond     string
	condBox  rect
	sections []sqSection
	depth    int
}

type sqBox struct {
	box   rect
	title string
	tbox  rect
}

// seqLayout is a placed sequence diagram, in em.
type seqLayout struct {
	W, H      float64
	cols      []float64
	heads     []sqHead // top, then bottom
	lifelines [][2]pt
	acts      []rect
	actCol    []int // the participant each bar in acts belongs to
	msgs      []sqMsg
	notes     []sqNote
	frames    []*sqFrame
	boxes     []sqBox
}

func layoutSequence(d *mr.Sequence, m measurer) (*seqLayout, error) {
	n := len(d.Participants)
	if n > MaxNodes {
		return nil, &mr.Error{Kind: mr.UnsupportedConstruct, Msg: fmt.Sprintf("%d participants (limit %d)", n, MaxNodes)}
	}
	measure := func(s string, bold bool, line int) (float64, float64, error) {
		if utf8.RuneCountInString(s) > MaxLabel {
			return 0, 0, &mr.Error{Kind: mr.UnsupportedConstruct, Line: line, Msg: fmt.Sprintf("a label longer than %d characters", MaxLabel)}
		}
		if s == "" {
			return 0, 0, nil
		}
		w, h, err := m(s, bold)
		if err != nil {
			return 0, 0, glyphErr(err, line)
		}
		return w, h, nil
	}
	idx := map[*mr.Participant]int{}
	heads := make([]sqHead, n)
	hdrH := 0.0
	for i, p := range d.Participants {
		idx[p] = i
		lw, lh, err := measure(p.Label, false, p.Line)
		if err != nil {
			return nil, err
		}
		w := math.Max(lw+2*padX, sqMinPartW)
		h := lh + 2*padY
		if p.Actor {
			w = math.Max(lw, 1.6)
			h = sqFigH + lh + 0.2
		}
		heads[i] = sqHead{box: rect{-w / 2, 0, w / 2, h}, label: p.Label, actor: p.Actor, lw: lw, lh: lh}
		hdrH = math.Max(hdrH, h)
	}

	// How far each participant's bars reach from its lifeline at their
	// deepest: what stands beside a lifeline stands beyond its bars.
	barReach := make([]float64, n)
	{
		dep := make([]int, n)
		for _, e := range d.Events {
			switch i := idx[e.From]; e.Kind {
			case mr.Activate:
				dep[i]++
				barReach[i] = math.Max(barReach[i], float64(dep[i]-1)*sqActStep+sqActW/2)
			case mr.Deactivate:
				dep[i] = max(dep[i]-1, 0)
			}
		}
	}

	// Columns: every constraint is between a column and one to its left.
	need := make([][]float64, n) // need[j][i]: x_j - x_i >= need, i < j
	for j := range need {
		need[j] = make([]float64, n)
	}
	atLeast := func(i, j int, dist float64) {
		if i > j {
			i, j = j, i
		}
		if i != j {
			need[j][i] = math.Max(need[j][i], dist)
		}
	}
	for i := 0; i+1 < n; i++ {
		d0 := heads[i].box.W()/2 + heads[i+1].box.W()/2 + sqPartSep
		if b0, b1 := d.Participants[i].Box, d.Participants[i+1].Box; b0 != b1 {
			if b0 != nil {
				d0 += sqBoxPad
			}
			if b1 != nil {
				d0 += sqBoxPad
			}
		}
		atLeast(i, i+1, d0)
	}
	type sized struct{ w, h float64 }
	sizes := make([]sized, len(d.Events))
	for k, e := range d.Events {
		var w, h float64
		var err error
		switch e.Kind {
		case mr.Message, mr.Note, mr.BlockSection:
			w, h, err = measure(e.Text, false, e.Line)
		case mr.BlockStart:
			w, h, err = measure(condText(e.Text), false, e.Line)
		}
		if err != nil {
			return nil, err
		}
		sizes[k] = sized{w, h}
		switch e.Kind {
		case mr.Message:
			a, b := idx[e.From], idx[e.To]
			if a == b {
				if a+1 < n {
					reach := math.Max(sqSelfW, sqSelfText+w+0.3)
					atLeast(a, a+1, barReach[a]+reach+0.8+barReach[a+1])
				}
				continue
			}
			l := math.Max(w+2*sqMsgPad, sqMsgMinLen) + barReach[a] + barReach[b]
			if e.Number != "" {
				l += 2 * sqNumR
			}
			atLeast(a, b, l)
		case mr.Note:
			nw := w + 2*sqNotePad
			a, b := idx[e.From], idx[e.To]
			switch e.Place {
			case mr.RightOf:
				if a+1 < n {
					atLeast(a, a+1, barReach[a]+sqNoteGap+nw+sqNoteGap+barReach[a+1])
				}
			case mr.LeftOf:
				if a > 0 {
					atLeast(a-1, a, barReach[a-1]+sqNoteGap+nw+sqNoteGap+barReach[a])
				}
			case mr.Over:
				lo, hi := min(a, b), max(a, b)
				if lo == hi {
					nw = math.Max(nw, sqMinNoteW)
					if lo+1 < n {
						atLeast(lo, lo+1, nw/2+sqNoteGap)
					}
					if lo > 0 {
						atLeast(lo-1, lo, nw/2+sqNoteGap)
					}
				} else {
					atLeast(lo, hi, nw-2*sqNoteOver)
					if hi+1 < n {
						atLeast(hi, hi+1, sqNoteOver+sqNoteGap)
					}
					if lo > 0 {
						atLeast(lo-1, lo, sqNoteOver+sqNoteGap)
					}
				}
			}
		}
	}
	cols := make([]float64, n)
	for j := 1; j < n; j++ {
		for i := 0; i < j; i++ {
			cols[j] = math.Max(cols[j], cols[i]+need[j][i])
		}
	}

	// Rows.
	sl := &seqLayout{cols: cols}
	y := 0.0
	boxTitleH := 0.0
	for _, b := range d.Boxes {
		if b.Title != "" && len(b.Participants) > 0 {
			_, th, err := measure(b.Title, false, b.Line)
			if err != nil {
				return nil, err
			}
			boxTitleH = math.Max(boxTitleH, th+0.4)
		}
	}
	y += boxTitleH
	for i := range heads {
		h := heads[i]
		h.box = rect{cols[i] + h.box.X0, y, cols[i] + h.box.X1, y + hdrH}
		sl.heads = append(sl.heads, h)
	}
	lifeTop := y + hdrH
	y = lifeTop + sqRowGap
	depth := map[int]int{}
	type openAct struct {
		start float64
		level int
	}
	acts := map[int][]openAct{}
	lastY := lifeTop
	var stack []*sqFrame
	contents := map[*sqFrame][]rect{}
	note := func(r rect) {
		for _, f := range stack {
			contents[f] = append(contents[f], r)
		}
	}
	// edge is where a message meets participant i's lifeline, heading
	// toward x: at the outside of its bars when it has any — the leftmost
	// bar's left side, the rightmost's right side (nested bars step
	// right), as mermaid's activationBounds does.
	edge := func(i int, toward float64, dep int) float64 {
		x := cols[i]
		if dep <= 0 {
			return x
		}
		if toward < x {
			return x - sqActW/2
		}
		return x + float64(dep-1)*sqActStep + sqActW/2
	}
	for k, e := range d.Events {
		sz := sizes[k]
		switch e.Kind {
		case mr.Message:
			a, b := idx[e.From], idx[e.To]
			textTop := y
			arrowY := y + 0.3
			if e.Text != "" {
				arrowY = y + sz.h + sqTextGap
			}
			// A message that activates its target ends on the new bar.
			dTo := depth[b]
			if k+1 < len(d.Events) && d.Events[k+1].Kind == mr.Activate && d.Events[k+1].From == e.To {
				dTo++
			}
			msg := sqMsg{dotted: e.Dotted, head: e.Head, both: e.BothEnds, text: e.Text, number: e.Number, line: e.Line}
			if a == b {
				x0 := edge(a, math.Inf(1), depth[a])
				x1 := edge(a, math.Inf(1), dTo)
				reach := cols[a] + barReach[a] + sqSelfW
				msg.pts = []pt{{x0, arrowY}, {reach, arrowY}, {reach, arrowY + sqSelfH}, {x1, arrowY + sqSelfH}}
				if e.Text != "" {
					tx := cols[a] + barReach[a] + sqSelfText
					msg.tbox = rect{tx, textTop, tx + sz.w, textTop + sz.h}
				}
				lastY = arrowY + sqSelfH
				y = lastY + sqRowGap
				note(rect{cols[a], arrowY, reach, arrowY + sqSelfH})
			} else {
				x0 := edge(a, cols[b], depth[a])
				x1 := edge(b, cols[a], dTo)
				msg.pts = []pt{{x0, arrowY}, {x1, arrowY}}
				if e.Text != "" {
					cx := (x0 + x1) / 2
					msg.tbox = rect{cx - sz.w/2, textTop, cx + sz.w/2, textTop + sz.h}
				}
				lastY = arrowY
				y = arrowY + sqRowGap
				note(rect{math.Min(x0, x1), arrowY - 0.3, math.Max(x0, x1), arrowY + 0.3})
			}
			msg.numAt = msg.pts[0]
			if e.BothEnds {
				// A head at the start too: the number stands behind it.
				dir := math.Copysign(1, msg.pts[1].X-msg.pts[0].X)
				msg.numAt.X -= dir * (arrowLen + sqNumR + 0.15)
			}
			if msg.tbox != (rect{}) {
				note(msg.tbox)
			}
			sl.msgs = append(sl.msgs, msg)
		case mr.Note:
			a, b := idx[e.From], idx[e.To]
			nw, nh := sz.w+2*sqNotePad, sz.h+2*sqNotePad
			var r rect
			switch e.Place {
			case mr.RightOf:
				x := cols[a] + barReach[a] + sqNoteGap
				r = rect{x, y, x + nw, y + nh}
			case mr.LeftOf:
				x := cols[a] - barReach[a] - sqNoteGap
				r = rect{x - nw, y, x, y + nh}
			default:
				lo, hi := cols[min(a, b)], cols[max(a, b)]
				if a == b {
					nw = math.Max(nw, sqMinNoteW)
					r = rect{lo - nw/2, y, lo + nw/2, y + nh}
				} else {
					x0, x1 := lo-sqNoteOver, hi+sqNoteOver
					if x1-x0 < nw {
						c := (lo + hi) / 2
						x0, x1 = c-nw/2, c+nw/2
					}
					r = rect{x0, y, x1, y + nh}
				}
			}
			sl.notes = append(sl.notes, sqNote{box: r, text: e.Text, line: e.Line})
			note(r)
			lastY = r.Y1
			y = r.Y1 + sqRowGap
		case mr.Activate:
			i := idx[e.From]
			acts[i] = append(acts[i], openAct{start: lastY, level: depth[i]})
			x := cols[i] + float64(depth[i])*sqActStep
			note(rect{x - sqActW/2, lastY, x + sqActW/2, lastY}) // a frame holds the bars that start in it
			depth[i]++
		case mr.Deactivate:
			i := idx[e.From]
			st := acts[i]
			if len(st) == 0 {
				continue // the parser refuses this; keep the layout safe anyway
			}
			o := st[len(st)-1]
			acts[i] = st[:len(st)-1]
			depth[i]--
			end := math.Max(lastY, o.start+sqMinBar)
			x := cols[i] + float64(o.level)*sqActStep
			sl.acts = append(sl.acts, rect{x - sqActW/2, o.start, x + sqActW/2, end})
			sl.actCol = append(sl.actCol, i)
			note(rect{x - sqActW/2, end, x + sqActW/2, end}) // and those that end in it
			if end > lastY {
				lastY = end
				y = math.Max(y, end+sqRowGap)
			}
		case mr.BlockStart:
			f := &sqFrame{kind: e.Block.String(), cond: condText(e.Text), depth: len(stack)}
			kw, kh, err := measure(f.kind, true, e.Line)
			if err != nil {
				return nil, err
			}
			tabH := math.Max(kh, sz.h) + 2*sqTabPad
			f.box.Y0 = y - 0.3
			f.tab = rect{0, f.box.Y0, kw + 2*sqTabPad + 0.4, f.box.Y0 + tabH}
			if f.cond != "" {
				f.condBox = rect{0, f.box.Y0 + (tabH-sz.h)/2, sz.w, f.box.Y0 + (tabH+sz.h)/2}
			}
			sl.frames = append(sl.frames, f)
			stack = append(stack, f)
			y = f.box.Y0 + tabH + 0.5
			lastY = y - 0.5
		case mr.BlockSection:
			f := stack[len(stack)-1]
			sy := y - 0.2
			s := sqSection{y: sy, text: condText(e.Text)}
			if s.text != "" {
				s.tbox = rect{0, sy + 0.25, sz.w, sy + 0.25 + sz.h}
				y = s.tbox.Y1 + 0.5
			} else {
				y = sy + 0.6
			}
			f.sections = append(f.sections, s)
			lastY = y - 0.5
		case mr.BlockEnd:
			f := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			f.box.Y1 = y - 0.2
			y = f.box.Y1 + 0.5
			lastY = f.box.Y1
		}
	}
	// Activations still open run to the end.
	for i, st := range acts {
		for _, o := range st {
			x := cols[i] + float64(o.level)*sqActStep
			sl.acts = append(sl.acts, rect{x - sqActW/2, o.start, x + sqActW/2, math.Max(y-sqRowGap/2, o.start+sqMinBar)})
			sl.actCol = append(sl.actCol, i)
		}
	}
	sl.sortActs()
	lifeBottom := y
	for i := range heads {
		sl.lifelines = append(sl.lifelines, [2]pt{{cols[i], lifeTop}, {cols[i], lifeBottom}})
		h := sl.heads[i]
		h.box = rect{h.box.X0, lifeBottom, h.box.X1, lifeBottom + hdrH}
		sl.heads = append(sl.heads, h)
	}
	sl.H = lifeBottom + hdrH

	// Frames: around what they hold, inner frames inside outer ones, wide
	// enough for their tab and condition.
	for i := len(sl.frames) - 1; i >= 0; i-- {
		f := sl.frames[i]
		x0, x1 := math.Inf(1), math.Inf(-1)
		for _, r := range contents[f] {
			x0, x1 = math.Min(x0, r.X0), math.Max(x1, r.X1)
		}
		for _, g := range sl.frames[i+1:] {
			if g.depth > f.depth && g.box.Y0 >= f.box.Y0 && g.box.Y1 <= f.box.Y1 {
				x0, x1 = math.Min(x0, g.box.X0), math.Max(x1, g.box.X1)
			}
		}
		if math.IsInf(x0, 1) {
			x0, x1 = 0, 0 // an empty block, perhaps with no participants at all
			if n > 0 {
				x0, x1 = cols[0], cols[0]
			}
		}
		x0 -= sqFramePad
		x1 += sqFramePad
		minW := f.tab.W() + 0.4 + f.condBox.W() + 0.6
		for _, s := range f.sections {
			minW = math.Max(minW, s.tbox.W()+1.2)
		}
		x1 = math.Max(x1, x0+minW)
		f.box.X0, f.box.X1 = x0, x1
		f.tab.X1 = x0 + f.tab.W()
		f.tab.X0 = x0
		if f.cond != "" {
			w := f.condBox.W()
			f.condBox.X0 = f.tab.X1 + 0.4
			f.condBox.X1 = f.condBox.X0 + w
		}
		for k := range f.sections {
			s := &f.sections[k]
			if s.text != "" {
				w := s.tbox.W()
				c := (x0 + x1) / 2
				s.tbox.X0, s.tbox.X1 = c-w/2, c+w/2
			}
		}
	}
	// Boxes: behind their headers, the whole height.
	for _, b := range d.Boxes {
		if len(b.Participants) == 0 {
			continue
		}
		x0, x1 := math.Inf(1), math.Inf(-1)
		for _, p := range b.Participants {
			h := sl.heads[idx[p]]
			x0, x1 = math.Min(x0, h.box.X0), math.Max(x1, h.box.X1)
		}
		sb := sqBox{box: rect{x0 - sqBoxPad, 0, x1 + sqBoxPad, sl.H}, title: b.Title}
		if b.Title != "" {
			tw, th, _ := measure(b.Title, false, b.Line)
			c := (sb.box.X0 + sb.box.X1) / 2
			sb.tbox = rect{c - tw/2, 0.2, c + tw/2, 0.2 + th}
			if sb.box.W() < tw+0.8 {
				sb.box.X0, sb.box.X1 = c-tw/2-0.4, c+tw/2+0.4
			}
		}
		sl.boxes = append(sl.boxes, sb)
	}

	// Bounds: shift everything so the leftmost element starts at 0.
	minX, maxX := math.Inf(1), math.Inf(-1)
	grow := func(r rect) { minX, maxX = math.Min(minX, r.X0), math.Max(maxX, r.X1) }
	for _, h := range sl.heads {
		grow(h.box)
	}
	for _, m := range sl.msgs {
		for _, p := range m.pts {
			grow(rect{p.X, p.Y, p.X, p.Y})
		}
		if m.tbox != (rect{}) {
			grow(m.tbox)
		}
		if m.number != "" {
			grow(rect{m.numAt.X - 2*sqNumR, 0, m.numAt.X + 2*sqNumR, 0})
		}
	}
	for _, nt := range sl.notes {
		grow(nt.box)
	}
	for _, f := range sl.frames {
		grow(f.box)
	}
	for _, b := range sl.boxes {
		grow(b.box)
	}
	// Deep bars on the last participant reach past its header.
	for _, a := range sl.acts {
		grow(a)
	}
	sl.shift(-minX)
	sl.W = maxX - minX
	return sl, nil
}

// condText is how a block's condition is shown: "[text]", or nothing.
func condText(s string) string {
	if s == "" {
		return ""
	}
	return "[" + s + "]"
}

// sortActs orders the bars left to right, then top to bottom, their
// owners with them: the open bars were collected from a map.
func (sl *seqLayout) sortActs() {
	rs, cs := sl.acts, sl.actCol
	for i := 1; i < len(rs); i++ {
		for j := i; j > 0 && (rs[j].X0 < rs[j-1].X0 || rs[j].X0 == rs[j-1].X0 && rs[j].Y0 < rs[j-1].Y0); j-- {
			rs[j], rs[j-1] = rs[j-1], rs[j]
			cs[j], cs[j-1] = cs[j-1], cs[j]
		}
	}
}

func (r rect) moved(dx float64) rect { return rect{r.X0 + dx, r.Y0, r.X1 + dx, r.Y1} }

func (sl *seqLayout) shift(dx float64) {
	for i := range sl.cols {
		sl.cols[i] += dx
	}
	for i := range sl.heads {
		sl.heads[i].box = sl.heads[i].box.moved(dx)
	}
	for i := range sl.lifelines {
		sl.lifelines[i][0].X += dx
		sl.lifelines[i][1].X += dx
	}
	for i := range sl.acts {
		sl.acts[i] = sl.acts[i].moved(dx)
	}
	for i := range sl.msgs {
		m := &sl.msgs[i]
		for k := range m.pts {
			m.pts[k].X += dx
		}
		if m.tbox != (rect{}) {
			m.tbox = m.tbox.moved(dx)
		}
		m.numAt.X += dx
	}
	for i := range sl.notes {
		sl.notes[i].box = sl.notes[i].box.moved(dx)
	}
	for _, f := range sl.frames {
		f.box, f.tab = f.box.moved(dx), f.tab.moved(dx)
		if f.cond != "" {
			f.condBox = f.condBox.moved(dx)
		}
		for k := range f.sections {
			if f.sections[k].text != "" {
				f.sections[k].tbox = f.sections[k].tbox.moved(dx)
			}
		}
	}
	for i := range sl.boxes {
		sl.boxes[i].box = sl.boxes[i].box.moved(dx)
		if sl.boxes[i].title != "" {
			sl.boxes[i].tbox = sl.boxes[i].tbox.moved(dx)
		}
	}
}

// drawSequence draws a placed sequence diagram.
func (c *canvas) drawSequence(sl *seqLayout, fn *Font) error {
	text := func(r rect, s string, bold bool, size float64, col color.Color) error {
		p := r.Center()
		return fn.drawText(c.img, (p.X+c.offX)*c.em, (p.Y+c.offY)*c.em, s, bold, c.em*size, col)
	}
	for _, b := range sl.boxes {
		c.outlineShape(roundRect(b.box, 0.3), colFrame, colFrameSt, 0.06, false)
		if b.title != "" {
			if err := text(b.tbox, b.title, false, 1, colText); err != nil {
				return err
			}
		}
	}
	for _, l := range sl.lifelines {
		c.polyline([]pt{l[0], l[1]}, lineW*0.8, true, colFrameSt)
	}
	for _, f := range sl.frames {
		c.outlineShape(roundRect(f.box, 0), nil, colFrameSt, lineW, false)
		tab := f.tab
		cut := math.Min(0.35, tab.H()/2)
		c.outlineShape([]pt{{tab.X0, tab.Y0}, {tab.X1, tab.Y0}, {tab.X1, tab.Y1 - cut}, {tab.X1 - cut, tab.Y1}, {tab.X0, tab.Y1}}, colNode, colFrameSt, lineW, false)
		if err := text(rect{tab.X0, tab.Y0, tab.X1 - 0.2, tab.Y1}, f.kind, true, 1, colText); err != nil {
			return err
		}
		if f.cond != "" {
			c.labelBG(rect{f.condBox.X0 - 0.15, f.condBox.Y0, f.condBox.X1 + 0.15, f.condBox.Y1})
			if err := text(f.condBox, f.cond, false, 1, colText); err != nil {
				return err
			}
		}
		for _, s := range f.sections {
			c.polyline([]pt{{f.box.X0, s.y}, {f.box.X1, s.y}}, lineW, true, colFrameSt)
			if s.text != "" {
				c.labelBG(rect{s.tbox.X0 - 0.15, s.tbox.Y0, s.tbox.X1 + 0.15, s.tbox.Y1})
				if err := text(s.tbox, s.text, false, 1, colText); err != nil {
					return err
				}
			}
		}
	}
	for _, a := range sl.acts {
		c.outlineShape(roundRect(a, 0), colNode, colStroke, lineW, false)
	}
	for _, m := range sl.msgs {
		c.polyline(m.pts, lineW, m.dotted, colEdge)
		n := len(m.pts)
		c.seqHead(m.head, m.pts[n-1], m.pts[n-2])
		if m.both {
			c.seqHead(m.head, m.pts[0], m.pts[1])
		}
		if m.tbox != (rect{}) {
			c.labelBG(rect{m.tbox.X0 - 0.15, m.tbox.Y0, m.tbox.X1 + 0.15, m.tbox.Y1})
			if err := text(m.tbox, m.text, false, 1, colText); err != nil {
				return glyphErr(err, m.line)
			}
		}
	}
	for _, m := range sl.msgs {
		if m.number == "" {
			continue
		}
		p := m.numAt
		w, _, err := fn.measureEm(m.number, true)
		if err != nil {
			return err
		}
		// Wide enough for the digits at 0.7 em: "10" and "1.25" too.
		rx := math.Max(sqNumR, w*0.7/2+0.2)
		c.fill(ellipse(p.X, p.Y, rx, sqNumR, 24), colEdge)
		if err := text(rect{p.X, p.Y, p.X, p.Y}, m.number, true, 0.7, colBG); err != nil {
			return err
		}
	}
	for _, nt := range sl.notes {
		c.outlineShape(roundRect(nt.box, 0), colNote, colNoteSt, lineW, false)
		if err := text(nt.box, nt.text, false, 1, colText); err != nil {
			return glyphErr(err, nt.line)
		}
	}
	for _, h := range sl.heads {
		if err := c.seqHeader(h, fn); err != nil {
			return err
		}
	}
	return nil
}

func (c *canvas) seqHead(h mr.ArrowHead, tip, from pt) {
	if h != mr.HeadNone {
		c.tracef("head %s at %.2f,%.2f", h, tip.X, tip.Y)
	}
	switch h {
	case mr.HeadFilled:
		c.head(mr.Arrow, tip, from)
	case mr.HeadCross:
		c.head(mr.CrossHead, tip, from)
	case mr.HeadOpen:
		dx, dy := tip.X-from.X, tip.Y-from.Y
		l := math.Hypot(dx, dy)
		if l == 0 {
			return
		}
		dx, dy = dx/l, dy/l
		b := pt{tip.X - dx*arrowLen, tip.Y - dy*arrowLen}
		c.segment(tip, pt{b.X - dy*arrowHalfW, b.Y + dx*arrowHalfW}, lineW*1.2, colEdge)
		c.segment(tip, pt{b.X + dy*arrowHalfW, b.Y - dx*arrowHalfW}, lineW*1.2, colEdge)
	}
}

// seqHeader draws a participant's box, or an actor's figure with its name
// below.
func (c *canvas) seqHeader(h sqHead, fn *Font) error {
	b := h.box
	if !h.actor {
		c.outlineShape(roundRect(b, 0.15), colNode, colStroke, lineW, false)
		p := b.Center()
		return fn.drawText(c.img, (p.X+c.offX)*c.em, (p.Y+c.offY)*c.em, h.label, false, c.em, colText)
	}
	cx := b.Center().X
	top := b.Y0 + 0.1
	r := 0.32
	head := ellipse(cx, top+r, r, r, 24)
	c.fill(head, colNode)
	c.polyline(append(head, head[0]), lineW, false, colStroke)
	neck, hip := top+2*r, top+1.45
	c.segment(pt{cx, neck}, pt{cx, hip}, lineW, colStroke)
	c.segment(pt{cx - 0.55, neck + 0.3}, pt{cx + 0.55, neck + 0.3}, lineW, colStroke)
	c.segment(pt{cx, hip}, pt{cx - 0.45, top + sqFigH - 0.15}, lineW, colStroke)
	c.segment(pt{cx, hip}, pt{cx + 0.45, top + sqFigH - 0.15}, lineW, colStroke)
	ly := b.Y0 + sqFigH + 0.1 + h.lh/2
	return fn.drawText(c.img, (cx+c.offX)*c.em, (ly+c.offY)*c.em, h.label, false, c.em, colText)
}
