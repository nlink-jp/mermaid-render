package raster

import (
	"math"
	"sort"

	mr "github.com/nlink-jp/mermaid-render"
)

// place turns layers and cross coordinates into the final layout: rank
// positions, frames, link paths through ports, then the direction's
// transform and clipping at the node shapes.
func (l *layouter) place() *Layout {
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
	faces := map[[3]int][]*port{} // (kind 0 node / 1 cluster, index, side 0 high / 1 low)
	var faceKeys [][3]int
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
	portX := map[*chain][2]float64{} // [low end, high end]
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
			c0, width := l.nodeItem[k[1]].x, 2*baseHalf[k[1]]*portSpreadOf(l.shapeOf(k[1]))
			lo, hi = c0-width/2, c0+width/2
			layer = l.rank[k[1]]
		} else {
			c := l.clusters[k[1]]
			lo, hi = c.L+framePad, c.R-framePad
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
		xs := alignPorts(lo, hi, want, occ)
		if xs == nil {
			xs = pickPorts(lo, hi, len(ps), occ)
		}
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
		tracks[k] = assignTracks(cs)
	}

	// Where a frame's title sits in abstract coordinates.
	titleLow := l.f.Direction == mr.TB  // before the first layer
	titleHigh := l.f.Direction == mr.BT // after the last layer (drawn on top once flipped)
	tspace := func(c *cluster) float64 {
		if c.titleH == 0 {
			return 0
		}
		return c.titleH + titleGap
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
				e := framePad
				if titleHigh {
					e += tspace(c)
				}
				if l.horiz {
					span := start[k] + band[k] - start[c.r0] + 2*framePad
					if need := c.titleW + 2*framePad; need > span {
						extra[ci] = need - span
						e += extra[ci]
					}
				}
				below = math.Max(below, e)
			}
			if c.r0 == k+1 {
				e := framePad
				if titleLow {
					e += tspace(c)
				}
				above = math.Max(above, e)
			}
		}
		margin := 0.0
		if below > 0 || above > 0 {
			margin = frameSep / 2
		}
		trackTop[k] = below + margin + trackIn
		need := rankGap
		if below > 0 || above > 0 {
			need = math.Max(need, below+above+frameSep)
		}
		if tracks[k] > 0 {
			need = math.Max(need, below+above+2*margin+trackIn+float64(tracks[k]-1)*trackSep+trackOut)
		}
		gapAfter[k] = need
		v += band[k] + need
	}
	center := func(k int) float64 { return start[k] + band[k]/2 }
	trackY := func(k, t int) float64 { return start[k] + band[k] + trackTop[k] + float64(t)*trackSep }

	// Frames, in abstract coordinates (cross = x, rank = y).
	frameRect := make([]Rect, len(l.clusters))
	for ci, c := range l.clusters {
		r := Rect{X0: c.L, X1: c.R, Y0: start[c.r0] - framePad, Y1: start[c.r1] + band[c.r1] + framePad}
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
	nodeRect := make([]Rect, len(l.nw))
	for i := range l.nw {
		it := l.nodeItem[i]
		rs := l.nh[i]
		if l.horiz {
			rs = l.nw[i]
		}
		cy := center(it.layer)
		nodeRect[i] = Rect{X0: it.x - baseHalf[i], X1: it.x + baseHalf[i], Y0: cy - rs/2, Y1: cy + rs/2}
	}

	// Paths in abstract coordinates, low end to high end.
	type path struct {
		pts        []Pt
		clipLow    int // node to clip the first segment at, or -1
		clipHigh   int
		label      Rect
		hasLabel   bool
		reversed   bool
		link       *mr.Link
		loopOfNode int
		loopIn     [2]Pt // abstract points inside the node, level with the loop's ends
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
			reach := loopReach
			y0, y1 := r.Center().Y-ch.loopOff, r.Center().Y+ch.loopOff
			p := &path{link: lk, clipLow: -1, clipHigh: -1, loopOfNode: n}
			p.pts = []Pt{{face, y0}, {edge + reach, y0}, {edge + reach, y1}, {face, y1}}
			p.loopIn = [2]Pt{{l.nodeItem[n].x, y0}, {l.nodeItem[n].x, y1}}
			extra := reach
			if ch.lw > 0 {
				cross, rs := ch.lw, ch.lh
				if l.horiz {
					cross, rs = ch.lh, ch.lw
				}
				cx := edge + reach + cross/2
				cy := (y0 + y1) / 2
				p.label = Rect{cx - cross/2, cy - rs/2, cx + cross/2, cy + rs/2}
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
			p.pts = append(p.pts, Pt{px[0], nodeRect[n].Center().Y}, Pt{px[0], start[ch.lo] + band[ch.lo]})
			p.clipLow = n
		} else {
			p.pts = append(p.pts, Pt{px[0], frameRect[lowEnd.cluster].Y1})
		}
		for ci, c := range chainCross[ch] {
			k := ch.lo + ci
			switch {
			case c.split:
				y1, y2 := trackY(k, c.track), trackY(k, c.track2)
				p.pts = append(p.pts, Pt{c.top, y1}, Pt{c.mid, y1}, Pt{c.mid, y2}, Pt{c.bot, y2})
			case c.track >= 0:
				y := trackY(k, c.track)
				p.pts = append(p.pts, Pt{c.top, y}, Pt{c.bot, y})
			}
			if ci < len(ch.items) {
				it := ch.items[ci]
				a, b := start[it.layer], start[it.layer]+band[it.layer]
				p.pts = append(p.pts, Pt{it.x, a})
				if b > a {
					p.pts = append(p.pts, Pt{it.x, b})
				}
				if it == ch.label {
					cross, rs := ch.lw, ch.lh
					if l.horiz {
						cross, rs = ch.lh, ch.lw
					}
					cy := center(it.layer)
					p.label = Rect{it.x - cross/2, cy - rs/2, it.x + cross/2, cy + rs/2}
					p.hasLabel = true
				}
			}
		}
		if highEnd.node >= 0 {
			n := highEnd.node
			p.pts = append(p.pts, Pt{px[1], start[ch.hi]}, Pt{px[1], nodeRect[n].Center().Y})
			p.clipHigh = n
		} else {
			p.pts = append(p.pts, Pt{px[1], frameRect[highEnd.cluster].Y0})
		}
		paths[i] = p
	}

	// Transform to the diagram's direction.
	tp := func(p Pt) Pt {
		switch l.f.Direction {
		case mr.BT:
			return Pt{p.X, -p.Y}
		case mr.LR:
			return Pt{p.Y, p.X}
		case mr.RL:
			return Pt{-p.Y, p.X}
		}
		return p
	}
	tr := func(r Rect) Rect {
		a, b := tp(Pt{r.X0, r.Y0}), tp(Pt{r.X1, r.Y1})
		return Rect{math.Min(a.X, b.X), math.Min(a.Y, b.Y), math.Max(a.X, b.X), math.Max(a.Y, b.Y)}
	}
	out := &Layout{}
	vis := make([]Rect, len(l.nw))
	for i := range l.nw {
		vis[i] = tr(nodeRect[i])
	}
	for i, n := range l.nodes {
		out.Nodes = append(out.Nodes, NodeBox{ID: n.ID, Label: n.Label, Shape: n.Shape, Box: vis[i],
			Slant: math.Min(l.slant0[i], 0.3*vis[i].W())})
	}
	for ci, c := range l.clusters {
		fr := tr(frameRect[ci])
		fb := FrameBox{ID: c.sg.ID, Title: c.sg.Title, Box: fr}
		if c.titleH > 0 {
			x0 := fr.X0 + framePad*0.7
			y0 := fr.Y0 + titleGap
			fb.TitleBox = Rect{x0, y0, x0 + c.titleW, y0 + c.titleH}
		}
		out.Frames = append(out.Frames, fb)
	}
	for _, p := range paths {
		pts := make([]Pt, len(p.pts))
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
		e := EdgePath{Link: p.link, Points: dedupe(pts), Label: p.link.Label}
		if p.hasLabel {
			e.LabelBox = tr(p.label)
		}
		out.Edges = append(out.Edges, e)
	}
	// Move to the origin.
	bb := Rect{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	grow := func(r Rect) {
		bb = Rect{math.Min(bb.X0, r.X0), math.Min(bb.Y0, r.Y0), math.Max(bb.X1, r.X1), math.Max(bb.Y1, r.Y1)}
	}
	for _, n := range out.Nodes {
		grow(n.Box)
	}
	for _, f := range out.Frames {
		grow(f.Box)
	}
	for _, e := range out.Edges {
		for _, p := range e.Points {
			grow(Rect{p.X, p.Y, p.X, p.Y})
		}
		if e.Label != "" {
			grow(e.LabelBox)
		}
	}
	if len(out.Nodes) == 0 && len(out.Frames) == 0 {
		bb = Rect{}
	}
	shift := func(r Rect) Rect { return Rect{r.X0 - bb.X0, r.Y0 - bb.Y0, r.X1 - bb.X0, r.Y1 - bb.Y0} }
	for i := range out.Nodes {
		out.Nodes[i].Box = shift(out.Nodes[i].Box)
	}
	for i := range out.Frames {
		out.Frames[i].Box = shift(out.Frames[i].Box)
		if out.Frames[i].TitleBox != (Rect{}) {
			out.Frames[i].TitleBox = shift(out.Frames[i].TitleBox)
		}
	}
	for i := range out.Edges {
		for j := range out.Edges[i].Points {
			p := out.Edges[i].Points[j]
			out.Edges[i].Points[j] = Pt{p.X - bb.X0, p.Y - bb.Y0}
		}
		if out.Edges[i].Label != "" {
			out.Edges[i].LabelBox = shift(out.Edges[i].LabelBox)
		}
	}
	out.W, out.H = bb.W(), bb.H()
	return out
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
func alignPorts(lo, hi float64, want, occ []float64) []float64 {
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
func pickPorts(lo, hi float64, n int, occ []float64) []float64 {
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

func (l *layouter) slantOf(n int, vis []Rect) float64 {
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
func assignTracks(cs []*crossing) int {
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
			c.split, c.mid = true, freeColumn((c.top+c.bot)/2, occupied)
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
func freeColumn(x float64, occupied []float64) float64 {
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

func dedupe(pts []Pt) []Pt {
	out := pts[:0]
	for i, p := range pts {
		if i > 0 && math.Abs(p.X-out[len(out)-1].X) < 1e-9 && math.Abs(p.Y-out[len(out)-1].Y) < 1e-9 {
			continue
		}
		out = append(out, p)
	}
	return out
}
