package raster

import (
	"math"

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
		nb := append(append([]*item(nil), it.up...), it.dn...)
		if it.kind == kHolder {
			c := it.cluster
			return (s.x[Lv(c)] + s.x[Rv(c)]) / 2, true
		}
		if len(nb) == 0 {
			return 0, false
		}
		sum := 0.0
		for _, n := range nb {
			sum += s.x[id[n]]
		}
		return sum / float64(len(nb)), true
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
			s.x[v] = math.Min(math.Max(t, lo), hi)
		}
	}
	for i, it := range items {
		it.x = s.x[i]
	}
	for ci, c := range l.clusters {
		c.L, c.R = s.x[Lv(ci)], s.x[Rv(ci)]
	}
	return nil
}
