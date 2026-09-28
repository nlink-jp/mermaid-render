package raster

import (
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mr "github.com/nlink-jp/mermaid-render"
)

func seqOf(t *testing.T, src string, m measurer) (*mr.Sequence, *seqLayout) {
	t.Helper()
	d, err := mr.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v\n%s", err, src)
	}
	s := d.(*mr.Sequence)
	sl, err := layoutSequence(s, m)
	if err != nil {
		t.Fatalf("layout: %v\n%s", err, src)
	}
	return s, sl
}

// checkSeq checks a sequence layout's properties: everything inside the
// picture; no text over another; messages down the page in order, each
// text above its arrow and centred between its ends, each end on its
// lifeline or bar; a note beside a lifeline crossing none, a note over
// lifelines crossing only its own; every frame holding what lies in its
// rows, and nested frames nested.
func checkSeq(t *testing.T, name string, d *mr.Sequence, sl *seqLayout) {
	t.Helper()
	fail := func(format string, a ...any) { t.Errorf("%s: %s", name, fmt.Sprintf(format, a...)) }
	const eps = 1e-6
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
		fail("%d messages and %d notes placed, want %d and %d", len(sl.msgs), len(sl.notes), len(msgEvents), len(noteEvents))
		return
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
			fail("message %d runs outside the picture", k)
		}
		if first.Y <= prevY {
			fail("message %d (%q) is not below the one before", k, e.Text)
		}
		prevY = first.Y
		if !reach(a, first.X) || !reach(b, last.X) {
			fail("message %d (%q) does not start and end on its lifelines", k, e.Text)
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
			fail("message %d (%q) points the wrong way", k, e.Text)
		}
		if a == b && m.pts[1].X <= first.X {
			fail("message %d (%q) to itself loops the wrong way", k, e.Text)
		}
		toRight := a == b || sl.cols[b] > sl.cols[a]
		if want := endAt(a, first, toRight); math.Abs(first.X-want) > eps {
			fail("message %d (%q) starts at %.3f, not at %.3f (its lifeline or bar)", k, e.Text, first.X, want)
		}
		fromRight := a == b || sl.cols[a] > sl.cols[b]
		if want := endAt(b, last, fromRight); math.Abs(last.X-want) > eps {
			fail("message %d (%q) ends at %.3f, not at %.3f (its lifeline or bar)", k, e.Text, last.X, want)
		}
		if m.tbox != (Rect{}) {
			texts = append(texts, labelled{m.tbox, "message " + e.Text})
			if m.tbox.Y1 > first.Y+eps {
				fail("message %d's text is not above its arrow", k)
			}
			if a != b && math.Abs(m.tbox.Center().X-(first.X+last.X)/2) > eps {
				fail("message %d's text is not centred on its arrow", k)
			}
			if !all.contains(m.tbox) {
				fail("message %d's text is outside the picture", k)
			}
			if a != b && (m.tbox.X0 < math.Min(first.X, last.X)-eps || m.tbox.X1 > math.Max(first.X, last.X)+eps) {
				fail("message %d's text (%q) is longer than its arrow", k, e.Text)
			}
		}
		// A number stands clear of a head at the start.
		if m.both && m.number != "" && math.Abs(m.numAt.X-first.X) < arrowLen+sqNumR-eps {
			fail("message %d's number covers its start head", k)
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
				fail("message %d (%q) to itself reaches the next lifeline", k, e.Text)
			}
		}
	}
	for k, nt := range sl.notes {
		e := noteEvents[k]
		texts = append(texts, labelled{nt.box, "note " + e.Text})
		if !all.contains(nt.box) {
			fail("note %q is outside the picture", e.Text)
		}
		lo, hi := min(idx[e.From], idx[e.To]), max(idx[e.From], idx[e.To])
		switch x := sl.cols[lo]; e.Place {
		case mr.LeftOf:
			if nt.box.X1 > x-eps {
				fail("note %q is not left of %s", e.Text, e.From.ID)
			}
		case mr.RightOf:
			if nt.box.X0 < x+eps {
				fail("note %q is not right of %s", e.Text, e.From.ID)
			}
		}
		for i, x := range sl.cols {
			crosses := x > nt.box.X0+eps && x < nt.box.X1-eps
			own := e.Place == mr.Over && i >= lo && i <= hi
			if crosses && !own {
				fail("note %q covers %s's lifeline", e.Text, d.Participants[i].ID)
			}
			if own && !crosses {
				fail("note %q is not over %s's lifeline", e.Text, d.Participants[i].ID)
			}
		}
	}
	for _, f := range sl.frames {
		if !all.contains(f.box) {
			fail("a %s frame is outside the picture", f.kind)
		}
		texts = append(texts, labelled{f.tab, f.kind + " tab"})
		if f.cond != "" {
			texts = append(texts, labelled{f.condBox, f.kind + " " + f.cond})
			if !f.box.contains(f.condBox) {
				fail("a %s frame's condition sticks out", f.kind)
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
					fail("a %s frame does not hold a message in its rows", f.kind)
				}
			}
			if m.tbox != (Rect{}) && in(m.tbox.Y0, m.tbox.Y1) && !f.box.contains(m.tbox) {
				fail("a %s frame does not hold the text %q in its rows", f.kind, m.text)
			}
		}
		for _, nt := range sl.notes {
			if in(nt.box.Y0, nt.box.Y1) && !f.box.contains(nt.box) {
				fail("a %s frame does not hold the note %q in its rows", f.kind, nt.text)
			}
		}
		for _, bar := range sl.acts {
			for _, y := range []float64{bar.Y0, bar.Y1} {
				if in(y, y) && (bar.X0 < f.box.X0 || bar.X1 > f.box.X1) {
					fail("a %s frame does not hold an activation that starts or ends in its rows", f.kind)
				}
			}
		}
		for _, g := range sl.frames {
			if g != f && in(g.box.Y0, g.box.Y1) && !f.box.contains(g.box) {
				fail("a %s frame does not hold the %s frame inside it", f.kind, g.kind)
			}
		}
	}
	for i, a := range texts {
		for _, b := range texts[i+1:] {
			if a.r.overlaps(b.r) {
				fail("%s overlaps %s", a.what, b.what)
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
			fail("no bar of %s %ss at message %d's arrow", e.From.ID, e.Kind, mi)
		}
	}
	for _, a := range sl.acts {
		if a.Y1 <= a.Y0 || !all.contains(a) {
			fail("an activation bar %v is empty or outside", a)
		}
	}
}

