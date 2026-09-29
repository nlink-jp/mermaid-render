package raster

import (
	"fmt"
	"strings"

	mr "github.com/nlink-jp/mermaid-render"
)

// Owners of grid cells: textFree, then one range per kind.
const (
	ownBox   = 1 << 20 // + node index
	ownFrame = 2 << 20 // + subgraph index
	ownTitle = 3 << 20 // + subgraph index
	ownLabel = 4 << 20 // + link index
	ownLine  = 5 << 20 // + link index
)

// debugArt returns the drawing with its faults, and textProbe changes a
// layout before it is drawn and checked (tests only: a fault no real
// layout has cannot be reached otherwise).
var (
	debugArt  = false
	textProbe func(*textFlow)
)

// flowText lays out and draws a flowchart as text art.
func flowText(f *mr.Flowchart, tm *textMeasure) (*tgrid, error) {
	for _, n := range f.Nodes {
		if err := tm.check(n.Label, n.Line); err != nil {
			return nil, err
		}
	}
	for _, sg := range f.Subgraphs {
		if err := tm.check(sg.Title, sg.Line); err != nil {
			return nil, err
		}
	}
	for _, lk := range f.Links {
		if err := tm.check(lk.Label, lk.Line); err != nil {
			return nil, err
		}
	}
	// A box is its text, a space and a border each side, an even number of
	// columns wide (a whole number of units).
	// A node with a link to itself has two interior rows for the loop's
	// ends.
	loops := map[string]bool{}
	for _, lk := range f.Links {
		if lk.From == lk.To && !lk.From.Subgraph {
			loops[lk.From.ID] = true
		}
	}
	sizes := make([][2]float64, len(f.Nodes))
	for i, n := range f.Nodes {
		w, h := tm.size(n.Label)
		cols := w + 4
		cols += cols % 2
		rows := h + 2
		if loops[n.ID] {
			rows = max(rows, 4)
		}
		sizes[i] = [2]float64{float64(cols) / 2, float64(rows)}
	}
	measure := func(text string, bold bool) (float64, float64, error) {
		if text == "" {
			return 0, 0, nil
		}
		w, h := tm.size(text)
		return float64(w+w%2) / 2, float64(h), nil
	}
	lay, err := layoutGraph(f, measure, &layouter{sp: &gridSpacing, boxes: true, sizes: sizes,
		portGap: gridPortGap, rankGap: gridRankGap, endRoom: gridEndRoom, labelRoom: gridSpacing.trackIn})
	if err != nil {
		return nil, err
	}
	return drawFlowText(f, snapFlow(f, lay), tm)
}

// textFlow is a flowchart's layout on the grid.
type textFlow struct {
	boxes  []iRect // per node
	frames []iRect // per subgraph
	titles []iRect
	paths  [][][2]int // per link: its cells' corners, first and last beside their boxes
	labels []iRect    // per link (zero when none)
	w, h   int
}

