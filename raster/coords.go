package raster

import (
	"math"
	"sort"

	mr "github.com/nlink-jp/mermaid-render"
)

// Cross-axis coordinates come from difference constraints, v >= u + d:
// items in a layer keep their order and spacing; a subgraph's members stay
// inside its [L, R], everything else in its layers stays outside; subgraphs
// keep their global order; a frame is wide enough for its title. A
// longest-path pass gives a feasible layout; coordinate descent then moves
// each variable toward its neighbours, staying inside the interval the
// other variables allow, so every step remains feasible.

type constraint struct {
	u, v int // v >= u + d
	d    float64
}

type solver struct {
	x   []float64
	out [][]constraint // by u
	in  [][]constraint // by v
}

func (s *solver) add(u, v int, d float64) {
	c := constraint{u, v, d}
	s.out[u] = append(s.out[u], c)
	s.in[v] = append(s.in[v], c)
}

// interval is where x[i] may move with every other variable fixed.
func (s *solver) interval(i int) (lo, hi float64) {
	lo, hi = math.Inf(-1), math.Inf(1)
	for _, c := range s.in[i] {
		lo = math.Max(lo, s.x[c.u]+c.d)
	}
	for _, c := range s.out[i] {
		hi = math.Min(hi, s.x[c.v]-c.d)
	}
	return
}

// intervalWith is where x[i] may move when the frame edges lv and rv
// move with it: their bounds on i give way to the edges' own limits (an edge
// moving outward only loosens its other members).
func (s *solver) intervalWith(i, lv, rv int) (lo, hi float64) {
	lo, hi = math.Inf(-1), math.Inf(1)
	for _, c := range s.in[i] {
		if c.u == lv {
			elo, _ := s.interval(lv)
			lo = math.Max(lo, elo+c.d)
			continue
		}
		lo = math.Max(lo, s.x[c.u]+c.d)
	}
	for _, c := range s.out[i] {
		if c.v == rv {
			_, ehi := s.interval(rv)
			hi = math.Min(hi, ehi-c.d)
			continue
		}
		hi = math.Min(hi, s.x[c.v]-c.d)
	}
	return
}

// cut keeps the items of one layer next to a subgraph out of its frame,
// which reaches into the gap on that side (its padding, and its title on
// the top): items left of at go left of the frame, the rest right.
type cut struct {
	cluster, layer int
	at             float64
}

// coordinates solves twice when subgraphs exist: the first solution says on
// which side of a frame each item in the layer just above or below it
// falls; the second keeps those items there. An item that links into the
// frame is left free: its link has to cross the frame's edge anyway.
func (l *layouter) coordinates() error {
	if err := l.solve(nil); err != nil {
		return err
	}
	var cuts []cut
	for ci, c := range l.clusters {
		for _, k := range []int{c.r0 - 1, c.r1 + 1} {
			if k >= 0 && k < len(l.layers) {
				cuts = append(cuts, cut{ci, k, (c.L + c.R) / 2})
			}
		}
	}
	if len(cuts) == 0 {
		return nil
	}
	return l.solve(cuts)
}