func TestSequenceLayoutCases(t *testing.T) {
	cases := map[string]string{
		"documentation blocks": `sequenceDiagram
    autonumber
    participant Alice
    actor John
    Alice->>+John: Hello John, how are you?
    loop Every minute
        John-->>Alice: Great!
    end
    alt is sick
        John->>Alice: Not so good :(
    else is well
        John->>Alice: Feeling fresh like a daisy
    end
    Note right of John: Rational thoughts!
    John-->>-Alice: I feel great!`,
		"notes everywhere": `sequenceDiagram
    A->>B: x
    Note left of A: a note on the left
    Note right of B: a note on the right
    Note over A: over one
    Note over A,B: over both, and rather long for its span`,
		"nested and self": `sequenceDiagram
    A->>A: think it over first
    par one
        A->>B: x
        critical deep
            B->>B: self inside
        option timeout
            B->>C: y
        end
    and two
        C->>A: z
    end`,
		"boxes": `sequenceDiagram
    box Aqua Group One
    participant A
    participant B
    end
    box Group Two
    participant C
    end
    A->>C: across`,
		"numbered two-headed arrows": `sequenceDiagram
    autonumber
    A<<->>B: both ways
    B<<-->>A: and back`,
		"a block holding only an activation": `sequenceDiagram
    participant A
    participant B
    participant C
    loop x
    activate C
    end
    deactivate C`,
		"deep nesting": `sequenceDiagram
    participant A
    participant B
    activate A
    activate A
    activate A
    activate A
    activate A
    activate A
    activate A
    activate A
    A->>A: think
    B->>A: in
    A->>B: out
    Note right of A: beside`,
		"activations stacked": `sequenceDiagram
    A->>+B: one
    A->>+B: two
    B->>C: from the bar
    B-->>-A: back
    B-->>-A: back again`,
	}
	for name, src := range cases {
		d, sl := seqOf(t, src, fakeMeasure)
		checkSeq(t, name, d, sl)
	}
}

func TestRealSequenceLayout(t *testing.T) {
	fn, err := DefaultFont()
	if err != nil {
		t.Skipf("no system font: %v", err)
	}
	files, _ := filepath.Glob("../testdata/real/*/*.mmd")
	n := 0
	for _, file := range files {
		b, _ := os.ReadFile(file)
		d, err := mr.Parse(string(b))
		if err != nil {
			continue
		}
		s, ok := d.(*mr.Sequence)
		if !ok {
			continue
		}
		sl, err := layoutSequence(s, fn.measureEm)
		if err != nil {
			t.Errorf("%s: %v", file, err)
			continue
		}
		checkSeq(t, filepath.Base(file), s, sl)
		if _, err := Render(d, Options{Font: fn}); err != nil {
			t.Errorf("%s: render: %v", file, err)
		}
		n++
	}
	if n != 10 {
		t.Errorf("laid out %d real sequence blocks, want 10", n)
	}
}