func snapFlow(f *mr.Flowchart, lay *flowLayout) *textFlow {
	sx, sy := &snapper{scale: 2}, &snapper{scale: 1}
	addR := func(r rect) {
		sx.add(r.X0)
		sx.add(r.X1)
		sy.add(r.Y0)
		sy.add(r.Y1)
	}
	for _, n := range lay.Nodes {
		addR(n.Box)
	}
	for _, fr := range lay.Frames {
		addR(fr.Box)
		addR(fr.TitleBox)
	}
	for _, e := range lay.Edges {
		for _, p := range e.Points {
			sx.add(p.X)
			sy.add(p.Y)
		}
		if e.Label != "" {
			addR(e.LabelBox)
		}
	}
	sx.done()
	sy.done()
	// A box keeps its size: its first cell by identity, its extent the
	// continuous one rounded.
	cellR := func(r rect) iRect {
		x0, y0 := sx.at(r.X0), sy.at(r.Y0)
		return iRect{x0, y0, x0 + int(r.W()*2+0.5) - 1, y0 + int(r.H()+0.5) - 1}
	}
	tf := &textFlow{}
	for _, n := range lay.Nodes {
		tf.boxes = append(tf.boxes, cellR(n.Box))
	}
	for _, fr := range lay.Frames {
		tf.frames = append(tf.frames, cellR(fr.Box))
		tf.titles = append(tf.titles, cellR(fr.TitleBox))
	}
	nodeAt := map[string]int{}
	for i, n := range f.Nodes {
		nodeAt[n.ID] = i
	}
	frameAt := map[string]int{}
	for i, sg := range f.Subgraphs {
		frameAt[sg.ID] = i
	}
	boxOf := func(e mr.Endpoint) iRect {
		if e.Subgraph {
			return tf.frames[frameAt[e.ID]]
		}
		return tf.boxes[nodeAt[e.ID]]
	}
	boxRect := func(e mr.Endpoint) rect {
		if e.Subgraph {
			return lay.Frames[frameAt[e.ID]].Box
		}
		return lay.Nodes[nodeAt[e.ID]].Box
	}
	for i, e := range lay.Edges {
		lk := f.Links[i]
		raw := make([][2]int, len(e.Points))
		for k, p := range e.Points {
			raw[k] = [2]int{sx.at(p.X), sy.at(p.Y)}
		}
		// An end sits on a face of its box: it goes to the cell just outside
		// the snapped box, its first run kept at right angles to the face
		// and moved, if it must, onto the face's interior cells.
		end := func(k, next int, p pt, r rect, b iRect) {
			const e = 1e-6
			q := raw[k]
			switch {
			case abs(p.Y-r.Y0) < e:
				q[1] = b.y0 - 1
				q[0] = clamp(q[0], b.x0+1, b.x1-1)
			case abs(p.Y-r.Y1) < e:
				q[1] = b.y1 + 1
				q[0] = clamp(q[0], b.x0+1, b.x1-1)
			case abs(p.X-r.X0) < e:
				q[0] = b.x0 - 1
				q[1] = clamp(q[1], b.y0+1, b.y1-1)
			case abs(p.X-r.X1) < e:
				q[0] = b.x1 + 1
				q[1] = clamp(q[1], b.y0+1, b.y1-1)
			default:
				return
			}
			if next >= 0 && next < len(raw) && len(raw) > 2 {
				// The first run is perpendicular to the face: the next corner
				// shares the moved cross coordinate.
				if raw[next][0] == raw[k][0] {
					raw[next][0] = q[0]
				} else if raw[next][1] == raw[k][1] {
					raw[next][1] = q[1]
				}
			}
			raw[k] = q
		}
		if n := len(raw); n > 0 {
			end(0, 1, e.Points[0], boxRect(lk.From), boxOf(lk.From))
			end(n-1, n-2, e.Points[n-1], boxRect(lk.To), boxOf(lk.To))
		}
		// A straight link whose two ends' faces share no interior row (or
		// column) steps across halfway: two corners.
		if len(raw) == 2 && raw[0][0] != raw[1][0] && raw[0][1] != raw[1][1] {
			a, b := raw[0], raw[1]
			if abs(e.Points[0].Y-e.Points[1].Y) < 1e-6 { // runs across
				m := (a[0] + b[0]) / 2
				raw = [][2]int{a, {m, a[1]}, {m, b[1]}, b}
			} else {
				m := (a[1] + b[1]) / 2
				raw = [][2]int{a, {a[0], m}, {b[0], m}, b}
			}
		}
		var pts [][2]int
		for _, q := range raw {
			if len(pts) == 0 || pts[len(pts)-1] != q {
				pts = append(pts, q)
			}
		}
		tf.paths = append(tf.paths, pts)
		var lr iRect
		if e.Label != "" {
			lr = cellR(e.LabelBox)
		}
		tf.labels = append(tf.labels, lr)
	}
	grow := func(x, y int) {
		tf.w, tf.h = max(tf.w, x+1), max(tf.h, y+1)
	}
	for _, rs := range [][]iRect{tf.boxes, tf.frames, tf.titles, tf.labels} {
		for _, r := range rs {
			grow(r.x1, r.y1)
		}
	}
	for _, p := range tf.paths {
		for _, q := range p {
			grow(q[0], q[1])
		}
	}
	return tf
}

// strokeGlyph is a link's straight runs and corners, by stroke.
func strokeGlyph(s mr.Stroke, dirs uint8) rune {
	switch dirs {
	case dLeft | dRight:
		return [...]rune{'─', '━', '┄'}[s]
	case dUp | dDown:
		return [...]rune{'│', '┃', '┆'}[s]
	}
	thick := s == mr.Thick
	switch dirs {
	case dDown | dRight:
		return pick(thick, '┏', '┌')
	case dDown | dLeft:
		return pick(thick, '┓', '┐')
	case dUp | dRight:
		return pick(thick, '┗', '└')
	case dUp | dLeft:
		return pick(thick, '┛', '┘')
	}
	return '?'
}

func pick(c bool, a, b rune) rune {
	if c {
		return a
	}
	return b
}

