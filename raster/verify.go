package raster

import (
	"fmt"
	"math"
	"slices"
	"strings"

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
func verify(d mr.Diagram, lay *flowLayout, el *erLayout, sl *seqLayout, pl *pieLayout, st *stateLayout, m measurer) []string {
	switch d := d.(type) {
	case *mr.StateDiagram:
		return stateFaults(d, st, m, false, nil)
	case *mr.Pie:
		return pieFaults(d, pl)
	case *mr.Flowchart:
		return flowFaults(d, lay, m, false, nil)
	case *mr.ER:
		return append(flowFaults(el.graph, el.flowLayout, m, false, nil), erFaults(d, el, m, false)...)
	case *mr.Sequence:
		return seqFaults(d, sl, false)
	}
	return nil
}

// segment is one straight piece of link k.
type segment struct {
	a, b pt
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
func flowFaults(f *mr.Flowchart, lay *flowLayout, m measurer, strict bool, frameCrossings *int) []string {
	var out faults
	if len(lay.Nodes) != len(f.Nodes) || len(lay.Edges) != len(f.Links) || len(lay.Frames) != len(f.Subgraphs) {
		out.add("placed %d nodes, %d links, %d frames; want %d, %d, %d", len(lay.Nodes), len(lay.Edges), len(lay.Frames), len(f.Nodes), len(f.Links), len(f.Subgraphs))
		return out
	}
	box := map[string]nodeBox{}
	for _, n := range lay.Nodes {
		box[n.ID] = n
	}
	frame := map[string]frameBox{}
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
			a, b   pt
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
	all := rect{-eps, -eps, lay.W + eps, lay.H + eps}
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
	// A node's label fits inside its shape (a state's bar or circle has
	// none to fit).
	for _, n := range lay.Nodes {
		if n.Label == "" {
			continue
		}
		tw, th, err := m(n.Label, false)
		if err != nil {
			continue
		}
		c := labelCenter(n)
		poly := outline(n.Shape, n.Box, n.Slant)
		for _, p := range []pt{{c.X - tw/2, c.Y - th/2}, {c.X + tw/2, c.Y - th/2}, {c.X + tw/2, c.Y + th/2}, {c.X - tw/2, c.Y + th/2}} {
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
	var labels []rect
	for _, e := range lay.Edges {
		if e.Label == "" {
			continue
		}
		if e.LabelBox == (rect{}) {
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
	var nodeGrid grid
	for i, n := range lay.Nodes {
		nodeGrid.add(i, n.Box, 0)
	}
	arrivals := map[string][]pt{}
	var arrivalOrder []string // map order must not reach the first fault
	for _, e := range lay.Edges {
		lk := e.Link
		name := lk.From.ID + "->" + lk.To.ID
		if len(e.Points) < 2 {
			out.add("link %s has %d points", name, len(e.Points))
			continue
		}
		endOK := func(ep mr.Endpoint, p pt) bool {
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
			at pt
		}{{lk.From, first}, {lk.To, last}} {
			if arrivals[p.ep.ID] == nil {
				arrivalOrder = append(arrivalOrder, p.ep.ID)
			}
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
			for _, ni := range nodeGrid.near(bbox(a, b), 0) {
				n := lay.Nodes[ni]
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
			if !all.contains(rect{math.Min(a.X, b.X), math.Min(a.Y, b.Y), math.Max(a.X, b.X), math.Max(a.Y, b.Y)}) {
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
	// Only segments near each other are compared: two whose boxes are
	// more than apart away cannot cross or come closer.
	var segGrid grid
	for i, sg := range segs {
		segGrid.add(i, bbox(sg.a, sg.b), apart/2)
	}
	for i := range segs {
		for _, j := range segGrid.near(bbox(segs[i].a, segs[i].b), apart/2) {
			if j <= i {
				continue
			}
			a, b := segs[i], segs[j]
			if a.link == b.link {
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
		for _, si := range segGrid.near(e.LabelBox, 0) {
			if sg := segs[si]; sg.link != li && segmentHitsRect(sg.a, sg.b, e.LabelBox, 1e-3) {
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
	for _, id := range arrivalOrder {
		pts := arrivals[id]
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
		p    pt
		node int
	}
	var ends []endAt
	var zones []rect
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
		if len(p) < 2 {
			zones = append(zones, rect{}, rect{}) // two per edge, as labels index them
			continue                              // flowFaults reports it
		}
		for _, s := range [][2]pt{{p[0], p[1]}, {p[len(p)-1], p[len(p)-2]}} {
			tip, next := s[0], s[1]
			if d := math.Hypot(next.X-tip.X, next.Y-tip.Y); d < run {
				out.add("link %s->%s: a marker's run is %.3f em (needs %.2f)", e.Link.From.ID, e.Link.To.ID, d, run)
			}
			ux, uy := (next.X - tip.X), (next.Y - tip.Y)
			l := math.Hypot(ux, uy)
			ux, uy = ux/l, uy/l
			a := pt{tip.X - uy*markHalf, tip.Y + ux*markHalf}
			b := pt{tip.X + ux*markerReach + uy*markHalf, tip.Y + uy*markerReach - ux*markHalf}
			zones = append(zones, rect{math.Min(a.X, b.X), math.Min(a.Y, b.Y), math.Max(a.X, b.X), math.Max(a.Y, b.Y)})
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
		if !strict || e.Label == "" || e.Link.From == e.Link.To || len(e.Points) < 2 {
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
	all := rect{-eps, -eps, sl.W + eps, sl.H + eps}
	type labelled struct {
		r    rect
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
	// A lifeline's reach is its bars' span, however deep they nest.
	lo := append([]float64(nil), sl.cols...)
	hi := append([]float64(nil), sl.cols...)
	for j, bar := range sl.acts {
		i := sl.actCol[j]
		lo[i], hi[i] = math.Min(lo[i], bar.X0), math.Max(hi[i], bar.X1)
	}
	reach := func(i int, x float64) bool { return x >= lo[i]-eps && x <= hi[i]+eps }
	// Whether the event after message k activates the message's receiver:
	// then the arrow ends on the bar it opens (A->>+B, or activate B next).
	activates := make([]bool, len(msgEvents))
	for k, mk := 0, 0; k < len(d.Events); k++ {
		if d.Events[k].Kind != mr.Message {
			continue
		}
		if k+1 < len(d.Events) && d.Events[k+1].Kind == mr.Activate && d.Events[k+1].From == d.Events[k].To {
			activates[mk] = true
		}
		mk++
	}
	prevY := math.Inf(-1)
	for k, m := range sl.msgs {
		e := msgEvents[k]
		a, b := idx[e.From], idx[e.To]
		first, last := m.pts[0], m.pts[len(m.pts)-1]
		if !all.contains(rect{first.X, first.Y, first.X, first.Y}) || !all.contains(rect{last.X, last.Y, last.X, last.Y}) {
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
		// open there, facing the other end: a bar opened before the
		// message, and at the receiver the one bar the message opens. A
		// bar opened after it (activate A after A->>B) is not open yet, as
		// in mermaid.
		endAt := func(i int, p pt, right, opens bool) float64 {
			x := sl.cols[i]
			use := func(bar rect) {
				if right {
					x = math.Max(x, bar.X1)
				} else {
					x = math.Min(x, bar.X0)
				}
			}
			var opened *rect
			for j, bar := range sl.acts {
				switch {
				case sl.actCol[j] != i || p.Y > bar.Y1+eps:
				case bar.Y0 < p.Y-eps:
					use(bar)
				case opens && math.Abs(bar.Y0-p.Y) <= eps && (opened == nil || bar.X0 < opened.X0):
					opened = &sl.acts[j]
				}
			}
			if opened != nil {
				use(*opened)
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
		// Where exactly on its lifeline or bar an end sits is precision,
		// not a wrong picture: reach above already refuses an end off its
		// participant.
		if strict {
			toRight := a == b || sl.cols[b] > sl.cols[a]
			if want := endAt(a, first, toRight, false); math.Abs(first.X-want) > eps {
				out.add("message %d (%q) starts at %.3f, not at %.3f (its lifeline or bar)", k, e.Text, first.X, want)
			}
			fromRight := a == b || sl.cols[a] > sl.cols[b]
			if want := endAt(b, last, fromRight, activates[k]); math.Abs(last.X-want) > eps {
				out.add("message %d (%q) ends at %.3f, not at %.3f (its lifeline or bar)", k, e.Text, last.X, want)
			}
		}
		if m.tbox != (rect{}) {
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
			if m.tbox != (rect{}) {
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
			if m.tbox != (rect{}) && in(m.tbox.Y0, m.tbox.Y1) && !f.box.contains(m.tbox) {
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
		found := false
		for j, bar := range sl.acts {
			near := sl.actCol[j] == idx[e.From]
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
func onOutline(poly []pt, p pt) bool {
	for i := range poly {
		a, b := poly[i], poly[(i+1)%len(poly)]
		if distToSeg(p, a, b) < 1e-4 {
			return true
		}
	}
	return false
}

func distToSeg(p, a, b pt) float64 {
	dx, dy := b.X-a.X, b.Y-a.Y
	l2 := dx*dx + dy*dy
	t := 0.0
	if l2 > 0 {
		t = math.Max(0, math.Min(1, ((p.X-a.X)*dx+(p.Y-a.Y)*dy)/l2))
	}
	return math.Hypot(p.X-(a.X+t*dx), p.Y-(a.Y+t*dy))
}

func onRectBorder(r rect, p pt) bool {
	return onOutline([]pt{{r.X0, r.Y0}, {r.X1, r.Y0}, {r.X1, r.Y1}, {r.X0, r.Y1}}, p)
}

func inPolygon(poly []pt, p pt) bool {
	in := false
	for i, j := 0, len(poly)-1; i < len(poly); j, i = i, i+1 {
		a, b := poly[i], poly[j]
		if (a.Y > p.Y) != (b.Y > p.Y) && p.X < (b.X-a.X)*(p.Y-a.Y)/(b.Y-a.Y)+a.X {
			in = !in
		}
	}
	return in
}

func segDist(a, b, c, d pt) float64 {
	return math.Min(math.Min(distToSeg(a, c, d), distToSeg(b, c, d)), math.Min(distToSeg(c, a, b), distToSeg(d, a, b)))
}

// angleBetween is the acute angle between two segments, in degrees.
func angleBetween(a, b segment) float64 {
	ang := func(p, q pt) float64 { return math.Atan2(q.Y-p.Y, q.X-p.X) }
	d := math.Abs(ang(a.a, a.b)-ang(b.a, b.b)) * 180 / math.Pi
	d = math.Mod(d, 180)
	return math.Min(d, 180-d)
}

func bbox(a, b pt) rect {
	return rect{math.Min(a.X, b.X), math.Min(a.Y, b.Y), math.Max(a.X, b.X), math.Max(a.Y, b.Y)}
}

// grid buckets rectangles by square cells, so a check compares only what
// lies near: comparing everything with everything took up to half a
// second at the limits.
type grid struct {
	cells map[[2]int][]int
}

const gridCell = 4.0 // em

// cells gives the cells r, grown by pad, covers; none for a rectangle
// that is not finite (the checks report it as outside the layout).
func cells(r rect, pad float64) (x0, y0, x1, y1 int, ok bool) {
	for _, v := range []float64{r.X0, r.Y0, r.X1, r.Y1} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return 0, 0, 0, 0, false
		}
	}
	f := func(v float64) int { return int(math.Floor(v / gridCell)) }
	return f(r.X0 - pad), f(r.Y0 - pad), f(r.X1 + pad), f(r.Y1 + pad), true
}

func (g *grid) add(i int, r rect, pad float64) {
	if g.cells == nil {
		g.cells = map[[2]int][]int{}
	}
	x0, y0, x1, y1, ok := cells(r, pad)
	if !ok {
		return
	}
	for x := x0; x <= x1; x++ {
		for y := y0; y <= y1; y++ {
			g.cells[[2]int{x, y}] = append(g.cells[[2]int{x, y}], i)
		}
	}
}

// near gives, in ascending order and once each, the indexes added in a
// cell that r, grown by pad, covers.
func (g *grid) near(r rect, pad float64) []int {
	x0, y0, x1, y1, ok := cells(r, pad)
	if !ok {
		return nil
	}
	var out []int
	for x := x0; x <= x1; x++ {
		for y := y0; y <= y1; y++ {
			out = append(out, g.cells[[2]int{x, y}]...)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// pieFaults checks a pie chart's layout: the slices are the items over 1%,
// in order, together a full turn with none empty; the legend names every
// item in order with its percentage ("<1%" for one with no slice), each row
// on one line below the last; a slice's percentage is its own; no text lies
// over another or outside the picture; a percentage written on a slice lies
// inside that slice; the legend is clear of the circle.
// Every check is about a wrong picture, so there is no strict reading.
func pieFaults(p *mr.Pie, pl *pieLayout) []string {
	var out faults
	kept, angle, pct := pieShares(p)
	if len(pl.wedges) != len(kept) {
		out.add("%d slices drawn, want %d", len(pl.wedges), len(kept))
		return out
	}
	for k, w := range pl.wedges {
		if w.item != kept[k] {
			out.add("slice %d is item %d, want %d", k, w.item, kept[k])
		}
		if w.pct != pct[k] {
			out.add("slice %d shows %q, want %q", k, w.pct, pct[k])
		}
		if !(w.a1 > w.a0) || math.Abs(w.a0-angle[k]) > 1e-9 || math.Abs(w.a1-angle[k+1]) > 1e-9 {
			out.add("slice %d runs %.4f to %.4f", k, w.a0, w.a1)
		}
	}
	if n := len(pl.wedges); n > 0 && math.Abs(pl.wedges[n-1].a1-2*math.Pi) > 1e-6 {
		out.add("the slices end at %.4f, not a full turn", pl.wedges[n-1].a1)
	}
	if len(pl.legend) != len(p.Slices) {
		out.add("%d legend rows, want %d", len(pl.legend), len(p.Slices))
		return out
	}
	for i, r := range pl.legend {
		if r.item != i {
			out.add("legend row %d is item %d", i, r.item)
		} else if want := legendPct(p, i); r.pct != want {
			out.add("legend row %d shows %q, want %q", i, r.pct, want)
		}
	}
	all := rect{-eps, -eps, pl.W + eps, pl.H + eps}
	circle := rect{pl.c.X - pieR, pl.c.Y - pieR, pl.c.X + pieR, pl.c.Y + pieR}
	var texts []rect
	var what []string
	for _, w := range pl.wedges {
		if !w.inside {
			continue
		}
		texts, what = append(texts, w.box), append(what, "the percentage of "+p.Slices[w.item].Label)
		if len(pl.wedges) > 1 && !inWedge(w.box, pl.c, pieR, w.a0, w.a1) ||
			len(pl.wedges) == 1 && !rectInCircle(w.box, pl.c, pieR) {
			out.add("the percentage of %q is not inside its slice", p.Slices[w.item].Label)
		}
	}
	for i, r := range pl.legend {
		// A row's swatch, label and percentage share one line, below the
		// row before: a percentage beside another item's label is wrong.
		y := r.swatch.Center().Y
		if math.Abs(r.box.Center().Y-y) > eps || r.pct != "" && math.Abs(r.pctBox.Center().Y-y) > eps {
			out.add("legend row %q is not on one line", r.text)
		}
		if i > 0 && !(y > pl.legend[i-1].swatch.Center().Y) {
			out.add("legend row %q is not below the one before", r.text)
		}
		parts := []rect{r.swatch, r.box}
		texts, what = append(texts, r.box, r.swatch), append(what, "legend "+r.text, "the swatch of "+r.text)
		if r.pct != "" {
			parts = append(parts, r.pctBox)
			texts, what = append(texts, r.pctBox), append(what, "the legend percentage of "+r.text)
		}
		for _, b := range parts {
			if rectMeetsCircle(b, pl.c, pieR) || b.overlaps(circle) && b.X0 < pl.c.X {
				out.add("legend row %q lies over the circle", r.text)
				break
			}
		}
	}
	for i, a := range texts {
		if !all.contains(a) {
			out.add("%s is outside the picture", what[i])
		}
		for j := i + 1; j < len(texts); j++ {
			if a.overlaps(texts[j]) {
				out.add("%s overlaps %s", what[i], what[j])
			}
		}
	}
	return out
}

// stateFaults checks a state diagram: every scope's layout has the
// flowchart's properties and holds exactly its states, notes and
// transitions; a titled state holds its title and lines; a composite's
// frame is its box in the parent's layout, and its title band and regions
// lie inside the frame, the regions below the band and apart.
func stateFaults(d *mr.StateDiagram, sl *stateLayout, m measurer, strict bool, frameCrossings *int) []string {
	var out faults
	for _, sc := range sl.scopes {
		s := sc.scope
		nodeNotes := 0
		for _, gi := range sc.noteNode {
			if gi >= 0 {
				nodeNotes++
			}
		}
		if len(sc.noteNode) != len(s.Notes) || len(sc.graph.Nodes) != len(s.States)+nodeNotes || len(sc.graph.Links) != len(s.Transitions)+nodeNotes {
			out.add("a scope laid out %d nodes and %d links, want %d and %d", len(sc.graph.Nodes), len(sc.graph.Links),
				len(s.States)+nodeNotes, len(s.Transitions)+nodeNotes)
			continue
		}
		if len(sc.graph.Nodes) > 0 {
			for _, f := range flowFaults(sc.graph, sc.lay, m, strict, frameCrossings) {
				out.add("%s", f)
			}
		}
		if len(sc.main) != len(s.States) || len(sc.noteBox) != len(s.Notes) {
			out.add("a scope split %d states and %d notes, want %d and %d", len(sc.main), len(sc.noteBox), len(s.States), len(s.Notes))
			continue
		}
		// A state's own part and the notes beside it lie in its node and
		// apart; every note holds its text.
		for i := range sc.main {
			if len(sc.graph.Nodes) > 0 && !within(sc.lay.Nodes[i].Box, sc.main[i]) {
				out.add("state %s's own part is outside its node", sc.states[i].ID)
			}
		}
		for k, nt := range s.Notes {
			b := sc.noteBox[k]
			tw, th, err := m(nt.Text, false)
			if err == nil && (tw > b.W()-2*padX+eps || th > b.H()-2*padY+eps) {
				out.add("the note on %s does not hold its text", nt.State)
			}
			if sc.noteNode[k] >= 0 {
				continue
			}
			i := sc.stateIndex(nt.State)
			if i < 0 {
				out.add("the note on %s has no state", nt.State)
				continue
			}
			node, mn := sc.lay.Nodes[i].Box, sc.main[i]
			if !within(node, b) || b.overlaps(mn) {
				out.add("the note on %s is not beside it", nt.State)
			}
			if nt.Left != (b.X1 <= mn.X0+eps) {
				out.add("the note on %s is on the wrong side", nt.State)
			}
			for j := range k {
				if sc.noteNode[j] < 0 && sc.noteBox[j].overlaps(b) {
					out.add("notes overlap beside %s", nt.State)
				}
			}
		}
		// A transition meets a state with notes beside it on the state's
		// own part, never on a note or the empty slot across from one.
		for _, e := range sc.lay.Edges {
			if len(e.Points) < 2 {
				continue
			}
			for _, end := range []struct {
				id string
				p  pt
			}{{e.Link.From.ID, e.Points[0]}, {e.Link.To.ID, e.Points[len(e.Points)-1]}} {
				i := sc.stateIndex(end.id)
				if i < 0 || sc.beside[i] == nil {
					continue
				}
				if mn := sc.main[i]; end.p.X < mn.X0-eps || end.p.X > mn.X1+eps {
					out.add("a transition meets %s beside its own part, at %v", end.id, end.p)
				}
			}
		}
		// A scope's transition labels stay in the scope's own area (its
		// links flowFaults keeps there): past it they run into a sibling
		// region or the frame.
		area := rect{-eps, -eps, sc.lay.W + eps, sc.lay.H + eps}
		for _, e := range sc.lay.Edges {
			if e.Label != "" && !within(area, e.LabelBox) {
				out.add("the label %q leaves its scope", e.Label)
			}
		}
		titled := make([]int, 0, len(sc.titled))
		for i := range sc.titled {
			titled = append(titled, i)
		}
		slices.Sort(titled)
		for _, i := range titled {
			t, b := sc.titled[i], sc.main[i]
			if math.Max(t.tw, t.lw) > b.W()+eps || t.th+stRuleGap+t.lh > b.H()-2*padY+eps {
				out.add("the title and lines of %s stick out of it", sc.states[i].ID)
			}
		}
	}
	for _, c := range sl.comps {
		if c.idx >= len(c.parent.lay.Nodes) {
			out.add("composite %s has no box", c.node.ID)
			continue
		}
		b := c.parent.main[c.idx]
		o := c.parent.off
		if want := (rect{b.X0 + o.X, b.Y0 + o.Y, b.X1 + o.X, b.Y1 + o.Y}); c.frame != want {
			out.add("composite %s's frame %v is not its box %v", c.node.ID, c.frame, want)
		}
		if !c.frame.contains(c.band) || !c.band.contains(c.title) {
			out.add("composite %s's title is outside its band or frame", c.node.ID)
		}
		if len(c.regionR) != len(c.regions) {
			out.add("composite %s placed %d of %d regions", c.node.ID, len(c.regionR), len(c.regions))
			continue
		}
		inner := rect{c.frame.X0, c.band.Y1, c.frame.X1, c.frame.Y1}
		for i, r := range c.regionR {
			rs := c.regions[i]
			if want := (rect{rs.off.X, rs.off.Y, rs.off.X + rs.lay.W, rs.off.Y + rs.lay.H}); r != want {
				out.add("composite %s's region %d is not where its scope is", c.node.ID, i+1)
			}
			if !inner.contains(r) {
				out.add("composite %s's region %d lies outside its frame", c.node.ID, i+1)
			}
			for _, q := range c.regionR[:i] {
				if r.overlaps(q) {
					out.add("composite %s's regions overlap", c.node.ID)
				}
			}
		}
	}
	return out
}

// ganttFaults checks a gantt chart: one bar per task in its row, in source
// order, where the time scale puts it; a task's text inside its bar or to
// its right in its row; section titles in the section column, within their
// run; no text over another text or another task's bar; axis labels in
// order; everything inside the picture.
func ganttFaults(g *mr.Gantt, gl *ganttLayout, m measurer) []string {
	var out faults
	var rows, verts []*mr.GanttTask
	for _, t := range g.Tasks {
		if t.Vert {
			verts = append(verts, t)
		} else {
			rows = append(rows, t)
		}
	}
	if len(gl.bars) != len(rows) || len(gl.verts) != len(verts) {
		out.add("placed %d bars and %d markers, want %d and %d", len(gl.bars), len(gl.verts), len(rows), len(verts))
		return out
	}
	near := func(a, b float64) bool { return math.Abs(a-b) < 1e-6 }
	all := rect{-eps, -eps, gl.W + eps, gl.H + eps}
	type labelled struct {
		r    rect
		what string
		own  int // the bar a task's text may lie on, or -1
	}
	var texts []labelled
	prevY := math.Inf(-1)
	for i, b := range gl.bars {
		t := b.task
		if i > 0 && b.task.Row <= gl.bars[i-1].task.Row {
			out.add("task %s is not below the task before it in the source", t.ID)
		}
		if !(b.box.Y0 > prevY) {
			out.add("task %s's bar is not below the bar before it", t.ID)
		}
		prevY = b.box.Y0
		x0 := gl.x(float64(t.Start))
		if t.Milestone {
			c := x0 + (gl.x(float64(t.End))-x0)/2
			if !near(b.box.Center().X, c) || !near(b.box.W(), gtBar) {
				out.add("milestone %s is not at its time", t.ID)
			}
		} else if !near(b.box.X0, x0) || !near(b.box.X1, math.Max(gl.x(float64(t.Bar)), x0)) {
			out.add("task %s's bar is not at its times", t.ID)
		}
		if t.Text == "" {
			continue
		}
		tw, th, err := m(t.Text, false)
		if err == nil && (b.text.W() < tw-eps || b.text.H() < th-eps) {
			out.add("task %s's text box does not hold its text", t.ID)
		}
		if b.inside {
			if !within(b.box, b.text) {
				out.add("task %s's text sticks out of its bar", t.ID)
			}
		} else if b.text.X0 < b.box.X1-eps || b.text.Y0 < b.box.Y0-gtRow || b.text.Y1 > b.box.Y1+gtRow {
			out.add("task %s's text is not to the right of its bar", t.ID)
		}
		texts = append(texts, labelled{b.text, "the text of " + t.ID, i})
	}
	// Each bar lies on its own row's stripe.
	if len(gl.stripes) != len(gl.bars) {
		out.add("%d row stripes for %d rows", len(gl.stripes), len(gl.bars))
	}
	for i, b := range gl.bars {
		if i >= len(gl.stripes) {
			break
		}
		if s := gl.stripes[i]; b.box.Y0 < s.Y0-eps || b.box.Y1 > s.Y1+eps {
			out.add("task %s's bar is not on its row", b.task.ID)
		}
	}
	// Excluded days are shaded where the scale puts them.
	var want []rect
	for k := 0; k+1 < len(g.Excluded); k += 2 {
		a := math.Max(gl.x(float64(g.Excluded[k])), gl.x0)
		b := math.Min(gl.x(float64(g.Excluded[k+1])), gl.x0+gl.T)
		if b > a {
			want = append(want, rect{a, gl.rowsTop, b, gl.rowsBottom})
		}
	}
	if len(want) != len(gl.excluded) {
		out.add("%d excluded runs shaded, want %d", len(gl.excluded), len(want))
	} else {
		for i, r := range gl.excluded {
			if !near(r.X0, want[i].X0) || !near(r.X1, want[i].X1) || !near(r.Y0, want[i].Y0) || !near(r.Y1, want[i].Y1) {
				out.add("an excluded run is shaded at %v, want %v", r, want[i])
			}
		}
	}
	for _, s := range gl.sections {
		if !within(s.run, s.text) {
			out.add("section title %q lies outside its rows or column", strings.Join(s.lines, " "))
		}
		texts = append(texts, labelled{s.text, "section title " + strings.Join(s.lines, " "), -1})
	}
	for i, tk := range gl.ticks {
		if i > 0 && !(tk.x > gl.ticks[i-1].x) {
			out.add("axis tick %q is out of order", tk.label)
		}
		if !near(tk.x, gl.x(tk.t)) {
			out.add("axis tick %q is not at its time", tk.label)
		}
		if tk.label != "" {
			texts = append(texts, labelled{tk.box, "axis label " + tk.label, -1})
		}
	}
	for _, v := range gl.verts {
		if !near(v.x, gl.x(float64(v.task.Start))) {
			out.add("marker %s is not at its time", v.task.ID)
		}
		if v.y0 > gl.rowsTop+eps || v.y1 < gl.rowsBottom-eps {
			out.add("marker %s does not cross every row", v.task.ID)
		}
		if v.task.Text != "" {
			texts = append(texts, labelled{v.label, "the text of marker " + v.task.ID, -1})
		}
	}
	for i, a := range texts {
		if !all.contains(a.r) {
			out.add("%s is outside the picture", a.what)
		}
		for j := i + 1; j < len(texts); j++ {
			if a.r.overlaps(texts[j].r) {
				out.add("%s overlaps %s", a.what, texts[j].what)
			}
		}
		for k, b := range gl.bars {
			if k != a.own && a.r.overlaps(b.box) {
				out.add("%s lies over the bar of %s", a.what, b.task.ID)
			}
		}
	}
	for _, b := range gl.bars {
		if !all.contains(b.box) {
			out.add("task %s's bar is outside the picture", b.task.ID)
		}
	}
	return out
}

// within is contains with room for rounding.
func within(outer, inner rect) bool {
	return outer.X0-eps <= inner.X0 && outer.Y0-eps <= inner.Y0 && inner.X1 <= outer.X1+eps && inner.Y1 <= outer.Y1+eps
}

// rectInCircle reports whether r lies inside the disc of radius rad
// around c.
func rectInCircle(r rect, c pt, rad float64) bool {
	for _, q := range []pt{{r.X0, r.Y0}, {r.X1, r.Y0}, {r.X0, r.Y1}, {r.X1, r.Y1}} {
		if math.Hypot(q.X-c.X, q.Y-c.Y) > rad+eps {
			return false
		}
	}
	return true
}

// rectMeetsCircle reports whether r shares area with the disc of radius
// rad around c.
func rectMeetsCircle(r rect, c pt, rad float64) bool {
	dx := math.Max(r.X0-c.X, math.Max(0, c.X-r.X1))
	dy := math.Max(r.Y0-c.Y, math.Max(0, c.Y-r.Y1))
	return math.Hypot(dx, dy) < rad-eps
}

// mindmapFaults checks a mind map's layout: every node placed once inside
// the picture, no two shapes' boxes overlapping, each text inside its
// shape and wrapped within mmWrap em, the tree on two sides in bands (a
// child one gap out from its parent's outer side, siblings in order, a
// parent between its first and last child, a band holding only its
// subtree), and each line from its parent's side to its child's with
// nothing but lines between them.
func mindmapFaults(m *mr.Mindmap, ml *mindmapLayout, measure measurer) []string {
	var out faults
	if len(ml.nodes) != len(m.Nodes) || len(ml.edges) != max(0, len(m.Nodes)-1) {
		out.add("placed %d nodes and %d lines, want %d and %d", len(ml.nodes), len(ml.edges), len(m.Nodes), max(0, len(m.Nodes)-1))
		return out
	}
	if len(m.Nodes) == 0 {
		return out
	}
	near := func(a, b float64) bool { return math.Abs(a-b) < 1e-6 }
	all := rect{-eps, -eps, ml.W + eps, ml.H + eps}
	// The subtree of each node, as a range of source order (preorder).
	end := make([]int, len(m.Nodes))
	for i := len(m.Nodes) - 1; i >= 0; i-- {
		end[i] = i + 1
		if k := m.Nodes[i].Children; len(k) > 0 {
			end[i] = end[k[len(k)-1]]
		}
	}
	for i, n := range ml.nodes {
		mn := m.Nodes[i]
		if !within(all, n.box) || !within(all, n.band) {
			out.add("node %d is outside the picture", i)
		}
		// The text: its box the size of what is drawn, inside the shape.
		tw, th, err := measure(n.text, false)
		if err != nil || !near(n.tbox.W(), tw) || !near(n.tbox.H(), th) {
			out.add("node %d: its text box is not its text's size", i)
		}
		poly := mmOutline(mn.Shape, n.box)
		t := n.tbox
		for _, q := range []pt{{t.X0, t.Y0}, {t.X1, t.Y0}, {t.X1, t.Y1}, {t.X0, t.Y1}, t.Center(), {t.Center().X, t.Y0}, {t.Center().X, t.Y1}, {t.X0, t.Center().Y}, {t.X1, t.Center().Y}} {
			if !inPolygon(poly, q) {
				out.add("node %d: its text leaves its shape", i)
				break
			}
		}
		if !within(n.box, polyBounds(poly)) {
			out.add("node %d: its shape leaves its box", i)
		}
		// Wrapping: nothing lost, every line within the width or unbreakable.
		if strip(n.text) != strip(mn.Text) {
			out.add("node %d: its wrapped text %q is not %q", i, n.text, mn.Text)
		}
		for _, l := range strings.Split(n.text, "\n") {
			if w, _, err := measure(l, false); err == nil && w > mmWrapOf(mn.Shape)+eps && len(breakSegments(l)) > 1 {
				out.add("node %d: line %q is wider than %v em and could break", i, l, mmWrapOf(mn.Shape))
			}
		}
		// Sides: the root in the middle, its children alternating from the
		// left, their descendants on their side.
		want := 0
		switch {
		case mn.Parent == 0:
			want = 2*(indexOf(m.Nodes[0].Children, i)%2) - 1
		case mn.Parent > 0:
			want = ml.nodes[mn.Parent].side
		}
		if n.side != want {
			out.add("node %d is on side %d, want %d", i, n.side, want)
			continue
		}
		if !near(n.cy, n.box.Center().Y) {
			out.add("node %d: its middle is off its box", i)
		}
		if mn.Parent >= 0 {
			p := ml.nodes[mn.Parent]
			if (n.side > 0 && !near(n.box.X0, p.box.X1+mmGapX)) || (n.side < 0 && !near(n.box.X1, p.box.X0-mmGapX)) {
				out.add("node %d does not stand one gap out from its parent's side", i)
			}
		}
		// The band: all of the subtree, and nothing else.
		for j := range ml.nodes {
			in := j >= i && j < end[i]
			if in && !within(n.band, ml.nodes[j].box) {
				out.add("node %d's band does not hold node %d", i, j)
			}
			if !in && i > 0 && ml.nodes[j].box.overlaps(n.band) {
				out.add("node %d lies in node %d's band", j, i)
			}
			if j > i && n.box.overlaps(ml.nodes[j].box) {
				out.add("nodes %d and %d overlap", i, j)
			}
		}
		// Children: bands in order, apart, the parent between the first and
		// the last one's middle.
		for s, kids := range mmKids(m, ml, i) {
			if len(kids) == 0 {
				continue
			}
			gap := mmGapY
			if i == 0 {
				gap = mmBranchGap
			}
			for k := 1; k < len(kids); k++ {
				if ml.nodes[kids[k-1]].band.Y1+gap > ml.nodes[kids[k]].band.Y0+eps {
					out.add("the bands of nodes %d and %d are out of order or too close", kids[k-1], kids[k])
				}
			}
			mid := (ml.nodes[kids[0]].cy + ml.nodes[kids[len(kids)-1]].cy) / 2
			if !near(n.cy, mid) {
				out.add("node %d is not between its children on side %d", i, s)
			}
		}
	}
	for k, e := range ml.edges {
		c := k + 1
		p := m.Nodes[c].Parent
		if e.from != p || e.to != c || len(e.pts) < 2 {
			out.add("line %d joins %d and %d, want %d and %d", k, e.from, e.to, p, c)
			continue
		}
		pn, cn := ml.nodes[p], ml.nodes[c]
		a, b := pt{pn.box.X1, pn.cy}, pt{cn.box.X0, cn.cy}
		if cn.side < 0 {
			a, b = pt{pn.box.X0, pn.cy}, pt{cn.box.X1, cn.cy}
		}
		first, last := e.pts[0], e.pts[len(e.pts)-1]
		if !near(first.X, a.X) || !near(first.Y, a.Y) || !near(last.X, b.X) || !near(last.Y, b.Y) {
			out.add("line %d does not run from its parent's side to its child's", k)
		}
		between := rect{math.Min(a.X, b.X), math.Min(a.Y, b.Y), math.Max(a.X, b.X), math.Max(a.Y, b.Y)}
		for _, q := range e.pts {
			if q.X < between.X0-eps || q.X > between.X1+eps || q.Y < between.Y0-eps || q.Y > between.Y1+eps {
				out.add("line %d leaves the space between its nodes", k)
				break
			}
		}
		strip := rect{between.X0 + eps, between.Y0 - eps, between.X1 - eps, between.Y1 + eps}
		for j, o := range ml.nodes {
			if o.box.overlaps(strip) {
				out.add("node %d lies between nodes %d and %d", j, p, c)
			}
		}
	}
	return out
}

// mmKids are a node's children in the layout, by side: the root's are its
// two sides.
func mmKids(m *mr.Mindmap, ml *mindmapLayout, i int) [][]int {
	if i == 0 {
		return ml.sides[:]
	}
	return [][]int{m.Nodes[i].Children}
}

func polyBounds(poly []pt) rect {
	r := rect{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	for _, q := range poly {
		r = rect{math.Min(r.X0, q.X), math.Min(r.Y0, q.Y), math.Max(r.X1, q.X), math.Max(r.Y1, q.Y)}
	}
	return r
}

// strip is a text without its spaces and line ends, to compare a wrapped
// text with the one it wraps.
func strip(s string) string {
	return strings.Map(func(r rune) rune {
		if r == ' ' || r == '\n' {
			return -1
		}
		return r
	}, s)
}

func indexOf(s []int, v int) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}
