package raster

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	mr "github.com/nlink-jp/mermaid-render"
)

// A state diagram is laid out scope by scope, innermost first (the RFP's
// 2b, the operator's decision of 2026-09-29): each scope — the top level, a
// composite's inside, a concurrent region — goes through the flowchart
// layout in its own direction, with every state passed in as a size, as ER
// passes its tables. A composite enters its parent's layout as a box the
// size of its title band and its regions side by side; once the parent is
// placed, the regions are moved into that box.

// Sizes, in em.
const (
	stStartD    = 1.0     // the start's filled circle
	stEndD      = 1.15    // the end's ring
	stEndDot    = 0.34    // the end's inner dot, as a share of its diameter
	stChoice    = 1.9     // the choice's diamond
	stBarLong   = 4.0     // a fork or join bar, across the scope's direction
	stBarShort  = portGap // and along it: about mermaid's 10 px on 14 px text, and ends on its two faces are a port gap apart
	stRuleGap   = 0.45    // a titled state's title to its lines, the rule midway
	stBandPadY  = 0.35    // a composite's title to its band's edges
	stRegionGap = 1.6     // between two regions; the dashed separator midway
	stEmptyW    = 2.5     // a composite with nothing inside
	stEmptyH    = 1.0
	// stCircleSpread is the share of a start or end circle's face its
	// links may use. The circle is sized up front for the links on its
	// busier side, so the layout never widens it into an ellipse.
	stCircleSpread = 0.8 // not 1: a port at the rim would meet the other face's at the equator
)

// stateLayout is a placed state diagram.
type stateLayout struct {
	W, H   float64
	root   *stScope
	scopes []*stScope // every scope, parents before children
	comps  []*stComp
}

// stScope is one scope laid out: its graph and layout in its own
// coordinates, and where its origin sits in the diagram.
type stScope struct {
	scope *mr.StateScope
	graph *mr.Flowchart
	lay   *flowLayout
	off   pt
	// kind of each node of the graph, in order: the scope's states, then
	// its notes.
	states []*mr.StateNode
	notes  []*mr.StateNote
	comps  []*stComp
	// titled: the text of a state with a title and lines, by node index.
	titled map[int]stTitled
}

type stTitled struct {
	title, lines string
	tw, th       float64 // the title's size
	lw, lh       float64 // the lines' size
}

// stComp is a composite: its frame (in the diagram's coordinates once
// placed), its title and its regions.
type stComp struct {
	node    *mr.StateNode
	idx     int // its node in the parent scope's layout
	parent  *stScope
	regions []*stScope
	titleW  float64
	titleH  float64
	frame   rect
	band    rect // the title band, down to its rule
	title   rect // the title's text box
	seps    [][2]pt
	regionR []rect // each region's box
}

func (c *stComp) bandH() float64 { return c.titleH + 2*stBandPadY }

// contentSize is the regions side by side.
func (c *stComp) contentSize() (w, h float64) {
	for i, r := range c.regions {
		if i > 0 {
			w += stRegionGap
		}
		w += r.lay.W
		h = math.Max(h, r.lay.H)
	}
	if w == 0 && h == 0 {
		return stEmptyW, stEmptyH
	}
	return w, h
}

func (c *stComp) size() (w, h float64) {
	cw, ch := c.contentSize()
	return math.Max(cw, c.titleW) + 2*framePad, c.bandH() + ch + 2*framePad
}

func layoutState(d *mr.StateDiagram, m measurer) (*stateLayout, error) {
	nodes, links, comps := 0, 0, 0
	var count func(s *mr.StateScope)
	count = func(s *mr.StateScope) {
		nodes += len(s.States) + len(s.Notes)
		links += len(s.Transitions) + len(s.Notes)
		for _, n := range s.States {
			if n.Kind == mr.StateComposite {
				comps++
			}
			for _, r := range n.Regions {
				count(r)
			}
		}
	}
	count(d.Root)
	if nodes > MaxNodes || links > MaxLinks || comps > MaxSubgraphs {
		return nil, &mr.Error{Kind: mr.UnsupportedConstruct, Msg: fmt.Sprintf(
			"too large to lay out: %d states and notes, %d transitions and note lines, %d composites (limits %d, %d, %d)",
			nodes, links, comps, MaxNodes, MaxLinks, MaxSubgraphs)}
	}
	sl := &stateLayout{}
	root, err := sl.layoutScope(d.Root, m)
	if err != nil {
		return nil, err
	}
	sl.root = root
	sl.W, sl.H = root.lay.W, root.lay.H
	sl.place(root, pt{})
	return sl, nil
}

