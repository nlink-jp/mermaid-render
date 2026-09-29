package raster

import (
	"fmt"
	"image/color"
	"math"
	"strings"
	"unicode"
	"unicode/utf8"

	mr "github.com/nlink-jp/mermaid-render"
)

// A mind map is laid out by fixed rules, in outline as mermaid's tidy-tree
// option lays it out: the root's children alternate left and right, each
// side grows outward, and every subtree has a horizontal band of its own
// (see the RFP).
const (
	// mermaid wraps a label at 200 px of its 16 px text, and at
	// flowchart.wrappingWidth (120 px) in the shapes whose width the
	// renderer zeroes: rectangle, rounded rectangle, hexagon.
	mmWrap       = 12.5 // em
	mmWrapNarrow = 7.5  // em
	mmGapX       = 2.4  // em, from a parent's outer side to its children
	mmGapY       = 0.5  // em, between siblings' bands
	mmBranchGap  = 1.0  // em, between the bands of the root's children
	mmCurveN     = 24   // segments per line
)

// mmPalette is eleven section colours for a white card (mermaid's
// MAX_SECTIONS - 1); the root has its own.
var (
	mmPalette   = piePalette[:mr.MindmapSections]
	mmRootColor = color.RGBA{0x3d, 0x4a, 0x5c, 0xff}
)

type mmNode struct {
	box  rect   // the shape's bounding box
	tbox rect   // the text's box
	text string // wrapped
	side int    // -1 left, +1 right, 0 the root
	band rect   // the subtree's band
	cy   float64
	// set by measure: the band's height, the node's middle within it, and
	// each child's offset from the band's top.
	bandH, mid float64
	offs       []float64
	kids       []int // children on the layout (the root: none; see sides)
}

type mmEdge struct {
	from, to int
	pts      []pt // the curve, outer side to inner side
	w        float64
}

type mindmapLayout struct {
	W, H  float64
	nodes []mmNode
	edges []mmEdge
	// sides are the root's children on the left and on the right.
	sides [2][]int
}

func mmColor(section int) color.RGBA {
	if section < 0 {
		return mmRootColor
	}
	return mmPalette[section%len(mmPalette)]
}

func layoutMindmap(m *mr.Mindmap, measure func(string, bool) (float64, float64, error)) (*mindmapLayout, error) {
	ml := &mindmapLayout{nodes: make([]mmNode, len(m.Nodes))}
	if len(m.Nodes) == 0 {
		return ml, nil
	}
	for i, n := range m.Nodes {
		if utf8.RuneCountInString(n.Text) > MaxLabel {
			return nil, &mr.Error{Kind: mr.UnsupportedConstruct, Line: n.Line, Msg: fmt.Sprintf("a label longer than %d characters", MaxLabel)}
		}
		text, err := wrapLabel(n.Text, mmWrapOf(n.Shape), measure)
		if err != nil {
			return nil, glyphErr(err, n.Line)
		}
		tw, th, err := measure(text, false)
		if err != nil {
			return nil, glyphErr(err, n.Line)
		}
		w, h := mmShapeSize(n.Shape, tw, th)
		ml.nodes[i] = mmNode{text: text, box: rect{0, 0, w, h}, tbox: rect{(w - tw) / 2, (h - th) / 2, (w + tw) / 2, (h + th) / 2}}
		if i > 0 {
			ml.nodes[i].kids = n.Children
		}
	}
	for k, c := range m.Nodes[0].Children {
		ml.sides[k%2] = append(ml.sides[k%2], c) // the first on the left (sides[0])
	}
	for i := len(m.Nodes) - 1; i > 0; i-- {
		ml.measure(i)
	}
	root := &ml.nodes[0]
	root.cy = root.box.H() / 2
	for s, kids := range ml.sides {
		if len(kids) == 0 {
			continue
		}
		side := 2*s - 1
		offs, _ := ml.stack(kids, mmBranchGap)
		first := offs[0] + ml.nodes[kids[0]].mid
		last := offs[len(kids)-1] + ml.nodes[kids[len(kids)-1]].mid
		top := root.cy - (first+last)/2
		inner := root.box.X0 - mmGapX
		if side > 0 {
			inner = root.box.X1 + mmGapX
		}
		for k, c := range kids {
			ml.assign(c, side, top+offs[k], inner)
		}
	}
	// Move everything to the origin.
	x0, y0 := math.Inf(1), math.Inf(1)
	x1, y1 := math.Inf(-1), math.Inf(-1)
	for _, n := range ml.nodes {
		for _, r := range []rect{n.box, n.band} {
			x0, y0 = math.Min(x0, r.X0), math.Min(y0, r.Y0)
			x1, y1 = math.Max(x1, r.X1), math.Max(y1, r.Y1)
		}
	}
	for i := range ml.nodes {
		n := &ml.nodes[i]
		n.box = n.box.shifted(-x0, -y0)
		n.tbox = n.tbox.shifted(-x0, -y0)
		n.band = n.band.shifted(-x0, -y0)
		n.cy -= y0
	}
	ml.W, ml.H = x1-x0, y1-y0
	ml.nodes[0].band = rect{0, 0, ml.W, ml.H} // the root's subtree is everything
	for i, n := range m.Nodes {
		if i == 0 {
			continue
		}
		ml.edges = append(ml.edges, ml.edge(n.Parent, i, m.Nodes[n.Parent].Level))
	}
	return ml, nil
}

