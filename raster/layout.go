package raster

import (
	"fmt"
	"math"
	"sort"
	"unicode/utf8"

	mr "github.com/nlink-jp/mermaid-render"
)

// The flowchart layout is a layered (Sugiyama) layout, in em units, with no
// knowledge of pixels. It follows the RFP's nine steps: cycle removal,
// longest-path layering, dummy nodes for links spanning layers (a link's
// label is a dummy too, so every link spans at least two layers), barycentre
// ordering with source order breaking ties, coordinates from difference
// constraints, polylines through the dummies, offset multi-links, loops for
// self-links, one level of subgraphs kept contiguous, and TD computed then
// transformed for the other directions.

// Pt is a point in em.
type Pt struct{ X, Y float64 }

// Rect is an axis-aligned rectangle in em.
type Rect struct{ X0, Y0, X1, Y1 float64 }

func (r Rect) W() float64 { return r.X1 - r.X0 }
func (r Rect) H() float64 { return r.Y1 - r.Y0 }
func (r Rect) Center() Pt { return Pt{(r.X0 + r.X1) / 2, (r.Y0 + r.Y1) / 2} }

// overlaps reports whether r and o share interior area.
func (r Rect) overlaps(o Rect) bool {
	return r.X0 < o.X1 && o.X0 < r.X1 && r.Y0 < o.Y1 && o.Y0 < r.Y1
}

func (r Rect) contains(o Rect) bool {
	return r.X0 <= o.X0 && r.Y0 <= o.Y0 && o.X1 <= r.X1 && o.Y1 <= r.Y1
}

// NodeBox is a placed node.
type NodeBox struct {
	ID    string
	Label string
	Shape mr.Shape
	Box   Rect
	// Slant is how far the slanted shapes' sides lean in: half the height
	// the node had before it grew to hold links, and at most 0.3 of its
	// width, so growing never eats into the room its text was given.
	Slant float64
}

// FrameBox is a placed subgraph frame and its title.
type FrameBox struct {
	ID, Title string
	Box       Rect
	TitleBox  Rect
}

// EdgePath is a routed link: Points run from the link's From to its To.
type EdgePath struct {
	Link     *mr.Link
	Points   []Pt
	Label    string
	LabelBox Rect // zero when there is no label
}

// Layout is a placed flowchart.
type Layout struct {
	W, H   float64
	Nodes  []NodeBox
	Frames []FrameBox
	Edges  []EdgePath
}

// measurer gives a text's size in em. It fails for a character no font can
// draw.
type measurer func(text string, bold bool) (w, h float64, err error)

// Limits on what is laid out. They keep a layout from running away; they
// are not a judgement of how a diagram looks.
const (
	MaxNodes     = 300 // nodes and subgraphs together
	MaxSubgraphs = 100
	MaxLinks     = mr.MaxLinks
	MaxLabel     = 1000 // characters in one label or title
	maxItems     = 20000
)

// Spacing, in em.
const (
	padX       = 0.9     // text to node side
	padY       = 0.55    // text to node top and bottom
	sepItem    = 1.6     // between items in a layer
	sepDummy   = 0.7     // next to a bare dummy
	rankGap    = 0.9     // between consecutive layers
	framePad   = 0.9     // frame to its content
	frameSep   = 1.2     // frame to anything outside it
	titleGap   = 0.4     // title to the frame's first content
	labelPad   = 0.3     // around a link label
	loopReach  = 1.6     // how far a self-link loops out
	portSpread = 0.6     // fraction of a face that link ports use
	portGap    = 0.8     // least distance between two ports: wider than a head
	trackSep   = portGap // between the runs of two links in a gap: 0.45 em read as one thick line (operator's check)
	trackIn    = 0.45    // a gap's start to its first track
	trackOut   = 0.9     // its last track to its end: room for an arrowhead
)

type itemKind int

const (
	kNode itemKind = iota
	kDummy
	kLabel
	kHolder // keeps a subgraph's column open in a layer without members
)

type item struct {
	kind    itemKind
	node    int // kNode
	link    int // kDummy, kLabel
	cluster int // subgraph index, or -1
	layer   int
	pos     int
	lw, rw  float64 // cross-axis extent left and right of x
	rs      float64 // rank-axis size
	x       float64
	up, dn  []*item
	key     float64
}

type cluster struct {
	sg      *mr.Subgraph
	members []int // node indices
	r0, r1  int
	L, R    float64
	titleW  float64
	titleH  float64
	holder  int // pseudo-node index for an empty subgraph, or -1
	order   int
}

type end struct{ node, cluster int } // exactly one is >= 0

