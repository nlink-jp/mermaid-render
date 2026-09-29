package raster

import (
	"errors"
	"flag"
	"fmt"
	"math"
	"math/rand"
	"strings"
	"testing"

	mr "github.com/nlink-jp/mermaid-render"
)

func stateOf(t *testing.T, src string) (*mr.StateDiagram, *stateLayout) {
	t.Helper()
	d, err := mr.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v\n%s", err, src)
	}
	sd := d.(*mr.StateDiagram)
	sl, err := layoutState(sd, fakeMeasure)
	if err != nil {
		t.Fatalf("layout: %v\n%s", err, src)
	}
	return sd, sl
}

// checkState holds the strict reading (frame crossings counted) and the
// render's reading, which must find nothing.
func checkState(t *testing.T, name string, d *mr.StateDiagram, sl *stateLayout, crossed *int) {
	t.Helper()
	for _, f := range stateFaults(d, sl, fakeMeasure, true, crossed) {
		t.Errorf("%s: %s", name, f)
	}
	for _, f := range stateFaults(d, sl, fakeMeasure, false, nil) {
		t.Errorf("%s: a render would refuse: %s", name, f)
	}
}

var stateCases = map[string]string{
	"the documentation's first": `stateDiagram-v2
    [*] --> Still
    Still --> [*]
    Still --> Moving
    Moving --> Still
    Moving --> Crash
    Crash --> [*]`,
	"composites and transitions between them": `stateDiagram-v2
    [*] --> First
    First --> Second
    First --> Third
    state First {
        [*] --> fir
        fir --> [*]
    }
    state Second {
        [*] --> sec
        sec --> [*]
    }
    state Third {
        [*] --> thi
        thi --> [*]
    }`,
	"deep nesting": `stateDiagram-v2
    [*] --> First
    state First {
        [*] --> Second
        state Second {
            [*] --> second
            second --> Third
            state Third {
                [*] --> third
                third --> [*]
            }
        }
    }`,
	"fork and join": `stateDiagram-v2
    state fork_state <<fork>>
    [*] --> fork_state
    fork_state --> State2
    fork_state --> State3
    state join_state <<join>>
    State2 --> join_state
    State3 --> join_state
    join_state --> State4
    State4 --> [*]`,
	"fork across LR": `stateDiagram-v2
    direction LR
    state f <<fork>>
    [*] --> f
    f --> a
    f --> b
    f --> c`,
	"choice": `stateDiagram-v2
    state if_state <<choice>>
    [*] --> IsPositive
    IsPositive --> if_state
    if_state --> False: if n < 0
    if_state --> True : if n >= 0`,
	"notes": `stateDiagram-v2
    State1: The state with a note
    note right of State1
        Important information! You can write
        notes.
    end note
    State1 --> State2
    note left of State2 : This is the note to the left.`,
	"concurrency": `stateDiagram-v2
    [*] --> Active
    state Active {
        [*] --> NumLockOff
        NumLockOff --> NumLockOn : EvNumLockPressed
        NumLockOn --> NumLockOff : EvNumLockPressed
        --
        [*] --> CapsLockOff
        CapsLockOff --> CapsLockOn : EvCapsLockPressed
        --
        direction LR
        [*] --> ScrollLockOff
        ScrollLockOff --> ScrollLockOn
    }`,
	"titled states and a self transition": `stateDiagram-v2
    A : first
    A : second line
    A : third line
    A --> A : again
    A --> B`,
	"notes stacked on both sides": `stateDiagram-v2
    A --> B
    note left of B : first on the left
    note left of B
        second on the left
        over two lines
    end note
    note right of B : on the right
    B --> C`,
	"an empty diagram":   "stateDiagram-v2",
	"an empty composite": "stateDiagram-v2\n  state X {\n  }\n  a --> X",
	"many into the end": `stateDiagram-v2
    a --> [*]
    b --> [*]
    c --> [*]
    d --> [*]
    [*] --> a
    [*] --> b
    [*] --> c`,
	"a direction per scope": `stateDiagram
    direction LR
    [*] --> A
    A --> B
    B --> C
    state B {
      direction LR
      a --> b
    }
    B --> D`,
}

func TestStateLayoutCases(t *testing.T) {
	for name, src := range stateCases {
		d, sl := stateOf(t, src)
		checkState(t, name, d, sl, nil)
	}
}

