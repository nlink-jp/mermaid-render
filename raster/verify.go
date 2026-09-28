package raster

import (
	"fmt"
	"math"
	"sort"

	mr "github.com/nlink-jp/mermaid-render"
)

// The checks below are the layout properties the tests hold, run on every
// render too: a property tests cannot generate a counterexample for is
// still a property a real diagram can break, and a picture that breaks one
// is refused like any other wrong (gem-agent ADR-0092 §5).
//
// strict is the tests' reading. Without it, only what makes the picture
// wrong is checked — something hidden, merged, cut off or pointing the
// wrong way. What only makes it harder to read (a shallow crossing, lines
// closer than the operator liked, a label near a bend) is the tests' job,
// never a reason to refuse: wrong is refused, ugly is not.

const eps = 1e-6

// faults collects what a layout gets wrong, at most maxFaults of them.
type faults []string

const maxFaults = 50

func (f *faults) add(format string, a ...any) {
	if len(*f) < maxFaults {
		*f = append(*f, fmt.Sprintf(format, a...))
	}
}

// verify runs the checks for whichever layout render made.
func verify(d mr.Diagram, lay *Layout, el *erLayout, sl *seqLayout, m measurer) []string {
	switch d := d.(type) {
	case *mr.Flowchart:
		return flowFaults(d, lay, m, false, nil)
	case *mr.ER:
		return append(flowFaults(el.graph, el.Layout, m, false, nil), erFaults(d, el, m, false)...)
	case *mr.Sequence:
		return seqFaults(d, sl, false)
	}
	return nil
}

// segment is one straight piece of link k.
type segment struct {
	a, b Pt
	link int
}

