package raster

import (
	"math"

	mr "github.com/nlink-jp/mermaid-render"
)

// outline is the polygon a shape is drawn as, in em. Curved shapes are
// approximated finely enough that a link ending on the outline touches the
// drawn curve. k is the slant of the slanted shapes (NodeBox.Slant).
func outline(s mr.Shape, r Rect, k float64) []Pt {
	x0, y0, x1, y1 := r.X0, r.Y0, r.X1, r.Y1
	w, h := r.W(), r.H()
	cx, cy := r.Center().X, r.Center().Y
	switch s {
	case mr.Rhombus:
		return []Pt{{cx, y0}, {x1, cy}, {cx, y1}, {x0, cy}}
	case mr.Hexagon:
		return []Pt{{x0 + k, y0}, {x1 - k, y0}, {x1, cy}, {x1 - k, y1}, {x0 + k, y1}, {x0, cy}}
	case mr.Parallelogram:
		return []Pt{{x0 + k, y0}, {x1, y0}, {x1 - k, y1}, {x0, y1}}
	case mr.ParallelogramAlt:
		return []Pt{{x0, y0}, {x1 - k, y0}, {x1, y1}, {x0 + k, y1}}
	case mr.Trapezoid:
		return []Pt{{x0 + k, y0}, {x1 - k, y0}, {x1, y1}, {x0, y1}}
	case mr.TrapezoidAlt:
		return []Pt{{x0, y0}, {x1, y0}, {x1 - k, y1}, {x0 + k, y1}}
	case mr.Asymmetric:
		return []Pt{{x0, y0}, {x1, y0}, {x1, y1}, {x0, y1}, {x0 + k, cy}}
	case mr.Circle, mr.DoubleCircle:
		return ellipse(cx, cy, w/2, h/2, 64)
	case mr.Stadium:
		return roundRect(r, h/2)
	case mr.Round:
		return roundRect(r, math.Min(0.5, h/2))
	}
	return []Pt{{x0, y0}, {x1, y0}, {x1, y1}, {x0, y1}}
}

func ellipse(cx, cy, rx, ry float64, n int) []Pt {
	pts := make([]Pt, n)
	for i := range pts {
		a := 2 * math.Pi * float64(i) / float64(n)
		pts[i] = Pt{cx + rx*math.Cos(a), cy + ry*math.Sin(a)}
	}
	return pts
}

func roundRect(r Rect, rad float64) []Pt {
	rad = math.Min(rad, math.Min(r.W(), r.H())/2)
	var pts []Pt
	corner := func(cx, cy, a0 float64) {
		for i := 0; i <= 8; i++ {
			a := a0 + float64(i)*math.Pi/2/8
			pts = append(pts, Pt{cx + rad*math.Cos(a), cy + rad*math.Sin(a)})
		}
	}
	corner(r.X1-rad, r.Y0+rad, -math.Pi/2)
	corner(r.X1-rad, r.Y1-rad, 0)
	corner(r.X0+rad, r.Y1-rad, math.Pi/2)
	corner(r.X0+rad, r.Y0+rad, math.Pi)
	return pts
}

// clipAt moves along the segment from outside toward inside and returns
// the first point on the shape's outline. If the segment misses the outline
// (it should not), the box's edge is used.
func clipAt(s mr.Shape, r Rect, k float64, outside, inside Pt) Pt {
	poly := outline(s, r, k)
	if p, ok := firstHit(poly, outside, inside); ok {
		return p
	}
	// Every outline holds the box's centre: aim there instead.
	if p, ok := firstHit(poly, outside, r.Center()); ok {
		return p
	}
	if p, ok := firstHit(outline(mr.Rect, r, 0), outside, inside); ok {
		return p
	}
	return inside
}

func firstHit(poly []Pt, a, b Pt) (Pt, bool) {
	best, found := math.Inf(1), false
	for i := range poly {
		c, d := poly[i], poly[(i+1)%len(poly)]
		if t, ok := segT(a, b, c, d); ok && t < best {
			best, found = t, true
		}
	}
	if !found {
		return Pt{}, false
	}
	return Pt{a.X + (b.X-a.X)*best, a.Y + (b.Y-a.Y)*best}, true
}

// segT is where segment ab meets segment cd, as a fraction of ab.
func segT(a, b, c, d Pt) (float64, bool) {
	rx, ry := b.X-a.X, b.Y-a.Y
	sx, sy := d.X-c.X, d.Y-c.Y
	den := rx*sy - ry*sx
	if math.Abs(den) < 1e-12 {
		return 0, false
	}
	t := ((c.X-a.X)*sy - (c.Y-a.Y)*sx) / den
	u := ((c.X-a.X)*ry - (c.Y-a.Y)*rx) / den
	if t < -1e-9 || t > 1+1e-9 || u < -1e-9 || u > 1+1e-9 {
		return 0, false
	}
	return t, true
}

// segmentHitsRect reports whether segment ab passes through r's interior
// (shrunk by eps so that touching the border does not count).
func segmentHitsRect(a, b Pt, r Rect, eps float64) bool {
	r = Rect{r.X0 + eps, r.Y0 + eps, r.X1 - eps, r.Y1 - eps}
	if r.W() <= 0 || r.H() <= 0 {
		return false
	}
	// Wholly beside the rectangle, it can touch no edge (the common case,
	// and the one the checks run thousands of times).
	if math.Max(a.X, b.X) < r.X0 || math.Min(a.X, b.X) > r.X1 || math.Max(a.Y, b.Y) < r.Y0 || math.Min(a.Y, b.Y) > r.Y1 {
		return false
	}
	in := func(p Pt) bool { return p.X > r.X0 && p.X < r.X1 && p.Y > r.Y0 && p.Y < r.Y1 }
	if in(a) || in(b) {
		return true
	}
	edges := [][2]Pt{{{r.X0, r.Y0}, {r.X1, r.Y0}}, {{r.X1, r.Y0}, {r.X1, r.Y1}}, {{r.X1, r.Y1}, {r.X0, r.Y1}}, {{r.X0, r.Y1}, {r.X0, r.Y0}}}
	for _, e := range edges {
		if _, ok := segT(a, b, e[0], e[1]); ok {
			return true
		}
	}
	return false
}
