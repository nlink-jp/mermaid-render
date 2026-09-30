package raster

import (
	"fmt"
	"strings"

	mr "github.com/nlink-jp/mermaid-render"
)

// Sequence text art: participants in boxes across the top and again at
// the bottom, │ lifelines (┃ while active), messages as arrows with their
// text above, notes as boxes, blocks as frames with their kind and label,
// sections split by a ┈ line. Columns are placed left to right, each as far
// as every message, note and frame spanning it needs; rows are events in
// order. Every cell is written through one gate that knows what may cover
// what: that is the check.

const (
	tqGap      = 3 // a lifeline to a frame's side, the innermost level
	tqTextPad  = 2 // a message's text to the lifelines it spans
	tqNoteGap  = 2 // a note to the lifeline it stands beside
	tqLoopW    = 3 // a message to self: out, down, back
	tqMinSpace = 4 // columns between two neighbours' boxes
)

type seqCell int

// What a cell holds, for the gate.
const (
	scFree seqCell = iota
	scLifeline
	scBox   // a participant's or a note's box
	scFrame // a block's border
	scArrow
	scText
	scGroup // a participant group's border: background, crossed by all
)

type seqArt struct {
	g     *tgrid
	kind  [][]seqCell
	tm    *textMeasure
	fault []string
}

func (a *seqArt) faultf(format string, args ...any) {
	a.fault = append(a.fault, fmt.Sprintf(format, args...))
}

// put writes a rune in a cell if what is there may be covered by what: a
// lifeline by anything, a frame's border only by an arrow's crossing
// (never), free cells by anything.
func (a *seqArt) put(x, y int, r rune, what seqCell) {
	if !a.g.in(x, y) {
		a.faultf("something is drawn outside the grid")
		return
	}
	have := a.kind[y][x]
	switch {
	case have == scFree:
	case have == scLifeline && what == scArrow:
		if r == '─' || r == '┈' {
			r = '┼'
		}
	case have == scLifeline && what == scFrame && (r == '─' || r == '┈'):
		r = '┼'
	case have == scLifeline && (what == scText || what == scBox || what == scFrame):
	case have == scGroup && what == scArrow && (r == '─' || r == '┈'):
		r = '┼'
	case have == scGroup && what != scGroup:
	default:
		a.faultf("%s drawn over %s at %d,%d", seqWhat(what), seqWhat(have), x, y)
		return
	}
	a.g.cells[y][x] = tcell{r: r}
	a.kind[y][x] = what
}

func seqWhat(c seqCell) string {
	return [...]string{"nothing", "a lifeline", "a box", "a frame", "an arrow", "text", "a group"}[c]
}

func (a *seqArt) text(x, y int, s string, what seqCell) {
	for _, r := range s {
		w := a.tm.width(r)
		a.put(x, y, r, what)
		for k := 1; k < w; k++ {
			if a.g.in(x+k, y) {
				if k := a.kind[y][x+k]; k != scFree && k != scLifeline && k != scGroup {
					a.faultf("text drawn over %s", seqWhat(k))
				}
				a.g.cells[y][x+k] = tcell{cont: true}
				a.kind[y][x+k] = what
			}
		}
		x += w
	}
}

func (a *seqArt) box(r iRect, text string) {
	// A lifeline running into a note's box joins its border above and below.
	var joins []int
	for x := r.x0 + 1; x < r.x1; x++ {
		if a.g.in(x, r.y0) && a.kind[r.y0][x] == scLifeline {
			joins = append(joins, x)
		}
	}
	defer func() {
		for _, x := range joins {
			if a.g.in(x, r.y0-1) && a.kind[r.y0-1][x] == scLifeline {
				a.g.cells[r.y0][x].r = '┴'
			}
			if a.g.in(x, r.y1+1) && a.kind[r.y1+1][x] == scLifeline {
				a.g.cells[r.y1][x].r = '┬'
			}
		}
	}()
	c := [6]rune{'┌', '─', '┐', '│', '└', '┘'}
	for x := r.x0; x <= r.x1; x++ {
		for y := r.y0; y <= r.y1; y++ {
			ch := ' '
			switch {
			case y == r.y0 && x == r.x0:
				ch = c[0]
			case y == r.y0 && x == r.x1:
				ch = c[2]
			case y == r.y1 && x == r.x0:
				ch = c[4]
			case y == r.y1 && x == r.x1:
				ch = c[5]
			case y == r.y0 || y == r.y1:
				ch = c[1]
			case x == r.x0 || x == r.x1:
				ch = c[3]
			}
			a.put(x, y, ch, scBox)
		}
	}
	// The label is inside the box it was sized for: written as is.
	lines := strings.Split(text, "\n")
	for k, l := range lines {
		x := r.x0 + 1 + (r.x1-r.x0-1-a.tm.cells(l))/2
		if a.g.in(x, r.y0+1+k) {
			a.g.text(x, r.y0+1+k, l, 0, a.tm)
		}
	}
}