// flowFaults checks the properties a flowchart layout must have:
// everything is placed; nothing overlaps what it must not; links start and
// end on their ends' outlines and pass through no other node; links
// sharing a node arrive at distinct points.
//
// A link crossing a subgraph's frame it does not belong to is not a wrong
// picture (the ends are still read from the heads; mermaid draws such
// crossings too), but it is avoided where the order allows. Strict checks
// fail on one unless frameCrossings is non-nil, where they are counted.
func flowFaults(f *mr.Flowchart, lay *Layout, m measurer, strict bool, frameCrossings *int) []string {
	var out faults
	if len(lay.Nodes) != len(f.Nodes) || len(lay.Edges) != len(f.Links) || len(lay.Frames) != len(f.Subgraphs) {
		out.add("placed %d nodes, %d links, %d frames; want %d, %d, %d", len(lay.Nodes), len(lay.Edges), len(lay.Frames), len(f.Nodes), len(f.Links), len(f.Subgraphs))
		return out
	}
	box := map[string]NodeBox{}
	for _, n := range lay.Nodes {
		box[n.ID] = n
	}
	frame := map[string]FrameBox{}
	for _, fr := range lay.Frames {
		frame[fr.ID] = fr
	}
	// An arrowhead has a straight run of its own: the segment it ends is
	// longer than the head, so the head never sits on a bend (the
	// operator's third check: a link drawn upward had 0.45 em there).
	// Shorter than the head itself, the head would point off the line.
	headRun := arrowLen
	if strict {
		headRun = arrowLen + 0.15
	}
	for _, e := range lay.Edges {
		p := e.Points
		if e.Link.From == e.Link.To || len(p) < 3 {
			continue
		}
		for _, h := range []struct {
			head   mr.Head
			a, b   Pt
			atFrom bool
		}{{e.Link.Start, p[0], p[1], true}, {e.Link.End, p[len(p)-1], p[len(p)-2], false}} {
			if h.head == mr.NoHead {
				continue
			}
			if d := math.Hypot(h.a.X-h.b.X, h.a.Y-h.b.Y); d < headRun {
				out.add("link %s->%s: the head at its %s end has a %.3f em run", e.Link.From.ID, e.Link.To.ID, map[bool]string{true: "From", false: "To"}[h.atFrom], d)
			}
		}
	}
	all := Rect{-eps, -eps, lay.W + eps, lay.H + eps}
	for i, a := range lay.Nodes {
		if !all.contains(a.Box) {
			out.add("node %s outside the layout", a.ID)
		}
		for _, b := range lay.Nodes[i+1:] {
			if a.Box.overlaps(b.Box) {
				out.add("nodes %s and %s overlap", a.ID, b.ID)
			}
		}
	}
	// A node's label fits inside its shape.
	for _, n := range lay.Nodes {
		tw, th, err := m(n.Label, false)
		if err != nil {
			continue
		}
		c := labelCenter(n)
		poly := outline(n.Shape, n.Box, n.Slant)
		for _, p := range []Pt{{c.X - tw/2, c.Y - th/2}, {c.X + tw/2, c.Y - th/2}, {c.X + tw/2, c.Y + th/2}, {c.X - tw/2, c.Y + th/2}} {
			if !inPolygon(poly, p) && !onOutline(poly, p) {
				out.add("label %q sticks out of node %s at %v", n.Label, n.ID, p)
				break
			}
		}
	}
	// Frames hold their members and nothing else, and do not overlap.
	for i, sg := range f.Subgraphs {
		fr := lay.Frames[i]
		members := map[string]bool{}
		for _, id := range sg.Nodes {
			members[id] = true
			if !fr.Box.contains(box[id].Box) {
				out.add("frame %s does not hold its member %s", sg.ID, id)
			}
		}
		for _, n := range lay.Nodes {
			if !members[n.ID] && fr.Box.overlaps(n.Box) {
				out.add("frame %s overlaps non-member %s", sg.ID, n.ID)
			}
		}
		for _, other := range lay.Frames[i+1:] {
			if fr.Box.overlaps(other.Box) {
				out.add("frames %s and %s overlap", fr.ID, other.ID)
			}
		}
		if sg.Title != "" && !fr.Box.contains(fr.TitleBox) {
			out.add("frame %s does not hold its title", sg.ID)
		}
	}
	// Labels overlap no node, no other label, no title.
	var labels []Rect
	for _, e := range lay.Edges {
		if e.Label == "" {
			continue
		}
		if e.LabelBox == (Rect{}) {
			out.add("link %s->%s lost its label %q", e.Link.From.ID, e.Link.To.ID, e.Label)
			continue
		}
		for _, n := range lay.Nodes {
			if e.LabelBox.overlaps(n.Box) {
				out.add("label %q overlaps node %s", e.Label, n.ID)
			}
		}
		for _, fr := range lay.Frames {
			if fr.Title != "" && e.LabelBox.overlaps(fr.TitleBox) {
				out.add("label %q overlaps the title of %s", e.Label, fr.ID)
			}
		}
		for _, o := range labels {
			if e.LabelBox.overlaps(o) {
				out.add("label %q overlaps another label", e.Label)
			}
		}
		labels = append(labels, e.LabelBox)
	}
	subgraphOf := map[string]string{}
	for _, n := range f.Nodes {
		subgraphOf[n.ID] = n.Subgraph
	}
	arrivals := map[string][]Pt{}
	for _, e := range lay.Edges {
		lk := e.Link
		name := lk.From.ID + "->" + lk.To.ID
		if len(e.Points) < 2 {
			out.add("link %s has %d points", name, len(e.Points))
			continue
		}
		endOK := func(ep mr.Endpoint, p Pt) bool {
			if ep.Subgraph {
				return onRectBorder(frame[ep.ID].Box, p)
			}
			n := box[ep.ID]
			return onOutline(outline(n.Shape, n.Box, n.Slant), p)
		}
		first, last := e.Points[0], e.Points[len(e.Points)-1]
		if !endOK(lk.From, first) {
			out.add("link %s does not start on %s's outline: %v", name, lk.From.ID, first)
		}
		if !endOK(lk.To, last) {
			out.add("link %s does not end on %s's outline: %v", name, lk.To.ID, last)
		}
		for _, p := range [2]struct {
			ep mr.Endpoint
			at Pt
		}{{lk.From, first}, {lk.To, last}} {
			arrivals[p.ep.ID] = append(arrivals[p.ep.ID], p.at)
		}
		// Which frames the link may enter: those holding or being an end.
		mine := map[string]bool{}
		for _, ep := range []mr.Endpoint{lk.From, lk.To} {
			if ep.Subgraph {
				mine[ep.ID] = true
			} else if sg := subgraphOf[ep.ID]; sg != "" {
				mine[sg] = true
			}
		}
		for i := 0; i+1 < len(e.Points); i++ {
			a, b := e.Points[i], e.Points[i+1]
			for _, n := range lay.Nodes {
				if n.ID == lk.From.ID && !lk.From.Subgraph || n.ID == lk.To.ID && !lk.To.Subgraph {
					continue
				}
				if segmentHitsRect(a, b, n.Box, 1e-3) {
					out.add("link %s passes through node %s", name, n.ID)
				}
			}
			if strict {
				for _, fr := range lay.Frames {
					if !mine[fr.ID] && segmentHitsRect(a, b, fr.Box, 1e-3) {
						if frameCrossings != nil {
							*frameCrossings++
						} else {
							out.add("link %s passes through frame %s", name, fr.ID)
						}
					}
				}
			}
			if !all.contains(Rect{math.Min(a.X, b.X), math.Min(a.Y, b.Y), math.Max(a.X, b.X), math.Max(a.Y, b.Y)}) {
				out.add("link %s leaves the layout", name)
			}
		}
	}
	// Links stay apart: two segments of different links either cross at a
	// clear angle or keep their distance (strict: 0.6 em, since 0.45 em
	// looked like one thick line in the operator's check; otherwise two
	// line widths, closer than which two lines draw as one); no link runs
	// through another link's label.
	apart, angle := 2*lineW, 3.0
	if strict {
		apart, angle = 0.6, 20
	}
	var segs []segment
	for li, e := range lay.Edges {
		for i := 0; i+1 < len(e.Points); i++ {
			segs = append(segs, segment{e.Points[i], e.Points[i+1], li})
		}
	}
	// Pairs are swept left to right: two segments whose boxes are more
	// than apart away cannot cross or come closer (a dense diagram has
	// thousands of segments; every pair took half a second).
	byX := make([]int, len(segs))
	for i := range byX {
		byX[i] = i
	}
	x0 := func(s segment) float64 { return math.Min(s.a.X, s.b.X) }
	sort.SliceStable(byX, func(i, j int) bool { return x0(segs[byX[i]]) < x0(segs[byX[j]]) })
	for ii, i := range byX {
		for _, j := range byX[ii+1:] {
			a, b := segs[i], segs[j]
			if x0(b) > math.Max(a.a.X, a.b.X)+apart {
				break
			}
			if a.link == b.link ||
				math.Min(b.a.Y, b.b.Y) > math.Max(a.a.Y, a.b.Y)+apart ||
				math.Min(a.a.Y, a.b.Y) > math.Max(b.a.Y, b.b.Y)+apart {
				continue
			}
			if a.link > b.link {
				a, b = b, a
			}
			if _, ok := segT(a.a, a.b, b.a, b.b); ok {
				if angleBetween(a, b) < angle {
					out.add("links %d and %d cross at %.1f degrees", a.link, b.link, angleBetween(a, b))
				}
				continue
			}
			if d := segDist(a.a, a.b, b.a, b.b); d < apart {
				out.add("links %d and %d run %.2f em apart", a.link, b.link, d)
			}
		}
	}
	for li, e := range lay.Edges {
		if e.Label == "" {
			continue
		}
		for _, sg := range segs {
			if sg.link != li && segmentHitsRect(sg.a, sg.b, e.LabelBox, 1e-3) {
				out.add("link %d runs through the label %q", sg.link, e.Label)
			}
		}
	}
	// Links meeting at a node arrive apart, so no two heads overlap
	// (strict: portGap, wider than a head); a self-link's two ends count
	// too.
	meet := 2 * arrowHalfW
	if strict {
		meet = portGap - 0.05
	}
	for id, pts := range arrivals {
		for i := range pts {
			for j := i + 1; j < len(pts); j++ {
				if math.Hypot(pts[i].X-pts[j].X, pts[i].Y-pts[j].Y) < meet {
					out.add("two link ends meet at the same point of %s: %v", id, pts[i])
				}
			}
		}
	}
	return out
}

