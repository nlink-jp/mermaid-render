package raster

import (
	"fmt"
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

// checkSeq checks a sequence layout's properties (seqFaults), strictly.
func checkSeq(t *testing.T, name string, d *mr.Sequence, sl *seqLayout) {
	t.Helper()
	for _, msg := range seqFaults(d, sl, true) {
		t.Errorf("%s: %s", name, msg)
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
	if _, err := render(d, Options{Font: fn}, probe{trace: func(t string) { drawn[t] = true }}); err != nil {
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