func long(s string, line int) error {
	if utf8.RuneCountInString(s) > MaxLabel {
		return &mr.Error{Kind: mr.UnsupportedConstruct, Line: line, Msg: fmt.Sprintf("a label longer than %d characters", MaxLabel)}
	}
	return nil
}

// layoutScope lays out a scope, its composites' insides first.
func (sl *stateLayout) layoutScope(s *mr.StateScope, m measurer) (*stScope, error) {
	sc := &stScope{scope: s, titled: map[int]stTitled{}}
	sl.scopes = append(sl.scopes, sc)
	f := &mr.Flowchart{Direction: s.Direction}
	sc.graph = f
	var sizes [][2]float64
	var spreads []float64
	across := s.Direction == mr.TB || s.Direction == mr.BT
	ins, outs := map[string]int{}, map[string]int{}
	for _, t := range s.Transitions {
		outs[t.From]++
		ins[t.To]++
	}
	circle := func(id string, d float64) float64 {
		if k := max(ins[id], outs[id]); k >= 2 {
			d = math.Max(d, float64(k)*portGap/stCircleSpread)
		}
		return d
	}
	for i, n := range s.States {
		gn := &mr.Node{ID: n.ID, Line: n.Line}
		var w, h float64
		switch n.Kind {
		case mr.StateStart:
			d := circle(n.ID, stStartD)
			gn.Shape, w, h = mr.Circle, d, d
		case mr.StateEnd:
			d := circle(n.ID, stEndD)
			gn.Shape, w, h = mr.Circle, d, d
		case mr.StateChoice:
			gn.Shape, w, h = mr.Rhombus, stChoice, stChoice
		case mr.StateFork, mr.StateJoin:
			gn.Shape, w, h = mr.Rect, stBarShort, stBarLong
			if across {
				w, h = h, w
			}
		case mr.StateComposite:
			gn.Shape = mr.Round
			c := &stComp{node: n, idx: i, parent: sc}
			if err := long(n.Label, n.Line); err != nil {
				return nil, err
			}
			tw, th, err := m(n.Label, true)
			if err != nil {
				return nil, glyphErr(err, n.Line)
			}
			c.titleW, c.titleH = tw, th
			for _, r := range n.Regions {
				rs, err := sl.layoutScope(r, m)
				if err != nil {
					return nil, err
				}
				c.regions = append(c.regions, rs)
			}
			sc.comps = append(sc.comps, c)
			sl.comps = append(sl.comps, c)
			w, h = c.size()
		default: // a plain state
			gn.Shape = mr.Round
			if len(n.Lines) == 0 {
				gn.Label = n.Label
				tw, th, err := m(n.Label, false)
				if err != nil {
					return nil, glyphErr(err, n.Line)
				}
				w, h = nodeSize(mr.Round, tw, th)
				break
			}
			t := stTitled{title: n.Label, lines: strings.Join(n.Lines, "\n")}
			for _, s := range append([]string{t.title}, n.Lines...) {
				if err := long(s, n.Line); err != nil {
					return nil, err
				}
			}
			var err error
			if t.tw, t.th, err = m(t.title, false); err != nil {
				return nil, glyphErr(err, n.Line)
			}
			if t.lw, t.lh, err = m(t.lines, false); err != nil {
				return nil, glyphErr(err, n.Line)
			}
			sc.titled[i] = t
			w, h = nodeSize(mr.Round, math.Max(t.tw, t.lw), t.th+stRuleGap+t.lh)
		}
		f.Nodes = append(f.Nodes, gn)
		sizes = append(sizes, [2]float64{w, h})
		spread := 0.0
		if n.Kind == mr.StateStart || n.Kind == mr.StateEnd {
			spread = stCircleSpread
		}
		spreads = append(spreads, spread)
		sc.states = append(sc.states, n)
	}
	for k, nt := range s.Notes {
		gn := &mr.Node{ID: "note\n" + strconv.Itoa(k), Label: nt.Text, Shape: mr.Rect, Line: nt.Line}
		tw, th, err := m(nt.Text, false)
		if err != nil {
			return nil, glyphErr(err, nt.Line)
		}
		w, h := nodeSize(mr.Rect, tw, th)
		f.Nodes = append(f.Nodes, gn)
		sizes = append(sizes, [2]float64{w, h})
		spreads = append(spreads, 0)
		sc.notes = append(sc.notes, nt)
	}
	for _, t := range s.Transitions {
		f.Links = append(f.Links, &mr.Link{From: mr.Endpoint{ID: t.From}, To: mr.Endpoint{ID: t.To},
			Label: t.Label, Stroke: mr.Solid, End: mr.Arrow, Length: 1, Line: t.Line})
	}
	for k, nt := range s.Notes {
		// dataFetcher's note edge: note -> state on the left, state -> note
		// on the right; dashed, no head.
		from, to := nt.State, "note\n"+strconv.Itoa(k)
		if nt.Left {
			from, to = to, from
		}
		f.Links = append(f.Links, &mr.Link{From: mr.Endpoint{ID: from}, To: mr.Endpoint{ID: to},
			Stroke: mr.Dotted, Length: 1, Line: nt.Line})
	}
	if len(f.Nodes) == 0 {
		sc.lay = &flowLayout{}
		return sc, nil
	}
	lay, err := layoutGraph(f, m, &layouter{portGap: portGap, rankGap: rankGap, endRoom: trackOut, labelRoom: trackIn, sizes: sizes, spreads: spreads})
	if err != nil {
		return nil, err
	}
	sc.lay = lay
	return sc, nil
}