func (r rect) shifted(dx, dy float64) rect { return rect{r.X0 + dx, r.Y0 + dy, r.X1 + dx, r.Y1 + dy} }

// stack lays bands top to bottom: each one's offset, and the height of all.
func (ml *mindmapLayout) stack(kids []int, gap float64) ([]float64, float64) {
	offs := make([]float64, len(kids))
	y := 0.0
	for k, c := range kids {
		offs[k] = y
		y += ml.nodes[c].bandH + gap
	}
	return offs, y - gap
}

// measure sizes node i's band from its children's (which are measured
// first: they come later in source order).
func (ml *mindmapLayout) measure(i int) {
	n := &ml.nodes[i]
	h := n.box.H()
	if len(n.kids) == 0 {
		n.bandH, n.mid = h, h/2
		return
	}
	offs, span := ml.stack(n.kids, mmGapY)
	first := offs[0] + ml.nodes[n.kids[0]].mid
	last := offs[len(offs)-1] + ml.nodes[n.kids[len(offs)-1]].mid
	mid := (first + last) / 2
	top, bottom := math.Min(0, mid-h/2), math.Max(span, mid+h/2)
	for k := range offs {
		offs[k] -= top
	}
	n.offs, n.bandH, n.mid = offs, bottom-top, mid-top
}

// assign places node i's band at top, its inner side at x inner.
func (ml *mindmapLayout) assign(i, side int, top, inner float64) {
	n := &ml.nodes[i]
	w, h := n.box.W(), n.box.H()
	x0 := inner
	if side < 0 {
		x0 = inner - w
	}
	n.side = side
	n.cy = top + n.mid
	dx, dy := x0-n.box.X0, n.cy-h/2-n.box.Y0
	n.box = n.box.shifted(dx, dy)
	n.tbox = n.tbox.shifted(dx, dy)
	n.band = rect{n.box.X0, top, n.box.X1, top + n.bandH}
	next := n.box.X1 + mmGapX
	if side < 0 {
		next = n.box.X0 - mmGapX
	}
	for k, c := range n.kids {
		ml.assign(c, side, top+n.offs[k], next)
		cb := ml.nodes[c].band
		n.band.X0, n.band.X1 = math.Min(n.band.X0, cb.X0), math.Max(n.band.X1, cb.X1)
	}
}

// edge is the line from parent p to child c: a curve from the middle of
// p's outer side to the middle of c's inner side, horizontal at both ends.
func (ml *mindmapLayout) edge(p, c, depth int) mmEdge {
	pn, cn := ml.nodes[p], ml.nodes[c]
	a, b := pt{pn.box.X1, pn.cy}, pt{cn.box.X0, cn.cy}
	if cn.side < 0 {
		a, b = pt{pn.box.X0, pn.cy}, pt{cn.box.X1, cn.cy}
	}
	mx := (a.X + b.X) / 2
	pts := make([]pt, mmCurveN+1)
	for k := range pts {
		t := float64(k) / mmCurveN
		u := 1 - t
		pts[k] = pt{
			u*u*u*a.X + 3*u*u*t*mx + 3*u*t*t*mx + t*t*t*b.X,
			u*u*u*a.Y + 3*u*u*t*a.Y + 3*u*t*t*b.Y + t*t*t*b.Y,
		}
	}
	return mmEdge{from: p, to: c, pts: pts, w: math.Max(0.1, 0.34-0.08*float64(depth))}
}