func randomSequence(seed int64) string {
	rng := rand.New(rand.NewSource(seed))
	var b strings.Builder
	b.WriteString("sequenceDiagram\n")
	n := 2 + rng.Intn(6)
	words := []string{"hi", "リクエスト送信", "a much longer message text here", "ok", "x"}
	if rng.Intn(3) == 0 {
		b.WriteString("autonumber\n")
	}
	if rng.Intn(4) == 0 {
		b.WriteString("box Group of two\nparticipant P0\nparticipant P1\nend\n")
	}
	for i := range n {
		if rng.Intn(3) == 0 {
			fmt.Fprintf(&b, "actor P%d as Actor %d\n", i, i)
		}
	}
	active := map[int]int{}
	depth := 0
	var open []string
	arrows := []string{"->>", "-->>", "->", "-->", "-x", "--x", "-)", "--)", "<<->>"}
	for range 3 + rng.Intn(14) {
		switch k := rng.Intn(10); {
		case k < 5:
			a, c := rng.Intn(n), rng.Intn(n)
			mod := ""
			if rng.Intn(4) == 0 {
				mod = "+"
				active[c]++
			} else if active[a] > 0 && rng.Intn(3) == 0 {
				mod = "-"
				active[a]--
			}
			fmt.Fprintf(&b, "P%d%s%sP%d: %s\n", a, arrows[rng.Intn(len(arrows))], mod, c, words[rng.Intn(len(words))])
		case k < 7:
			a := rng.Intn(n)
			switch rng.Intn(3) {
			case 0:
				fmt.Fprintf(&b, "Note left of P%d: %s\n", a, words[rng.Intn(len(words))])
			case 1:
				fmt.Fprintf(&b, "Note right of P%d: %s\n", a, words[rng.Intn(len(words))])
			default:
				fmt.Fprintf(&b, "Note over P%d,P%d: %s\n", a, rng.Intn(n), words[rng.Intn(len(words))])
			}
		case k < 9 && depth < 3:
			kind := []string{"loop", "alt", "par", "critical", "opt", "break"}[rng.Intn(6)]
			fmt.Fprintf(&b, "%s %s\n", kind, words[rng.Intn(len(words))])
			open = append(open, kind)
			depth++
		default:
			if depth > 0 {
				kind := open[len(open)-1]
				if sec := map[string]string{"alt": "else", "par": "and", "critical": "option"}[kind]; sec != "" && rng.Intn(2) == 0 {
					fmt.Fprintf(&b, "%s %s\n", sec, words[rng.Intn(len(words))])
					continue
				}
				b.WriteString("end\n")
				open = open[:len(open)-1]
				depth--
			}
		}
	}
	for range open {
		b.WriteString("end\n")
	}
	return b.String()
}

func TestSequenceLayoutRandom(t *testing.T) {
	for seed := int64(1); seed <= int64(*randomN); seed++ {
		src := randomSequence(seed)
		d, sl := seqOf(t, src, fakeMeasure)
		checkSeq(t, fmt.Sprintf("seed %d", seed), d, sl)
		if t.Failed() {
			t.Logf("source:\n%s", src)
			return
		}
	}
}

// What is drawn matches the source: each message's line is dashed exactly
// when it is dotted, and its heads are of its kind at its end (and its
// start, for two-headed arrows).
func TestSequenceDrawnHeads(t *testing.T) {
	fn, err := DefaultFont()
	if err != nil {
		t.Skipf("no system font: %v", err)
	}
	src := `sequenceDiagram
    A->B: 1
    A-->B: 2
    A->>B: 3
    B-->>A: 4
    A<<->>B: 5
    B<<-->>A: 6
    A-xB: 7
    B--xA: 8
    A-)B: 9
    A--)A: 10`
	d, err := mr.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	s := d.(*mr.Sequence)
	sl, err := layoutSequence(s, fn.measureEm)
	if err != nil {
		t.Fatal(err)
	}
	drawn := map[string]bool{}
	if _, err := render(d, Options{Font: fn}, func(t string) { drawn[t] = true }); err != nil {
		t.Fatal(err)
	}
	k := 0
	for _, e := range s.Events {
		if e.Kind != mr.Message {
			continue
		}
		m := sl.msgs[k]
		k++
		p, q := m.pts[0], m.pts[len(m.pts)-1]
		if line := fmt.Sprintf("line dashed=%v %.2f,%.2f", e.Dotted, p.X, p.Y); !drawn[line] {
			t.Errorf("message %q: no %q", e.Text, line)
		}
		heads := []Pt{}
		if e.Head != mr.HeadNone {
			heads = append(heads, q)
			if e.BothEnds {
				heads = append(heads, p)
			}
		}
		for _, h := range heads {
			if key := fmt.Sprintf("head %s at %.2f,%.2f", e.Head, h.X, h.Y); !drawn[key] {
				t.Errorf("message %q: no %q", e.Text, key)
			}
		}
		if e.Head == mr.HeadNone || !e.BothEnds {
			for _, other := range []mr.ArrowHead{mr.HeadFilled, mr.HeadCross, mr.HeadOpen} {
				if drawn[fmt.Sprintf("head %s at %.2f,%.2f", other, p.X, p.Y)] {
					t.Errorf("message %q: a head at its start", e.Text)
				}
			}
		}
	}
}