// place puts a scope's origin at off and its composites' regions into
// their frames.
func (sl *stateLayout) place(sc *stScope, off pt) {
	sc.off = off
	for _, c := range sc.comps {
		b := sc.lay.Nodes[c.idx].Box
		c.frame = rect{b.X0 + off.X, b.Y0 + off.Y, b.X1 + off.X, b.Y1 + off.Y}
		band := c.bandH()
		c.band = rect{c.frame.X0, c.frame.Y0, c.frame.X1, c.frame.Y0 + band}
		cx := c.frame.Center().X
		c.title = rect{cx - c.titleW/2, c.frame.Y0 + stBandPadY, cx + c.titleW/2, c.frame.Y0 + stBandPadY + c.titleH}
		cw, _ := c.contentSize()
		x := cx - cw/2
		y := c.band.Y1 + framePad
		c.seps, c.regionR = nil, nil
		for i, r := range c.regions {
			if i > 0 {
				mid := x + stRegionGap/2
				c.seps = append(c.seps, [2]pt{{mid, c.band.Y1}, {mid, c.frame.Y1}})
				x += stRegionGap
			}
			c.regionR = append(c.regionR, rect{x, y, x + r.lay.W, y + r.lay.H})
			sl.place(r, pt{x, y})
			x += r.lay.W
		}
	}
}

// drawState draws every scope, outermost first: a scope's shapes, its
// composites' insides, then its transitions over its own gaps.
func (c *canvas) drawState(sl *stateLayout, fn *Font) error {
	return c.drawScope(sl.root, fn)
}