type chain struct {
	link     int
	from, to end
	lo, hi   int     // layers of the low and the high end
	fromLow  bool    // the link's From is the low end
	items    []*item // intermediate items, low to high
	label    *item
	lw, lh   float64 // label size
	self     bool
	loopOff  float64 // a self-link: its ends' distance from the face's middle
}

type layouter struct {
	f        *mr.Flowchart
	m        measurer
	horiz    bool // LR or RL: the rank axis is horizontal
	nodes    []*mr.Node
	nw, nh   []float64 // visual size of each node (and pseudo-node)
	slant0   []float64 // half the visual height before any growth
	rank     []int
	clusters []*cluster
	inClus   []int // node -> cluster or -1
	chains   []*chain
	layers   [][]*item
	nodeItem []*item
	lineOf   map[string]int
	// facePorts counts the links on each face: (0 node / 1 subgraph,
	// index, 0 high side / 1 low side).
	facePorts map[[3]int]int
	// faceThrough counts the links that cross a subgraph's face to or from
	// its members: (subgraph, 0 high side / 1 low side).
	faceThrough map[[2]int]int
	// The coordinate solver, kept for place's straightening pass.
	sol      *solver
	solID    map[*item]int
	solItems int     // the frame edges' variables follow the items
	solList  []*item // the item behind each variable
}

func layoutFlowchart(f *mr.Flowchart, m measurer) (*Layout, error) {
	// Empty subgraphs become nodes of their own, so they count as nodes.
	if len(f.Nodes)+len(f.Subgraphs) > MaxNodes || len(f.Subgraphs) > MaxSubgraphs || len(f.Links) > MaxLinks {
		return nil, &mr.Error{Kind: mr.UnsupportedConstruct, Msg: fmt.Sprintf(
			"too large to lay out: %d nodes, %d subgraphs, %d links (limits %d nodes and subgraphs, %d subgraphs, %d links)",
			len(f.Nodes), len(f.Subgraphs), len(f.Links), MaxNodes, MaxSubgraphs, MaxLinks)}
	}
	long := func(s string, line int) error {
		if utf8.RuneCountInString(s) > MaxLabel {
			return &mr.Error{Kind: mr.UnsupportedConstruct, Line: line, Msg: fmt.Sprintf("a label longer than %d characters", MaxLabel)}
		}
		return nil
	}
	for _, n := range f.Nodes {
		if err := long(n.Label, n.Line); err != nil {
			return nil, err
		}
	}
	for _, sg := range f.Subgraphs {
		if err := long(sg.Title, sg.Line); err != nil {
			return nil, err
		}
	}
	for _, lk := range f.Links {
		if err := long(lk.Label, lk.Line); err != nil {
			return nil, err
		}
	}
	l := &layouter{f: f, m: m, horiz: f.Direction == mr.LR || f.Direction == mr.RL}
	if err := l.measure(); err != nil {
		return nil, err
	}
	if err := l.buildChains(); err != nil {
		return nil, err
	}
	l.assignRanks()
	if err := l.makeItems(); err != nil {
		return nil, err
	}
	l.order()
	if err := l.coordinates(); err != nil {
		return nil, err
	}
	return l.place(), nil
}

func glyphErr(err error, line int) error {
	if me, ok := err.(*MissingGlyphError); ok {
		return &mr.Error{Kind: mr.UnsupportedConstruct, Line: line, Msg: me.Error()}
	}
	return err
}

// nodeSize is a node's visual size for a text of tw x th em.
func nodeSize(s mr.Shape, tw, th float64) (w, h float64) {
	w, h = tw+2*padX, th+2*padY
	switch s {
	case mr.Stadium:
		w = tw + h + padX
	case mr.Subroutine:
		w += 0.8
	case mr.Cylinder:
		h += 0.9
	case mr.Asymmetric:
		w += h / 2
	case mr.Parallelogram, mr.ParallelogramAlt, mr.Trapezoid, mr.TrapezoidAlt:
		w += h
	case mr.Hexagon:
		w += h
	case mr.Rhombus:
		// The text box fits when tw'/w + th'/h <= 1; w = 2tw', h = 2th' is
		// the smallest such diamond.
		w, h = 2*(tw+0.5), 2*(th+0.35)
	case mr.Circle:
		d := math.Hypot(tw, th) + 2*padY
		w, h = d, d
	case mr.DoubleCircle:
		d := math.Hypot(tw, th) + 2*padY + 0.8
		w, h = d, d
	}
	return math.Max(w, 2.5), h
}

