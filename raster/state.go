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
	// stNoteGap is a note beside its state to the state, where the dotted
	// line runs; stNoteStack between two notes on one side.
	stNoteGap   = 1.0
	stNoteStack = 0.5
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
	// main is each state's own part of its node: the whole box, or the
	// middle when notes stand beside it. noteBox is each note's box;
	// noteNode its node in the graph, or -1 when it stands beside its
	// state. beside, by state index, the notes on each side.
	main     []rect
	noteBox  []rect
	noteNode []int
	beside   map[int]*stBeside
	noteSize [][2]float64
}

// stBeside are the notes beside one state (TB and BT: a note's side is
// the side it names). The node is the state with a slot of the wider
// side's width on each side, so the state stays at the node's middle
// where its links' ports are.
type stBeside struct {
	side        float64 // a slot's width, the gap included
	left, right []int   // note indices, top to bottom
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
	// Notes stand beside their state in TB and BT, on the side they
	// name. A start, end, choice, fork or join cannot stretch to a note's
	// height, and a state looping to itself loops out of the node's side,
	// where the slot is: those notes are nodes of the graph instead.
	index := map[string]int{}
	for i, n := range s.States {
		index[n.ID] = i
	}
	selfLoop := map[string]bool{}
	for _, t := range s.Transitions {
		if t.From == t.To {
			selfLoop[t.From] = true
		}
	}
	sc.beside = map[int]*stBeside{}
	noteW, noteH := make([]float64, len(s.Notes)), make([]float64, len(s.Notes))
	sc.noteNode = make([]int, len(s.Notes))
	for k, nt := range s.Notes {
		if err := long(nt.Text, nt.Line); err != nil {
			return nil, err
		}
		tw, th, err := m(nt.Text, false)
		if err != nil {
			return nil, glyphErr(err, nt.Line)
		}
		noteW[k], noteH[k] = nodeSize(mr.Rect, tw, th)
		sc.noteSize = append(sc.noteSize, [2]float64{noteW[k], noteH[k]})
		sc.noteNode[k] = -1
		i := index[nt.State]
		n := s.States[i]
		if across && (n.Kind == mr.StatePlain || n.Kind == mr.StateComposite) && !selfLoop[n.ID] {
			bs := sc.beside[i]
			if bs == nil {
				bs = &stBeside{}
				sc.beside[i] = bs
			}
			if nt.Left {
				bs.left = append(bs.left, k)
			} else {
				bs.right = append(bs.right, k)
			}
			bs.side = math.Max(bs.side, noteW[k]+stNoteGap)
			continue
		}
		sc.noteNode[k] = 0 // a node; its index is set below
	}
	stack := func(ks []int) float64 {
		h := 0.0
		for j, k := range ks {
			if j > 0 {
				h += stNoteStack
			}
			h += noteH[k]
		}
		return h
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
		spread := 0.0
		if n.Kind == mr.StateStart || n.Kind == mr.StateEnd {
			spread = stCircleSpread
		}
		if bs := sc.beside[i]; bs != nil {
			// The ports stay on the state's own part, at the node's middle.
			spread = portSpreadOf(gn.Shape) * w / (w + 2*bs.side)
			h = math.Max(h, math.Max(stack(bs.left), stack(bs.right)))
			w += 2 * bs.side
		}
		f.Nodes = append(f.Nodes, gn)
		sizes = append(sizes, [2]float64{w, h})
		spreads = append(spreads, spread)
		sc.states = append(sc.states, n)
	}
	for k, nt := range s.Notes {
		sc.notes = append(sc.notes, nt)
		if sc.noteNode[k] < 0 {
			continue
		}
		sc.noteNode[k] = len(f.Nodes)
		f.Nodes = append(f.Nodes, &mr.Node{ID: "note\n" + strconv.Itoa(k), Label: nt.Text, Shape: mr.Rect, Line: nt.Line})
		sizes = append(sizes, [2]float64{noteW[k], noteH[k]})
		spreads = append(spreads, 0)
	}
	for _, t := range s.Transitions {
		f.Links = append(f.Links, &mr.Link{From: mr.Endpoint{ID: t.From}, To: mr.Endpoint{ID: t.To},
			Label: t.Label, Stroke: mr.Solid, End: mr.Arrow, Length: 1, Line: t.Line})
	}
	for k, nt := range s.Notes {
		if sc.noteNode[k] < 0 {
			continue
		}
		// A note that is a node follows its line to the side it names in
		// LR and RL; elsewhere the line runs as dataFetcher's note edge
		// does (note -> state on the left, state -> note on the right).
		// Dotted, no head.
		from, to := nt.State, "note\n"+strconv.Itoa(k)
		if nt.Left != (s.Direction == mr.RL) {
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
	sc.split()
	return sc, nil
}

// split finds each state's own part of its node and each note's box.
func (sc *stScope) split() {
	sc.main = make([]rect, len(sc.states))
	sc.noteBox = make([]rect, len(sc.notes))
	for i := range sc.states {
		b := sc.lay.Nodes[i].Box
		bs := sc.beside[i]
		if bs == nil {
			sc.main[i] = b
			continue
		}
		mn := rect{b.X0 + bs.side, b.Y0, b.X1 - bs.side, b.Y1}
		sc.main[i] = mn
		for _, side := range []struct {
			ks   []int
			left bool
		}{{bs.left, true}, {bs.right, false}} {
			h := 0.0
			for j, k := range side.ks {
				if j > 0 {
					h += stNoteStack
				}
				h += sc.noteSize[k][1]
			}
			y := b.Center().Y - h/2
			for _, k := range side.ks {
				w, nh := sc.noteSize[k][0], sc.noteSize[k][1]
				x := mn.X1 + stNoteGap
				if side.left {
					x = mn.X0 - stNoteGap - w
				}
				sc.noteBox[k] = rect{x, y, x + w, y + nh}
				y += nh + stNoteStack
			}
		}
	}
	for k, gi := range sc.noteNode {
		if gi >= 0 {
			sc.noteBox[k] = sc.lay.Nodes[gi].Box
		}
	}
}

// place puts a scope's origin at off and its composites' regions into
// their frames.
func (sl *stateLayout) place(sc *stScope, off pt) {
	sc.off = off
	for _, c := range sc.comps {
		b := sc.main[c.idx]
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
		b := sc.main[i]
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
	for k, nt := range sc.notes {
		b := sc.noteBox[k]
		if sc.noteNode[k] < 0 {
			// Beside its state: a dotted line across the gap, level with
			// the note's middle as far as the state reaches.
			mn := sc.main[sc.stateIndex(nt.State)]
			y := math.Min(math.Max(b.Center().Y, mn.Y0+0.3), mn.Y1-0.3)
			if nt.Left {
				c.polyline([]pt{{b.X1, y}, {mn.X0, y}}, lineW, true, colEdge)
			} else {
				c.polyline([]pt{{mn.X1, y}, {b.X0, y}}, lineW, true, colEdge)
			}
			c.tracef("note beside %s left %v", nt.State, nt.Left)
		}
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
		b := sc.main[i]
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
		b := sc.noteBox[k]
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

func (sc *stScope) stateIndex(id string) int {
	for i, n := range sc.states {
		if n.ID == id {
			return i
		}
	}
	return -1
}
