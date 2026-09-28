package raster

import (
	"image"
	"image/color"
	"image/draw"
	"math"

	"golang.org/x/image/vector"

	mr "github.com/nlink-jp/mermaid-render"
)

var (
	colBG      = color.RGBA{0xff, 0xff, 0xff, 0xff}
	colCard    = color.RGBA{0xd0, 0xd5, 0xdd, 0xff}
	colNode    = color.RGBA{0xee, 0xf2, 0xf7, 0xff}
	colStroke  = color.RGBA{0x5b, 0x6b, 0x7f, 0xff}
	colText    = color.RGBA{0x1f, 0x29, 0x33, 0xff}
	colEdge    = color.RGBA{0x4a, 0x55, 0x68, 0xff}
	colFrame   = color.RGBA{0xf7, 0xf8, 0xfa, 0xff}
	colFrameSt = color.RGBA{0xa0, 0xae, 0xc0, 0xff}
)

// Stroke widths and marks, in em.
const (
	lineW      = 0.09
	thickW     = 0.2
	arrowLen   = 0.7
	arrowHalfW = 0.32
	markR      = 0.28 // circle head radius
	markX      = 0.3  // cross head half size
)

// canvas draws em-unit geometry onto an image. Every primitive rasterizes
// only its own bounding box.
type canvas struct {
	img        *image.RGBA
	em         float64 // pixels per em
	offX, offY float64 // em
	// rels marks an ER diagram's links: they end in cardinality markers.
	rels map[*mr.Link]*mr.Relationship
}

func (c *canvas) px(p Pt) (float32, float32) {
	return float32((p.X + c.offX) * c.em), float32((p.Y + c.offY) * c.em)
}

func (c *canvas) fill(pts []Pt, col color.Color) {
	if len(pts) < 3 {
		return
	}
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, p := range pts {
		x, y := c.px(p)
		minX, minY = math.Min(minX, float64(x)), math.Min(minY, float64(y))
		maxX, maxY = math.Max(maxX, float64(x)), math.Max(maxY, float64(y))
	}
	r := image.Rect(int(math.Floor(minX)), int(math.Floor(minY)), int(math.Ceil(maxX))+1, int(math.Ceil(maxY))+1)
	r = r.Intersect(c.img.Bounds())
	if r.Empty() {
		return
	}
	z := vector.NewRasterizer(r.Dx(), r.Dy())
	for i, p := range pts {
		x, y := c.px(p)
		x, y = x-float32(r.Min.X), y-float32(r.Min.Y)
		if i == 0 {
			z.MoveTo(x, y)
		} else {
			z.LineTo(x, y)
		}
	}
	z.ClosePath()
	z.Draw(c.img, r, image.NewUniform(col), image.Point{})
}

// segment strokes a straight segment of width w.
func (c *canvas) segment(a, b Pt, w float64, col color.Color) {
	dx, dy := b.X-a.X, b.Y-a.Y
	l := math.Hypot(dx, dy)
	if l == 0 {
		return
	}
	nx, ny := -dy/l*w/2, dx/l*w/2
	c.fill([]Pt{{a.X + nx, a.Y + ny}, {b.X + nx, b.Y + ny}, {b.X - nx, b.Y - ny}, {a.X - nx, a.Y - ny}}, col)
}

// polyline strokes a path; dotted draws it in dashes.
func (c *canvas) polyline(pts []Pt, w float64, dotted bool, col color.Color) {
	if !dotted {
		for i := 0; i+1 < len(pts); i++ {
			c.segment(pts[i], pts[i+1], w, col)
			if i+2 < len(pts) {
				c.fill(ellipse(pts[i+1].X, pts[i+1].Y, w/2, w/2, 12), col)
			}
		}
		return
	}
	const on, off = 0.35, 0.25
	phase := 0.0
	for i := 0; i+1 < len(pts); i++ {
		a, b := pts[i], pts[i+1]
		l := math.Hypot(b.X-a.X, b.Y-a.Y)
		for s := -phase; s < l; s += on + off {
			s0, s1 := math.Max(s, 0), math.Min(s+on, l)
			if s1 > s0 {
				c.segment(Pt{a.X + (b.X-a.X)*s0/l, a.Y + (b.Y-a.Y)*s0/l}, Pt{a.X + (b.X-a.X)*s1/l, a.Y + (b.Y-a.Y)*s1/l}, w, col)
			}
		}
		phase = math.Mod(phase+l, on+off)
	}
}

func (c *canvas) outlineShape(pts []Pt, fillCol, strokeCol color.Color, w float64, dashed bool) {
	if fillCol != nil {
		c.fill(pts, fillCol)
	}
	if strokeCol != nil {
		closed := append(append([]Pt(nil), pts...), pts[0])
		c.polyline(closed, w, dashed, strokeCol)
		if !dashed {
			c.fill(ellipse(pts[0].X, pts[0].Y, w/2, w/2, 12), strokeCol)
		}
	}
}

// node draws a node's shape (its label is drawn by the caller).
func (c *canvas) node(n NodeBox) {
	r := n.Box
	if n.Shape == mr.Cylinder {
		c.cylinder(r)
		return
	}
	c.outlineShape(outline(n.Shape, r, n.Slant), colNode, colStroke, lineW, false)
	switch n.Shape {
	case mr.Subroutine:
		c.segment(Pt{r.X0 + 0.4, r.Y0}, Pt{r.X0 + 0.4, r.Y1}, lineW, colStroke)
		c.segment(Pt{r.X1 - 0.4, r.Y0}, Pt{r.X1 - 0.4, r.Y1}, lineW, colStroke)
	case mr.DoubleCircle:
		cx, cy := r.Center().X, r.Center().Y
		in := ellipse(cx, cy, r.W()/2-0.3, r.H()/2-0.3, 64)
		c.polyline(append(in, in[0]), lineW, false, colStroke)
	}
}