// drawFlowText draws a flowchart laid out on the grid, checking it as it
// goes: a fault is a LayoutFault.
func drawFlowText(f *mr.Flowchart, tf *textFlow, tm *textMeasure) (*tgrid, error) {
	if textProbe != nil {
		textProbe(tf)
	}
	g, err := newGrid(tf.w, tf.h)
	if err != nil {
		return nil, err
	}
	var faults []string
	fault := func(format string, a ...any) { faults = append(faults, fmt.Sprintf(format, a...)) }
	// Frames first: nodes and lines go over them.
	for i, fr := range tf.frames {
		drawBorder(g, fr, [6]rune{'╔', '═', '╗', '║', '╚', '╝'}, ownFrame+i)
		for _, id := range f.Subgraphs[i].Nodes {
			for k, n := range f.Nodes {
				if n.ID == id {
					b := tf.boxes[k]
					if !(b.x0 > fr.x0 && b.x1 < fr.x1 && b.y0 > fr.y0 && b.y1 < fr.y1) {
						fault("subgraph %q does not hold node %q", f.Subgraphs[i].ID, id)
					}
				}
			}
		}
	}
	for i, n := range f.Nodes {
		b := tf.boxes[i]
		w, h := tm.size(n.Label)
		if b.x1-b.x0+1 < w+4 || b.y1-b.y0+1 < h+2 {
			fault("node %q: its box is smaller than its text", n.ID)
		}
		for j := 0; j < i; j++ {
			if b.overlaps(tf.boxes[j]) {
				fault("nodes %q and %q overlap", f.Nodes[j].ID, n.ID)
			}
		}
		drawBorder(g, b, cornersOf(n.Shape), ownBox+i)
		for y := b.y0 + 1; y < b.y1; y++ {
			for x := b.x0 + 1; x < b.x1; x++ {
				g.cells[y][x] = tcell{own: ownBox + i}
			}
		}
		centreText(g, iRect{b.x0 + 1, b.y0 + 1, b.x1 - 1, b.y1 - 1}, n.Label, ownBox+i, tm)
	}
	// Lines: each link's cells and the directions it leaves them in.
	type use struct {
		link int
		dirs uint8
	}
	uses := map[[2]int][]use{}
	for i, pts := range tf.paths {
		lk := f.Links[i]
		if len(pts) == 0 {
			fault("link %d has no path", i)
			continue
		}
		cells, dirs, ok := walk(pts)
		if !ok {
			fault("link %s-%s leaves the grid's lines (not a right angle)", lk.From.ID, lk.To.ID)
			continue
		}
		// The ends point into their boxes.
		intoBox := func(c [2]int, b iRect) (uint8, bool) {
			switch {
			case c[1] == b.y0-1 && c[0] > b.x0 && c[0] < b.x1:
				return dDown, true
			case c[1] == b.y1+1 && c[0] > b.x0 && c[0] < b.x1:
				return dUp, true
			case c[0] == b.x0-1 && c[1] > b.y0 && c[1] < b.y1:
				return dRight, true
			case c[0] == b.x1+1 && c[1] > b.y0 && c[1] < b.y1:
				return dLeft, true
			}
			return 0, false
		}
		fromBox, toBox := endBox(f, tf, lk.From), endBox(f, tf, lk.To)
		d0, ok0 := intoBox(cells[0], fromBox)
		d1, ok1 := intoBox(cells[len(cells)-1], toBox)
		if !ok0 || !ok1 {
			fault("link %s-%s does not end beside the interior of its boxes", lk.From.ID, lk.To.ID)
			continue
		}
		dirs[0] |= d0
		dirs[len(dirs)-1] |= d1
		for k, c := range cells {
			uses[c] = append(uses[c], use{i, dirs[k]})
		}
	}
	for c, us := range uses {
		x, y := c[0], c[1]
		if !g.in(x, y) {
			fault("a line leaves the grid")
			continue
		}
		own := g.cells[y][x].own
		switch {
		case own >= ownBox && own < ownFrame:
			fault("link %d runs through node %q", us[0].link, f.Nodes[own-ownBox].ID)
			continue
		}
		switch len(us) {
		case 1:
			u := us[0]
			if u.dirs == dUp|dDown || u.dirs == dLeft|dRight || isCorner(u.dirs) {
				g.cells[y][x] = tcell{r: strokeGlyph(f.Links[u.link].Stroke, u.dirs), own: ownLine + u.link, dirs: u.dirs}
			} else {
				fault("link %d turns back on itself", u.link)
			}
		case 2:
			a, b := us[0], us[1]
			if a.dirs|b.dirs == dUp|dDown|dLeft|dRight && (a.dirs == dUp|dDown || a.dirs == dLeft|dRight) && (b.dirs == dUp|dDown || b.dirs == dLeft|dRight) {
				g.cells[y][x] = tcell{r: crossGlyph(f, a.link, a.dirs, b.link), own: ownLine + a.link, dirs: a.dirs | b.dirs}
			} else {
				fault("links %d and %d meet other than crossing at a right angle", a.link, b.link)
			}
		default:
			fault("%d links meet in one cell", len(us))
		}
	}
	// Titles: in the frame's first rows, slid along them clear of the
	// links that cross (the picture draws its titles over links; text
	// cannot).
	for i, fr := range tf.frames {
		title := f.Subgraphs[i].Title
		if title == "" {
			continue
		}
		t := tf.titles[i]
		w, h := tm.size(title)
		if !(t.y0 > fr.y0 && t.y0+h-1 < fr.y1) {
			fault("subgraph %q: its title's rows are not inside its frame", f.Subgraphs[i].ID)
			continue
		}
		// Rows above the members (the title's own and the padding below
		// it), else below them: a title the links crossing the top leave no
		// room for stands at the bottom.
		top, bottom := fr.y1-h, fr.y0+1
		for _, id := range f.Subgraphs[i].Nodes {
			for k, n := range f.Nodes {
				if n.ID == id {
					top = min(top, tf.boxes[k].y0-h)
					bottom = max(bottom, tf.boxes[k].y1+1)
				}
			}
		}
		var rows []int
		for y := t.y0; y <= top; y++ {
			rows = append(rows, y)
		}
		for y := bottom; y <= fr.y1-h; y++ {
			rows = append(rows, y)
		}
		free := func(x0, y0 int) bool {
			for y := y0; y < y0+h; y++ {
				for x := x0 - 1; x <= x0+w; x++ {
					if c := g.cells[y][x]; c.own != textFree && x > fr.x0 && x < fr.x1 {
						return false
					}
				}
			}
			return true
		}
		want := t.x0 + (t.x1-t.x0+1-w)/2
		bx, by := -1, -1
		for _, y0 := range rows {
			for d := 0; bx < 0 && d <= fr.x1-fr.x0; d++ {
				for _, x0 := range []int{want - d, want + d} {
					if bx < 0 && x0 > fr.x0 && x0+w-1 < fr.x1 && free(x0, y0) {
						bx, by = x0, y0
					}
				}
			}
		}
		if bx < 0 {
			fault("subgraph %q: no room for its title clear of links", f.Subgraphs[i].ID)
			continue
		}
		for k, l := range strings.Split(title, "\n") {
			x := bx + (w-tm.cells(l))/2
			g.text(x, by+k, l, ownTitle+i, tm)
		}
	}
	// Heads: the end cell beside its box shows the head.
	for i, pts := range tf.paths {
		lk := f.Links[i]
		if len(pts) == 0 {
			continue
		}
		cells, _, ok := walk(pts)
		if !ok {
			continue
		}
		put := func(c [2]int, h mr.Head, b iRect) {
			if h == mr.NoHead || !g.in(c[0], c[1]) {
				return
			}
			if d := g.cells[c[1]][c[0]].dirs; d != dUp|dDown && d != dLeft|dRight {
				fault("link %s-%s: a head does not end a straight run", lk.From.ID, lk.To.ID)
			}
			var r rune
			switch {
			case h == mr.CircleHead:
				r = '○'
			case h == mr.CrossHead:
				r = '×'
			case c[1] == b.y0-1:
				r = '▼'
			case c[1] == b.y1+1:
				r = '▲'
			case c[0] == b.x0-1:
				r = '►'
			default:
				r = '◄'
			}
			g.cells[c[1]][c[0]].r = r
		}
		put(cells[0], lk.Start, endBox(f, tf, lk.From))
		put(cells[len(cells)-1], lk.End, endBox(f, tf, lk.To))
	}
	// Labels go over their own link's line, which breaks for them.
	for i, lk := range f.Links {
		if lk.Label == "" {
			continue
		}
		r := tf.labels[i]
		w, h := tm.size(lk.Label)
		if r.x1-r.x0+1 < w || r.y1-r.y0+1 < h {
			fault("link %d: its label's room is smaller than its text", i)
		}
		for y := r.y0; y <= r.y1; y++ {
			for x := r.x0; x <= r.x1; x++ {
				if !g.in(x, y) {
					fault("link %d: its label leaves the grid", i)
					continue
				}
				own := g.cells[y][x].own
				if own != textFree && own != ownLine+i {
					fault("link %d: its label lies over something else", i)
				}
			}
		}
		lines := strings.Split(lk.Label, "\n")
		top := r.y0 + (r.y1-r.y0+1-len(lines))/2
		for k, l := range lines {
			lw := tm.cells(l)
			x0 := r.x0 + (r.x1-r.x0+1-lw)/2
			// The line breaks under the text only.
			g.text(x0, top+k, l, ownLabel+i, tm)
		}
	}
	if len(faults) > 0 {
		if debugArt {
			return g, &mr.Error{Kind: mr.LayoutFault, Msg: "text art: " + strings.Join(faults, "; ")}
		}
		return nil, &mr.Error{Kind: mr.LayoutFault, Msg: "text art: " + faults[0]}
	}
	return g, nil
}