// erFaults checks what an ER layout adds to the flowchart properties: each
// end runs straight past its marker, link ends on one face stay far enough
// apart that crow's feet side by side do not touch, no label lies over a
// marker, and every table fits its box.
func erFaults(d *mr.ER, el *erLayout, m measurer, strict bool) []string {
	var out faults
	type endAt struct {
		p    Pt
		node int
	}
	var ends []endAt
	var zones []Rect
	idx := map[string]int{}
	for i, n := range el.Nodes {
		idx[n.ID] = i
	}
	run := markerReach
	if strict {
		run = markerReach + 0.1
	}
	for _, e := range el.Edges {
		p := e.Points
		for _, s := range [][2]Pt{{p[0], p[1]}, {p[len(p)-1], p[len(p)-2]}} {
			tip, next := s[0], s[1]
			if d := math.Hypot(next.X-tip.X, next.Y-tip.Y); d < run {
				out.add("link %s->%s: a marker's run is %.3f em (needs %.2f)", e.Link.From.ID, e.Link.To.ID, d, run)
			}
			ux, uy := (next.X - tip.X), (next.Y - tip.Y)
			l := math.Hypot(ux, uy)
			ux, uy = ux/l, uy/l
			a := Pt{tip.X - uy*markHalf, tip.Y + ux*markHalf}
			b := Pt{tip.X + ux*markerReach + uy*markHalf, tip.Y + uy*markerReach - ux*markHalf}
			zones = append(zones, Rect{math.Min(a.X, b.X), math.Min(a.Y, b.Y), math.Max(a.X, b.X), math.Max(a.Y, b.Y)})
		}
		ends = append(ends, endAt{p[0], idx[e.Link.From.ID]}, endAt{p[len(p)-1], idx[e.Link.To.ID]})
	}
	// Two crow's feet side by side must not touch; strict wants 0.4 em
	// between them — from the marker's width, not from erPortGap, so a
	// change to the spacing is checked against what it must hold.
	need := 2 * markHalf
	if strict {
		need = 2*markHalf + 0.4 - 0.05
	}
	for i, a := range ends {
		for _, b := range ends[i+1:] {
			if a.node != b.node {
				continue
			}
			sameFace := math.Abs(a.p.X-b.p.X) < 1e-6 || math.Abs(a.p.Y-b.p.Y) < 1e-6
			if d := math.Hypot(a.p.X-b.p.X, a.p.Y-b.p.Y); sameFace && d < need {
				out.add("two link ends on %s are %.3f em apart (needs %.2f)", el.Nodes[a.node].Label, d, need)
			}
		}
	}
	for i, e := range el.Edges {
		if e.Label == "" {
			continue
		}
		for j, z := range zones {
			if e.LabelBox.overlaps(z) {
				out.add("label %q lies over a marker", e.Label)
			}
			// Its own markers: 0.7 em clear (the operator's second ER
			// check: labels against the lower marker read as too low).
			if strict && j/2 == i && e.Link.From != e.Link.To {
				dx := math.Max(0, math.Max(z.X0-e.LabelBox.X1, e.LabelBox.X0-z.X1))
				dy := math.Max(0, math.Max(z.Y0-e.LabelBox.Y1, e.LabelBox.Y0-z.Y1))
				if d := math.Hypot(dx, dy); d < 0.7 {
					out.add("label %q is %.3f em from its link's marker", e.Label, d)
				}
			}
		}
	}
	// A bend keeps its distance from its own link's label (a self-link's
	// label sits beside its loop by design).
	for _, e := range el.Edges {
		if !strict || e.Label == "" || e.Link.From == e.Link.To {
			continue
		}
		p := e.Points
		for _, q := range p[1 : len(p)-1] {
			dx := math.Max(0, math.Max(e.LabelBox.X0-q.X, q.X-e.LabelBox.X1))
			dy := math.Max(0, math.Max(e.LabelBox.Y0-q.Y, q.Y-e.LabelBox.Y1))
			// 1.2 em as the operator's check asked, not erLabelRoom: the
			// check must not loosen with the constant it guards.
			if d := math.Hypot(dx, dy); d < 1.2-0.05 && d > 1e-9 {
				out.add("link %s->%s bends %.3f em from its label", e.Link.From.ID, e.Link.To.ID, d)
			}
		}
	}
	for i, n := range el.Nodes {
		t := el.tables[i]
		if t.cols == nil {
			continue
		}
		lw, _, _ := m(d.Entities[i].Label, true)
		w, h := t.size(lw)
		if w > n.Box.W()+1e-6 || h > n.Box.H()+1e-6 {
			out.add("table %s (%.2fx%.2f) does not fit its box %.2fx%.2f", n.Label, w, h, n.Box.W(), n.Box.H())
		}
	}
	return out
}