func (l *layouter) measure() error {
	l.nodes = l.f.Nodes
	l.lineOf = map[string]int{}
	idx := map[string]int{}
	for i, n := range l.nodes {
		idx[n.ID] = i
		l.lineOf[n.ID] = n.Line
		tw, th, err := l.m(n.Label, false)
		if err != nil {
			return glyphErr(err, n.Line)
		}
		w, h := nodeSize(n.Shape, tw, th)
		l.nw, l.nh = append(l.nw, w), append(l.nh, h)
		l.slant0 = append(l.slant0, h/2)
	}
	l.inClus = make([]int, len(l.nodes))
	for i := range l.inClus {
		l.inClus[i] = -1
	}
	for ci, sg := range l.f.Subgraphs {
		c := &cluster{sg: sg, holder: -1}
		tw, th, err := l.m(sg.Title, true)
		if err != nil {
			return glyphErr(err, sg.Line)
		}
		if sg.Title == "" {
			tw, th = 0, 0
		}
		c.titleW, c.titleH = tw, th
		for _, id := range sg.Nodes {
			c.members = append(c.members, idx[id])
			l.inClus[idx[id]] = ci
		}
		if len(c.members) == 0 {
			// An empty subgraph still takes a place: a pseudo-node the size
			// of an empty frame.
			c.holder = len(l.nw)
			l.nw, l.nh = append(l.nw, 3), append(l.nh, 1)
			l.slant0 = append(l.slant0, 0.5)
			l.inClus = append(l.inClus, ci)
			c.members = []int{c.holder}
		}
		l.clusters = append(l.clusters, c)
	}
	return nil
}

func (l *layouter) buildChains() error {
	idx := map[string]int{}
	for i, n := range l.nodes {
		idx[n.ID] = i
	}
	cidx := map[string]int{}
	for i, c := range l.clusters {
		cidx[c.sg.ID] = i
	}
	endOf := func(e mr.Endpoint) end {
		if e.Subgraph {
			return end{node: -1, cluster: cidx[e.ID]}
		}
		return end{node: idx[e.ID], cluster: -1}
	}
	for i, lk := range l.f.Links {
		ch := &chain{link: i, from: endOf(lk.From), to: endOf(lk.To)}
		if lk.Label != "" {
			w, h, err := l.m(lk.Label, false)
			if err != nil {
				return glyphErr(err, lk.Line)
			}
			ch.lw, ch.lh = w+2*labelPad, h+2*labelPad
		}
		if ch.from == ch.to {
			if ch.from.cluster >= 0 {
				return &mr.Error{Kind: mr.UnsupportedConstruct, Line: lk.Line, Msg: "a link from a subgraph to itself"}
			}
			ch.self = true
		}
		// A link between a subgraph and one of its own members has no side
		// of the frame to leave from.
		for _, pair := range [][2]end{{ch.from, ch.to}, {ch.to, ch.from}} {
			if pair[0].cluster >= 0 && pair[1].node >= 0 && l.inClus[pair[1].node] == pair[0].cluster {
				return &mr.Error{Kind: mr.UnsupportedConstruct, Line: lk.Line, Msg: "a link between a subgraph and its own member"}
			}
		}
		l.chains = append(l.chains, ch)
	}
	return nil
}

// attachmentsClear reports whether, on node i's outline, the ends of its
// self-links (the outermost at off from the middle, the others inside)
// keep portGap from wherever a port may land on its two rank faces (their
// whole port range, ends included). It works in the abstract frame: the
// rank faces are top and bottom, the loops leave the right side.
func (l *layouter) attachmentsClear(i int, off float64) bool {
	cross, rs := l.nw[i], l.nh[i]
	if l.horiz {
		cross, rs = l.nh[i], l.nw[i]
	}
	// The outline in the abstract frame: the visual outline transformed.
	r := Rect{-cross / 2, -rs / 2, cross / 2, rs / 2}
	vis := r
	if l.horiz {
		vis = Rect{-rs / 2, -cross / 2, rs / 2, cross / 2}
	}
	poly := outline(l.shapeOf(i), vis, math.Min(l.slant0[i], 0.3*vis.W()))
	if l.horiz {
		for j, p := range poly {
			poly[j] = Pt{p.Y, p.X}
		}
	}
	if l.f.Direction == mr.BT || l.f.Direction == mr.RL {
		for j, p := range poly {
			poly[j] = Pt{p.X, -p.Y}
		}
	}
	hit := func(from, to Pt) (Pt, bool) { return firstHit(poly, from, to) }
	var loopEnds []Pt
	for o := loopBase; o <= off+1e-9; o += portGap {
		for _, y := range []float64{-o, o} {
			if p, ok := hit(Pt{cross, y}, Pt{0, y}); ok {
				loopEnds = append(loopEnds, p)
			}
		}
	}
	for _, y := range []float64{-off, off} {
		if p, ok := hit(Pt{cross, y}, Pt{0, y}); ok {
			loopEnds = append(loopEnds, p)
		}
	}
	half := cross / 2 * portSpreadOf(l.shapeOf(i))
	steps := int(math.Ceil(2*half/0.1)) + 1
	for _, face := range []float64{-rs, rs} {
		for j := range steps {
			// Both ends of the range exactly: aligned ports sit on them.
			x := -half + 2*half*float64(j)/float64(max(steps-1, 1))
			p, ok := hit(Pt{x, face}, Pt{x, 0})
			if !ok {
				continue
			}
			for _, q := range loopEnds {
				if math.Hypot(p.X-q.X, p.Y-q.Y) < portGap {
					return false
				}
			}
		}
	}
	return true
}