// seqBlock is an open block while laying out.
type seqBlock struct {
	kind       mr.BlockKind
	start      int // event index
	lo, hi     int // participant span
	depthBelow int // nested levels inside it
}

func seqText(d *mr.Sequence, tm *textMeasure) (*tgrid, error) {
	ps := d.Participants
	if len(ps) == 0 {
		// Blocks with no one in them have no columns to stand in: empty art
		// would say the diagram is empty.
		if len(d.Events) > 0 {
			return nil, &mr.Error{Kind: mr.UnsupportedConstruct, Msg: "text art: blocks with no participant in them"}
		}
		return newGrid(0, 0)
	}
	idx := map[*mr.Participant]int{}
	for i, p := range ps {
		idx[p] = i
		if err := tm.check(p.Label, p.Line); err != nil {
			return nil, err
		}
	}
	for _, b := range d.Boxes {
		if err := tm.check(b.Title, b.Line); err != nil {
			return nil, err
		}
	}
	text := func(e *mr.Event) string {
		if e.Kind == mr.Message && e.Number != "" {
			return e.Number + ". " + e.Text
		}
		return e.Text
	}
	for _, e := range d.Events {
		if err := tm.check(text(e), e.Line); err != nil {
			return nil, err
		}
	}
	// Blocks: each one's participant span and nesting below it.
	blockSpan := map[int]*seqBlock{} // by start event
	var open []*seqBlock
	for i, e := range d.Events {
		switch e.Kind {
		case mr.BlockStart:
			b := &seqBlock{kind: e.Block, start: i, lo: len(ps), hi: -1}
			open = append(open, b)
			blockSpan[i] = b
		case mr.BlockEnd:
			if n := len(open); n > 0 {
				b := open[n-1]
				open = open[:n-1]
				if b.hi < 0 { // an empty block spans its neighbours' first
					b.lo, b.hi = 0, 0
				}
				if n > 1 {
					p := open[n-2]
					p.lo, p.hi = min(p.lo, b.lo), max(p.hi, b.hi)
					p.depthBelow = max(p.depthBelow, b.depthBelow+1)
				}
			}
		case mr.Message, mr.Note:
			lo, hi := idx[e.From], idx[e.From]
			if e.To != nil {
				lo, hi = min(lo, idx[e.To]), max(hi, idx[e.To])
			}
			for _, b := range open {
				b.lo, b.hi = min(b.lo, lo), max(b.hi, hi)
			}
		}
	}
	maxDepth := 0
	for _, b := range blockSpan {
		maxDepth = max(maxDepth, b.depthBelow+1)
	}
	// Column needs between lifelines i < j.
	n := len(ps)
	need := make([][]int, n)
	for i := range need {
		need[i] = make([]int, n)
	}
	boxW := make([]int, n)
	boxH := 0
	for i, p := range ps {
		w, h := tm.size(p.Label)
		boxW[i] = w + 4
		boxH = max(boxH, h+2)
	}
	for i := 0; i+1 < n; i++ {
		need[i][i+1] = boxW[i]/2 + boxW[i+1]/2 + tqMinSpace
	}
	left, right := boxW[0]/2+1, boxW[n-1]/2+1 // room beyond the outer lifelines
	for _, b := range d.Boxes {
		w, _ := tm.size(b.Title)
		if len(b.Participants) > 0 {
			lo, hi := idx[b.Participants[0]], idx[b.Participants[len(b.Participants)-1]]
			if hi > lo {
				need[lo][hi] = max(need[lo][hi], w+4-boxW[lo]/2-boxW[hi]/2)
			} else {
				// A group of one widens past its box to hold its title
				// (groupX): keep that room to its right.
				// (The last one's is in the width, from groupX.)
				if lo+1 < n {
					need[lo][lo+1] = max(need[lo][lo+1], w+2-boxW[lo]/2+boxW[lo+1]/2+3)
				}
			}
		}
	}
	for _, e := range d.Events {
		w, _ := tm.size(text(e))
		switch e.Kind {
		case mr.Message:
			i, j := idx[e.From], idx[e.To]
			if i == j {
				if i+1 < n {
					need[i][i+1] = max(need[i][i+1], tqLoopW+2+w+tqTextPad)
				} else {
					right = max(right, tqLoopW+2+w+1)
				}
				continue
			}
			i, j = min(i, j), max(i, j)
			need[i][j] = max(need[i][j], w+2*tqTextPad, 3)
		case mr.Note:
			i := idx[e.From]
			nw := w + 4
			switch {
			case e.Place == mr.LeftOf:
				if i > 0 {
					need[i-1][i] = max(need[i-1][i], nw+2*tqNoteGap)
				} else {
					left = max(left, nw+tqNoteGap+1)
				}
			case e.Place == mr.RightOf:
				if i+1 < n {
					need[i][i+1] = max(need[i][i+1], nw+2*tqNoteGap)
				} else {
					right = max(right, nw+tqNoteGap+1)
				}
			case e.To != nil && e.To != e.From:
				a, b := min(i, idx[e.To]), max(i, idx[e.To])
				need[a][b] = max(need[a][b], nw-2)
			default:
				if i > 0 {
					need[i-1][i] = max(need[i-1][i], nw/2+tqNoteGap+1)
				} else {
					left = max(left, nw/2+1)
				}
				if i+1 < n {
					need[i][i+1] = max(need[i][i+1], nw/2+tqNoteGap+1)
				} else {
					right = max(right, nw/2+1)
				}
			}
		}
	}
	pos := make([]int, n)
	pos[0] = left
	for j := 1; j < n; j++ {
		for i := 0; i < j; i++ {
			pos[j] = max(pos[j], pos[i]+need[i][j])
		}
	}
	// Each event's extent across; a block's frame holds its events' and its
	// inner frames', a column out per level, and its label.
	extent := func(e *mr.Event) (int, int) {
		w, _ := tm.size(text(e))
		switch e.Kind {
		case mr.Message:
			i, j := pos[idx[e.From]], pos[idx[e.To]]
			if i == j {
				return i, i + tqLoopW + 1 + w
			}
			lo, hi := min(i, j), max(i, j)
			mid := lo + (hi-lo-w)/2 + 1
			return min(lo, mid), max(hi, mid+w-1)
		case mr.Note:
			x0, nw := noteX(e, w, pos, idx)
			return x0, x0 + nw - 1
		}
		return 1 << 30, -1
	}
	frames := map[int][2]int{} // by start event
	{
		type acc struct {
			start  int
			lo, hi int
		}
		var st []*acc
		for i, e := range d.Events {
			switch e.Kind {
			case mr.BlockStart:
				st = append(st, &acc{start: i, lo: 1 << 30, hi: -1})
			case mr.BlockEnd:
				if len(st) == 0 {
					continue
				}
				a := st[len(st)-1]
				st = st[:len(st)-1]
				b := blockSpan[a.start]
				if a.hi < 0 { // an empty block: its first participant's lifeline
					a.lo, a.hi = pos[b.lo], pos[b.lo]
				}
				x0, x1 := a.lo-2, a.hi+2
				label := 0
				for _, s := range []int{a.start} {
					w, _ := tm.size(d.Events[s].Text)
					label = w + tm.cells(blockWord(b.kind.String()))
				}
				for k := a.start + 1; k < i; k++ {
					if d.Events[k].Kind == mr.BlockSection && startOf(d.Events, d.Events[k]) == a.start {
						w, _ := tm.size(d.Events[k].Text)
						label = max(label, w+tm.cells(blockWord(sectionWord(b.kind))))
					}
				}
				x1 = max(x1, x0+label+3)
				frames[a.start] = [2]int{x0, x1}
				if len(st) > 0 {
					p := st[len(st)-1]
					p.lo, p.hi = min(p.lo, x0-1), max(p.hi, x1+1)
				}
			case mr.Message, mr.Note:
				lo, hi := extent(e)
				for _, a := range st {
					a.lo, a.hi = min(a.lo, lo), max(a.hi, hi)
				}
			}
		}
	}
	// Shift everything right so that nothing starts left of column 0.
	minX, maxX := 0, pos[n-1]+right
	for _, e := range d.Events {
		lo, hi := extent(e)
		if hi >= 0 {
			minX, maxX = min(minX, lo), max(maxX, hi)
		}
	}
	for _, f := range frames {
		minX, maxX = min(minX, f[0]), max(maxX, f[1])
	}
	// groupX is a participant group's frame across: around its members'
	// boxes, and wide enough for its title.
	groupX := func(b *mr.Box) (int, int) {
		lo, hi := idx[b.Participants[0]], idx[b.Participants[len(b.Participants)-1]]
		w, _ := tm.size(b.Title)
		x0, x1 := pos[lo]-boxW[lo]/2-1, pos[hi]-boxW[hi]/2+boxW[hi]
		return x0, max(x1, x0+w+3)
	}
	for _, b := range d.Boxes {
		if len(b.Participants) > 0 {
			x0, x1 := groupX(b)
			minX, maxX = min(minX, x0), max(maxX, x1)
		}
	}
	if minX < 0 {
		for i := range pos {
			pos[i] -= minX
		}
		for k, f := range frames {
			frames[k] = [2]int{f[0] - minX, f[1] - minX}
		}
		maxX -= minX
	}
	width := maxX + 2
	// Rows.
	top := 0
	if len(d.Boxes) > 0 {
		top = 2 // a group's frame border and title above the boxes
	}
	type row struct{ y, h int }
	rows := make([]row, len(d.Events))
	y := top + boxH + 1
	for i, e := range d.Events {
		_, h := tm.size(text(e))
		switch e.Kind {
		case mr.Message:
			if e.From == e.To {
				h = max(h, 1) + 1
			} else {
				h++
			}
		case mr.Note:
			h += 2
		case mr.BlockStart, mr.BlockSection, mr.BlockEnd:
			h = 1
		default:
			h = 0
		}
		rows[i] = row{y, h}
		if h > 0 {
			y += h + 1
		}
	}
	bottom := y
	height := bottom + boxH
	if len(d.Boxes) > 0 {
		height++
	}
	g, err := newGrid(width, height)
	if err != nil {
		return nil, err
	}
	a := &seqArt{g: g, tm: tm, kind: make([][]seqCell, height)}
	for k := range a.kind {
		a.kind[k] = make([]seqCell, width)
	}
	// Lifelines, ┃ where active.
	active := make([][]bool, n)
	for i := range active {
		active[i] = make([]bool, height)
	}
	// Activations and deactivations right after a message (A->>+B,
	// B-->>-A, or activate / deactivate lines) all happen at that message's
	// arrow, as the picture's do: a bar open before the message runs to the
	// arrow, one open after the run of toggles runs on from it.
	toggles := func(k mr.EventKind) bool { return k == mr.Activate || k == mr.Deactivate }
	depth := make([]int, n)
	for i, e := range d.Events {
		switch e.Kind {
		case mr.Activate:
			depth[idx[e.From]]++
		case mr.Deactivate:
			if depth[idx[e.From]] > 0 {
				depth[idx[e.From]]--
			}
		}
		next := height
		if i+1 < len(rows) {
			next = rows[i+1].y
		}
		mark := func(p, from, to int) {
			for yy := from; yy < to && yy < bottom; yy++ {
				active[p][yy] = true
			}
		}
		if e.Kind != mr.Message {
			for p := range ps {
				if depth[p] > 0 {
					mark(p, rows[i].y, next)
				}
			}
			continue
		}
		arrow := rows[i].y + rows[i].h - 1
		after := append([]int(nil), depth...)
		for k := i + 1; k < len(d.Events) && toggles(d.Events[k].Kind); k++ {
			q := idx[d.Events[k].From]
			if d.Events[k].Kind == mr.Activate {
				after[q]++
			} else if after[q] > 0 {
				after[q]--
			}
		}
		for p := range ps {
			if depth[p] > 0 {
				mark(p, rows[i].y, arrow+1)
			}
			if after[p] > 0 {
				mark(p, arrow, next)
			}
		}
	}
	for p := range ps {
		for yy := top + boxH; yy < bottom; yy++ {
			r := '│'
			if active[p][yy] {
				r = '┃'
			}
			a.g.cells[yy][pos[p]] = tcell{r: r}
			a.kind[yy][pos[p]] = scLifeline
		}
	}
	// Participant groups: background frames around their members.
	for _, b := range d.Boxes {
		if len(b.Participants) == 0 {
			continue
		}
		x0, x1 := groupX(b)
		a.frameRow(x0, x1, top-1, '┌', '┐', '─', b.Title)
		a.frameRow(x0, x1, height-1, '└', '┘', '─', "")
		for yy := top; yy < height-1; yy++ {
			a.put(x0, yy, '│', scGroup)
			a.put(x1, yy, '│', scGroup)
		}
	}
	// Frames of blocks, then events over lifelines.
	frameX := func(b *seqBlock) (int, int) {
		f := frames[b.start]
		return f[0], f[1]
	}
	var stack []*seqBlock
	for i, e := range d.Events {
		r := rows[i]
		switch e.Kind {
		case mr.BlockStart:
			b := blockSpan[i]
			stack = append(stack, b)
			x0, x1 := frameX(b)
			label := blockWord(b.kind.String()) + e.Text
			a.frameRow(x0, x1, r.y, '┌', '┐', '─', label)
		case mr.BlockSection:
			if len(stack) > 0 {
				b := stack[len(stack)-1]
				x0, x1 := frameX(b)
				a.frameRow(x0, x1, r.y, '├', '┤', '┈', blockWord(sectionWord(b.kind))+e.Text)
			}
		case mr.BlockEnd:
			if len(stack) > 0 {
				b := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				x0, x1 := frameX(b)
				a.frameRow(x0, x1, r.y, '└', '┘', '─', "")
				// Sides between the top and this row.
				y0 := rows[b.start].y
				for yy := y0 + 1; yy < r.y; yy++ {
					if a.kind[yy][x0] == scFrame {
						continue // a section's row
					}
					a.put(x0, yy, '│', scFrame)
					a.put(x1, yy, '│', scFrame)
				}
			}
		}
	}
	for i, e := range d.Events {
		r := rows[i]
		switch e.Kind {
		case mr.Message:
			a.message(e, text(e), pos[idx[e.From]], pos[idx[e.To]], r.y, r.h, active)
		case mr.Note:
			a.note(e, text(e), pos, idx, r.y, r.h)
		}
	}
	// Participants, top and bottom; groups around them.
	for i, p := range ps {
		x0 := pos[i] - boxW[i]/2
		a.box(iRect{x0, top, x0 + boxW[i] - 1, top + boxH - 1}, p.Label)
		a.box(iRect{x0, bottom, x0 + boxW[i] - 1, bottom + boxH - 1}, p.Label)
		// The lifeline joins its boxes' borders.
		a.g.cells[top+boxH-1][pos[i]].r = '┬'
		a.g.cells[bottom][pos[i]].r = '┴'
	}
	if len(stack) > 0 {
		a.faultf("a block is not closed")
	}
	if len(a.fault) > 0 {
		err := &mr.Error{Kind: mr.LayoutFault, Msg: "text art: " + a.fault[0]}
		if debugArt {
			err.Msg = "text art: " + strings.Join(a.fault, "; ")
			return g, err
		}
		return nil, err
	}
	return g, nil
}

