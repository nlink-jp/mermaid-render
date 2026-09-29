package raster

import (
	"math"
	"sort"

	mr "github.com/nlink-jp/mermaid-render"
)

// place turns layers and cross coordinates into the final layout: rank
// positions, frames, link paths through ports, then the direction's
// transform and clipping at the node shapes.
func (l *layouter) place() *flowLayout {
	nl := len(l.layers)
	band := make([]float64, nl)
	for k, layer := range l.layers {
		for _, it := range layer {
			band[k] = math.Max(band[k], it.rs)
		}
	}
	// Node half-widths across the rank axis.
	baseHalf := make([]float64, len(l.nw))
	for i := range l.nw {
		cross := l.nw[i]
		if l.horiz {
			cross = l.nh[i]
		}
		baseHalf[i] = cross / 2
	}

	// Ports: each link attaching to a face gets its own point on it,
	// ordered by where the link goes next. They depend only on cross
	// positions, so they come before the rank axis is laid out.
	type port struct {
		ch   *chain
		low  bool // the chain's low end
		next float64
	}
	faceOf := func(e end, side int) [3]int {
		if e.node >= 0 {
			return [3]int{0, e.node, side}
		}
		return [3]int{1, e.cluster, side}
	}
	crossOf := func(e end) float64 {
		if e.node >= 0 {
			return l.nodeItem[e.node].x
		}
		c := l.clusters[e.cluster]
		return (c.L + c.R) / 2
	}
	ends := func(ch *chain) (end, end) {
		if ch.fromLow {
			return ch.from, ch.to
		}
		return ch.to, ch.from
	}
	assignPorts := func() map[*chain][2]float64 {
		faces := map[[3]int][]*port{} // (kind 0 node / 1 cluster, index, side 0 high / 1 low)
		var faceKeys [][3]int
		portX := map[*chain][2]float64{} // [low end, high end]
		for _, ch := range l.chains {
			if ch.self {
				continue
			}
			lowEnd, highEnd := ends(ch)
			nextLow, nextHigh := crossOf(highEnd), crossOf(lowEnd)
			if len(ch.items) > 0 {
				nextLow, nextHigh = ch.items[0].x, ch.items[len(ch.items)-1].x
			}
			for _, p := range []struct {
				e    end
				side int
				low  bool
				next float64
			}{{lowEnd, 0, true, nextLow}, {highEnd, 1, false, nextHigh}} {
				k := faceOf(p.e, p.side)
				if faces[k] == nil {
					faceKeys = append(faceKeys, k)
				}
				faces[k] = append(faces[k], &port{ch: ch, low: p.low, next: p.next})
			}
		}
		// Faces leaving toward later layers first (side 0), then the faces
		// links arrive on (side 1); nodes before frames within each. A frame's
		// ports keep clear of the columns that links to its members pass its
		// face in (else they run alongside). An arriving port also keeps clear
		// of the columns links come down in: not needed for correctness — two
		// links swapping near-equal columns get a detour in assignTracks — but
		// it spares most detours.
		sort.SliceStable(faceKeys, func(i, j int) bool {
			if faceKeys[i][2] != faceKeys[j][2] {
				return faceKeys[i][2] < faceKeys[j][2]
			}
			return faceKeys[i][0] < faceKeys[j][0]
		})
		for _, k := range faceKeys {
			ps := faces[k]
			sort.SliceStable(ps, func(i, j int) bool { return ps[i].next < ps[j].next })
			var lo, hi float64
			var occ []float64
			layer := -1
			if k[0] == 0 {
				c0, width := l.nodeItem[k[1]].x, 2*baseHalf[k[1]]*l.spreadOf(k[1])
				lo, hi = c0-width/2, c0+width/2
				layer = l.rank[k[1]]
			} else {
				c := l.clusters[k[1]]
				lo, hi = c.L+l.sp.framePad, c.R-l.sp.framePad
				occ = l.memberColumns(k[1], k[2], portX)
				layer = c.r1
				if k[2] == 1 {
					layer = c.r0
				}
			}
			if k[2] == 1 && layer > 0 {
				// Not the columns of the links arriving on this very face: a
				// port right below its own link is a straight drop, and two of
				// them swapping columns is resolved by a detour (assignTracks).
				mine := map[*chain]bool{}
				for _, p := range ps {
					mine[p.ch] = true
				}
				occ = append(occ, l.columnsLeaving(layer-1, portX, mine)...)
			}
			want := make([]float64, len(ps))
			for i, p := range ps {
				want[i] = p.next
			}
			xs := alignPorts(lo, hi, want, occ, l.portGap, l.sp.trackSep)
			if xs == nil {
				xs = pickPorts(lo, hi, len(ps), occ, l.portGap, l.sp.trackSep)
			}
			// A snap may leave the port range only on a node without
			// self-links: the range keeps ports clear of loop ends. It
			// stays on the node's face, off its rim: a state's end circle
			// is narrow enough for a 0.2 em snap to miss it.
			slo, shi := math.Inf(-1), math.Inf(1)
			if k[0] == 1 || l.hasLoop(k[1]) {
				slo, shi = lo, hi
			} else {
				c0, h := l.nodeItem[k[1]].x, 0.9*baseHalf[k[1]]
				slo, shi = c0-h, c0+h
			}
			snapPorts(xs, want, occ, slo, shi, l.portGap, l.sp.trackSep)
			for i, p := range ps {
				x := xs[i]
				v := portX[p.ch]
				if p.low {
					v[0] = x
				} else {
					v[1] = x
				}
				portX[p.ch] = v
			}
		}
		return portX
	}
	// Ports, then each link's bend points moved onto its port's column
	// where the constraints let them, then ports again: a link whose two
	// ends share a column runs straight.
	portX := assignPorts()
	l.straighten(portX)
	portX = assignPorts()
	// Then nodes move onto the column of a face that has one link, so the
	// link meets the node's middle (a rhombus's vertex) and a box sits
	// centred on its line; ports and dummies follow.
	if l.centerNodes() {
		portX = assignPorts()
		l.straighten(portX)
		portX = assignPorts()
	}
	// Last, a link from a fan into a node that takes it alone meets that
	// node's middle, and steps beside the fan instead.
	if l.centerLoneEnds(portX) {
		portX = assignPorts()
	}

	// Each chain crosses the gap after every layer from its low end to the
	// layer before its high end, from one column (top) to another
	// (bottom). A crossing that changes column runs down, across on a track
	// of its own in the gap, and down again: crossings then meet only at
	// right angles, never running alongside each other.
	gapCross := make([][]*crossing, nl)
	chainCross := map[*chain][]*crossing{}
	for _, ch := range l.chains {
		if ch.self {
			continue
		}
		px := portX[ch]
		cols := []float64{px[0]}
		for _, it := range ch.items {
			cols = append(cols, it.x)
		}
		cols = append(cols, px[1])
		for i := 0; i+1 < len(cols); i++ {
			c := &crossing{ch: ch, top: cols[i], bot: cols[i+1], track: -1}
			gapCross[ch.lo+i] = append(gapCross[ch.lo+i], c)
			chainCross[ch] = append(chainCross[ch], c)
		}
	}
	tracks := make([]int, nl)
	for k, cs := range gapCross {
		tracks[k] = assignTracks(cs, l.sp.trackSep)
	}
	// A link turning in the gap right after its low end, with a head
	// there (a link drawn against the layer order, or one with a head at
	// both ends), needs the arrowhead's room above its track as well: at
	// trackIn the head sits on the bend (the operator's third check).
	headTop := make([]bool, nl)
	for _, ch := range l.chains {
		cs := chainCross[ch]
		if ch.self || len(cs) == 0 || cs[0].track < 0 {
			continue
		}
		lk := l.f.Links[ch.link]
		h := lk.End
		if ch.fromLow {
			h = lk.Start
		}
		if h != mr.NoHead {
			headTop[ch.lo] = true
		}
	}
	// A link that bends in the gap right below its label keeps labelRoom
	// between the two (the operator's ER check: labels too near a bend).
	labelTop := make([]bool, nl)
	for _, ch := range l.chains {
		if ch.self || ch.label == nil {
			continue
		}
		if ci := ch.label.layer - ch.lo; ci >= 0 && ci < len(chainCross[ch]) && chainCross[ch][ci].track >= 0 {
			labelTop[ch.label.layer] = true
		}
	}

	// Where a frame's title sits in abstract coordinates.
	titleLow := l.f.Direction == mr.TB  // before the first layer
	titleHigh := l.f.Direction == mr.BT // after the last layer (drawn on top once flipped)
	tspace := func(c *cluster) float64 {
		if c.titleH == 0 {
			return 0
		}
		return c.titleH + l.sp.titleGap
	}
	// Gaps between layers make room for frame edges, titles and tracks,
	// computed in layer order so that a horizontal layout's frame, which
	// must span its title along the rank axis, can push its end (and the
	// tracks beyond it) as far as the title needs.
	gapAfter := make([]float64, nl)
	trackTop := make([]float64, nl) // offset of track 0 from the gap's start
	start := make([]float64, nl)
	extra := make([]float64, len(l.clusters)) // rank-axis extension for a title
	v := 0.0
	for k := range nl {
		start[k] = v
		below, above := 0.0, 0.0
		for ci, c := range l.clusters {
			if c.r1 == k {
				e := l.sp.framePad
				if titleHigh {
					e += tspace(c)
				}
				if l.horiz {
					span := start[k] + band[k] - start[c.r0] + 2*l.sp.framePad
					if need := c.titleW + 2*l.sp.framePad; need > span {
						extra[ci] = need - span
						e += extra[ci]
					}
				}
				below = math.Max(below, e)
			}
			if c.r0 == k+1 {
				e := l.sp.framePad
				if titleLow {
					e += tspace(c)
				}
				above = math.Max(above, e)
			}
		}
		margin := 0.0
		if below > 0 || above > 0 {
			margin = l.sp.frameSep / 2
		}
		in := l.sp.trackIn
		if headTop[k] {
			in = l.endRoom
		}
		if labelTop[k] {
			in = math.Max(in, l.labelRoom)
		}
		trackTop[k] = below + margin + in
		need := l.rankGap
		if below > 0 || above > 0 {
			need = math.Max(need, below+above+l.sp.frameSep)
		}
		if tracks[k] > 0 {
			need = math.Max(need, below+above+2*margin+in+float64(tracks[k]-1)*l.sp.trackSep+l.endRoom)
		}
		gapAfter[k] = need
		v += band[k] + need
	}
	center := func(k int) float64 { return start[k] + band[k]/2 }
	trackY := func(k, t int) float64 { return start[k] + band[k] + trackTop[k] + float64(t)*l.sp.trackSep }

	// Frames, in abstract coordinates (cross = x, rank = y).
	frameRect := make([]rect, len(l.clusters))
	for ci, c := range l.clusters {
		r := rect{X0: c.L, X1: c.R, Y0: start[c.r0] - l.sp.framePad, Y1: start[c.r1] + band[c.r1] + l.sp.framePad}
		if titleLow {
			r.Y0 -= tspace(c)
		}
		if titleHigh {
			r.Y1 += tspace(c)
		}
		r.Y1 += extra[ci]
		frameRect[ci] = r
	}

	// Node boxes in abstract coordinates.
	nodeRect := make([]rect, len(l.nw))
	for i := range l.nw {
		it := l.nodeItem[i]
		rs := l.nh[i]
		if l.horiz {
			rs = l.nw[i]
		}
		cy := center(it.layer)
		nodeRect[i] = rect{X0: it.x - baseHalf[i], X1: it.x + baseHalf[i], Y0: cy - rs/2, Y1: cy + rs/2}
	}

	// Paths in abstract coordinates, low end to high end.
	type path struct {
		pts        []pt
		clipLow    int // node to clip the first segment at, or -1
		clipHigh   int
		label      rect
		hasLabel   bool
		reversed   bool
		link       *mr.Link
		loopOfNode int
		loopIn     [2]pt // abstract points inside the node, level with the loop's ends
	}
	paths := make([]*path, len(l.chains))
	loops := map[int]float64{} // node -> reach used so far
	for i, ch := range l.chains {
		lk := l.f.Links[ch.link]
		if ch.self {
			n := ch.from.node
			r := nodeRect[n]
			used := loops[n]
			face := l.nodeItem[n].x + baseHalf[n]
			edge := face + used
			reach := l.sp.loopReach
			y0, y1 := r.Center().Y-ch.loopOff, r.Center().Y+ch.loopOff
			p := &path{link: lk, clipLow: -1, clipHigh: -1, loopOfNode: n}
			p.pts = []pt{{face, y0}, {edge + reach, y0}, {edge + reach, y1}, {face, y1}}
			p.loopIn = [2]pt{{l.nodeItem[n].x, y0}, {l.nodeItem[n].x, y1}}
			extra := reach
			if ch.lw > 0 {
				cross, rs := ch.lw, ch.lh
				if l.horiz {
					cross, rs = ch.lh, ch.lw
				}
				cx := edge + reach + cross/2
				cy := (y0 + y1) / 2
				p.label = rect{cx - cross/2, cy - rs/2, cx + cross/2, cy + rs/2}
				p.hasLabel = true
				extra += cross
			}
			loops[n] = used + extra
			paths[i] = p
			continue
		}
		lowEnd, highEnd := ends(ch)
		px := portX[ch]
		p := &path{link: lk, clipLow: -1, clipHigh: -1, reversed: !ch.fromLow, loopOfNode: -1}
		if lowEnd.node >= 0 {
			n := lowEnd.node
			p.pts = append(p.pts, pt{px[0], nodeRect[n].Center().Y}, pt{px[0], start[ch.lo] + band[ch.lo]})
			p.clipLow = n
		} else {
			p.pts = append(p.pts, pt{px[0], frameRect[lowEnd.cluster].Y1})
		}
		for ci, c := range chainCross[ch] {
			k := ch.lo + ci
			switch {
			case c.split:
				y1, y2 := trackY(k, c.track), trackY(k, c.track2)
				p.pts = append(p.pts, pt{c.top, y1}, pt{c.mid, y1}, pt{c.mid, y2}, pt{c.bot, y2})
			case c.track >= 0:
				y := trackY(k, c.track)
				p.pts = append(p.pts, pt{c.top, y}, pt{c.bot, y})
			}
			if ci < len(ch.items) {
				it := ch.items[ci]
				a, b := start[it.layer], start[it.layer]+band[it.layer]
				p.pts = append(p.pts, pt{it.x, a})
				if b > a {
					p.pts = append(p.pts, pt{it.x, b})
				}
				if it == ch.label {
					cross, rs := ch.lw, ch.lh
					if l.horiz {
						cross, rs = ch.lh, ch.lw
					}
					cy := center(it.layer)
					p.label = rect{it.x - cross/2, cy - rs/2, it.x + cross/2, cy + rs/2}
					p.hasLabel = true
				}
			}
		}
		if highEnd.node >= 0 {
			n := highEnd.node
			p.pts = append(p.pts, pt{px[1], start[ch.hi]}, pt{px[1], nodeRect[n].Center().Y})
			p.clipHigh = n
		} else {
			p.pts = append(p.pts, pt{px[1], frameRect[highEnd.cluster].Y0})
		}
		paths[i] = p
	}

	// Transform to the diagram's direction.
	tp := func(p pt) pt {
		switch l.f.Direction {
		case mr.BT:
			return pt{p.X, -p.Y}
		case mr.LR:
			return pt{p.Y, p.X}
		case mr.RL:
			return pt{-p.Y, p.X}
		}
		return p
	}
	tr := func(r rect) rect {
		a, b := tp(pt{r.X0, r.Y0}), tp(pt{r.X1, r.Y1})
		return rect{math.Min(a.X, b.X), math.Min(a.Y, b.Y), math.Max(a.X, b.X), math.Max(a.Y, b.Y)}
	}
	out := &flowLayout{}
	vis := make([]rect, len(l.nw))
	for i := range l.nw {
		vis[i] = tr(nodeRect[i])
	}
	for i, n := range l.nodes {
		out.Nodes = append(out.Nodes, nodeBox{ID: n.ID, Label: n.Label, Shape: n.Shape, Box: vis[i],
			Slant: math.Min(l.slant0[i], 0.3*vis[i].W())})
	}
	for ci, c := range l.clusters {
		fr := tr(frameRect[ci])
		fb := frameBox{ID: c.sg.ID, Title: c.sg.Title, Box: fr}
		if c.titleH > 0 {
			x0 := fr.X0 + l.sp.framePad*0.7
			y0 := fr.Y0 + l.sp.titleGap
			fb.TitleBox = rect{x0, y0, x0 + c.titleW, y0 + c.titleH}
		}
		out.Frames = append(out.Frames, fb)
	}
	for _, p := range paths {
		pts := make([]pt, len(p.pts))
		for i, q := range p.pts {
			pts[i] = tp(q)
		}
		if p.clipLow >= 0 && len(pts) >= 2 {
			pts[0] = clipAt(l.shapeOf(p.clipLow), vis[p.clipLow], l.slantOf(p.clipLow, vis), pts[1], pts[0])
		}
		if p.clipHigh >= 0 && len(pts) >= 2 {
			n := len(pts)
			pts[n-1] = clipAt(l.shapeOf(p.clipHigh), vis[p.clipHigh], l.slantOf(p.clipHigh, vis), pts[n-2], pts[n-1])
		}
		if p.loopOfNode >= 0 {
			// The loop starts and ends on the node's face.
			n := p.loopOfNode
			pts[0] = clipAt(l.shapeOf(n), vis[n], l.slantOf(n, vis), pts[1], tp(p.loopIn[0]))
			pts[len(pts)-1] = clipAt(l.shapeOf(n), vis[n], l.slantOf(n, vis), pts[len(pts)-2], tp(p.loopIn[1]))
		}
		if p.reversed {
			for i, j := 0, len(pts)-1; i < j; i, j = i+1, j-1 {
				pts[i], pts[j] = pts[j], pts[i]
			}
		}
		e := edgePath{Link: p.link, Points: dedupe(pts), Label: p.link.Label}
		if p.hasLabel {
			e.LabelBox = tr(p.label)
		}
		out.Edges = append(out.Edges, e)
	}
	// Move to the origin.
	bb := rect{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	grow := func(r rect) {
		bb = rect{math.Min(bb.X0, r.X0), math.Min(bb.Y0, r.Y0), math.Max(bb.X1, r.X1), math.Max(bb.Y1, r.Y1)}
	}
	for _, n := range out.Nodes {
		grow(n.Box)
	}
	for _, f := range out.Frames {
		grow(f.Box)
	}
	for _, e := range out.Edges {
		for _, p := range e.Points {
			grow(rect{p.X, p.Y, p.X, p.Y})
		}
		if e.Label != "" {
			grow(e.LabelBox)
		}
	}
	if len(out.Nodes) == 0 && len(out.Frames) == 0 {
		bb = rect{}
	}
	shift := func(r rect) rect { return rect{r.X0 - bb.X0, r.Y0 - bb.Y0, r.X1 - bb.X0, r.Y1 - bb.Y0} }
	for i := range out.Nodes {
		out.Nodes[i].Box = shift(out.Nodes[i].Box)
	}
	for i := range out.Frames {
		out.Frames[i].Box = shift(out.Frames[i].Box)
		if out.Frames[i].TitleBox != (rect{}) {
			out.Frames[i].TitleBox = shift(out.Frames[i].TitleBox)
		}
	}
	for i := range out.Edges {
		for j := range out.Edges[i].Points {
			p := out.Edges[i].Points[j]
			out.Edges[i].Points[j] = pt{p.X - bb.X0, p.Y - bb.Y0}
		}
		if out.Edges[i].Label != "" {
			out.Edges[i].LabelBox = shift(out.Edges[i].LabelBox)
		}
	}
	out.W, out.H = bb.W(), bb.H()
	return out
}

// straighten moves each link's dummies onto one of its ports' columns when
// every dummy can stand there without breaking a constraint, the rest held
// still: the link then leaves or arrives straight instead of stepping aside
// by a fraction of an em. The low end's column is tried first.
func (l *layouter) straighten(portX map[*chain][2]float64) {
	s, id := l.sol, l.solID
	if s == nil {
		return
	}
	// Repeat until nothing moves: a dummy held by its neighbour may be
	// free once that neighbour has moved (a fan of links packed side by
	// side frees from one end).
	done := map[*chain]bool{}
	for pass := 0; pass < len(l.chains)+1; pass++ {
		moved := false
		for _, ch := range l.chains {
			if ch.self || len(ch.items) == 0 || done[ch] {
				continue
			}
			px := portX[ch]
			for _, c := range []float64{px[0], px[1]} {
				ok := true
				for _, it := range ch.items {
					lo, hi := s.interval(id[it])
					if c < lo-1e-9 || c > hi+1e-9 {
						ok = false
						break
					}
				}
				if !ok {
					continue
				}
				for _, it := range ch.items {
					s.x[id[it]] = c
					it.x = c
				}
				done[ch], moved = true, true
				break
			}
		}
		if !moved {
			break
		}
	}
	// A link held off its column by one other link's dummy may take it when
	// that other link shifts aside as a whole: its dummies must all stand
	// in the new column and its ends' faces reach it.
	owner := map[*item]*chain{}
	for _, ch := range l.chains {
		for _, it := range ch.items {
			owner[it] = ch
		}
	}
	for _, ch := range l.chains {
		if ch.self || len(ch.items) == 0 || done[ch] {
			continue
		}
		px := portX[ch]
		for _, c := range []float64{px[0], px[1]} {
			if l.tryWithShift(ch, c, owner, done) {
				done[ch] = true
				break
			}
		}
	}
	// A link that cannot run in one column still keeps a column as long as
	// it can: each dummy takes the column of the station before it where
	// the constraints allow, so the link steps aside only where it must —
	// and only when that saves a step. Carrying the port's column into the
	// middle of a link that already runs in one column there moves its
	// step from beside the node into the open (the operator's third check:
	// bend next to the node).
	changes := func(cols []float64) int {
		n := 0
		for i := 0; i+1 < len(cols); i++ {
			if math.Abs(cols[i+1]-cols[i]) > 1e-9 {
				n++
			}
		}
		return n
	}
	for _, ch := range l.chains {
		if ch.self || len(ch.items) == 0 || done[ch] {
			continue
		}
		px := portX[ch]
		before := []float64{px[0]}
		for _, it := range ch.items {
			before = append(before, it.x)
		}
		before = append(before, px[1])
		after := []float64{px[0]}
		col := px[0]
		for _, it := range ch.items {
			x := it.x
			lo, hi := s.interval(id[it])
			if col >= lo-1e-9 && col <= hi+1e-9 {
				x = col
			}
			after = append(after, x)
			col = x
		}
		after = append(after, px[1])
		if changes(after) >= changes(before) {
			continue
		}
		for i, it := range ch.items {
			s.x[id[it]] = after[i+1]
			it.x = after[i+1]
		}
	}
}

func (l *layouter) hasLoop(n int) bool {
	for _, ch := range l.chains {
		if ch.self && ch.from.node == n {
			return true
		}
	}
	return false
}

// tryWithShift puts ch's dummies in column c, moving the one other link
// whose dummy blocks them aside by as much as the gap needs. It commits
// only when every dummy of both links then satisfies its constraints and
// the other link's new column lies on both of its ends' faces.
func (l *layouter) tryWithShift(ch *chain, c float64, owner map[*item]*chain, done map[*chain]bool) bool {
	s, id := l.sol, l.solID
	save := map[int]float64{}
	set := func(it *item, x float64) {
		if _, ok := save[id[it]]; !ok {
			save[id[it]] = s.x[id[it]]
		}
		s.x[id[it]], it.x = x, x
	}
	undo := func() {
		for v, x := range save {
			s.x[v] = x
		}
		for _, it := range ch.items {
			it.x = s.x[id[it]]
		}
	}
	var other *chain
	delta := 0.0
	for _, it := range ch.items {
		lo, hi := s.interval(id[it])
		if c >= lo-1e-9 && c <= hi+1e-9 {
			continue
		}
		// Find the single dummy of another link that bounds it.
		var blk *item
		var need float64
		for _, k := range s.in[id[it]] {
			if k.u < l.solItems && s.x[k.u]+k.d > c+1e-9 {
				if b := l.itemAt(k.u); b != nil && owner[b] != nil && owner[b] != ch {
					blk, need = b, c-k.d-s.x[k.u]
				}
			}
		}
		for _, k := range s.out[id[it]] {
			if k.v < l.solItems && s.x[k.v]-k.d < c-1e-9 {
				if b := l.itemAt(k.v); b != nil && owner[b] != nil && owner[b] != ch {
					blk, need = b, c+k.d-s.x[k.v]
				}
			}
		}
		if blk == nil || (other != nil && owner[blk] != other) {
			return false
		}
		other = owner[blk]
		if math.Abs(need) > math.Abs(delta) {
			delta = need
		}
	}
	if other == nil || other.self {
		return false
	}
	col := other.items[0].x + delta
	for _, it := range other.items {
		if math.Abs(it.x-other.items[0].x) > 1e-9 {
			return false // only a link already in one column is shifted
		}
	}
	for _, e := range []end{other.from, other.to} {
		if e.node < 0 {
			return false
		}
		half := l.nw[e.node] / 2
		if l.horiz {
			half = l.nh[e.node] / 2
		}
		half *= l.spreadOf(e.node)
		if math.Abs(col-l.nodeItem[e.node].x) > half {
			return false
		}
	}
	for _, it := range ch.items {
		set(it, c)
	}
	for _, it := range other.items {
		set(it, col)
	}
	for _, it := range append(append([]*item(nil), ch.items...), other.items...) {
		lo, hi := s.interval(id[it])
		if s.x[id[it]] < lo-1e-9 || s.x[id[it]] > hi+1e-9 {
			undo()
			for _, it := range other.items {
				it.x = s.x[id[it]]
			}
			return false
		}
	}
	done[other] = true
	return true
}

// itemAt is the item behind solver variable v (nil for a frame edge).
func (l *layouter) itemAt(v int) *item {
	if v < len(l.solList) {
		return l.solList[v]
	}
	return nil
}

// snapPorts closes steps too small to see as a step: a port within 0.2 em
// of the column its link goes on in moves onto it, when that keeps portGap
// from its neighbours on the face and clear of occupied columns.
func snapPorts(xs, want, occ []float64, lo, hi, portGap, trackSep float64) {
	for i := range xs {
		w := want[i]
		if d := math.Abs(xs[i] - w); d == 0 || d >= 0.2 || w < lo || w > hi {
			continue
		}
		if i > 0 && w-xs[i-1] < portGap || i+1 < len(xs) && xs[i+1]-w < portGap {
			continue
		}
		clear := true
		for _, o := range occ {
			if math.Abs(w-o) < trackSep && math.Abs(xs[i]-o) >= trackSep {
				clear = false
				break
			}
		}
		if clear {
			xs[i] = w
		}
	}
}

// centerNodes moves each node whose face has exactly one link onto that
// link's column next to it (the face toward earlier layers tried first),
// where the constraints allow; a frame around it widens as far as its own
// constraints allow. It reports whether anything moved.
func (l *layouter) centerNodes() bool {
	s, id := l.sol, l.solID
	if s == nil {
		return false
	}
	Lv := func(c int) int { return l.solItems + 2*c }
	Rv := func(c int) int { return l.solItems + 2*c + 1 }
	moved := false
	for pass := 0; pass < 4; pass++ {
		any := false
		for i, it := range l.nodeItem {
			if i >= len(l.nodes) {
				continue
			}
			var cands []float64
			// A link ending on this node's frame is the frame's to centre.
			if len(it.up) == 1 && it.up[0].dnFrame == 0 {
				cands = append(cands, it.up[0].x)
			}
			if len(it.dn) == 1 && it.dn[0].upFrame == 0 {
				cands = append(cands, it.dn[0].x)
			}
			if len(cands) == 0 {
				continue
			}
			ci := it.cluster
			var saveL, saveR float64
			if ci >= 0 {
				// Loosen the frame for the move, tighten it after.
				saveL, saveR = s.x[Lv(ci)], s.x[Rv(ci)]
				lo, _ := s.interval(Lv(ci))
				_, hi := s.interval(Rv(ci))
				s.x[Lv(ci)], s.x[Rv(ci)] = lo, hi
			}
			shifted := false
			for _, c := range cands {
				if math.Abs(c-it.x) < 1e-9 {
					break
				}
				lo, hi := s.interval(id[it])
				if c >= lo-1e-9 && c <= hi+1e-9 {
					s.x[id[it]], it.x = c, c
					shifted = true
					break
				}
			}
			any = any || shifted
			if ci >= 0 && !shifted {
				// Nothing moved: the frame stays where it was. Tightening
				// it anyway could move it (it need not have been tight),
				// and the ports on its faces would not follow.
				s.x[Lv(ci)], s.x[Rv(ci)] = saveL, saveR
			} else if ci >= 0 {
				_, hi := s.interval(Lv(ci))
				s.x[Lv(ci)] = hi
				lo, _ := s.interval(Rv(ci))
				s.x[Rv(ci)] = lo
				l.clusters[ci].L, l.clusters[ci].R = s.x[Lv(ci)], s.x[Rv(ci)]
			}
		}
		if !any {
			break
		}
		moved = true
	}
	return moved
}

// centerLoneEnds moves the bend points of each link that one end takes
// alone and the other end fans out with others onto the lone end's middle,
// where every one of them can stand (the operator's third check: a link
// entering a box off its middle). A link that already runs straight stays:
// a straight line off the middle beats a centred one with two bends. It
// reports whether anything moved.
func (l *layouter) centerLoneEnds(portX map[*chain][2]float64) bool {
	s, id := l.sol, l.solID
	if s == nil {
		return false
	}
	moved := false
	for _, ch := range l.chains {
		if ch.self || len(ch.items) == 0 {
			continue
		}
		low, high := ch.from, ch.to
		if !ch.fromLow {
			low, high = ch.to, ch.from
		}
		if low.node < 0 || high.node < 0 {
			continue
		}
		straight := true
		for _, it := range ch.items {
			if math.Abs(it.x-portX[ch][0]) > 1e-9 || math.Abs(it.x-portX[ch][1]) > 1e-9 {
				straight = false
			}
		}
		if straight {
			continue
		}
		lowN := l.facePorts[[3]int{0, low.node, 0}]
		highN := l.facePorts[[3]int{0, high.node, 1}]
		var c float64
		switch {
		case lowN > 1 && highN == 1:
			c = l.nodeItem[high.node].x
		case highN > 1 && lowN == 1:
			c = l.nodeItem[low.node].x
		default:
			continue
		}
		ok, same := true, true
		for _, it := range ch.items {
			lo, hi := s.interval(id[it])
			if c < lo-1e-9 || c > hi+1e-9 {
				ok = false
				break
			}
			if math.Abs(it.x-c) > 1e-9 {
				same = false
			}
		}
		if !ok || same {
			continue
		}
		for _, it := range ch.items {
			s.x[id[it]], it.x = c, c
		}
		moved = true
	}
	return moved
}

// spread places n ports evenly across width around c0.
func spread(c0, width float64, n int) []float64 {
	xs := make([]float64, n)
	for i := range xs {
		xs[i] = c0
		if n > 1 {
			xs[i] = c0 - width/2 + width*(float64(i)+0.5)/float64(n)
		}
	}
	return xs
}

// memberColumns are the columns in which links to and from a subgraph's
// members cross its face (side 0: the high side, toward later layers; 1:
// the low side): the member dummies in the layer next to it and the
// members' ports on that side.
func (l *layouter) memberColumns(ci, side int, portX map[*chain][2]float64) []float64 {
	c := l.clusters[ci]
	layer := c.r1
	if side == 1 {
		layer = c.r0
	}
	var occ []float64
	for _, it := range l.layers[layer] {
		if it.cluster == ci && (it.kind == kDummy || it.kind == kLabel) {
			occ = append(occ, it.x)
		}
	}
	for _, ch := range l.chains {
		if ch.self {
			continue
		}
		for endIdx, e := range []end{ch.from, ch.to} {
			if e.node < 0 || l.inClus[e.node] != ci || l.rank[e.node] != layer {
				continue
			}
			isLow := (endIdx == 0) == ch.fromLow
			// A low end leaves from its node's high side (side 0).
			if (side == 0) == isLow {
				if v, ok := portX[ch]; ok {
					if isLow {
						occ = append(occ, v[0])
					} else {
						occ = append(occ, v[1])
					}
				}
			}
		}
	}
	return occ
}

// columnsLeaving are the columns links leave layer k in toward layer k+1:
// the dummies there and the leaving ports already placed, except skip's.
func (l *layouter) columnsLeaving(k int, portX map[*chain][2]float64, skip map[*chain]bool) []float64 {
	var out []float64
	for _, ch := range l.chains {
		if ch.self || ch.lo > k || ch.hi <= k || skip[ch] {
			continue
		}
		if k == ch.lo {
			if v, ok := portX[ch]; ok {
				out = append(out, v[0])
			}
			continue
		}
		out = append(out, ch.items[k-ch.lo-1].x)
	}
	return out
}

// alignPorts puts each port under (or over) the column its link goes on
// in, so the link runs straight instead of stepping aside: the wanted
// columns, clamped to the face, pushed portGap apart in order, and moved
// off occupied columns. nil when that cannot be done; pickPorts decides.
func alignPorts(lo, hi float64, want, occ []float64, portGap, trackSep float64) []float64 {
	n := len(want)
	if n == 0 || hi-lo < float64(n-1)*portGap-1e-9 {
		return nil
	}
	xs := make([]float64, n)
	for i, w := range want {
		xs[i] = math.Min(math.Max(w, lo), hi)
	}
	for i := 1; i < n; i++ {
		xs[i] = math.Max(xs[i], xs[i-1]+portGap)
	}
	if xs[n-1] > hi {
		xs[n-1] = hi
		for i := n - 2; i >= 0; i-- {
			xs[i] = math.Min(xs[i], xs[i+1]-portGap)
		}
	}
	clear := func(x float64) bool {
		for _, o := range occ {
			if math.Abs(x-o) < trackSep {
				return false
			}
		}
		return true
	}
	for i := range xs {
		if clear(xs[i]) {
			continue
		}
		moved := false
		for d := 0.05; d < hi-lo+1; d += 0.05 {
			for _, x := range []float64{xs[i] - d, xs[i] + d} {
				if x < lo || x > hi || !clear(x) {
					continue
				}
				if i > 0 && x < xs[i-1]+portGap || i+1 < n && x > xs[i+1]-portGap {
					continue
				}
				xs[i], moved = x, true
				break
			}
			if moved {
				break
			}
		}
		if !moved {
			return nil
		}
	}
	return xs
}

// pickPorts places n ports between lo and hi, portGap apart where the room
// allows, keeping trackSep from the occupied columns; when that is not
// possible it spreads them evenly.
func pickPorts(lo, hi float64, n int, occ []float64, portGap, trackSep float64) []float64 {
	c0, width := (lo+hi)/2, hi-lo
	even := spread(c0, width, n)
	if len(occ) == 0 {
		return even
	}
	clear := func(x float64) bool {
		for _, o := range occ {
			if math.Abs(x-o) < trackSep {
				return false
			}
		}
		return true
	}
	ok := true
	for _, x := range even {
		if !clear(x) {
			ok = false
			break
		}
	}
	if ok {
		return even
	}
	var free []float64
	for x := lo; x <= hi+1e-9; x += 0.05 {
		if clear(x) {
			free = append(free, x)
		}
	}
	if len(free) < n {
		return even
	}
	xs := make([]float64, 0, n)
	for i := range n {
		x := free[int(math.Round((float64(i)+0.5)*float64(len(free))/float64(n)-0.5))]
		if len(xs) > 0 && x-xs[len(xs)-1] < portGap*0.95 {
			return even
		}
		xs = append(xs, x)
	}
	return xs
}

func (l *layouter) slantOf(n int, vis []rect) float64 {
	return math.Min(l.slant0[n], 0.3*vis[n].W())
}

// crossing is a link's passage through the gap after one layer, from its
// column there (top) to its column in the next layer (bot).
type crossing struct {
	ch       *chain
	top, bot float64
	track    int // -1: straight down
	// A split crossing takes a detour: across on track to mid, down, and
	// across on track2 to bot.
	split  bool
	mid    float64
	track2 int
}

// assignTracks gives each crossing that changes column a track in its gap
// and returns how many tracks the gap needs. Crossings on one track keep
// their runs trackSep apart. A crossing leaving from (near) another's
// arrival column turns above it, so the first's drop and the second's rise
// never share a column. Two crossings that must each turn above the other
// (they swap near-equal columns) form a cycle; one of them then detours
// through a free column between, which puts its two halves on either side
// of the other and breaks the cycle.
// maxSoftParts bounds the crossing-avoiding preferences of assignTracks
// (quadratic pairs, each checked for a cycle).
const maxSoftParts = 60

func assignTracks(cs []*crossing, trackSep float64) int {
	type part struct {
		c        *crossing
		half     int // 0 whole, 1 upper, 2 lower
		lo, hi   float64
		top, bot float64
	}
	var occupied []float64
	for _, c := range cs {
		occupied = append(occupied, c.top, c.bot)
	}
	near := func(a, b float64) bool { return math.Abs(a-b) < trackSep }
	for attempt := 0; attempt <= len(cs); attempt++ {
		var ps []*part
		for _, c := range cs {
			add := func(half int, t, b float64) {
				if math.Abs(t-b) < 1e-6 {
					return
				}
				ps = append(ps, &part{c: c, half: half, lo: math.Min(t, b), hi: math.Max(t, b), top: t, bot: b})
			}
			if c.split {
				add(1, c.top, c.mid)
				add(2, c.mid, c.bot)
			} else {
				add(0, c.top, c.bot)
			}
		}
		if len(ps) == 0 {
			return 0
		}
		indeg := make([]int, len(ps))
		below := make([][]int, len(ps))
		for i, a := range ps {
			for j, b := range ps {
				if i == j {
					continue
				}
				// A detour's middle column runs only between its own two
				// tracks, so its halves are ordered directly, upper first;
				// the column rule is for runs that reach the gap's ends.
				if a.c == b.c {
					if a.half == 1 && b.half == 2 {
						below[i] = append(below[i], j)
						indeg[j]++
					}
					continue
				}
				if near(a.top, b.bot) {
					below[i] = append(below[i], j) // a turns above b
					indeg[j]++
				}
			}
		}
		// Then, where no rule above decides, a run that passes over
		// another crossing's drop column turns below it, and one that
		// passes over its rise column turns above it, so neither crosses
		// the other's vertical (a fan leaving one node stays uncrossed).
		// A preference that would close a cycle is dropped; many parts
		// in one gap skip this (ugly, not wrong).
		if len(ps) <= maxSoftParts {
			reaches := func(from, to int) bool {
				seen := make([]bool, len(ps))
				stack := []int{from}
				for len(stack) > 0 {
					v := stack[len(stack)-1]
					stack = stack[:len(stack)-1]
					if v == to {
						return true
					}
					for _, w := range below[v] {
						if !seen[w] {
							seen[w] = true
							stack = append(stack, w)
						}
					}
				}
				return false
			}
			inside := func(x float64, a *part) bool { return x > a.lo+1e-6 && x < a.hi-1e-6 }
			for i, a := range ps {
				for j, b := range ps {
					if i == j || a.c == b.c {
						continue
					}
					// b's drop column reaches the gap's top unless b is a
					// detour's lower half; its rise column reaches the
					// bottom unless b is an upper half.
					over := b.half != 2 && inside(b.top, a)  // a turns below b
					under := b.half != 1 && inside(b.bot, a) // a turns above b
					if over == under {
						continue // a crossing either way, or none
					}
					hi, lo := j, i
					if under {
						hi, lo = i, j
					}
					if reaches(lo, hi) {
						continue
					}
					below[hi] = append(below[hi], lo)
					indeg[lo]++
				}
			}
		}
		// Topological order, lowest left end first.
		order := make([]int, 0, len(ps))
		done := make([]bool, len(ps))
		stuck := -1
		for len(order) < len(ps) {
			pick := -1
			for i := range ps {
				if !done[i] && indeg[i] == 0 && (pick < 0 || ps[i].lo < ps[pick].lo) {
					pick = i
				}
			}
			if pick < 0 {
				for i := range ps {
					if !done[i] && !ps[i].c.split && (stuck < 0 || ps[i].lo < ps[stuck].lo) {
						stuck = i
					}
				}
				break
			}
			done[pick] = true
			order = append(order, pick)
			for _, j := range below[pick] {
				indeg[j]--
			}
		}
		if len(order) < len(ps) && stuck >= 0 && attempt < len(cs) {
			c := ps[stuck].c
			c.split, c.mid = true, freeColumn((c.top+c.bot)/2, occupied, trackSep)
			occupied = append(occupied, c.mid)
			continue
		}
		// Anything left in a cycle (none should be) goes last, in order.
		for i := range ps {
			if !done[i] {
				order = append(order, i)
			}
		}
		minTrack := make([]int, len(ps))
		var runs [][][2]float64 // per track, occupied horizontal runs
		n := 0
		for _, i := range order {
			a := ps[i]
			t := minTrack[i]
			for ; ; t++ {
				for len(runs) <= t {
					runs = append(runs, nil)
				}
				free := true
				for _, r := range runs[t] {
					if a.lo < r[1]+trackSep && r[0] < a.hi+trackSep {
						free = false
						break
					}
				}
				if free {
					break
				}
			}
			runs[t] = append(runs[t], [2]float64{a.lo, a.hi})
			switch a.half {
			case 0, 1:
				a.c.track = t
			case 2:
				a.c.track2 = t
			}
			n = max(n, t+1)
			for _, j := range below[i] {
				minTrack[j] = max(minTrack[j], t+1)
			}
		}
		return n
	}
	return 0
}

// freeColumn is the position nearest to x that keeps trackSep from every
// occupied column.
func freeColumn(x float64, occupied []float64, trackSep float64) float64 {
	for d := 0.0; d < 1000; d += 0.05 {
		for _, c := range []float64{x + d, x - d} {
			ok := true
			for _, o := range occupied {
				if math.Abs(c-o) < trackSep {
					ok = false
					break
				}
			}
			if ok {
				return c
			}
		}
	}
	return x
}

func (l *layouter) shapeOf(n int) mr.Shape {
	if n < len(l.nodes) {
		return l.nodes[n].Shape
	}
	return mr.Rect
}

// spreadOf is the share of node n's faces its link ends may use.
func (l *layouter) spreadOf(n int) float64 {
	if n < len(l.spreads) && l.spreads[n] > 0 {
		return l.spreads[n]
	}
	if l.faceSpread > 0 {
		return l.faceSpread
	}
	return portSpreadOf(l.shapeOf(n))
}

// portSpreadOf narrows the port range to the part of a face a straight
// link can reach it on: curved and pointed shapes, and the slanted ones,
// whose slants take up to 0.3 of the width at each end (outline), leave the
// middle 0.4 flat.
func portSpreadOf(s mr.Shape) float64 {
	switch s {
	case mr.Rhombus, mr.Circle, mr.DoubleCircle, mr.Hexagon,
		mr.Parallelogram, mr.ParallelogramAlt, mr.Trapezoid, mr.TrapezoidAlt, mr.Asymmetric:
		return 0.4
	}
	return portSpread
}

// dedupe drops repeated points and points in the middle of a straight run:
// a path is its corners. (A run split at a band edge read, to a spacing
// check, as a segment ending beside a link it actually crosses.)
func dedupe(pts []pt) []pt {
	pts = dedupeRepeats(pts)
	if len(pts) < 3 {
		return pts
	}
	out := []pt{pts[0]}
	for i := 1; i+1 < len(pts); i++ {
		a, b, c := out[len(out)-1], pts[i], pts[i+1]
		if math.Abs((b.X-a.X)*(c.Y-b.Y)-(b.Y-a.Y)*(c.X-b.X)) < 1e-9 &&
			(b.X-a.X)*(c.X-b.X)+(b.Y-a.Y)*(c.Y-b.Y) >= 0 {
			continue // b lies on a straight run from a to c
		}
		out = append(out, b)
	}
	return append(out, pts[len(pts)-1])
}

func dedupeRepeats(pts []pt) []pt {
	out := pts[:0]
	for i, p := range pts {
		if i > 0 && math.Abs(p.X-out[len(out)-1].X) < 1e-9 && math.Abs(p.Y-out[len(out)-1].Y) < 1e-9 {
			continue
		}
		out = append(out, p)
	}
	return out
}
