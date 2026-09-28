package raster

import (
	"errors"
	"fmt"
	"math"
	"math/rand"
	"strings"
	"testing"
	"time"

	mr "github.com/nlink-jp/mermaid-render"
)

// Every render checks its layout, and a fault is a refusal: a corrupted
// layout of each type comes back as a LayoutFault naming what is wrong.
func TestRenderRefusesLayoutFaults(t *testing.T) {
	fn := systemFont(t)
	cases := []struct {
		name, src string
		corrupt   func(*Layout, *seqLayout)
		want      string
	}{
		{"flowchart: a box on another", "flowchart TD\n    A --> B\n    A --> C",
			func(l *Layout, _ *seqLayout) { l.Nodes[2].Box = l.Nodes[1].Box }, "overlap"},
		{"flowchart: a label lost", "flowchart TD\n    A -->|yes| B",
			func(l *Layout, _ *seqLayout) { l.Edges[0].LabelBox = Rect{} }, "lost its label"},
		{"ER: an entity on another", "erDiagram\n    A ||--o{ B : has\n    A ||--o{ C : owns",
			func(l *Layout, _ *seqLayout) { l.Nodes[2].Box = l.Nodes[1].Box }, "overlap"},
		{"ER: a label over a crow's foot", "erDiagram\n    A ||--o{ B : has",
			func(l *Layout, _ *seqLayout) {
				p := l.Edges[0].Points
				d := math.Hypot(p[1].X-p[0].X, p[1].Y-p[0].Y)
				c := Pt{p[0].X + (p[1].X-p[0].X)*0.6/d, p[0].Y + (p[1].Y-p[0].Y)*0.6/d}
				l.Edges[0].LabelBox = Rect{c.X - 0.1, c.Y - 0.1, c.X + 0.1, c.Y + 0.1}
			}, "over a marker"},
		{"sequence: an arrow off its lifelines", "sequenceDiagram\n    A->>B: hi",
			func(_ *Layout, s *seqLayout) {
				p := s.msgs[0].pts
				p[0], p[len(p)-1] = p[len(p)-1], p[0]
			}, "lifelines"},
		{"sequence: a note over a header", "sequenceDiagram\n    Note over A: hi",
			func(_ *Layout, s *seqLayout) { s.notes[0].box = s.heads[0].box }, "overlaps"},
	}
	for _, c := range cases {
		d, err := mr.Parse(c.src)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if _, err := render(d, Options{Font: fn}, probe{}); err != nil {
			t.Fatalf("%s: uncorrupted: %v", c.name, err)
		}
		_, err = render(d, Options{Font: fn}, probe{corrupt: c.corrupt})
		var e *mr.Error
		if !errors.As(err, &e) || e.Kind != mr.LayoutFault {
			t.Errorf("%s: %v, want a layout fault", c.name, err)
			continue
		}
		if !strings.Contains(e.Msg, c.want) {
			t.Errorf("%s: %q, want it to name %q", c.name, e.Msg, c.want)
		}
	}
}

// What only reads worse is held by the tests and never refused by a
// render: a message's text off the middle of its arrow, links closer than
// the operator liked, a head close to its bend.
func TestVerifyRefusesWrongNotUgly(t *testing.T) {
	d, sl := seqOf(t, "sequenceDiagram\n    A->>B: a longer message text", fakeMeasure)
	sl.msgs[0].tbox.X0 += 0.05
	sl.msgs[0].tbox.X1 += 0.05
	if len(seqFaults(d, sl, true)) == 0 {
		t.Error("strict: a text off the middle of its arrow passed")
	}
	if fs := seqFaults(d, sl, false); len(fs) > 0 {
		t.Errorf("a render would refuse a text off the middle of its arrow: %v", fs)
	}

	f, lay := layoutOf(t, "flowchart LR\n    A --> B\n    C --> D", fakeMeasure)
	// Two parallel links 0.3 em apart: under the operator's 0.6 em, far
	// over the two line widths that would draw as one.
	e0, e1 := lay.Edges[0].Points, lay.Edges[1].Points
	shift := e0[0].Y + 0.3 - e1[0].Y
	for i := range e1 {
		e1[i].Y += shift
	}
	for i := range lay.Nodes {
		if lay.Nodes[i].ID == "C" || lay.Nodes[i].ID == "D" {
			lay.Nodes[i].Box.Y0 += shift
			lay.Nodes[i].Box.Y1 += shift
		}
	}
	strict := strings.Join(flowFaults(f, lay, fakeMeasure, true, nil), "; ")
	if !strings.Contains(strict, "apart") {
		t.Errorf("strict: links 0.3 em apart passed (%s)", strict)
	}
	for _, msg := range flowFaults(f, lay, fakeMeasure, false, nil) {
		if strings.Contains(msg, "apart") {
			t.Errorf("a render would refuse links 0.3 em apart: %s", msg)
		}
	}
	// The same links run 0.1 em apart draw as one line: wrong.
	for i := range e1 {
		e1[i].Y -= 0.2
	}
	if !strings.Contains(strings.Join(flowFaults(f, lay, fakeMeasure, false, nil), "; "), "apart") {
		t.Error("a render drew two links 0.1 em apart")
	}
}