// mmShapeSize is a shape's box around a text of tw × th em.
func mmShapeSize(s mr.MindmapShape, tw, th float64) (w, h float64) {
	switch s {
	case mr.MindmapRect:
		return tw + 1.6, th + 1.0
	case mr.MindmapRounded:
		return tw + 2.0, th + 1.1
	case mr.MindmapCircle:
		d := math.Hypot(tw, th) + 0.9
		return d, d
	case mr.MindmapCloud, mr.MindmapBang:
		rx, ry := mmBlob(s, tw, th)
		return 2 * rx, 2 * ry
	case mr.MindmapHexagon:
		h = th + 1.0
		return tw + 1.4 + 2*mmHexK(h), h
	}
	return tw + 1.8, th + 0.9
}

func mmHexK(h float64) float64 { return h / 2 * 0.6 }

// mmWrapOf is the width a shape's label wraps at.
func mmWrapOf(s mr.MindmapShape) float64 {
	switch s {
	case mr.MindmapRect, mr.MindmapRounded, mr.MindmapHexagon:
		return mmWrapNarrow
	}
	return mmWrap
}

// mmBlob is the outer radii of a cloud or a bang: an ellipse through the
// padded text box's corners (√2 times its half sides), grown by the bumps
// or spikes that stand out of it.
func mmBlob(s mr.MindmapShape, tw, th float64) (rx, ry float64) {
	bx, by := (tw/2+0.5)*math.Sqrt2, (th/2+0.4)*math.Sqrt2
	if s == mr.MindmapBang {
		// A spike's foot is on a polygon inscribed in the ellipse.
		k := 1 / math.Cos(math.Pi/mmSpikes)
		return bx * k * mmSpikeOut, by * k * mmSpikeOut
	}
	return bx * mmBumpOut, by * mmBumpOut
}

const (
	mmSpikes   = 14
	mmSpikeOut = 1.22
	mmBumpOut  = 1.1
)

// mmOutline is the polygon a node is drawn as.
func mmOutline(s mr.MindmapShape, r rect) []pt {
	c := r.Center()
	switch s {
	case mr.MindmapRect:
		return []pt{{r.X0, r.Y0}, {r.X1, r.Y0}, {r.X1, r.Y1}, {r.X0, r.Y1}}
	case mr.MindmapRounded:
		return roundRect(r, math.Min(0.9, r.H()/2))
	case mr.MindmapCircle:
		return ellipse(c.X, c.Y, r.W()/2, r.H()/2, 96)
	case mr.MindmapHexagon:
		k := mmHexK(r.H())
		return []pt{{r.X0 + k, r.Y0}, {r.X1 - k, r.Y0}, {r.X1, c.Y}, {r.X1 - k, r.Y1}, {r.X0 + k, r.Y1}, {r.X0, c.Y}}
	case mr.MindmapCloud:
		// Bumps: the ellipse pushed out by up to mmBumpOut, eight times
		// round.
		rx, ry := r.W()/2/mmBumpOut, r.H()/2/mmBumpOut
		pts := make([]pt, 128)
		for k := range pts {
			a := 2 * math.Pi * float64(k) / float64(len(pts))
			f := 1 + (mmBumpOut-1)*math.Abs(math.Sin(4*a))
			pts[k] = pt{c.X + rx*f*math.Cos(a), c.Y + ry*f*math.Sin(a)}
		}
		return pts
	case mr.MindmapBang:
		rx, ry := r.W()/2/mmSpikeOut, r.H()/2/mmSpikeOut
		pts := make([]pt, 2*mmSpikes)
		for k := range pts {
			a := math.Pi * float64(k) / mmSpikes
			f := 1.0
			if k%2 == 1 {
				f = mmSpikeOut
			}
			pts[k] = pt{c.X + rx*f*math.Cos(a), c.Y + ry*f*math.Sin(a)}
		}
		return pts
	}
	return roundRect(r, math.Min(0.3, r.H()/2))
}