// randomState is a state diagram of nested scopes: composites (some split
// into regions) up to three deep, starts and ends, choices and forks,
// labels, notes and directions. Every transition stays in its scope, as a
// drawable diagram's must.
func randomState(seed int64) string {
	r := rand.New(rand.NewSource(seed))
	var b strings.Builder
	b.WriteString("stateDiagram-v2\n")
	next := 0
	var scope func(depth int, indent string)
	scope = func(depth int, indent string) {
		if r.Intn(4) == 0 {
			fmt.Fprintf(&b, "%sdirection %s\n", indent, []string{"TB", "BT", "LR", "RL"}[r.Intn(4)])
		}
		n := 1 + r.Intn(6)
		ids, plain := []string{}, []string{}
		for range n {
			id := fmt.Sprintf("s%d", next)
			next++
			k := r.Intn(10)
			switch {
			case k == 0:
				fmt.Fprintf(&b, "%sstate %s <<choice>>\n", indent, id)
			case k == 1:
				fmt.Fprintf(&b, "%sstate %s <<fork>>\n", indent, id)
			case k == 2 && depth < 3:
				fmt.Fprintf(&b, "%sstate %s {\n", indent, id)
				regions := 1
				if r.Intn(3) == 0 {
					regions = 2 + r.Intn(2)
				}
				for g := range regions {
					if g > 0 {
						fmt.Fprintf(&b, "%s    --\n", indent)
					}
					scope(depth+1, indent+"    ")
				}
				fmt.Fprintf(&b, "%s}\n", indent)
			case k == 3:
				fmt.Fprintf(&b, "%s%s : a longer description %d\n", indent, id, r.Intn(100))
				if r.Intn(2) == 0 {
					fmt.Fprintf(&b, "%s%s : and a line\n", indent, id)
				}
			default:
				fmt.Fprintf(&b, "%s%s\n", indent, id)
			}
			if k > 1 {
				plain = append(plain, id)
			}
			ids = append(ids, id)
		}
		looped := map[string]bool{}
		pick := func() string {
			if r.Intn(6) == 0 {
				return "[*]"
			}
			return ids[r.Intn(len(ids))]
		}
		for range r.Intn(2 * n) {
			a, c := pick(), pick()
			if a == c {
				looped[a] = true
			}
			fmt.Fprintf(&b, "%s%s --> %s", indent, a, c)
			if r.Intn(3) == 0 {
				fmt.Fprintf(&b, " : event %d", r.Intn(50))
			}
			b.WriteString("\n")
		}
		// A note stands beside a state that can stretch and has no loop
		// (in a top-down scope anything else is unsupported).
		if len(plain) > 0 && r.Intn(4) == 0 {
			if id := plain[r.Intn(len(plain))]; !looped[id] {
				fmt.Fprintf(&b, "%snote %s of %s : note %d\n", indent, []string{"left", "right"}[r.Intn(2)], id, r.Intn(9))
			}
		}
	}
	scope(0, "    ")
	return b.String()
}

var stateRandomN = flag.Int("staterandom", 400, "number of random state diagrams TestStateLayoutRandom lays out")

func TestStateLayoutRandom(t *testing.T) {
	laid, refused, crossed := 0, 0, 0
	reasons := map[string]int{}
	for seed := int64(1); seed <= int64(*stateRandomN); seed++ {
		src := randomState(seed)
		d, err := mr.Parse(src)
		if err != nil {
			var e *mr.Error
			if errors.As(err, &e) && e.Kind == mr.UnsupportedConstruct {
				refused++
				reasons[e.Msg]++
				continue
			}
			t.Fatalf("seed %d: parse: %v\n%s", seed, err, src)
		}
		sd := d.(*mr.StateDiagram)
		sl, err := layoutState(sd, fakeMeasure)
		if err != nil {
			t.Fatalf("seed %d: %v\n%s", seed, err, src)
		}
		before := t.Failed()
		checkState(t, fmt.Sprintf("seed %d", seed), sd, sl, &crossed)
		if t.Failed() && !before {
			t.Logf("source of seed %d:\n%s", seed, src)
			return
		}
		laid++
	}
	t.Logf("laid out %d random state diagrams, refused %d: %v", laid, refused, reasons)
	if laid < *stateRandomN*9/10 {
		t.Errorf("only %d of %d random state diagrams were laid out", laid, *stateRandomN)
	}
}