func endBox(f *mr.Flowchart, tf *textFlow, e mr.Endpoint) iRect {
	if e.Subgraph {
		for i, sg := range f.Subgraphs {
			if sg.ID == e.ID {
				return tf.frames[i]
			}
		}
	}
	for i, n := range f.Nodes {
		if n.ID == e.ID {
			return tf.boxes[i]
		}
	}
	return iRect{}
}

func isCorner(d uint8) bool {
	switch d {
	case dDown | dRight, dDown | dLeft, dUp | dRight, dUp | dLeft:
		return true
	}
	return false
}

// crossGlyph is two straight runs crossing: thick where a run is thick.
func crossGlyph(f *mr.Flowchart, a int, adirs uint8, b int) rune {
	h, v := a, b
	if adirs == dUp|dDown {
		h, v = b, a
	}
	th, tv := f.Links[h].Stroke == mr.Thick, f.Links[v].Stroke == mr.Thick
	switch {
	case th && tv:
		return '╋'
	case th:
		return '┿'
	case tv:
		return '╂'
	}
	return '┼'
}

// walk lists the cells of a path of right-angled runs and the directions
// each cell's line leaves it in; ok is false when two corners are not on
// one row or column.
func walk(pts [][2]int) (cells [][2]int, dirs []uint8, ok bool) {
	cells = [][2]int{pts[0]}
	dirs = []uint8{0}
	for k := 1; k < len(pts); k++ {
		a, b := pts[k-1], pts[k]
		if a[0] != b[0] && a[1] != b[1] {
			return nil, nil, false
		}
		dx, dy := sign(b[0]-a[0]), sign(b[1]-a[1])
		fwd, back := dirOf(dx, dy), dirOf(-dx, -dy)
		for c := a; c != b; {
			dirs[len(dirs)-1] |= fwd
			c = [2]int{c[0] + dx, c[1] + dy}
			cells = append(cells, c)
			dirs = append(dirs, back)
		}
	}
	return cells, dirs, true
}