func (c *canvas) drawMindmap(m *mr.Mindmap, ml *mindmapLayout, fn *Font) error {
	for _, e := range ml.edges {
		// The line runs on under both nodes to their middles, so it meets
		// every outline, bumps and spikes included.
		pts := append([]pt{ml.nodes[e.from].box.Center()}, e.pts...)
		pts = append(pts, ml.nodes[e.to].box.Center())
		col := mmColor(m.Nodes[e.to].Section)
		c.polyline(pts, e.w, false, col)
		f, l := pts[0], pts[len(pts)-1]
		c.tracef("edge %d %d col=%s w=%.3f from=%.3f,%.3f to=%.3f,%.3f", e.from, e.to, hexColor(col), e.w, f.X, f.Y, l.X, l.Y)
	}
	for i, n := range ml.nodes {
		col := mmColor(m.Nodes[i].Section)
		poly := mmOutline(m.Nodes[i].Shape, n.box)
		c.fill(poly, col)
		ctr, tc := n.tbox.Center(), textOn(col)
		x, y := (ctr.X+c.offX)*c.em, (ctr.Y+c.offY)*c.em
		if err := fn.drawText(c.img, x, y, n.text, false, c.em, tc); err != nil {
			return glyphErr(err, m.Nodes[i].Line)
		}
		c.tracef("node %d %q fill=%s text=%s at=%.3f,%.3f poly=%d area=%.3f", i, n.text, hexColor(col), hexColor(tc), x/c.em-c.offX, y/c.em-c.offY, len(poly), polyArea(poly))
	}
	return nil
}

// wrapLabel breaks each line of a label that is wider than width em, as a
// browser breaks mermaid's label box: at spaces, after a hyphen, and
// between characters where either is CJK, never before closing punctuation
// or after an opening bracket. A run with no break stays whole.
func wrapLabel(text string, width float64, measure func(string, bool) (float64, float64, error)) (string, error) {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		w, _, err := measure(line, false)
		if err != nil {
			return "", err
		}
		if w <= width {
			out = append(out, line)
			continue
		}
		cur := ""
		for _, seg := range breakSegments(line) {
			cand := cur + seg
			if cur != "" {
				cw, _, err := measure(strings.TrimRight(cand, " "), false)
				if err != nil {
					return "", err
				}
				if cw > width {
					out = append(out, strings.TrimRight(cur, " "))
					cand = seg
				}
			}
			cur = cand
		}
		out = append(out, strings.TrimRight(cur, " "))
	}
	return strings.Join(out, "\n"), nil
}

// breakSegments cuts a line where it may break: each segment but the last
// ends where a break is allowed (after its trailing spaces).
func breakSegments(line string) []string {
	rs := []rune(line)
	var segs []string
	start := 0
	for i := 1; i < len(rs); i++ {
		a, b := rs[i-1], rs[i]
		brk := false
		switch {
		case a == ' ' && b != ' ':
			brk = true
		case a == ' ' || b == ' ':
		case a == '-' && i >= 2 && rs[i-2] != ' ' && !unicode.IsDigit(b):
			brk = true // after a hyphen inside a word, not before a digit
		case isCJK(a) || isCJK(b):
			brk = !strings.ContainsRune(noBreakBefore, b) && !strings.ContainsRune(noBreakAfter, a)
		}
		if brk {
			segs = append(segs, string(rs[start:i]))
			start = i
		}
	}
	return append(segs, string(rs[start:]))
}

const (
	noBreakBefore = "、。，．）」』】〉》〕｝！？ー・：；ぁぃぅぇぉっゃゅょゎァィゥェォッャュョヮヵヶ々〻)]}!?,.:;%"
	noBreakAfter  = "（「『【〈《〔｛([{"
)

func isCJK(r rune) bool {
	return unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul) ||
		(r >= 0x3000 && r <= 0x303f) || (r >= 0xff00 && r <= 0xffef)
}

func hexColor(c color.RGBA) string { return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B) }

// polyArea is a polygon's area (the shoelace formula).
func polyArea(poly []pt) float64 {
	a := 0.0
	for k, p := range poly {
		q := poly[(k+1)%len(poly)]
		a += p.X*q.Y - q.X*p.Y
	}
	return math.Abs(a) / 2
}
