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