// seqFaults checks a sequence layout's properties: everything inside the
// picture; no text over another; messages down the page in order, each
// text above its arrow (strict: centred between its ends), each end on its
// lifeline or bar; a note beside a lifeline crossing none, a note over
// lifelines crossing only its own; every frame holding what lies in its
// rows, and nested frames nested.
func seqFaults(d *mr.Sequence, sl *seqLayout, strict bool) []string {
	var out faults
	all := Rect{-eps, -eps, sl.W + eps, sl.H + eps}
	type labelled struct {
		r    Rect
		what string
	}
	var texts []labelled
	for _, h := range sl.heads {
		texts = append(texts, labelled{h.box, "header " + h.label})
	}
	idx := map[*mr.Participant]int{}
	for i, p := range d.Participants {
		idx[p] = i
	}
	var msgEvents, noteEvents []*mr.Event
	for _, e := range d.Events {
		switch e.Kind {
		case mr.Message:
			msgEvents = append(msgEvents, e)
		case mr.Note:
			noteEvents = append(noteEvents, e)
		}
	}
	if len(msgEvents) != len(sl.msgs) || len(noteEvents) != len(sl.notes) {
		out.add("%d messages and %d notes placed, want %d and %d", len(sl.msgs), len(sl.notes), len(msgEvents), len(noteEvents))
		return out
	}
	reach := func(i int, x float64) bool {
		return math.Abs(x-sl.cols[i]) <= sqActW/2+10*sqActStep+eps
	}
	prevY := math.Inf(-1)
	for k, m := range sl.msgs {
		e := msgEvents[k]
		a, b := idx[e.From], idx[e.To]
		first, last := m.pts[0], m.pts[len(m.pts)-1]
		if !all.contains(Rect{first.X, first.Y, first.X, first.Y}) || !all.contains(Rect{last.X, last.Y, last.X, last.Y}) {
			out.add("message %d runs outside the picture", k)
		}
		if first.Y <= prevY {
			out.add("message %d (%q) is not below the one before", k, e.Text)
		}
		prevY = first.Y
		if !reach(a, first.X) || !reach(b, last.X) {
			out.add("message %d (%q) does not start and end on its lifelines", k, e.Text)
		}
		// Each end is on its lifeline, or on the side of the outermost bar
		// open there, facing the other end.
		endAt := func(i int, p Pt, right bool) float64 {
			x := sl.cols[i]
			for _, bar := range sl.acts {
				if bar.Y0-eps <= p.Y && p.Y <= bar.Y1+eps && bar.X0 < x+10*sqActStep && bar.X1 > x-sqActW {
					if right {
						x = math.Max(x, bar.X1)
					} else {
						x = math.Min(x, bar.X0)
					}
				}
			}
			return x
		}
		// An arrow points at its receiver; a loop to itself goes right.
		if a != b && (last.X-first.X)*(sl.cols[b]-sl.cols[a]) <= 0 {
			out.add("message %d (%q) points the wrong way", k, e.Text)
		}
		if a == b && m.pts[1].X <= first.X {
			out.add("message %d (%q) to itself loops the wrong way", k, e.Text)
		}
		toRight := a == b || sl.cols[b] > sl.cols[a]
		if want := endAt(a, first, toRight); math.Abs(first.X-want) > eps {
			out.add("message %d (%q) starts at %.3f, not at %.3f (its lifeline or bar)", k, e.Text, first.X, want)
		}
		fromRight := a == b || sl.cols[a] > sl.cols[b]
		if want := endAt(b, last, fromRight); math.Abs(last.X-want) > eps {
			out.add("message %d (%q) ends at %.3f, not at %.3f (its lifeline or bar)", k, e.Text, last.X, want)
		}
		if m.tbox != (Rect{}) {
			texts = append(texts, labelled{m.tbox, "message " + e.Text})
			if m.tbox.Y1 > first.Y+eps {
				out.add("message %d's text is not above its arrow", k)
			}
			if strict && a != b && math.Abs(m.tbox.Center().X-(first.X+last.X)/2) > eps {
				out.add("message %d's text is not centred on its arrow", k)
			}
			if !all.contains(m.tbox) {
				out.add("message %d's text is outside the picture", k)
			}
			if a != b && (m.tbox.X0 < math.Min(first.X, last.X)-eps || m.tbox.X1 > math.Max(first.X, last.X)+eps) {
				out.add("message %d's text (%q) is longer than its arrow", k, e.Text)
			}
		}
		// A number stands clear of a head at the start.
		if m.both && m.number != "" && math.Abs(m.numAt.X-first.X) < arrowLen+sqNumR-eps {
			out.add("message %d's number covers its start head", k)
		}
		// A message to itself stays short of the next lifeline, text and
		// loop.
		if a == b && a+1 < len(sl.cols) {
			right := 0.0
			for _, p := range m.pts {
				right = math.Max(right, p.X)
			}
			if m.tbox != (Rect{}) {
				right = math.Max(right, m.tbox.X1)
			}
			if right >= sl.cols[a+1]-eps {
				out.add("message %d (%q) to itself reaches the next lifeline", k, e.Text)
			}
		}
	}
	for k, nt := range sl.notes {
		e := noteEvents[k]
		texts = append(texts, labelled{nt.box, "note " + e.Text})
		if !all.contains(nt.box) {
			out.add("note %q is outside the picture", e.Text)
		}
		lo, hi := min(idx[e.From], idx[e.To]), max(idx[e.From], idx[e.To])
		switch x := sl.cols[lo]; e.Place {
		case mr.LeftOf:
			if nt.box.X1 > x-eps {
				out.add("note %q is not left of %s", e.Text, e.From.ID)
			}
		case mr.RightOf:
			if nt.box.X0 < x+eps {
				out.add("note %q is not right of %s", e.Text, e.From.ID)
			}
		}
		for i, x := range sl.cols {
			crosses := x > nt.box.X0+eps && x < nt.box.X1-eps
			own := e.Place == mr.Over && i >= lo && i <= hi
			if crosses && !own {
				out.add("note %q covers %s's lifeline", e.Text, d.Participants[i].ID)
			}
			if own && !crosses {
				out.add("note %q is not over %s's lifeline", e.Text, d.Participants[i].ID)
			}
		}
	}
	for _, f := range sl.frames {
		if !all.contains(f.box) {
			out.add("a %s frame is outside the picture", f.kind)
		}
		texts = append(texts, labelled{f.tab, f.kind + " tab"})
		if f.cond != "" {
			texts = append(texts, labelled{f.condBox, f.kind + " " + f.cond})
			if !f.box.contains(f.condBox) {
				out.add("a %s frame's condition sticks out", f.kind)
			}
		}
		for _, s := range f.sections {
			if s.text != "" {
				texts = append(texts, labelled{s.tbox, f.kind + " " + s.text})
			}
		}
		in := func(y0, y1 float64) bool { return y0 > f.box.Y0 && y1 < f.box.Y1 }
		for _, m := range sl.msgs {
			for _, p := range m.pts {
				if in(p.Y, p.Y) && (p.X < f.box.X0 || p.X > f.box.X1) {
					out.add("a %s frame does not hold a message in its rows", f.kind)
				}
			}
			if m.tbox != (Rect{}) && in(m.tbox.Y0, m.tbox.Y1) && !f.box.contains(m.tbox) {
				out.add("a %s frame does not hold the text %q in its rows", f.kind, m.text)
			}
		}
		for _, nt := range sl.notes {
			if in(nt.box.Y0, nt.box.Y1) && !f.box.contains(nt.box) {
				out.add("a %s frame does not hold the note %q in its rows", f.kind, nt.text)
			}
		}
		for _, bar := range sl.acts {
			for _, y := range []float64{bar.Y0, bar.Y1} {
				if in(y, y) && (bar.X0 < f.box.X0 || bar.X1 > f.box.X1) {
					out.add("a %s frame does not hold an activation that starts or ends in its rows", f.kind)
				}
			}
		}
		for _, g := range sl.frames {
			if g != f && in(g.box.Y0, g.box.Y1) && !f.box.contains(g.box) {
				out.add("a %s frame does not hold the %s frame inside it", f.kind, g.kind)
			}
		}
	}
	for i, a := range texts {
		for _, b := range texts[i+1:] {
			if a.r.overlaps(b.r) {
				out.add("%s overlaps %s", a.what, b.what)
			}
		}
	}
	// A bar starts at the message that activates its participant (A->>+B,
	// or a message followed by activate) and ends at the one it sends
	// before deactivating (B-->>-A).
	mi := -1
	for k, e := range d.Events {
		if e.Kind == mr.Message {
			mi++
		}
		if mi < 0 || k == 0 || d.Events[k-1].Kind != mr.Message {
			continue
		}
		prev, y := d.Events[k-1], sl.msgs[mi].pts[0].Y
		if prev.From == prev.To {
			y = sl.msgs[mi].pts[len(sl.msgs[mi].pts)-1].Y
		}
		col := sl.cols[idx[e.From]]
		found := false
		for _, bar := range sl.acts {
			near := bar.X0 < col+10*sqActStep && bar.X1 > col-sqActW
			if e.Kind == mr.Activate && e.From == prev.To && near && math.Abs(bar.Y0-y) < eps ||
				e.Kind == mr.Deactivate && e.From == prev.From && near && math.Abs(bar.Y1-y) < eps {
				found = true
			}
		}
		if (e.Kind == mr.Activate && e.From == prev.To || e.Kind == mr.Deactivate && e.From == prev.From) && !found {
			out.add("no bar of %s %ss at message %d's arrow", e.From.ID, e.Kind, mi)
		}
	}
	for _, a := range sl.acts {
		if a.Y1 <= a.Y0 || !all.contains(a) {
			out.add("an activation bar %v is empty or outside", a)
		}
	}
	return out
}