// loopBase is how far from the face's middle the innermost self-link's
// ends sit: portGap apart from each other.
const loopBase = portGap / 2

// members of an end: the node, or the subgraph's members.
func (l *layouter) members(e end) []int {
	if e.node >= 0 {
		return []int{e.node}
	}
	return l.clusters[e.cluster].members
}

type redge struct{ a, b, min int }

// rankDAG gives each of n nodes a layer: longest path over the DAG left
// after dropping DFS back edges (visited in index and edge order). A dropped
// edge u->v has v as a DFS ancestor of u, so the kept path already puts u
// below v; its link is drawn upward. A node with no predecessors is then
// pulled down next to its nearest successor, so a lone source does not sit
// at the top with one long link. Layers start at 0.
func rankDAG(n int, edges []redge) []int {
	out := make([][]int, n)
	for i, e := range edges {
		out[e.a] = append(out[e.a], i)
	}
	dropped := make([]bool, len(edges))
	state := make([]int, n) // 0 new, 1 on stack, 2 done
	var visit func(int)
	visit = func(u int) {
		state[u] = 1
		for _, ei := range out[u] {
			switch v := edges[ei].b; state[v] {
			case 0:
				visit(v)
			case 1:
				dropped[ei] = true
			}
		}
		state[u] = 2
	}
	for u := range n {
		if state[u] == 0 {
			visit(u)
		}
	}
	indeg := make([]int, n)
	hasPred := make([]bool, n)
	for i, e := range edges {
		if !dropped[i] {
			indeg[e.b]++
			hasPred[e.b] = true
		}
	}
	rank := make([]int, n)
	var queue, topo []int
	for u := range n {
		if indeg[u] == 0 {
			queue = append(queue, u)
		}
	}
	for len(queue) > 0 {
		sort.Ints(queue)
		u := queue[0]
		queue = queue[1:]
		topo = append(topo, u)
		for _, ei := range out[u] {
			if dropped[ei] {
				continue
			}
			e := edges[ei]
			rank[e.b] = max(rank[e.b], rank[u]+e.min)
			if indeg[e.b]--; indeg[e.b] == 0 {
				queue = append(queue, e.b)
			}
		}
	}
	for i := len(topo) - 1; i >= 0; i-- {
		u := topo[i]
		if hasPred[u] {
			continue
		}
		best := math.MaxInt
		for _, ei := range out[u] {
			if !dropped[ei] {
				best = min(best, rank[edges[ei].b]-edges[ei].min)
			}
		}
		if best != math.MaxInt && best > rank[u] {
			rank[u] = best
		}
	}
	lo := math.MaxInt
	for _, r := range rank {
		lo = min(lo, r)
	}
	for i := range rank {
		rank[i] -= lo
	}
	return rank
}

// assignRanks lays out layers in two levels, as compound layouts do: each
// subgraph's members are ranked among themselves, then each subgraph is one
// tall block in the top-level ranking. A subgraph's layers therefore never
// straddle anything it links with, whatever cycles run through it.
func (l *layouter) assignRanks() {
	N := len(l.nw)
	l.rank = make([]int, N)
	// Inside each subgraph.
	height := make([]int, len(l.clusters))
	local := make([]int, N)
	for ci, c := range l.clusters {
		pos := map[int]int{}
		for i, m := range c.members {
			pos[m] = i
		}
		var edges []redge
		for _, ch := range l.chains {
			if ch.self || ch.from.node < 0 || ch.to.node < 0 {
				continue
			}
			a, okA := pos[ch.from.node]
			b, okB := pos[ch.to.node]
			if okA && okB && l.inClus[ch.from.node] == ci && l.inClus[ch.to.node] == ci {
				edges = append(edges, redge{a, b, 2 * l.f.Links[ch.link].Length})
			}
		}
		r := rankDAG(len(c.members), edges)
		for i, m := range c.members {
			local[m] = r[i]
			height[ci] = max(height[ci], r[i])
		}
	}
	// The top level: nodes outside subgraphs, and one block per subgraph.
	top := func(e end) int {
		if e.cluster >= 0 {
			return N + e.cluster
		}
		if c := l.inClus[e.node]; c >= 0 {
			return N + c
		}
		return e.node
	}
	var edges []redge
	for _, ch := range l.chains {
		if ch.self {
			continue
		}
		a, b := top(ch.from), top(ch.to)
		if a == b {
			continue
		}
		minLen := 2 * l.f.Links[ch.link].Length
		if a >= N {
			// Below the whole block.
			minLen += height[a-N]
		}
		edges = append(edges, redge{a, b, minLen})
	}
	R := rankDAG(N+len(l.clusters), edges)
	for i := range N {
		if c := l.inClus[i]; c >= 0 {
			l.rank[i] = R[N+c] + local[i]
		} else {
			l.rank[i] = R[i]
		}
	}
	for _, c := range l.clusters {
		c.r0, c.r1 = math.MaxInt, math.MinInt
		for _, m := range c.members {
			c.r0, c.r1 = min(c.r0, l.rank[m]), max(c.r1, l.rank[m])
		}
	}
}