func startOf(evs []*mr.Event, e *mr.Event) int {
	// The block a start or a section belongs to: the innermost open one.
	var open []int
	for i, x := range evs {
		switch x.Kind {
		case mr.BlockStart:
			open = append(open, i)
		case mr.BlockEnd:
			if len(open) > 0 {
				open = open[:len(open)-1]
			}
		}
		if x == e && len(open) > 0 {
			return open[len(open)-1]
		}
	}
	return -1
}

func sectionWord(k mr.BlockKind) string {
	switch k {
	case mr.Par:
		return "and"
	case mr.Critical:
		return "option"
	}
	return "else"
}

// blockWord is the bracketed word a block's or a section's label starts
// with; measured, not counted, since the caller's widths decide its cells.
func blockWord(w string) string { return "[" + w + "] " }

// frameRow draws a frame's border row with an optional label after its
// corner.

func (a *seqArt) frameRow(x0, x1, y int, l, r, fill rune, label string) {
	if x1-x0 < 2 {
		a.faultf("a frame too narrow")
		return
	}
	a.put(x0, y, l, scFrame)
	a.put(x1, y, r, scFrame)
	for x := x0 + 1; x < x1; x++ {
		a.put(x, y, fill, scFrame)
	}
	if label != "" {
		if a.tm.cells(label)+2 > x1-x0-1 {
			a.faultf("a frame's label %q does not fit", label)
			return
		}
		// Written over the border it labels.
		x := x0 + 2
		for _, ch := range label {
			w := a.tm.width(ch)
			for k := 0; k < w; k++ {
				if a.g.in(x+k, y) {
					a.g.cells[y][x+k] = tcell{r: ch, cont: k > 0}
				}
			}
			x += w
		}
	}
}