// onOutline reports whether p lies on the polygon's boundary.
func onOutline(poly []Pt, p Pt) bool {
	for i := range poly {
		a, b := poly[i], poly[(i+1)%len(poly)]
		if distToSeg(p, a, b) < 1e-4 {
			return true
		}
	}
	return false
}

func distToSeg(p, a, b Pt) float64 {
	dx, dy := b.X-a.X, b.Y-a.Y
	l2 := dx*dx + dy*dy
	t := 0.0
	if l2 > 0 {
		t = math.Max(0, math.Min(1, ((p.X-a.X)*dx+(p.Y-a.Y)*dy)/l2))
	}
	return math.Hypot(p.X-(a.X+t*dx), p.Y-(a.Y+t*dy))
}

func onRectBorder(r Rect, p Pt) bool {
	return onOutline([]Pt{{r.X0, r.Y0}, {r.X1, r.Y0}, {r.X1, r.Y1}, {r.X0, r.Y1}}, p)
}

func inPolygon(poly []Pt, p Pt) bool {
	in := false
	for i, j := 0, len(poly)-1; i < len(poly); j, i = i, i+1 {
		a, b := poly[i], poly[j]
		if (a.Y > p.Y) != (b.Y > p.Y) && p.X < (b.X-a.X)*(p.Y-a.Y)/(b.Y-a.Y)+a.X {
			in = !in
		}
	}
	return in
}

func segDist(a, b, c, d Pt) float64 {
	return math.Min(math.Min(distToSeg(a, c, d), distToSeg(b, c, d)), math.Min(distToSeg(c, a, b), distToSeg(d, a, b)))
}

// angleBetween is the acute angle between two segments, in degrees.
func angleBetween(a, b segment) float64 {
	ang := func(p, q Pt) float64 { return math.Atan2(q.Y-p.Y, q.X-p.X) }
	d := math.Abs(ang(a.a, a.b)-ang(b.a, b.b)) * 180 / math.Pi
	d = math.Mod(d, 180)
	return math.Min(d, 180-d)
}