// endLayer is the layer a chain leaves from at end e, toward the other end.
func (l *layouter) endLayer(e end, towardHigher bool) int {
	if e.node >= 0 {
		return l.rank[e.node]
	}
	c := l.clusters[e.cluster]
	if towardHigher {
		return c.r1
	}
	return c.r0
}

func (l *layouter) makeItems() error {
	// Count the dummies before building any: a link spans as many layers
	// as its ends are apart.
	total := len(l.nw)
	for _, ch := range l.chains {
		if ch.self {
			continue
		}
		a, b := l.endLayer(ch.from, true), l.endLayer(ch.to, false)
		if a > b {
			a, b = l.endLayer(ch.to, true), l.endLayer(ch.from, false)
		}
		total += max(b-a-1, 0)
		if total > maxItems {
			return &mr.Error{Kind: mr.UnsupportedConstruct, Msg: "too large to lay out: links span too many layers"}
		}
	}
	maxRank := 0
	for _, r := range l.rank {
		maxRank = max(maxRank, r)
	}
	l.layers = make([][]*item, maxRank+1)
	add := func(it *item) {
		it.layer = min(max(it.layer, 0), maxRank)
		l.layers[it.layer] = append(l.layers[it.layer], it)
	}
	// Count the links on each face, and widen a node whose face must hold
	// more ports than it has room for.
	l.facePorts = map[[3]int]int{}
	for _, ch := range l.chains {
		if ch.self {
			continue
		}
		fa, ta := l.endLayer(ch.from, true), l.endLayer(ch.to, false)
		lowEnd, highEnd := ch.from, ch.to
		if fa >= ta {
			lowEnd, highEnd = ch.to, ch.from
		}
		for _, p := range []struct {
			e    end
			side int
		}{{lowEnd, 0}, {highEnd, 1}} {
			k := [3]int{0, p.e.node, p.side}
			if p.e.cluster >= 0 {
				k = [3]int{1, p.e.cluster, p.side}
			}
			l.facePorts[k]++
		}
	}
	// Self-links loop out of one side face, each nested outside the last:
	// their ends at least portGap from the inner loop's, and beyond every
	// inner loop's label, which sits level with the face's middle.
	lastOff := make([]float64, len(l.nw))
	labelHalf := make([]float64, len(l.nw))
	for _, ch := range l.chains {
		if !ch.self {
			continue
		}
		n := ch.from.node
		off := loopBase
		if lastOff[n] > 0 {
			off = math.Max(lastOff[n]+portGap, labelHalf[n]+0.3)
		}
		ch.loopOff, lastOff[n] = off, off
		if ch.lw > 0 {
			rs := ch.lh
			if l.horiz {
				rs = ch.lw
			}
			labelHalf[n] = math.Max(labelHalf[n], rs/2)
		}
	}
	for i, off := range lastOff {
		if off == 0 {
			continue
		}
		// The ends must land on the part of the side face a straight run
		// reaches: within 0.3 em of a flat face's ends; within the middle
		// 0.4 of a slanted shape's side; within the middle 0.3 of a shape
		// whose every edge is shared between faces.
		need := 2 * (off + 0.3)
		switch l.shapeOf(i) {
		case mr.Rhombus, mr.Circle, mr.DoubleCircle:
			need = 2 * off / 0.3
		case mr.Hexagon, mr.Parallelogram, mr.ParallelogramAlt, mr.Trapezoid, mr.TrapezoidAlt, mr.Asymmetric:
			need = 2 * off / 0.4
		}
		if l.horiz {
			l.nw[i] = math.Max(l.nw[i], need)
		} else {
			l.nh[i] = math.Max(l.nh[i], need)
		}
		// On those shared edges a loop's end and a port of the face beside
		// it are 0.15 of the diagonal apart at the least: grow until that
		// is portGap.
		switch l.shapeOf(i) {
		case mr.Rhombus, mr.Circle, mr.DoubleCircle:
			if l.facePorts[[3]int{0, i, 0}]+l.facePorts[[3]int{0, i, 1}] > 0 {
				if d := math.Hypot(l.nw[i], l.nh[i]); 0.15*d < portGap {
					f := portGap / (0.15 * d)
					l.nw[i], l.nh[i] = l.nw[i]*f, l.nh[i]*f
				}
			}
		}
	}
	for i := range l.nw {
		k := max(l.facePorts[[3]int{0, i, 0}], l.facePorts[[3]int{0, i, 1}])
		if k < 2 {
			continue
		}
		need := float64(k) * portGap / portSpreadOf(l.shapeOf(i))
		if l.horiz {
			l.nh[i] = math.Max(l.nh[i], need)
		} else {
			l.nw[i] = math.Max(l.nw[i], need)
		}
	}
	// Last, after every other growth: measure, on the node's real outline,
	// the points links and loops
	// attach at; grow the node until points of different faces are
	// portGap apart. Shape-by-shape rules missed the shared slopes of a
	// rhombus and the rounded ends of a stadium; measuring covers any shape.
	for i, off := range lastOff {
		if off == 0 {
			continue
		}
		for try := 0; try < 30 && !l.attachmentsClear(i, off); try++ {
			l.nw[i] *= 1.12
			l.nh[i] *= 1.12
		}
	}

	l.nodeItem = make([]*item, len(l.nw))
	for i := range l.nw {
		cross, rs := l.nw[i], l.nh[i]
		if l.horiz {
			cross, rs = l.nh[i], l.nw[i]
		}
		it := &item{kind: kNode, node: i, cluster: l.inClus[i], layer: l.rank[i], lw: cross / 2, rw: cross / 2, rs: rs}
		l.nodeItem[i] = it
		add(it)
	}
	for _, ch := range l.chains {
		if ch.self {
			// Reserve the loop and its label beside the node.
			it := l.nodeItem[ch.from.node]
			reach := loopReach
			if ch.lw > 0 {
				// The label sits beyond the loop: its cross size widens the
				// node's reservation, its rank size its layer.
				cross, rs := ch.lw, ch.lh
				if l.horiz {
					cross, rs = ch.lh, ch.lw
				}
				reach += cross
				it.rs = math.Max(it.rs, rs)
			}
			it.rw += reach
			continue
		}
		// Which end is low?
		fa := l.endLayer(ch.from, true)
		ta := l.endLayer(ch.to, false)
		if fa < ta {
			ch.fromLow = true
			ch.lo, ch.hi = fa, ta
		} else {
			fb := l.endLayer(ch.from, false)
			tb := l.endLayer(ch.to, true)
			ch.fromLow = false
			ch.lo, ch.hi = tb, fb
		}
		if ch.hi-ch.lo < 1 {
			return &mr.Error{Kind: mr.UnsupportedConstruct, Line: l.f.Links[ch.link].Line,
				Msg: "a link to a subgraph that the other end sits beside"}
		}
		lowEnd, highEnd := ch.from, ch.to
		if !ch.fromLow {
			lowEnd, highEnd = ch.to, ch.from
		}
		labelAt := ch.lo + (ch.hi-ch.lo)/2
		if (labelAt-ch.lo)%2 == 0 && labelAt+1 < ch.hi {
			labelAt++
		}
		for k := ch.lo + 1; k < ch.hi; k++ {
			it := &item{kind: kDummy, link: ch.link, layer: k, cluster: l.dummyCluster(lowEnd, highEnd, k)}
			if k == labelAt && ch.lw > 0 {
				it.kind = kLabel
				cross, rs := ch.lw, ch.lh
				if l.horiz {
					cross, rs = ch.lh, ch.lw
				}
				it.lw, it.rw, it.rs = cross/2, cross/2, rs
				ch.label = it
			}
			ch.items = append(ch.items, it)
			add(it)
		}
	}
	// Holders keep each subgraph's column open in its layers without members.
	for ci, c := range l.clusters {
		for k := c.r0; k <= c.r1; k++ {
			has := false
			for _, it := range l.layers[k] {
				if it.cluster == ci {
					has = true
					break
				}
			}
			if !has {
				add(&item{kind: kHolder, cluster: ci, layer: k})
			}
		}
	}
	// Links crossing a frame's face to reach a member: each takes a column
	// on that face that the frame's own ports must keep clear of.
	l.faceThrough = map[[2]int]int{}
	clusterOf := func(e end) int {
		if e.cluster >= 0 {
			return -2 // the frame itself: its port is counted in facePorts
		}
		return l.inClus[e.node]
	}
	for _, ch := range l.chains {
		if ch.self {
			continue
		}
		lowEnd, highEnd := ch.from, ch.to
		if !ch.fromLow {
			lowEnd, highEnd = ch.to, ch.from
		}
		cl := []int{clusterOf(lowEnd)}
		for _, it := range ch.items {
			cl = append(cl, it.cluster)
		}
		cl = append(cl, clusterOf(highEnd))
		for i := 0; i+1 < len(cl); i++ {
			layer := ch.lo + i
			a, b := cl[i], cl[i+1]
			if b >= 0 && a != b && layer+1 == l.clusters[b].r0 {
				l.faceThrough[[2]int{b, 1}]++
			}
			if a >= 0 && a != b && layer == l.clusters[a].r1 {
				l.faceThrough[[2]int{a, 0}]++
			}
		}
	}
	// Neighbours along each chain.
	link := func(a, b *item) { a.dn = append(a.dn, b); b.up = append(b.up, a) }
	for _, ch := range l.chains {
		if ch.self {
			continue
		}
		lowEnd, highEnd := ch.from, ch.to
		if !ch.fromLow {
			lowEnd, highEnd = ch.to, ch.from
		}
		prev := l.endItems(lowEnd, ch.lo)
		for _, it := range ch.items {
			for _, p := range prev {
				link(p, it)
			}
			prev = []*item{it}
		}
		for _, p := range prev {
			for _, q := range l.endItems(highEnd, ch.hi) {
				link(p, q)
			}
		}
	}
	return nil
}