func (c *canvas) drawScope(sc *stScope, fn *Font) error {
	em := c.em
	baseX, baseY := c.offX, c.offY
	c.offX, c.offY = baseX+sc.off.X, baseY+sc.off.Y
	restore := func() { c.offX, c.offY = baseX, baseY }
	text := func(p pt, s string, bold bool, line int) error {
		if err := fn.drawText(c.img, (p.X+c.offX)*em, (p.Y+c.offY)*em, s, bold, em, colText); err != nil {
			return glyphErr(err, line)
		}
		return nil
	}
	for i, n := range sc.states {
		b := sc.lay.Nodes[i].Box
		switch n.Kind {
		case mr.StateStart:
			ctr := b.Center()
			c.fill(ellipse(ctr.X, ctr.Y, b.W()/2, b.H()/2, 48), colEdge)
		case mr.StateEnd:
			ctr := b.Center()
			ring := ellipse(ctr.X, ctr.Y, b.W()/2, b.H()/2, 48)
			c.outlineShape(ring, colBG, colEdge, lineW*1.3, false)
			c.fill(ellipse(ctr.X, ctr.Y, b.W()*stEndDot, b.H()*stEndDot, 32), colEdge)
		case mr.StateChoice:
			c.outlineShape(outline(mr.Rhombus, b, 0), colNode, colStroke, lineW, false)
		case mr.StateFork, mr.StateJoin:
			c.fill(outline(mr.Rect, b, 0), colEdge)
		case mr.StateComposite:
			// Drawn with its inside, below.
		default:
			c.outlineShape(outline(mr.Round, b, 0), colNode, colStroke, lineW, false)
		}
		c.tracef("state %s %s", n.Kind, n.ID)
	}
	for k := range sc.notes {
		b := sc.lay.Nodes[len(sc.states)+k].Box
		c.outlineShape(outline(mr.Rect, b, 0), colNote, colNoteSt, lineW, false)
	}
	restore()
	for _, cp := range sc.comps {
		c.outlineShape(outline(mr.Round, cp.frame, 0), colFrame, colStroke, lineW, false)
		c.segment(pt{cp.band.X0, cp.band.Y1}, pt{cp.band.X1, cp.band.Y1}, lineW*0.8, colFrameSt)
		for _, s := range cp.seps {
			c.polyline([]pt{s[0], s[1]}, lineW*0.8, true, colFrameSt)
		}
		if err := fn.drawText(c.img, (cp.title.Center().X+c.offX)*em, (cp.title.Center().Y+c.offY)*em, cp.node.Label, true, em, colText); err != nil {
			return glyphErr(err, cp.node.Line)
		}
		c.tracef("composite %s regions %d", cp.node.ID, len(cp.regions))
		for _, r := range cp.regions {
			if err := c.drawScope(r, fn); err != nil {
				return err
			}
		}
	}
	c.offX, c.offY = baseX+sc.off.X, baseY+sc.off.Y
	defer restore()
	for _, e := range sc.lay.Edges {
		c.edge(e)
	}
	for _, e := range sc.lay.Edges {
		c.heads(e)
		c.tracef("transition %s %s %s", e.Link.From.ID, e.Link.To.ID, e.Link.Stroke)
	}
	for i, n := range sc.states {
		b := sc.lay.Nodes[i].Box
		if t, ok := sc.titled[i]; ok {
			top := b.Y0 + padY
			if err := text(pt{b.Center().X, top + t.th/2}, t.title, false, n.Line); err != nil {
				return err
			}
			rule := top + t.th + stRuleGap/2
			c.segment(pt{b.X0, rule}, pt{b.X1, rule}, lineW*0.8, colStroke)
			if err := text(pt{b.Center().X, top + t.th + stRuleGap + t.lh/2}, t.lines, false, n.Line); err != nil {
				return err
			}
			continue
		}
		if n.Kind == mr.StatePlain {
			if err := text(b.Center(), n.Label, false, n.Line); err != nil {
				return err
			}
		}
	}
	for k, nt := range sc.notes {
		b := sc.lay.Nodes[len(sc.states)+k].Box
		if err := text(b.Center(), nt.Text, false, nt.Line); err != nil {
			return err
		}
		c.tracef("note %s", nt.State)
	}
	for _, e := range sc.lay.Edges {
		if e.Label == "" {
			continue
		}
		c.labelBG(e.LabelBox)
		if err := text(e.LabelBox.Center(), e.Label, false, e.Link.Line); err != nil {
			return err
		}
	}
	return nil
}
