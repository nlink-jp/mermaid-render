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
	// Where a frame's title sits in abstract coordinates.
	titleLow := l.f.Direction == mr.TB  // before the first layer
	titleHigh := l.f.Direction == mr.BT // after the last layer (drawn on top once flipped)
	tspace := func(c *cluster) float64 {
		if c.titleH == 0 {
			return 0
		}
		return c.titleH + titleGap
	}
	// Gaps between layers make room for frame edges and titles.
	gapAfter := make([]float64, nl)
	for k := range gapAfter {
		below, above := 0.0, 0.0
		for _, c := range l.clusters {
			if c.r1 == k {
				e := framePad
				if titleHigh {
					e += tspace(c)
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
		g := rankGap
		if below > 0 || above > 0 {
			g = math.Max(g, below+above+frameSep)
		}
		gapAfter[k] = g
	}
	// Horizontal layouts put the title across the rank axis; a frame must
	// span it.
	start := make([]float64, nl)
	compute := func() {
		v := 0.0
		for k := 0; k < nl; k++ {
			start[k] = v
			v += band[k] + gapAfter[k]
		}
	}
	compute()
	if l.horiz {
		for _, c := range l.clusters {
			span := start[c.r1] + band[c.r1] - start[c.r0] + 2*framePad
			if need := c.titleW + 2*framePad; need > span && c.r1 < nl-1 {
				gapAfter[c.r1] += need - span
			}
		}
		compute()
	}
	center := func(k int) float64 { return start[k] + band[k]/2 }

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
		if l.horiz {
			if need := c.titleW + 2*framePad; r.H() < need {
				r.Y1 = r.Y0 + need
			}
		}
		frameRect[ci] = r
	}

	// Node boxes in abstract coordinates.
	nodeRect := make([]Rect, len(l.nw))
	baseHalf := make([]float64, len(l.nw))
	for i := range l.nw {
		it := l.nodeItem[i]
		cross, rs := l.nw[i], l.nh[i]
		if l.horiz {
			cross, rs = l.nh[i], l.nw[i]
		}
		baseHalf[i] = cross / 2
		cy := center(it.layer)
		nodeRect[i] = Rect{X0: it.x - cross/2, X1: it.x + cross/2, Y0: cy - rs/2, Y1: cy + rs/2}
	}

	// Ports: each link attaching to a face gets its own point on it,
	// ordered by where the link goes next.
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
	for _, ch := range l.chains {
		if ch.self {
			continue
		}
		lowEnd, highEnd := ch.from, ch.to
		if !ch.fromLow {
			lowEnd, highEnd = ch.to, ch.from
		}
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
	for _, k := range faceKeys {
		ps := faces[k]
		sort.SliceStable(ps, func(i, j int) bool { return ps[i].next < ps[j].next })
		var c0, width float64
		if k[0] == 0 {
			c0, width = l.nodeItem[k[1]].x, 2*baseHalf[k[1]]*portSpreadOf(l.shapeOf(k[1]))
		} else {
			c := l.clusters[k[1]]
			c0, width = (c.L+c.R)/2, (c.R-c.L)*portSpread
		}
		for i, p := range ps {
			x := c0
			if len(ps) > 1 {
				x = c0 - width/2 + width*(float64(i)+0.5)/float64(len(ps))
			}
			v := portX[p.ch]
			if p.low {
				v[0] = x
			} else {
				v[1] = x
			}
			portX[p.ch] = v
		}
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
	loopsAt := map[int][]int{}
	for i, ch := range l.chains {
		lk := l.f.Links[ch.link]
		if ch.self {
			n := ch.from.node
			r := nodeRect[n]
			used := loops[n]
			edge := l.nodeItem[n].x + baseHalf[n] + used
			reach := loopReach
			o := loopOffset(len(loopsAt[n]))
			y0, y1 := r.Center().Y-o, r.Center().Y+o
			p := &path{link: lk, clipLow: -1, clipHigh: -1, loopOfNode: n}
			loopsAt[n] = append(loopsAt[n], i)
			face := l.nodeItem[n].x + baseHalf[n]
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
		lowEnd, highEnd := ch.from, ch.to
		if !ch.fromLow {
			lowEnd, highEnd = ch.to, ch.from
		}
		px := portX[ch]
		p := &path{link: lk, clipLow: -1, clipHigh: -1, reversed: !ch.fromLow, loopOfNode: -1}
		if lowEnd.node >= 0 {
			n := lowEnd.node
			p.pts = append(p.pts, Pt{px[0], nodeRect[n].Center().Y}, Pt{px[0], start[ch.lo] + band[ch.lo]})
			p.clipLow = n
		} else {
			p.pts = append(p.pts, Pt{px[0], frameRect[lowEnd.cluster].Y1})
		}
		for _, it := range ch.items {
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
		out.Nodes = append(out.Nodes, NodeBox{ID: n.ID, Label: n.Label, Shape: n.Shape, Box: vis[i]})
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
			pts[0] = clipAt(l.shapeOf(p.clipLow), vis[p.clipLow], pts[1], pts[0])
		}
		if p.clipHigh >= 0 && len(pts) >= 2 {
			n := len(pts)
			pts[n-1] = clipAt(l.shapeOf(p.clipHigh), vis[p.clipHigh], pts[n-2], pts[n-1])
		}
		if p.loopOfNode >= 0 {
			// The loop starts and ends on the node's face.
			n := p.loopOfNode
			pts[0] = clipAt(l.shapeOf(n), vis[n], pts[1], tp(p.loopIn[0]))
			pts[len(pts)-1] = clipAt(l.shapeOf(n), vis[n], pts[len(pts)-2], tp(p.loopIn[1]))
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