// dummyCluster: a dummy inside the layers of a subgraph that one of its
// ends belongs to runs inside that frame.
func (l *layouter) dummyCluster(lowEnd, highEnd end, k int) int {
	for _, e := range []end{lowEnd, highEnd} {
		c := e.cluster
		if e.node >= 0 {
			c = l.inClus[e.node]
		}
		if c >= 0 && k >= l.clusters[c].r0 && k <= l.clusters[c].r1 {
			return c
		}
	}
	return -1
}

// endItems are the items a chain attaches to at an end in layer k.
func (l *layouter) endItems(e end, k int) []*item {
	if e.node >= 0 {
		return []*item{l.nodeItem[e.node]}
	}
	var out []*item
	for _, it := range l.layers[k] {
		if it.cluster == e.cluster && (it.kind == kNode || it.kind == kHolder) {
			out = append(out, it)
		}
	}
	return out
}

// order runs barycentre sweeps and keeps the order with the fewest crossings.
func (l *layouter) order() {
	for _, layer := range l.layers {
		sort.SliceStable(layer, func(i, j int) bool { return l.initialKey(layer[i]) < l.initialKey(layer[j]) })
		l.group(layer)
	}
	// Subgraph order is set here and after every sweep: the coordinates
	// rely on it even when no sweep runs.
	l.alignClusters()
	best := l.snapshot()
	bestX := l.crossings()
	for iter := 0; iter < 12 && bestX > 0; iter++ {
		down := iter%2 == 0
		if down {
			for k := 1; k < len(l.layers); k++ {
				l.sweep(l.layers[k], true)
			}
		} else {
			for k := len(l.layers) - 2; k >= 0; k-- {
				l.sweep(l.layers[k], false)
			}
		}
		l.alignClusters()
		if x := l.crossings(); x < bestX {
			bestX, best = x, l.snapshot()
		}
	}
	l.restore(best)
	l.alignClusters()
}