// A link through a frame it does not belong to is avoided, and counted by
// the tests, but never refused: the heads still say where it goes.
func TestVerifyAcceptsAFrameCrossing(t *testing.T) {
	f, base := layoutOf(t, "flowchart TD\n    A --> B --> C --> D\n    A --> D\n    subgraph S\n    X\n    end", fakeMeasure)
	fi := -1
	for i, fr := range base.Frames {
		if fr.ID == "S" {
			fi = i
		}
	}
	// Slide the frame and its member over the layout until the only thing
	// the tests hold against it is a link through the frame.
	for dy := -base.H; dy <= base.H; dy += 0.25 {
		for dx := -base.W; dx <= base.W; dx += 0.25 {
			lay := *base
			lay.Nodes = append([]NodeBox(nil), base.Nodes...)
			lay.Frames = append([]FrameBox(nil), base.Frames...)
			move := func(r Rect) Rect { return Rect{r.X0 + dx, r.Y0 + dy, r.X1 + dx, r.Y1 + dy} }
			lay.Frames[fi].Box = move(lay.Frames[fi].Box)
			lay.Frames[fi].TitleBox = move(lay.Frames[fi].TitleBox)
			for i := range lay.Nodes {
				if lay.Nodes[i].ID == "X" {
					lay.Nodes[i].Box = move(lay.Nodes[i].Box)
				}
			}
			crossed := 0
			if len(flowFaults(f, &lay, fakeMeasure, true, &crossed)) > 0 || crossed == 0 {
				continue
			}
			if fs := flowFaults(f, &lay, fakeMeasure, true, nil); len(fs) == 0 {
				t.Fatal("strict: a link through a foreign frame passed")
			}
			if fs := flowFaults(f, &lay, fakeMeasure, false, nil); len(fs) > 0 {
				t.Fatalf("a render would refuse a frame crossing: %v", fs)
			}
			return
		}
	}
	t.Fatal("no placement found where a frame crossing is the only fault")
}

// Each loose threshold still refuses what is wrong: heads on top of each
// other, a head longer than its run, a crow's foot off its line, two feet
// touching.
func TestVerifyLooseStillRefuses(t *testing.T) {
	near := func(to, from Pt, d float64) Pt {
		l := math.Hypot(from.X-to.X, from.Y-to.Y)
		return Pt{to.X + (from.X-to.X)*d/l, to.Y + (from.Y-to.Y)*d/l}
	}
	type flowCase struct {
		name, src string
		corrupt   func(*Layout)
		want      string
	}
	for _, c := range []flowCase{
		{"two heads on one point", "flowchart TD\n    A --> B\n    A --> C",
			func(l *Layout) { l.Edges[1].Points[0] = l.Edges[0].Points[0] }, "meet at the same point"},
		{"a head longer than its run", "flowchart TD\n    A --> B\n    A --> C\n    A --> D",
			func(l *Layout) {
				for i, e := range l.Edges {
					if p := e.Points; len(p) >= 3 {
						n := len(p)
						l.Edges[i].Points[n-2] = near(p[n-1], p[n-2], arrowLen/2)
						return
					}
				}
				t.Fatal("no bent link to shorten")
			}, "em run"},
	} {
		f, lay := layoutOf(t, c.src, fakeMeasure)
		c.corrupt(lay)
		if got := strings.Join(flowFaults(f, lay, fakeMeasure, false, nil), "; "); !strings.Contains(got, c.want) {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
	type erCase struct {
		name    string
		corrupt func(*erLayout)
		want    string
	}
	for _, c := range []erCase{
		{"a marker longer than its run", func(el *erLayout) {
			p := el.Edges[0].Points
			el.Edges[0].Points[1] = near(p[0], p[1], markerReach/2)
		}, "marker's run"},
		{"two crow's feet touching", func(el *erLayout) {
			a, b := el.Edges[0].Points[0], el.Edges[1].Points[0]
			if math.Abs(a.Y-b.Y) < 1e-6 {
				el.Edges[1].Points[0].X = a.X + markHalf
			} else {
				el.Edges[1].Points[0].Y = a.Y + markHalf
			}
		}, "apart (needs"},
	} {
		d, el := erOf(t, "erDiagram\n    A ||--o{ B : x\n    A ||--o{ C : y", fakeMeasure)
		c.corrupt(el)
		if got := strings.Join(erFaults(d, el, fakeMeasure, false), "; "); !strings.Contains(got, c.want) {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

// The check runs on every render, so it must stay cheap at the limits:
// comparing every pair of segments took half a second on this diagram
// (499 labelled links among 299 nodes); swept, it takes tens of ms.
func TestVerifyCost(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	var b strings.Builder
	b.WriteString("flowchart TD\n")
	for i := range 499 {
		a := r.Intn(299)
		fmt.Fprintf(&b, " n%d -->|l%d| n%d\n", a, i, (a+1+r.Intn(5))%299)
	}
	d, err := mr.Parse(b.String())
	if err != nil {
		t.Fatal(err)
	}
	f := d.(*mr.Flowchart)
	lay, err := layoutFlowchart(f, fakeMeasure)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if fs := flowFaults(f, lay, fakeMeasure, false, nil); len(fs) > 0 {
		t.Errorf("faults: %v", fs)
	}
	if d := time.Since(start); d > 250*time.Millisecond {
		t.Errorf("the check took %v", d)
	}
}