// What is drawn is what the source says: each state by its kind, each
// transition solid and each note line dotted, each composite with its
// regions.
func TestStateDrawn(t *testing.T) {
	fn := systemFont(t)
	d, err := mr.Parse(`stateDiagram-v2
    state c <<choice>>
    state f <<fork>>
    state j <<join>>
    [*] --> c
    c --> f : yes
    f --> A
    f --> B
    A --> j
    B --> j
    j --> [*]
    note right of A : a note
    state Box {
        x --> y
        --
        z
    }
    c --> Box`)
	if err != nil {
		t.Fatal(err)
	}
	var trace []string
	if _, err := render(d, Options{Font: fn}, probe{trace: func(s string) { trace = append(trace, s) }}); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(trace, "\n")
	for _, want := range []string{
		"state choice c", "state fork f", "state join j", "state start root_start", "state end root_end",
		"state state A", "state composite Box", "composite Box regions 2", "state state x", "state state z",
		"transition c f solid", "note beside A left false", "transition x y solid", "note A",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("trace lacks %q:\n%s", want, got)
		}
	}
}

// Every render checks a state diagram's layout: a corrupted one is a
// layout fault.
func TestRenderRefusesStateFaults(t *testing.T) {
	fn := systemFont(t)
	src := `stateDiagram-v2
    [*] --> Box
    A : title
    A : line
    Box --> A
    note right of A : a note
    note right of A : another note
    state Box {
        x --> y : go
        --
        z
    }`
	for name, corrupt := range map[string]func(*stateLayout){
		"a frame off its box":    func(sl *stateLayout) { sl.comps[0].frame.X0 -= 1 },
		"a region off its scope": func(sl *stateLayout) { sl.comps[0].regionR[0].Y0 += 0.5 },
		"a region out of the frame": func(sl *stateLayout) {
			c := sl.comps[0]
			r := c.regions[1]
			dy := c.frame.Y1 - r.off.Y
			r.off.Y += dy
			c.regionR[1].Y0 += dy
			c.regionR[1].Y1 += dy
		},
		"regions overlapping": func(sl *stateLayout) {
			c := sl.comps[0]
			r := c.regions[1]
			dx := c.regionR[0].X0 - c.regionR[1].X0
			r.off.X += dx
			c.regionR[1].X0 += dx
			c.regionR[1].X1 += dx
		},
		"a title outside its band": func(sl *stateLayout) { sl.comps[0].title.Y1 = sl.comps[0].band.Y1 + 1 },
		"a transition left out of its scope's graph": func(sl *stateLayout) {
			// The graph and its layout agree; only the scope has one more.
			sc := sl.root.scope
			sc.Transitions = append(sc.Transitions, &mr.Transition{From: "A", To: "Box"})
		},
		"a note on the wrong side": func(sl *stateLayout) {
			sc := sl.root
			for k, nt := range sc.notes {
				mn, b := sc.main[sc.stateIndex(nt.State)], sc.noteBox[k]
				sc.noteBox[k] = rect{2*mn.Center().X - b.X1, b.Y0, 2*mn.Center().X - b.X0, b.Y1}
			}
		},
		"a note over its state": func(sl *stateLayout) {
			// Still on its side, holding its text, inside the node: only
			// 0.2 em into the state.
			sc := sl.root
			mn, b := sc.main[sc.stateIndex(sc.notes[0].State)], sc.noteBox[0]
			d := b.X0 - (mn.X1 - 0.2)
			sc.noteBox[0] = rect{b.X0 - d, b.Y0, b.X1 - d, b.Y1}
		},
		"a note too small for its text": func(sl *stateLayout) {
			sl.root.noteBox[0].X1 = sl.root.noteBox[0].X0 + 0.5
		},
		"a transition on a note's slot": func(sl *stateLayout) {
			sc := sl.root
			i := sc.stateIndex(sc.notes[0].State)
			for j, e := range sc.lay.Edges {
				if e.Link.To.ID == sc.states[i].ID {
					p := e.Points[len(e.Points)-1]
					sc.lay.Edges[j].Points[len(e.Points)-1] = pt{sc.noteBox[0].Center().X, p.Y}
				}
			}
		},
		"notes overlapping beside a state": func(sl *stateLayout) {
			// Each keeps its size and its side: only 0.2 em into the other.
			a, b := sl.root.noteBox[0], sl.root.noteBox[1]
			d := b.Y0 - (a.Y1 - 0.2)
			sl.root.noteBox[1] = rect{b.X0, b.Y0 - d, b.X1, b.Y1 - d}
		},
		"a state's own part outside its node": func(sl *stateLayout) {
			sc := sl.root
			i := sc.stateIndex("A")
			sc.main[i].Y1 = sc.lay.Nodes[i].Box.Y1 + 1
		},
		"a label out of its scope": func(sl *stateLayout) {
			for _, sc := range sl.scopes {
				for j, e := range sc.lay.Edges {
					if e.Label != "" {
						sc.lay.Edges[j].LabelBox.X0 = -5
						return
					}
				}
			}
		},
		"states overlapping inside a region": func(sl *stateLayout) {
			r := sl.comps[0].regions[0]
			r.lay.Nodes[1].Box, r.main[1] = r.lay.Nodes[0].Box, r.main[0]
		},
		"a titled state too small": func(sl *stateLayout) {
			// Its own part, still inside its node.
			for i := range sl.root.titled {
				sl.root.main[i].Y1 = sl.root.main[i].Y0 + 0.5
			}
		},
	} {
		d, _ := mr.Parse(src)
		_, err := render(d, Options{Font: fn}, probe{corruptState: corrupt})
		var e *mr.Error
		if !errors.As(err, &e) || e.Kind != mr.LayoutFault {
			t.Errorf("%s: %v, want a layout fault", name, err)
		}
	}
}