// initialKey orders a layer by source order: nodes by index, dummies after
// by link index.
func (l *layouter) initialKey(it *item) float64 {
	switch it.kind {
	case kNode:
		return float64(it.node)
	case kHolder:
		return float64(len(l.nw)) + float64(it.cluster)
	}
	return float64(len(l.nw)+len(l.clusters)) + float64(it.link)
}

func (l *layouter) renumber(layer []*item) {
	for i, it := range layer {
		it.pos = i
	}
}

func (l *layouter) sweep(layer []*item, down bool) {
	for _, it := range layer {
		nb := it.up
		if !down {
			nb = it.dn
		}
		if len(nb) == 0 {
			it.key = float64(it.pos)
			continue
		}
		s := 0.0
		for _, n := range nb {
			s += float64(n.pos)
		}
		it.key = s / float64(len(nb))
	}
	sort.SliceStable(layer, func(i, j int) bool { return layer[i].key < layer[j].key })
	l.group(layer)
}

// group gathers each subgraph's items into one run, placed where the mean
// of their positions falls.
func (l *layouter) group(layer []*item) {
	l.renumber(layer)
	type elem struct {
		key   float64
		items []*item
		first int
	}
	var elems []*elem
	byCluster := map[int]*elem{}
	for _, it := range layer {
		if it.cluster < 0 {
			elems = append(elems, &elem{key: float64(it.pos), items: []*item{it}, first: it.pos})
			continue
		}
		e := byCluster[it.cluster]
		if e == nil {
			e = &elem{first: it.pos}
			byCluster[it.cluster] = e
			elems = append(elems, e)
		}
		e.items = append(e.items, it)
		e.key += float64(it.pos)
	}
	for _, e := range elems {
		if len(e.items) > 1 || e.items[0].cluster >= 0 {
			e.key /= float64(len(e.items))
		}
	}
	sort.SliceStable(elems, func(i, j int) bool {
		if elems[i].key != elems[j].key {
			return elems[i].key < elems[j].key
		}
		return elems[i].first < elems[j].first
	})
	i := 0
	for _, e := range elems {
		for _, it := range e.items {
			layer[i] = it
			i++
		}
	}
	l.renumber(layer)
}