// sideOf is -1 or +1: the side of the frame an item in a layer next to it
// belongs on. It follows the item's link into the frame's layers, where the
// order already put it on one side; failing that, the first solution.
func (l *layouter) sideOf(it *item, ct cut) int {
	c := l.clusters[ct.cluster]
	nb, inner := it.dn, c.r0
	if ct.layer > c.r1 {
		nb, inner = it.up, c.r1
	}
	layer := l.layers[inner]
	first, last := -1, -1
	for i, x := range layer {
		if x.cluster == ct.cluster {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	for _, n := range nb {
		if n.layer != inner || n.cluster == ct.cluster || first < 0 {
			continue
		}
		if n.pos < first {
			return -1
		}
		if n.pos > last {
			return 1
		}
	}
	if it.x < ct.at {
		return -1
	}
	return 1
}

// shiftBlock moves a subgraph — its items and frame edges together — by
// the median offset of its links to items outside it, as far as the
// constraints between the block and the rest allow.
func (l *layouter) shiftBlock(s *solver, items []*item, id map[*item]int, ci, lv, rv int) {
	in := map[int]bool{lv: true, rv: true}
	for _, it := range items {
		if it.cluster == ci {
			in[id[it]] = true
		}
	}
	var offs []float64
	mid := (s.x[lv] + s.x[rv]) / 2
	for _, it := range items {
		// A link ending on this frame: from the frame's middle.
		if it.cluster != ci && (it.upFrame == ci+1 || it.dnFrame == ci+1) {
			offs = append(offs, s.x[id[it]]-mid)
		}
		if it.cluster != ci {
			continue
		}
		for _, n := range it.up {
			if n.cluster != ci && n.dnFrame == 0 {
				offs = append(offs, s.x[id[n]]-s.x[id[it]])
			}
		}
		for _, n := range it.dn {
			if n.cluster != ci && n.upFrame == 0 {
				offs = append(offs, s.x[id[n]]-s.x[id[it]])
			}
		}
	}
	if len(offs) == 0 {
		return
	}
	sort.Float64s(offs)
	want := offs[len(offs)/2]
	if len(offs)%2 == 0 {
		want = (offs[len(offs)/2-1] + offs[len(offs)/2]) / 2
	}
	lo, hi := math.Inf(-1), math.Inf(1)
	for v := range in {
		for _, c := range s.in[v] {
			if !in[c.u] {
				lo = math.Max(lo, s.x[c.u]+c.d-s.x[v])
			}
		}
		for _, c := range s.out[v] {
			if !in[c.v] {
				hi = math.Min(hi, s.x[c.v]-c.d-s.x[v])
			}
		}
	}
	d := math.Min(math.Max(want, lo), hi)
	if d == 0 || lo > hi {
		return
	}
	for v := range in {
		s.x[v] += d
	}
}

// linksInto reports whether it connects to an item of cluster ci.
func linksInto(it *item, ci int) bool {
	for _, n := range append(append([]*item(nil), it.up...), it.dn...) {
		if n.cluster == ci {
			return true
		}
	}
	return false
}

func (l *layouter) solve(cuts []cut) error {
	var items []*item
	for _, layer := range l.layers {
		items = append(items, layer...)
	}
	id := map[*item]int{}
	for i, it := range items {
		id[it] = i
	}
	nc := len(l.clusters)
	nv := len(items) + 2*nc
	Lv := func(c int) int { return len(items) + 2*c }
	Rv := func(c int) int { return len(items) + 2*c + 1 }
	s := &solver{x: make([]float64, nv), out: make([][]constraint, nv), in: make([][]constraint, nv)}

	gap := func(a, b *item) float64 {
		if a.kind == kDummy || b.kind == kDummy || a.kind == kHolder || b.kind == kHolder {
			return sepDummy
		}
		return sepItem
	}
	for _, layer := range l.layers {
		for i := 0; i+1 < len(layer); i++ {
			a, b := layer[i], layer[i+1]
			s.add(id[a], id[b], a.rw+b.lw+gap(a, b))
		}
		// Subgraph containment, and keeping the rest out.
		for ci := range l.clusters {
			first, last := -1, -1
			for i, it := range layer {
				if it.cluster == ci {
					if first < 0 {
						first = i
					}
					last = i
				}
			}
			if first < 0 {
				continue
			}
			for i, it := range layer {
				switch {
				case it.cluster == ci:
					low := framePad
					if l.horiz && l.clusters[ci].titleH > 0 {
						// The title runs across the top: the cross axis.
						low += l.clusters[ci].titleH + titleGap
					}
					s.add(Lv(ci), id[it], low+it.lw)
					s.add(id[it], Rv(ci), framePad+it.rw)
				case i < first:
					s.add(id[it], Lv(ci), it.rw+frameSep)
				case i > last:
					s.add(Rv(ci), id[it], it.lw+frameSep)
				}
			}
		}
	}
	for _, ct := range cuts {
		layer := l.layers[ct.layer]
		// Each item's side: the side its link passes the frame on in the
		// neighbouring layer inside it, else where the first solution put
		// it. Sides must run left...left right...right to be satisfiable;
		// the split that overrules the fewest wishes wins.
		var free []*item
		var right []bool
		for _, it := range layer {
			if it.cluster == ct.cluster || linksInto(it, ct.cluster) {
				continue
			}
			free = append(free, it)
			right = append(right, l.sideOf(it, ct) > 0)
		}
		split, bestCost := 0, len(free)+1
		for k := 0; k <= len(free); k++ {
			cost := 0
			for i, r := range right {
				if (i < k) == r {
					cost++
				}
			}
			if cost < bestCost {
				split, bestCost = k, cost
			}
		}
		for i, it := range free {
			if i < split {
				s.add(id[it], Lv(ct.cluster), it.rw+frameSep)
			} else {
				s.add(Rv(ct.cluster), id[it], it.lw+frameSep)
			}
		}
	}
	for ci, c := range l.clusters {
		minW := 2 * framePad
		// A face holding ports must also leave room around the columns
		// links to its members cross it in.
		for side := range 2 {
			k := l.facePorts[[3]int{1, ci, side}]
			if k == 0 {
				continue
			}
			through := l.faceThrough[[2]int{ci, side}]
			minW = math.Max(minW, float64(k)*portGap/portSpread)
			// Each link through the face occupies its column there and the
			// column it comes down in from the layer beyond.
			minW = math.Max(minW, 2*framePad+float64(k+1)*portGap+float64(2*through)*2*trackSep)
		}
		if !l.horiz {
			minW = math.Max(minW, c.titleW+2*framePad)
		}
		s.add(Lv(ci), Rv(ci), minW)
		for cj, d := range l.clusters {
			if c.order < d.order && c.r0 <= d.r1 && d.r0 <= c.r1 {
				s.add(Rv(ci), Lv(cj), frameSep)
			}
		}
	}

	// Feasible start: longest path, relaxing until nothing moves. The
	// constraint graph is acyclic when orders are consistent; the bound on
	// passes turns a cycle into an error instead of a hang.
	for pass := 0; ; pass++ {
		if pass > nv+1 {
			return &mr.Error{Kind: mr.UnsupportedConstruct, Msg: "layout constraints are inconsistent"}
		}
		changed := false
		for v := 0; v < nv; v++ {
			for _, c := range s.in[v] {
				if t := s.x[c.u] + c.d; t > s.x[v]+1e-9 {
					s.x[v] = t
					changed = true
				}
			}
		}
		if !changed {
			break
		}
	}

	// Coordinate descent toward neighbour means.
	target := func(i int) (float64, bool) {
		if i >= len(items) {
			return 0, false
		}
		it := items[i]
		if it.kind == kHolder {
			c := it.cluster
			return (s.x[Lv(c)] + s.x[Rv(c)]) / 2, true
		}
		// A link ending on a subgraph counts as the frame's middle; the
		// members it is tied to are not pulled one by one (the frame
		// moves as a block toward it).
		centre := func(f int) float64 { return (s.x[Lv(f-1)] + s.x[Rv(f-1)]) / 2 }
		var xs []float64
		up, dn := 0, 0
		if it.upFrame > 0 {
			xs = append(xs, centre(it.upFrame))
		} else {
			for _, n := range it.up {
				if n.dnFrame == 0 {
					xs = append(xs, s.x[id[n]])
					up++
				}
			}
		}
		if it.dnFrame > 0 {
			xs = append(xs, centre(it.dnFrame))
		} else {
			for _, n := range it.dn {
				if n.upFrame == 0 {
					xs = append(xs, s.x[id[n]])
					dn++
				}
			}
		}
		if len(xs) == 0 {
			return 0, false
		}
		// A node with one link on one side and a fan on the other lines up
		// with the one (its trunk); the fan spreads from it.
		if it.kind == kNode {
			if up == 1 && dn > 1 {
				return xs[0], true
			}
			if dn == 1 && up > 1 {
				return xs[up], true
			}
		}
		// The median of the neighbours, not their mean: the mean lines up
		// with none of them, so every link bends; the median lines up with
		// at least one (with an even count, the middle value nearer the
		// current position), and chains of those come out straight.
		// One link in and one out: the one in, so a chain of such items
		// settles on one column instead of a staircase of ties.
		if len(xs) == 2 && (up == 1 || it.upFrame > 0) {
			return xs[0], true
		}
		sort.Float64s(xs)
		m := len(xs) / 2
		if len(xs)%2 == 1 {
			return xs[m], true
		}
		cur := s.x[i]
		if math.Abs(xs[m-1]-cur) <= math.Abs(xs[m]-cur) {
			return xs[m-1], true
		}
		return xs[m], true
	}
	for pass := 0; pass < 40; pass++ {
		order := make([]int, 0, nv)
		for v := 0; v < nv; v++ {
			order = append(order, v)
		}
		if pass%2 == 1 {
			for i, j := 0, len(order)-1; i < j; i, j = i+1, j-1 {
				order[i], order[j] = order[j], order[i]
			}
		}
		for _, v := range order {
			lo, hi := s.interval(v)
			var t float64
			if v >= len(items) {
				// A frame edge: as tight as its constraints allow.
				if (v-len(items))%2 == 0 {
					t = hi
				} else {
					t = lo
				}
			} else if tt, ok := target(v); ok {
				t = tt
			} else {
				continue
			}
			// A member moves its frame's edges with it: an edge that hugs
			// the widest member would otherwise pin it, and the other
			// members with it, short of their neighbours.
			if v < len(items) && items[v].cluster >= 0 {
				lv, rv := Lv(items[v].cluster), Rv(items[v].cluster)
				lo, hi = s.intervalWith(v, lv, rv)
				x := math.Min(math.Max(t, lo), hi)
				s.x[v] = x
				for _, c := range s.in[v] {
					if c.u == lv {
						s.x[lv] = math.Min(s.x[lv], x-c.d)
					}
				}
				for _, c := range s.out[v] {
					if c.v == rv {
						s.x[rv] = math.Max(s.x[rv], x+c.d)
					}
				}
				continue
			}
			s.x[v] = math.Min(math.Max(t, lo), hi)
		}
		// One variable at a time cannot move a subgraph: its members stop
		// at the frame's edges and the edges hug the members. Every few
		// passes, shift each subgraph whole toward where its outside links
		// go.
		if pass%4 == 3 {
			for ci := range l.clusters {
				l.shiftBlock(s, items, id, ci, Lv(ci), Rv(ci))
			}
		}
	}
	l.alignRuns(s, items, id, Lv, Rv)
	for i, it := range items {
		it.x = s.x[i]
	}
	l.sol, l.solID, l.solItems, l.solList = s, id, len(items), items
	for ci, c := range l.clusters {
		c.L, c.R = s.x[Lv(ci)], s.x[Rv(ci)]
	}
	return nil
}

// alignRuns puts each run of items linked one to one (each the other's only
// neighbour on that side) on one column where every item of the run can
// reach it: the descent moves one item at a time, and an item held off its
// neighbour's column by the layer beside it leaves a staircase below it.
// Candidates are the run's own columns, nearest its median first; frames
// widen for their members and are tightened after.
func (l *layouter) alignRuns(s *solver, items []*item, id map[*item]int, Lv, Rv func(int) int) {
	next := func(it *item) *item {
		if len(it.dn) == 1 && len(it.dn[0].up) == 1 && it.dnFrame == 0 {
			return it.dn[0]
		}
		return nil
	}
	head := func(it *item) bool {
		return !(len(it.up) == 1 && len(it.up[0].dn) == 1 && it.upFrame == 0)
	}
	moved := false
	for _, it := range items {
		if !head(it) || next(it) == nil {
			continue
		}
		var run []*item
		for v := it; v != nil; v = next(v) {
			run = append(run, v)
		}
		xs := make([]float64, len(run))
		for i, v := range run {
			xs[i] = s.x[id[v]]
		}
		sorted := append([]float64(nil), xs...)
		sort.Float64s(sorted)
		if sorted[len(sorted)-1]-sorted[0] < 1e-9 {
			continue
		}
		med := sorted[len(sorted)/2]
		sort.SliceStable(sorted, func(a, b int) bool { return math.Abs(sorted[a]-med) < math.Abs(sorted[b]-med) })
		reach := func(v *item, c float64) bool {
			lo, hi := s.interval(id[v])
			if v.cluster >= 0 {
				lo, hi = s.intervalWith(id[v], Lv(v.cluster), Rv(v.cluster))
			}
			return c >= lo-1e-9 && c <= hi+1e-9
		}
		for _, c := range sorted {
			ok := true
			for _, v := range run {
				if !reach(v, c) {
					ok = false
					break
				}
			}
			if !ok {
				continue
			}
			for _, v := range run {
				i := id[v]
				s.x[i] = c
				if ci := v.cluster; ci >= 0 {
					for _, k := range s.in[i] {
						if k.u == Lv(ci) {
							s.x[k.u] = math.Min(s.x[k.u], c-k.d)
						}
					}
					for _, k := range s.out[i] {
						if k.v == Rv(ci) {
							s.x[k.v] = math.Max(s.x[k.v], c+k.d)
						}
					}
				}
			}
			moved = true
			break
		}
	}
	if !moved {
		return
	}
	for ci := range l.clusters {
		_, hi := s.interval(Lv(ci))
		s.x[Lv(ci)] = hi
		lo, _ := s.interval(Rv(ci))
		s.x[Rv(ci)] = lo
	}
}