// cylinder draws a drum inside its box: elliptic caps of height 2*cylCap
// whose outer halves touch the box's top and bottom. Links clip at the box,
// which the drawing fills at the cap's middle.
const cylCap = 0.35

func (c *canvas) cylinder(r Rect) {
	cx, rx := r.Center().X, r.W()/2
	arc := func(cy, from, to float64) []Pt {
		var pts []Pt
		for i := 0; i <= 32; i++ {
			a := from + (to-from)*float64(i)/32
			pts = append(pts, Pt{cx + rx*math.Cos(a), cy + cylCap*math.Sin(a)})
		}
		return pts
	}
	top, bot := r.Y0+cylCap, r.Y1-cylCap
	// Body: the top cap's upper half, down the right, the bottom cap's lower
	// half, up the left.
	body := append(arc(top, math.Pi, 2*math.Pi), arc(bot, 0, math.Pi)...)
	c.outlineShape(body, colNode, colStroke, lineW, false)
	// The top cap's front edge.
	c.polyline(arc(top, 0, math.Pi), lineW, false, colStroke)
}

func labelCenter(n NodeBox) Pt {
	p := n.Box.Center()
	switch n.Shape {
	case mr.Cylinder:
		p.Y += 0.2
	case mr.Asymmetric:
		p.X += n.Slant / 2 // half the notch
	}
	return p
}

// edge draws a link's line and heads.
func (c *canvas) edge(e EdgePath) {
	pts := append([]Pt(nil), e.Points...)
	if len(pts) < 2 {
		return
	}
	if c.rels[e.Link] != nil {
		// An ER relationship: the line runs to the entity; markers sit on it.
		c.polyline(pts, lineW, e.Link.Stroke == mr.Dotted, colEdge)
		c.heads(e)
		return
	}
	w := lineW
	if e.Link.Stroke == mr.Thick {
		w = thickW
	}
	dotted := e.Link.Stroke == mr.Dotted
	// Pull the line back where a head sits, so it does not show through.
	trim := func(h mr.Head, tip, prev Pt) Pt {
		d := math.Hypot(tip.X-prev.X, tip.Y-prev.Y)
		cut := 0.0
		switch h {
		case mr.Arrow:
			cut = arrowLen * 0.8
		case mr.CircleHead:
			cut = 2 * markR
		}
		if d == 0 || cut == 0 {
			return tip
		}
		cut = math.Min(cut, d*0.9)
		return Pt{tip.X - (tip.X-prev.X)*cut/d, tip.Y - (tip.Y-prev.Y)*cut/d}
	}
	n := len(pts)
	endTip, endPrev := pts[n-1], pts[n-2]
	startTip, startNext := pts[0], pts[1]
	pts[n-1] = trim(e.Link.End, endTip, endPrev)
	pts[0] = trim(e.Link.Start, startTip, startNext)
	c.polyline(pts, w, dotted, colEdge)
	c.heads(e)
}

// heads draws a link's heads alone. Render draws them again after the
// frame titles, whose backgrounds would otherwise hide a head arriving
// under a title.
func (c *canvas) heads(e EdgePath) {
	pts := e.Points
	if len(pts) < 2 {
		return
	}
	n := len(pts)
	if r := c.rels[e.Link]; r != nil {
		c.marker(r.ToCard, pts[n-1], pts[n-2])
		c.marker(r.FromCard, pts[0], pts[1])
		return
	}
	c.head(e.Link.End, pts[n-1], pts[n-2])
	c.head(e.Link.Start, pts[0], pts[1])
}

func (c *canvas) head(h mr.Head, tip, from Pt) {
	dx, dy := tip.X-from.X, tip.Y-from.Y
	l := math.Hypot(dx, dy)
	if l == 0 {
		return
	}
	dx, dy = dx/l, dy/l
	switch h {
	case mr.Arrow:
		b := Pt{tip.X - dx*arrowLen, tip.Y - dy*arrowLen}
		c.fill([]Pt{tip, {b.X - dy*arrowHalfW, b.Y + dx*arrowHalfW}, {b.X + dy*arrowHalfW, b.Y - dx*arrowHalfW}}, colEdge)
	case mr.CircleHead:
		ctr := Pt{tip.X - dx*markR, tip.Y - dy*markR}
		ring := ellipse(ctr.X, ctr.Y, markR, markR, 24)
		c.fill(ring, colBG)
		c.polyline(append(ring, ring[0]), lineW, false, colEdge)
	case mr.CrossHead:
		ctr := Pt{tip.X - dx*markX, tip.Y - dy*markX}
		for _, s := range []float64{1, -1} {
			ax, ay := (dx-s*dy)*markX, (dy+s*dx)*markX
			c.segment(Pt{ctr.X - ax, ctr.Y - ay}, Pt{ctr.X + ax, ctr.Y + ay}, lineW*1.3, colEdge)
		}
	}
}

func (c *canvas) frame(f FrameBox) {
	c.outlineShape(roundRect(f.Box, 0.4), colFrame, colFrameSt, 0.07, true)
}

func (c *canvas) labelBG(r Rect) {
	c.fill(roundRect(r, 0.2), colBG)
}

var _ draw.Image = (*image.RGBA)(nil)