// alignClusters makes the left-to-right order of subgraphs the same in
// every layer, so their frames can be rectangles that do not overlap.
func (l *layouter) alignClusters() {
	if len(l.clusters) == 0 {
		return
	}
	sum := make([]float64, len(l.clusters))
	cnt := make([]float64, len(l.clusters))
	for _, layer := range l.layers {
		for _, it := range layer {
			if it.cluster >= 0 {
				sum[it.cluster] += float64(it.pos) / float64(len(layer))
				cnt[it.cluster]++
			}
		}
	}
	order := make([]int, len(l.clusters))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		return sum[order[a]]/cnt[order[a]] < sum[order[b]]/cnt[order[b]]
	})
	for rank, ci := range order {
		l.clusters[ci].order = rank
	}
	for _, layer := range l.layers {
		// The runs of subgraph items, in their current slots.
		var runs [][]*item
		var slots []int
		for i := 0; i < len(layer); {
			c := layer[i].cluster
			if c < 0 {
				i++
				continue
			}
			j := i
			for j < len(layer) && layer[j].cluster == c {
				j++
			}
			runs = append(runs, append([]*item(nil), layer[i:j]...))
			slots = append(slots, i)
			i = j
		}
		if len(runs) < 2 {
			continue
		}
		sort.SliceStable(runs, func(a, b int) bool {
			return l.clusters[runs[a][0].cluster].order < l.clusters[runs[b][0].cluster].order
		})
		// Rebuild: non-cluster items keep their relative order, cluster
		// runs fill the run slots in the global order.
		isSlot := map[int]bool{}
		for _, s := range slots {
			isSlot[s] = true
		}
		out := make([]*item, 0, len(layer))
		ri := 0
		for i := 0; i < len(layer); {
			if isSlot[i] {
				c := layer[i].cluster
				j := i
				for j < len(layer) && layer[j].cluster == c {
					j++
				}
				out = append(out, runs[ri]...)
				ri++
				i = j
				continue
			}
			if layer[i].cluster < 0 {
				out = append(out, layer[i])
			}
			i++
		}
		copy(layer, out)
		l.renumber(layer)
	}
}

func (l *layouter) snapshot() [][]*item {
	s := make([][]*item, len(l.layers))
	for i, layer := range l.layers {
		s[i] = append([]*item(nil), layer...)
	}
	return s
}

func (l *layouter) restore(s [][]*item) {
	for i := range l.layers {
		copy(l.layers[i], s[i])
		l.renumber(l.layers[i])
	}
}

// crossings scores an order: crossing segment pairs between adjacent
// layers, plus, weighted heavily, segments that change sides of a subgraph
// present in both their layers — such a link has to cross the frame.
func (l *layouter) crossings() int {
	n := 0
	side := func(it *item, c int, layer []*item) int {
		first, last := -1, -1
		for i, x := range layer {
			if x.cluster == c {
				if first < 0 {
					first = i
				}
				last = i
			}
		}
		switch {
		case first < 0 || it.cluster == c:
			return 0
		case it.pos < first:
			return -1
		case it.pos > last:
			return 1
		}
		return 0
	}
	for k := 0; k+1 < len(l.layers); k++ {
		for _, it := range l.layers[k] {
			for _, d := range it.dn {
				for ci := range l.clusters {
					a, b := side(it, ci, l.layers[k]), side(d, ci, l.layers[k+1])
					if a != 0 && b != 0 && a != b {
						n += 100
					}
				}
			}
		}
	}
	for k := 0; k+1 < len(l.layers); k++ {
		type seg struct{ a, b int }
		var segs []seg
		for _, it := range l.layers[k] {
			for _, d := range it.dn {
				segs = append(segs, seg{it.pos, d.pos})
			}
		}
		for i := range segs {
			for j := i + 1; j < len(segs); j++ {
				if (segs[i].a-segs[j].a)*(segs[i].b-segs[j].b) < 0 {
					n++
				}
			}
		}
	}
	return n
}