// message draws an arrow on its last row, its text above, or a loop to
// the right for a message to self.
func (a *seqArt) message(e *mr.Event, txt string, xf, xt, y, h int, active [][]bool) {
	lines := strings.Split(txt, "\n")
	line := '─'
	if e.Dotted {
		line = '┈'
	}
	if xf == xt {
		// ├─┐ text
		// │◄┘
		ay := y
		start, end := line, line
		if e.BothEnds {
			start = seqHead(e.Head, -1)
		}
		if e.Head != mr.HeadNone {
			end = seqHead(e.Head, -1)
		}
		a.put(xf+1, ay, start, scArrow)
		a.put(xf+2, ay, '┐', scArrow)
		for k := ay + 1; k < y+h-1; k++ {
			a.put(xf+2, k, '│', scArrow)
		}
		a.put(xf+2, y+h-1, '┘', scArrow)
		a.put(xf+1, y+h-1, end, scArrow)
		for k, l := range lines {
			a.text(xf+tqLoopW+1, y+k, l, scText)
		}
		return
	}
	ay := y + h - 1
	dir := 1
	if xt < xf {
		dir = -1
	}
	for x := xf + dir; x != xt; x += dir {
		a.put(x, ay, line, scArrow)
	}
	if e.Head != mr.HeadNone {
		a.g.cells[ay][xt-dir] = tcell{r: seqHead(e.Head, dir)}
	}
	if e.BothEnds {
		a.g.cells[ay][xf+dir] = tcell{r: seqHead(e.Head, -dir)}
	}
	lo, hi := min(xf, xt), max(xf, xt)
	for k, l := range lines {
		w := a.tm.cells(l)
		a.text(lo+(hi-lo-w)/2+1, ay-len(lines)+k, l, scText)
	}
}

func seqHead(h mr.ArrowHead, dir int) rune {
	switch h {
	case mr.HeadCross:
		return '×'
	case mr.HeadOpen:
		if dir > 0 {
			return ')'
		}
		return '('
	}
	if dir > 0 {
		return '►'
	}
	return '◄'
}

func (a *seqArt) note(e *mr.Event, txt string, pos []int, idx map[*mr.Participant]int, y, h int) {
	w, _ := a.tm.size(txt)
	x0, nw := noteX(e, w, pos, idx)
	a.box(iRect{x0, y, x0 + nw - 1, y + h - 1}, txt)
}

// noteX is a note's first column and width.
func noteX(e *mr.Event, w int, pos []int, idx map[*mr.Participant]int) (int, int) {
	nw := w + 4
	i := idx[e.From]
	switch {
	case e.Place == mr.LeftOf:
		return pos[i] - tqNoteGap - nw, nw
	case e.Place == mr.RightOf:
		return pos[i] + tqNoteGap + 1, nw
	case e.To != nil && e.To != e.From:
		lo, hi := min(i, idx[e.To]), max(i, idx[e.To])
		return pos[lo] - 1, max(nw, pos[hi]-pos[lo]+3)
	}
	return pos[i] - nw/2, nw
}
