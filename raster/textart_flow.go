package raster

import (
	"fmt"
	"sort"
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
	return drawFlowText(f, flowGrid(f, lay, tm), tm)
}

// flowGrid is a flowchart's layout on the grid: snapped, labelled links
// straightened.
func flowGrid(f *mr.Flowchart, lay *flowLayout, tm *textMeasure) *textFlow {
	tf := snapFlow(f, lay)
	// The passes only grow the grid: art already past the bound is refused
	// by the drawing without them (MaxTextCells bounds time too).
	if tf.w*tf.h > MaxTextCells {
		return tf
	}
	alignLeaves(f, tf, 2)
	straighten(f, tf, tm)
	labelsOnLines(f, tf, 2)
	titleRoom(f, tf, tm)
	return tf
}

// textFlow is a flowchart's layout on the grid.
type textFlow struct {
	boxes  []iRect // per node
	frames []iRect // per subgraph
	titles []iRect
	paths  [][][2]int // per link: its cells' corners, first and last beside their boxes
	labels []iRect    // per link (zero when none)
	w, h   int
	// An ER diagram's: each node's table, and each link's cardinality
	// marks at its From and its To in place of heads.
	tables []*textTable
	marks  [][2][2]rune
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
	// Everything placed lies on the grid: a pass that moved or widened
	// something past it must be refused, not clipped (or index past it).
	inside := func(r iRect) bool {
		return r.x0 >= 0 && r.y0 >= 0 && r.x1 < tf.w && r.y1 < tf.h && r.x0 <= r.x1 && r.y0 <= r.y1
	}
	for i, b := range tf.boxes {
		if !inside(b) {
			return nil, &mr.Error{Kind: mr.LayoutFault, Msg: fmt.Sprintf("text art: node %q leaves the grid", f.Nodes[i].ID)}
		}
	}
	for i, fr := range tf.frames {
		if !inside(fr) {
			return nil, &mr.Error{Kind: mr.LayoutFault, Msg: fmt.Sprintf("text art: subgraph %q leaves the grid", f.Subgraphs[i].ID)}
		}
		// Flowcharts nest no subgraphs: frames stand apart, a cell at least
		// between their borders, or two would read as one.
		for j := 0; j < i; j++ {
			o := tf.frames[j]
			if fr.overlaps(iRect{o.x0 - 1, o.y0 - 1, o.x1 + 1, o.y1 + 1}) {
				fault("subgraphs %q and %q touch", f.Subgraphs[j].ID, f.Subgraphs[i].ID)
			}
		}
	}
	// Frames first: nodes and lines go over them.
	for i, fr := range tf.frames {
		drawBorder(g, fr, [6]rune{'╔', '═', '╗', '║', '╚', '╝'}, ownFrame+i)
		member := map[string]bool{}
		for _, id := range f.Subgraphs[i].Nodes {
			member[id] = true
		}
		for k, n := range f.Nodes {
			b := tf.boxes[k]
			switch {
			case member[n.ID] && !(b.x0 > fr.x0 && b.x1 < fr.x1 && b.y0 > fr.y0 && b.y1 < fr.y1):
				fault("subgraph %q does not hold node %q", f.Subgraphs[i].ID, n.ID)
			case !member[n.ID] && b.overlaps(fr):
				// A frame holds its members and nothing else: a node inside
				// one reads as a member (flowcharts nest no subgraphs).
				fault("node %q stands in subgraph %q, which it is not in", n.ID, f.Subgraphs[i].ID)
			}
		}
	}
	for i, n := range f.Nodes {
		b := tf.boxes[i]
		w, h := tm.size(n.Label)
		w, h = w+4, h+2
		if tf.tables != nil {
			w, h = tf.tables[i].w, tf.tables[i].h
		}
		if b.x1-b.x0+1 < w || b.y1-b.y0+1 < h {
			fault("node %q: its box is smaller than its text", n.ID)
		}
		for j := 0; j < i; j++ {
			if b.overlaps(tf.boxes[j]) {
				fault("nodes %q and %q overlap", f.Nodes[j].ID, n.ID)
			}
		}
		if tf.tables != nil {
			drawTable(g, b, tf.tables[i], ownBox+i, tm)
			continue
		}
		drawBorder(g, b, cornersOf(n.Shape), ownBox+i)
		fillOwn(g, b, ownBox+i)
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
	// In reading order, so the first fault named is the same every run.
	for _, c := range readingOrder(uses) {
		us := uses[c]
		x, y := c[0], c[1]
		if !g.in(x, y) {
			fault("a line leaves the grid")
			continue
		}
		own := g.cells[y][x].own
		overFrame := own >= ownFrame && own < ownTitle
		switch {
		case own >= ownBox && own < ownFrame:
			fault("link %d runs through node %q", us[0].link, f.Nodes[own-ownBox].ID)
			continue
		}
		// On a frame's border a line only crosses it: straight across a
		// side, never along it, never turning on it — a line that runs
		// along or turns on a border reads as ending at the frame.
		if overFrame {
			if len(us) != 1 || !crossesBorder(tf.frames[own-ownFrame], x, y, us[0].dirs) {
				fault("link %d runs along or turns on the border of subgraph %q", us[0].link, f.Subgraphs[own-ownFrame].ID)
				continue
			}
		}
		switch len(us) {
		case 1:
			u := us[0]
			if u.dirs == dUp|dDown || u.dirs == dLeft|dRight || isCorner(u.dirs) {
				r := strokeGlyph(f.Links[u.link].Stroke, u.dirs)
				// A line across a frame's double border crosses it.
				if overFrame && u.dirs == dUp|dDown {
					r = '╪'
				} else if overFrame && u.dirs == dLeft|dRight {
					r = '╫'
				}
				g.cells[y][x] = tcell{r: r, own: ownLine + u.link, dirs: u.dirs}
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
	// Where a line meets a box without a head, its border joins it: the
	// border's stroke runs along the cell's middle, a line into it would
	// stop half a cell short.
	for i, pts := range tf.paths {
		lk := f.Links[i]
		if len(pts) == 0 || tf.marks != nil {
			continue
		}
		cells, _, ok := walk(pts)
		if !ok {
			continue
		}
		join := func(c [2]int, h mr.Head, b iRect) {
			if h != mr.NoHead {
				return
			}
			var x, y int
			var r rune
			switch {
			case c[1] == b.y0-1:
				x, y, r = c[0], b.y0, '┴'
			case c[1] == b.y1+1:
				x, y, r = c[0], b.y1, '┬'
			case c[0] == b.x0-1:
				x, y, r = b.x0, c[1], '┤'
			case c[0] == b.x1+1:
				x, y, r = b.x1, c[1], '├'
			default:
				return
			}
			if g.in(x, y) && (g.cells[y][x].r == '─' || g.cells[y][x].r == '│' || g.cells[y][x].r == '═' || g.cells[y][x].r == '║') {
				if g.cells[y][x].r == '═' || g.cells[y][x].r == '║' {
					r = map[rune]rune{'┴': '╧', '┬': '╤', '┤': '╢', '├': '╟'}[r]
				}
				g.cells[y][x].r = r
			}
		}
		join(cells[0], lk.Start, endBox(f, tf, lk.From))
		join(cells[len(cells)-1], lk.End, endBox(f, tf, lk.To))
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
		rows := titleRows(f, tf, i, h)
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
		if tf.marks != nil {
			// Cardinality in the two cells next to each table, on a run
			// across into its left or right face.
			mark := func(c, before [2]int, m [2]rune, b iRect) {
				if !g.in(c[0], c[1]) || !g.in(before[0], before[1]) || c[1] != before[1] ||
					g.cells[c[1]][c[0]].dirs != dLeft|dRight || g.cells[before[1]][before[0]].dirs != dLeft|dRight {
					fault("link %s-%s: a cardinality mark does not stand on a run across into its table", lk.From.ID, lk.To.ID)
					return
				}
				if c[0] == b.x0-1 { // the table on the right
					g.cells[c[1]][before[0]].r, g.cells[c[1]][c[0]].r = m[0], m[1]
				} else {
					mm := mirrored(m)
					g.cells[c[1]][c[0]].r, g.cells[c[1]][before[0]].r = mm[0], mm[1]
				}
			}
			if len(cells) < 4 {
				fault("link %s-%s: no room for its cardinality marks", lk.From.ID, lk.To.ID)
				continue
			}
			n := len(cells)
			mark(cells[0], cells[1], tf.marks[i][0], endBox(f, tf, lk.From))
			mark(cells[n-1], cells[n-2], tf.marks[i][1], endBox(f, tf, lk.To))
			// A cell of line beyond each mark, clear of the label.
			if lk.Label != "" {
				for _, c := range [][2]int{cells[2], cells[n-3]} {
					if tf.labels[i].has(c[0], c[1]) {
						fault("link %s-%s: its label touches a cardinality mark", lk.From.ID, lk.To.ID)
					}
				}
			}
			continue
		}
		put(cells[0], lk.Start, endBox(f, tf, lk.From))
		put(cells[len(cells)-1], lk.End, endBox(f, tf, lk.To))
	}
	// Labels go over their own link's line, which breaks for them.
	lineIdx := indexLines(tf)
	for i, lk := range f.Links {
		if lk.Label == "" {
			continue
		}
		r := tf.labels[i]
		w, h := tm.size(lk.Label)
		if r.x1-r.x0+1 < w || r.y1-r.y0+1 < h {
			fault("link %d: its label's room is smaller than its text", i)
		}
		// On its own line, or — a loop's — beside it: a label anywhere else
		// names whatever line it happens to stand by.
		if !labelByItsLine(tf, lineIdx, i, r, lk.From == lk.To) {
			fault("link %d: its label is not on its line", i)
		}
		for y := r.y0; y <= r.y1; y++ {
			for x := r.x0; x <= r.x1; x++ {
				if !g.in(x, y) {
					fault("link %d: its label leaves the grid", i)
					continue
				}
				c := g.cells[y][x]
				if c.own != textFree && c.own != ownLine+i {
					fault("link %d: its label lies over something else", i)
				}
				if c.own == ownLine+i && c.r != strokeGlyph(lk.Stroke, c.dirs) {
					fault("link %d: its label lies over its head or mark", i)
				} else if c.own == ownLine+i && isCorner(c.dirs) {
					fault("link %d: its label hides a turn of its line", i)
				}
			}
		}
		// Centred in its room, or a cell right when that puts the text on
		// its line: a room is whole units wide, so a one-cell label on a
		// line down would stand beside it (Y│), not break it.
		lines := strings.Split(lk.Label, "\n")
		top := r.y0 + (r.y1-r.y0+1-len(lines))/2
		covers := func(shift int) bool {
			for k, l := range lines {
				x0 := r.x0 + (r.x1-r.x0+1-tm.cells(l))/2 + shift
				for x := x0; x < x0+tm.cells(l); x++ {
					if g.in(x, top+k) && g.cells[top+k][x].own == ownLine+i {
						return true
					}
				}
			}
			return false
		}
		shift := 0
		if !covers(0) && covers(1) {
			shift = 1
		}
		if lk.From != lk.To && !covers(shift) {
			fault("link %d: its label's text is not on its line", i)
		}
		for k, l := range lines {
			lw := tm.cells(l)
			x0 := r.x0 + (r.x1-r.x0+1-lw)/2 + shift
			if x0+lw-1 > r.x1 {
				fault("link %d: its label's text leaves its room", i)
				continue
			}
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

// readingOrder is a cell map's keys, row by row, left to right.
func readingOrder[V any](m map[[2]int]V) [][2]int {
	cells := make([][2]int, 0, len(m))
	for c := range m {
		cells = append(cells, c)
	}
	sort.Slice(cells, func(a, b int) bool {
		if cells[a][1] != cells[b][1] {
			return cells[a][1] < cells[b][1]
		}
		return cells[a][0] < cells[b][0]
	})
	return cells
}

// crossesBorder reports whether a line with dirs at (x, y), a cell of
// fr's border, crosses it straight: up and down through its top or bottom,
// across through a side, never at a corner.
func crossesBorder(fr iRect, x, y int, dirs uint8) bool {
	switch {
	case (y == fr.y0 || y == fr.y1) && x > fr.x0 && x < fr.x1:
		return dirs == dUp|dDown
	case (x == fr.x0 || x == fr.x1) && y > fr.y0 && y < fr.y1:
		return dirs == dLeft|dRight
	}
	return false
}

// labelByItsLine reports whether a label in r for link i covers a cell of
// its line or, for a loop, stands within two steps of it — nearer it than
// any other line, and touching no other label, or it would read as theirs.
func labelByItsLine(tf *textFlow, idx map[[2]int][]lineUse, i int, r iRect, loop bool) bool {
	cells, _, ok := walk(tf.paths[i])
	if !ok {
		return true // the line's own checks name it
	}
	// Steps across plus steps down: a line diagonally off the label's
	// corner is further than one level with it.
	dist := func(x, y int) int { return max(r.x0-x, x-r.x1, 0) + max(r.y0-y, y-r.y1, 0) }
	own := 1 << 30
	for _, c := range cells {
		own = min(own, dist(c[0], c[1]))
	}
	if !loop {
		return own == 0
	}
	if own > 2 {
		return false
	}
	for y := r.y0 - own; y <= r.y1+own; y++ {
		for x := r.x0 - own; x <= r.x1+own; x++ {
			if dist(x, y) <= own && others(idx, [2]int{x, y}, i) {
				return false
			}
		}
	}
	for j, o := range tf.labels {
		if j != i && o != (iRect{}) && r.overlaps(iRect{o.x0 - 1, o.y0 - 1, o.x1 + 1, o.y1 + 1}) {
			return false
		}
	}
	return true
}

// titleRows are the rows a subgraph's title of h rows may start on:
// above its members (the title's own row and the padding below it), else
// below them — a title the links crossing the top leave no room for
// stands at the bottom.
func titleRows(f *mr.Flowchart, tf *textFlow, i, h int) []int {
	fr := tf.frames[i]
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
	for y := tf.titles[i].y0; y <= top; y++ {
		rows = append(rows, y)
	}
	for y := bottom; y <= fr.y1-h; y++ {
		rows = append(rows, y)
	}
	return rows
}

// titleRoom widens a frame whose title the links crossing its rows leave
// no room for (the picture draws a title over its links; text cannot):
// by the fewest columns, into columns beside it that nothing else uses,
// right before left. A title it cannot help is left to the drawing,
// which refuses it.
func titleRoom(f *mr.Flowchart, tf *textFlow, tm *textMeasure) {
	base := tf.occupied() // boxes, lines and labels: no pass here moves them
	for i := range tf.frames {
		title := f.Subgraphs[i].Title
		if title == "" {
			continue
		}
		w, h := tm.size(title)
		// taken is a cell something other than frame i holds: seen from
		// frame i, a frame around it holds only its border.
		taken := func(c [2]int) bool {
			if base[c] {
				return true
			}
			fr := tf.frames[i]
			for j, o := range tf.frames {
				switch {
				case j == i || !o.has(c[0], c[1]):
				case o.x0 < fr.x0 && o.x1 > fr.x1 && o.y0 < fr.y0 && o.y1 > fr.y1:
					if c[0] == o.x0 || c[0] == o.x1 || c[1] == o.y0 || c[1] == o.y1 {
						return true
					}
				default:
					return true
				}
			}
			return false
		}
		// fits reports a place for the title in fr, pad cells from its
		// sides: the drawing's own rule lets a title touch them, a widened
		// frame leaves it a cell each side.
		fits := func(fr iRect, pad int) bool {
			for _, y0 := range titleRows(f, tf, i, h) {
				for x0 := fr.x0 + 1 + pad; x0+w-1 < fr.x1-pad; x0++ {
					ok := true
					for y := y0; ok && y < y0+h; y++ {
						for x := max(x0-1, fr.x0+1); ok && x <= min(x0+w, fr.x1-1); x++ {
							ok = !taken([2]int{x, y})
						}
					}
					if ok {
						return true
					}
				}
			}
			return false
		}
		fr := tf.frames[i]
		if fits(fr, 0) {
			continue
		}
		// clear reports whether columns x0..x1 over the frame's rows are
		// on the grid and hold nothing — the border of a frame around this
		// one included.
		clear := func(x0, x1 int) bool {
			if x0 < 0 {
				return false
			}
			for y := fr.y0; y <= fr.y1; y++ {
				for x := x0; x <= x1; x++ {
					if taken([2]int{x, y}) {
						return false
					}
				}
			}
			return true
		}
		for k := 1; k <= w+3; k++ {
			if wide := (iRect{fr.x0, fr.y0, fr.x1 + k, fr.y1}); clear(fr.x1+1, fr.x1+k+1) && fits(wide, 1) {
				tf.frames[i], tf.w = wide, max(tf.w, wide.x1+2)
				break
			}
			if wide := (iRect{fr.x0 - k, fr.y0, fr.x1, fr.y1}); clear(fr.x0-k-1, fr.x0-1) && fits(wide, 1) {
				tf.frames[i] = wide
				break
			}
		}
	}
}

// occupied is every cell a box, a line or a label takes.
func (tf *textFlow) occupied() map[[2]int]bool {
	taken := map[[2]int]bool{}
	fill := func(r iRect) {
		for y := r.y0; y <= r.y1; y++ {
			for x := r.x0; x <= r.x1; x++ {
				taken[[2]int{x, y}] = true
			}
		}
	}
	for _, b := range tf.boxes {
		fill(b)
	}
	for _, l := range tf.labels {
		if l != (iRect{}) {
			fill(l)
		}
	}
	for _, pts := range tf.paths {
		if cells, _, ok := walk(pts); ok {
			for _, c := range cells {
				taken[c] = true
			}
		}
	}
	return taken
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
// each cell's line leaves it in; ok is false for no path, or when two
// corners are not on one row or column.
func walk(pts [][2]int) (cells [][2]int, dirs []uint8, ok bool) {
	if len(pts) == 0 {
		return nil, nil, false
	}
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

// straighten re-routes a link that bends four times or more around its
// label (the label's layer sits between its ends, so the link steps to
// the label's column and on to its target's) onto two bends: down its
// source's column and across once, or across once and down its target's —
// the label moved onto the long straight run — when the route is clear of
// every box, title, label and link but for right-angled crossings (the
// operator's check of the text art, round 1).
func straighten(f *mr.Flowchart, tf *textFlow, tm *textMeasure) {
	type use struct {
		link int
		dirs uint8
	}
	occ := map[[2]int][]use{}
	add := func(i int, pts [][2]int, sign int) {
		cells, dirs, ok := walk(pts)
		if !ok {
			return
		}
		for k, c := range cells {
			if sign > 0 {
				occ[c] = append(occ[c], use{i, dirs[k]})
				continue
			}
			us := occ[c][:0]
			for _, u := range occ[c] {
				if u.link != i {
					us = append(us, u)
				}
			}
			occ[c] = us
		}
	}
	for i, p := range tf.paths {
		if len(p) > 0 {
			add(i, p, 1)
		}
	}
	solid := func(x, y, link int) bool {
		for _, b := range tf.boxes {
			if b.has(x, y) {
				return true
			}
		}
		for _, t := range tf.titles {
			if t.has(x, y) {
				return true
			}
		}
		for j, r := range tf.labels {
			if j != link && f.Links[j].Label != "" && r.has(x, y) {
				return true
			}
		}
		return false
	}
	for i, pts := range tf.paths {
		lk := f.Links[i]
		if lk.Label == "" || lk.From == lk.To || len(pts) < 6 {
			continue
		}
		p0, pn := pts[0], pts[len(pts)-1]
		vertical := p0[0] == pts[1][0] && pn[0] == pts[len(pts)-2][0]
		horizontal := p0[1] == pts[1][1] && pn[1] == pts[len(pts)-2][1]
		if !vertical && !horizontal {
			continue
		}
		lr := tf.labels[i]
		lw, lh := lr.x1-lr.x0+1, lr.y1-lr.y0+1
		var cands [][][2]int
		var labs []iRect
		if vertical {
			first, last := pts[1][1], pts[len(pts)-2][1]
			// Across at the first corner's row, then down the target's column.
			cands = append(cands, [][2]int{p0, {p0[0], first}, {pn[0], first}, pn})
			labs = append(labs, iRect{pn[0] - lw/2, lr.y0, pn[0] - lw/2 + lw - 1, lr.y1})
			// Down the source's column to the last corner's row, then across.
			cands = append(cands, [][2]int{p0, {p0[0], last}, {pn[0], last}, pn})
			labs = append(labs, iRect{p0[0] - lw/2, lr.y0, p0[0] - lw/2 + lw - 1, lr.y1})
		} else {
			first, last := pts[1][0], pts[len(pts)-2][0]
			cands = append(cands, [][2]int{p0, {first, p0[1]}, {first, pn[1]}, pn})
			labs = append(labs, iRect{lr.x0, pn[1] - lh/2, lr.x1, pn[1] - lh/2 + lh - 1})
			cands = append(cands, [][2]int{p0, {last, p0[1]}, {last, pn[1]}, pn})
			labs = append(labs, iRect{lr.x0, p0[1] - lh/2, lr.x1, p0[1] - lh/2 + lh - 1})
		}
		add(i, pts, -1)
		chosen := false
		for k, c := range cands {
			cells, dirs, ok := walk(dedupePts(c))
			if !ok || len(cells) < 3 {
				continue
			}
			// A head ends a straight run: the cell before it is no corner.
			if (lk.End != mr.NoHead && isCorner(dirs[len(dirs)-2])) || (lk.Start != mr.NoHead && isCorner(dirs[1])) {
				continue
			}
			clear := true
			for n, cl := range cells {
				if solid(cl[0], cl[1], i) {
					clear = false
					break
				}
				for _, u := range occ[cl] {
					straight := func(d uint8) bool { return d == dUp|dDown || d == dLeft|dRight }
					if !(straight(u.dirs) && straight(dirs[n]) && u.dirs != dirs[n]) {
						clear = false
					}
				}
			}
			// The label on its new run: over this link's own cells only, and
			// the run it sits on straight through it.
			own := map[[2]int]bool{}
			for _, cl := range cells {
				own[cl] = true
			}
			l := labs[k]
			for y := l.y0; clear && y <= l.y1; y++ {
				for x := l.x0; x <= l.x1; x++ {
					if solid(x, y, i) || len(occ[[2]int{x, y}]) > 0 {
						clear = false
						break
					}
				}
			}
			if vertical && clear {
				// The label's rows lie on the new vertical run.
				col := c[3][0]
				if k == 1 {
					col = c[0][0]
				}
				clear = own[[2]int{col, l.y0}] && own[[2]int{col, l.y1}]
			}
			if !vertical && clear {
				row := c[3][1]
				if k == 1 {
					row = c[0][1]
				}
				clear = own[[2]int{l.x0, row}] && own[[2]int{l.x1, row}]
			}
			if clear {
				tf.paths[i], tf.labels[i] = dedupePts(c), l
				chosen = true
				break
			}
		}
		if chosen {
			add(i, tf.paths[i], 1)
		} else {
			add(i, pts, 1)
		}
	}
	_ = tm
}

// dedupePts drops repeated corners and corners in the middle of a run.
func dedupePts(pts [][2]int) [][2]int {
	var out [][2]int
	for _, p := range pts {
		if len(out) > 0 && out[len(out)-1] == p {
			continue
		}
		if n := len(out); n >= 2 {
			a, b := out[n-2], out[n-1]
			if (a[0] == b[0] && b[0] == p[0]) || (a[1] == b[1] && b[1] == p[1]) {
				out[n-1] = p
				continue
			}
		}
		out = append(out, p)
	}
	return out
}

// lineUse is one link's use of a cell: its directions there, and whether
// the cell is an end of the link.
type lineUse struct {
	link int
	dirs uint8
	end  bool
}

// indexLines maps every cell a line takes to the links that take it.
func indexLines(tf *textFlow) map[[2]int][]lineUse {
	idx := map[[2]int][]lineUse{}
	for j, p := range tf.paths {
		if cells, dirs, ok := walk(p); ok {
			for m, c := range cells {
				idx[c] = append(idx[c], lineUse{j, dirs[m], m == 0 || m == len(cells)-1})
			}
		}
	}
	return idx
}

// others reports whether a link other than i takes cell c.
func others(idx map[[2]int][]lineUse, c [2]int, i int) bool {
	for _, u := range idx[c] {
		if u.link != i {
			return true
		}
	}
	return false
}

// labelsOnLines puts each label on its own line (placeLabel): the
// operator's check of the text art, round 2, found a label beside its
// line, where the line ran a track away from the label's layer.
func labelsOnLines(f *mr.Flowchart, tf *textFlow, keep int) {
	idx := indexLines(tf)
	for i, lk := range f.Links {
		if lk.Label != "" && len(tf.paths[i]) >= 2 {
			placeLabel(f, tf, i, keep, idx)
		}
	}
}

// placeLabel keeps link i's label where it is when it stands on the line
// (a loop's: on it or beside it) over no turn of it, and otherwise moves
// it to the nearest straight stretch that holds it clear of everything
// else and of keep cells at each end (its head and a cell of line, or an
// ER mark and a cell of line) — for a loop, beside a stretch too. A label
// over a turn hides which way the line goes. It reports whether the label ends where it may stand.
func placeLabel(f *mr.Flowchart, tf *textFlow, i, keep int, idx map[[2]int][]lineUse) bool {
	type key [2]int
	own, turn := map[key]bool{}, map[key]bool{}
	cells, dirs, ok := walk(tf.paths[i])
	if !ok {
		return false
	}
	for k, c := range cells {
		own[key(c)] = true
		if isCorner(dirs[k]) {
			turn[key(c)] = true
		}
	}
	loop := f.Links[i].From == f.Links[i].To
	solid := func(x, y int) bool {
		if turn[key{x, y}] || others(idx, [2]int{x, y}, i) {
			return true
		}
		for _, b := range tf.boxes {
			if b.has(x, y) {
				return true
			}
		}
		for _, fr := range tf.frames {
			if (x == fr.x0 || x == fr.x1) && y >= fr.y0 && y <= fr.y1 || (y == fr.y0 || y == fr.y1) && x >= fr.x0 && x <= fr.x1 {
				return true
			}
		}
		for _, t := range tf.titles {
			if t.has(x, y) {
				return true
			}
		}
		for j, r := range tf.labels {
			if j != i && f.Links[j].Label != "" && r.has(x, y) {
				return true
			}
		}
		return false
	}
	r := tf.labels[i]
	// Where it is: on the line (or by a loop), over no turn.
	on, turns := false, false
	for y := r.y0; y <= r.y1; y++ {
		for x := r.x0; x <= r.x1; x++ {
			on = on || own[key{x, y}] && !turn[key{x, y}]
			turns = turns || turn[key{x, y}]
		}
	}
	if (on || loop && labelByItsLine(tf, idx, i, r, true)) && !turns && r.x0 >= 0 && r.y0 >= 0 {
		return true
	}
	w, h := r.x1-r.x0+1, r.y1-r.y0+1
	ends := map[key]bool{}
	for k := 0; k < keep && k < len(cells); k++ {
		ends[key(cells[k])] = true
		ends[key(cells[len(cells)-1-k])] = true
	}
	pts := tf.paths[i]
	var cands []iRect
	// stretch is the run of its line a candidate may cover: the one it
	// stands on, or none for a loop's beside it. Covering any other part of
	// its line would break it twice.
	stretch := map[iRect]iRect{}
	none := iRect{1, 1, 0, 0}
	for k := 1; k < len(pts); k++ {
		a, b := pts[k-1], pts[k]
		switch {
		case a[1] == b[1]: // across
			lo, hi := min(a[0], b[0]), max(a[0], b[0])
			// Every place along the stretch, between its corners.
			for x0 := lo + 1; h == 1 && x0+w-1 <= hi-1; x0++ {
				c := iRect{x0, a[1], x0 + w - 1, a[1]}
				cands, stretch[c] = append(cands, c), iRect{lo, a[1], hi, a[1]}
			}
			for x0 := lo - w + 1; loop && x0 <= hi; x0++ { // above or below it, overlapping it
				for _, c := range []iRect{{x0, a[1] - h, x0 + w - 1, a[1] - 1}, {x0, a[1] + 1, x0 + w - 1, a[1] + h}} {
					cands, stretch[c] = append(cands, c), none
				}
			}
		case a[0] == b[0]: // down
			lo, hi := min(a[1], b[1]), max(a[1], b[1])
			for y0 := lo + 1; y0+h-1 <= hi-1; y0++ {
				c := iRect{a[0] - w/2, y0, a[0] - w/2 + w - 1, y0 + h - 1}
				cands, stretch[c] = append(cands, c), iRect{a[0], lo, a[0], hi}
			}
			for y0 := lo - h + 1; loop && y0 <= hi; y0++ { // a cell clear of it, either side
				for _, c := range []iRect{{a[0] + 2, y0, a[0] + 1 + w, y0 + h - 1}, {a[0] - 1 - w, y0, a[0] - 2, y0 + h - 1}} {
					cands, stretch[c] = append(cands, c), none
				}
			}
		}
	}
	best, bestD := iRect{}, -1
	for _, c := range cands {
		clear := c.x0 >= 0 && c.y0 >= 0
		for y := c.y0; y <= c.y1 && clear; y++ {
			for x := c.x0; x <= c.x1; x++ {
				if solid(x, y) || ends[key{x, y}] || own[key{x, y}] && !stretch[c].has(x, y) {
					clear = false
					break
				}
			}
		}
		clear = clear && (!loop || labelByItsLine(tf, idx, i, c, true))
		d := abs(float64(c.x0-r.x0)) + abs(float64(c.y0-r.y0))
		if clear && (bestD < 0 || int(d) < bestD) {
			best, bestD = c, int(d)
		}
	}
	if bestD < 0 {
		return false
	}
	tf.labels[i] = best
	tf.w, tf.h = max(tf.w, best.x1+1), max(tf.h, best.y1+1)
	return true
}

// alignLeaves straightens a link that steps across halfway (its two ends'
// faces share no interior row or column on the grid) by moving the box at
// one end — a node no other link touches — along the face until the ends
// line up, when the moved box hits nothing (the operator's check of the
// text art, round 3: a leaf's link stepped where the leaf could move).
func alignLeaves(f *mr.Flowchart, tf *textFlow, keep int) {
	degree := map[string]int{}
	for _, lk := range f.Links {
		degree[lk.From.ID]++
		degree[lk.To.ID]++
	}
	nodeAt := map[string]int{}
	for i, n := range f.Nodes {
		nodeAt[n.ID] = i
	}
	inFrame := func(id string) int {
		for k, sg := range f.Subgraphs {
			for _, m := range sg.Nodes {
				if m == id {
					return k
				}
			}
		}
		return -1
	}
	idx := indexLines(tf) // rebuilt after each move
	fits := func(node int, b iRect, skip int) bool {
		if b.x0 < 0 || b.y0 < 0 {
			return false
		}
		for j, o := range tf.boxes {
			if j != node && b.overlaps(iRect{o.x0 - 1, o.y0 - 1, o.x1 + 1, o.y1 + 1}) {
				return false
			}
		}
		k := inFrame(f.Nodes[node].ID)
		for j, fr := range tf.frames {
			inside := b.x0 > fr.x0 && b.x1 < fr.x1 && b.y0 > fr.y0 && b.y1 < fr.y1
			// Only into its own frame: flowcharts nest no subgraphs, so a
			// node inside another frame would read as its member.
			if j == k && !inside || j != k && b.overlaps(fr) {
				return false
			}
		}
		for _, t := range tf.titles {
			if b.overlaps(t) {
				return false
			}
		}
		for j, r := range tf.labels {
			if f.Links[j].Label != "" && j != skip && b.overlaps(r) {
				return false
			}
		}
		for y := b.y0 - 1; y <= b.y1+1; y++ {
			for x := b.x0 - 1; x <= b.x1+1; x++ {
				if others(idx, [2]int{x, y}, skip) {
					return false
				}
			}
		}
		return true
	}
	// The straight link: through no box, title or other label, meeting
	// other links only crossing at right angles.
	clearPath := func(link, node int, b iRect, pts [][2]int) bool {
		cells, dirs, ok := walk(pts)
		if !ok {
			return false
		}
		for n, c := range cells {
			for j, o := range tf.boxes {
				if j == node {
					o = b
				}
				if o.has(c[0], c[1]) {
					return false
				}
			}
			for _, t := range tf.titles {
				if t.has(c[0], c[1]) {
					return false
				}
			}
			for j, r := range tf.labels {
				if j != link && f.Links[j].Label != "" && r.has(c[0], c[1]) {
					return false
				}
			}
			straight := func(d uint8) bool { return d == dUp|dDown || d == dLeft|dRight }
			for _, u := range idx[c] {
				if u.link != link && (!(straight(u.dirs) && straight(dirs[n]) && u.dirs != dirs[n]) || u.end) {
					return false
				}
			}
		}
		return true
	}
	for i, lk := range f.Links {
		p := tf.paths[i]
		if len(p) != 4 || lk.From.Subgraph || lk.To.Subgraph || lk.From == lk.To {
			continue
		}
		across := p[0][1] == p[1][1] && p[1][0] == p[2][0] && p[2][1] == p[3][1]
		down := p[0][0] == p[1][0] && p[1][1] == p[2][1] && p[2][0] == p[3][0]
		if !across && !down {
			continue
		}
		axis := 1 // the coordinate the step changes
		if down {
			axis = 0
		}
		delta := p[0][axis] - p[3][axis]
		try := func(id string, sign int, straight [][2]int) bool {
			if degree[id] != 1 {
				return false
			}
			n := nodeAt[id]
			b := tf.boxes[n]
			if axis == 1 {
				b.y0, b.y1 = b.y0+sign*delta, b.y1+sign*delta
			} else {
				b.x0, b.x1 = b.x0+sign*delta, b.x1+sign*delta
			}
			if !fits(n, b, i) || !clearPath(i, n, b, straight) {
				return false
			}
			// The label goes with its line, or the leaf stays.
			ob, op, ol, ow, oh := tf.boxes[n], tf.paths[i], tf.labels[i], tf.w, tf.h
			tf.boxes[n], tf.paths[i] = b, straight
			tf.w, tf.h = max(tf.w, b.x1+1), max(tf.h, b.y1+1)
			nidx := indexLines(tf)
			if lk.Label != "" && !placeLabel(f, tf, i, keep, nidx) {
				tf.boxes[n], tf.paths[i], tf.labels[i], tf.w, tf.h = ob, op, ol, ow, oh
				return false
			}
			idx = nidx
			return true
		}
		var toEnd, fromEnd [2]int
		toEnd, fromEnd = p[3], p[0]
		toEnd[axis], fromEnd[axis] = p[0][axis], p[3][axis]
		if !try(lk.To.ID, 1, [][2]int{p[0], toEnd}) {
			try(lk.From.ID, -1, [][2]int{fromEnd, p[3]})
		}
	}
}