func sign(v int) int {
	switch {
	case v > 0:
		return 1
	case v < 0:
		return -1
	}
	return 0
}

func dirOf(dx, dy int) uint8 {
	switch {
	case dx > 0:
		return dRight
	case dx < 0:
		return dLeft
	case dy > 0:
		return dDown
	}
	return dUp
}

// cornersOf is a node's corners by its shape's family.
func cornersOf(s mr.Shape) [6]rune {
	switch s {
	case mr.Round, mr.Stadium, mr.Circle, mr.DoubleCircle:
		return [6]rune{'╭', '─', '╮', '│', '╰', '╯'}
	case mr.Rhombus, mr.Hexagon:
		return [6]rune{'◇', '─', '◇', '│', '◇', '◇'}
	}
	return [6]rune{'┌', '─', '┐', '│', '└', '┘'}
}

// drawBorder draws a rectangle's border: corners and sides.
func drawBorder(g *tgrid, r iRect, c [6]rune, own int) {
	set := func(x, y int, ch rune) {
		if g.in(x, y) {
			g.cells[y][x] = tcell{r: ch, own: own}
		}
	}
	for x := r.x0 + 1; x < r.x1; x++ {
		set(x, r.y0, c[1])
		set(x, r.y1, c[1])
	}
	for y := r.y0 + 1; y < r.y1; y++ {
		set(r.x0, y, c[3])
		set(r.x1, y, c[3])
	}
	set(r.x0, r.y0, c[0])
	set(r.x1, r.y0, c[2])
	set(r.x0, r.y1, c[4])
	set(r.x1, r.y1, c[5])
}

// centreText writes a (multi-line) text centred in r, the odd cell left
// over on the right.
func centreText(g *tgrid, r iRect, s string, own int, tm *textMeasure) {
	lines := strings.Split(s, "\n")
	top := r.y0 + (r.y1-r.y0+1-len(lines))/2
	for k, l := range lines {
		x := r.x0 + (r.x1-r.x0+1-tm.cells(l))/2
		g.text(x, top+k, l, own, tm)
	}
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func clamp(v, lo, hi int) int {
	return max(lo, min(v, hi))
}