// Start and end stay circles however many links meet them (sized up front,
// never widened by the layout), and a fork or join bar lies across its
// scope's direction.
func TestStateShapes(t *testing.T) {
	for _, dir := range []string{"TB", "LR"} {
		src := "stateDiagram-v2\n  direction " + dir + `
  state f <<fork>>
  [*] --> f
  [*] --> a
  [*] --> b
  f --> c
  a --> [*]
  b --> [*]
  c --> [*]`
		_, sl := stateOf(t, src)
		for i, n := range sl.root.states {
			b := sl.root.lay.Nodes[i].Box
			switch n.Kind {
			case mr.StateStart, mr.StateEnd:
				if math.Abs(b.W()-b.H()) > 1e-9 {
					t.Errorf("%s: %s is %.2f x %.2f, not a circle", dir, n.ID, b.W(), b.H())
				}
			case mr.StateFork:
				if (dir == "TB") != (b.W() > b.H()) {
					t.Errorf("%s: the fork bar is %.2f x %.2f", dir, b.W(), b.H())
				}
			}
		}
	}
}

// Sources that exposed a defect, kept as written: the generator has
// changed since.
var stateRegressions = map[string]string{
	// Seed 8960 (seed 3987 before the generator changed): a 0.2 em snap
	// took the port past a narrow end circle's rim.
	"seed 8960": `stateDiagram-v2
    direction BT
    s0
    s1
    s2
    s3 : a longer description 50
    s3 : and a line
    s1 --> s2 : event 14
    s2 --> s1 : event 27
    [*] --> [*]
    [*] --> s0 : event 42
    s2 --> s0
    [*] --> s0 : event 10
    [*] --> s2 : event 38`,
}

func TestStateLayoutRegressions(t *testing.T) {
	for name, src := range stateRegressions {
		d, sl := stateOf(t, src)
		checkState(t, name, d, sl, nil)
	}
}

// A note stands on the side it names, in every direction: beside its
// state in TB and BT, a rank before or after it in LR and RL.
func TestStateNoteSides(t *testing.T) {
	for _, dir := range []string{"TB", "BT", "LR", "RL"} {
		_, sl := stateOf(t, "stateDiagram-v2\n  direction "+dir+`
  A --> B
  B --> C
  note left of B : on the left
  note right of B : on the right`)
		sc := sl.root
		mn := sc.main[sc.stateIndex("B")]
		for k, nt := range sc.notes {
			b := sc.noteBox[k]
			if left := b.Center().X < mn.Center().X; left != nt.Left {
				t.Errorf("%s: %q stands on the wrong side (note %v, state %v)", dir, nt.Text, b, mn)
			}
			if beside := sc.noteNode[k] < 0; beside != (dir == "TB" || dir == "BT") {
				t.Errorf("%s: %q beside %v", dir, nt.Text, beside)
			}
		}
	}
}

// Handed a note beside a state looping to itself in TB (which Parse
// refuses), the layout makes the note a node rather than a slot the loop
// would leave from.
func TestStateNoteOnLoopIsANode(t *testing.T) {
	d := &mr.StateDiagram{Root: &mr.StateScope{Direction: mr.TB,
		States:      []*mr.StateNode{{ID: "A", Kind: mr.StatePlain, Label: "A"}},
		Transitions: []*mr.Transition{{From: "A", To: "A"}},
		Notes:       []*mr.StateNote{{State: "A", Text: "x"}}}}
	sl, err := layoutState(d, fakeMeasure)
	if err != nil {
		t.Fatal(err)
	}
	if sl.root.noteNode[0] < 0 {
		t.Error("the note stands beside a state that loops out of its side")
	}
}
