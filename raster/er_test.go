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

func erOf(t *testing.T, src string, m measurer) (*mr.ER, *erLayout) {
	t.Helper()
	d, err := mr.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v\n%s", err, src)
	}
	e := d.(*mr.ER)
	el, err := layoutER(e, m)
	if err != nil {
		t.Fatalf("layout: %v\n%s", err, src)
	}
	return e, el
}

// checkER checks the flowchart layout properties on the laid-out graph,
// then what an ER diagram adds: each end runs straight past its marker,
// link ends on one face stay far enough apart that crow's feet side by
// side do not touch, no label lies over a marker, and every table fits its box.
func checkER(t *testing.T, name string, d *mr.ER, el *erLayout, m measurer) {
	t.Helper()
	checkLayout(t, name, el.graph, el.Layout, m)
	fail := func(format string, a ...any) { t.Errorf("%s: %s", name, fmt.Sprintf(format, a...)) }
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
	for _, e := range el.Edges {
		p := e.Points
		for _, s := range [][2]Pt{{p[0], p[1]}, {p[len(p)-1], p[len(p)-2]}} {
			tip, next := s[0], s[1]
			if d := math.Hypot(next.X-tip.X, next.Y-tip.Y); d < markerReach+0.1 {
				fail("link %s->%s: a marker's run is %.3f em (needs %.2f)", e.Link.From.ID, e.Link.To.ID, d, markerReach+0.1)
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
	for i, a := range ends {
		for _, b := range ends[i+1:] {
			if a.node != b.node {
				continue
			}
			sameFace := math.Abs(a.p.X-b.p.X) < 1e-6 || math.Abs(a.p.Y-b.p.Y) < 1e-6
			// Two crow's feet side by side, with 0.4 em between them —
			// from the marker's width, not from erPortGap, so a change to
			// the spacing is checked against what it must hold.
			need := 2*markHalf + 0.4
			if d := math.Hypot(a.p.X-b.p.X, a.p.Y-b.p.Y); sameFace && d < need-0.05 {
				fail("two link ends on %s are %.3f em apart (needs %.2f)", el.Nodes[a.node].Label, d, need)
			}
		}
	}
	for i, e := range el.Edges {
		if e.Label == "" {
			continue
		}
		for j, z := range zones {
			if e.LabelBox.overlaps(z) {
				fail("label %q lies over a marker", e.Label)
			}
			// Its own markers: 0.7 em clear (the operator's second ER
			// check: labels against the lower marker read as too low).
			if j/2 == i && e.Link.From != e.Link.To {
				dx := math.Max(0, math.Max(z.X0-e.LabelBox.X1, e.LabelBox.X0-z.X1))
				dy := math.Max(0, math.Max(z.Y0-e.LabelBox.Y1, e.LabelBox.Y0-z.Y1))
				if d := math.Hypot(dx, dy); d < 0.7 {
					fail("label %q is %.3f em from its link's marker", e.Label, d)
				}
			}
		}
	}
	// A bend keeps its distance from its own link's label (a self-link's
	// label sits beside its loop by design).
	for _, e := range el.Edges {
		if e.Label == "" || e.Link.From == e.Link.To {
			continue
		}
		p := e.Points
		for _, q := range p[1 : len(p)-1] {
			dx := math.Max(0, math.Max(e.LabelBox.X0-q.X, q.X-e.LabelBox.X1))
			dy := math.Max(0, math.Max(e.LabelBox.Y0-q.Y, q.Y-e.LabelBox.Y1))
			// 1.2 em as the operator's check asked, not erLabelRoom: the
			// check must not loosen with the constant it guards.
			if d := math.Hypot(dx, dy); d < 1.2-0.05 && d > 1e-9 {
				fail("link %s->%s bends %.3f em from its label", e.Link.From.ID, e.Link.To.ID, d)
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
			fail("table %s (%.2fx%.2f) does not fit its box %.2fx%.2f", n.Label, w, h, n.Box.W(), n.Box.H())
		}
	}
}

func TestERLayoutCases(t *testing.T) {
	cases := map[string]string{
		"documentation": `erDiagram
    CUSTOMER ||--o{ ORDER : places
    ORDER ||--|{ LINE-ITEM : contains
    CUSTOMER }|..|{ DELIVERY-ADDRESS : uses
    CUSTOMER {
        string name
        string custNumber
    }`,
		"fan of five": `erDiagram
    A ||--o{ B : x
    A ||--o{ C : x
    A }|--o{ D : x
    A |o--|| E : x
    A }o..o{ F : x`,
		"cycle and self": `erDiagram
    A ||--o{ B : x
    B ||--o{ C : y
    C ||--o{ A : z
    A ||--o| A : parent`,
		"two between one pair": `erDiagram
    A ||--o{ B : owns
    A }o..o{ B : shares`,
	}
	for name, src := range cases {
		for _, dir := range []string{"TB", "BT", "LR", "RL"} {
			d, el := erOf(t, strings.Replace(src, "erDiagram", "erDiagram\n    direction "+dir, 1), fakeMeasure)
			checkER(t, name+" "+dir, d, el, fakeMeasure)
		}
	}
}

func TestRealERLayout(t *testing.T) {
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
		e, ok := d.(*mr.ER)
		if !ok {
			continue
		}
		el, err := layoutER(e, fn.measureEm)
		if err != nil {
			t.Errorf("%s: %v", file, err)
			continue
		}
		checkER(t, filepath.Base(file), e, el, fn.measureEm)
		if _, err := Render(d, Options{Font: fn}); err != nil {
			t.Errorf("%s: render: %v", file, err)
		}
		n++
	}
	if n != 11 {
		t.Errorf("laid out %d real ER blocks, want 11", n)
	}
}

func randomER(seed int64, dir string) string {
	rng := rand.New(rand.NewSource(seed))
	var b strings.Builder
	fmt.Fprintf(&b, "erDiagram\n    direction %s\n", dir)
	n := 2 + rng.Intn(10)
	words := []string{"id", "name", "調査対象", "created_at", "x", "a_rather_long_column_name"}
	for i := range n {
		if rng.Intn(3) == 0 {
			fmt.Fprintf(&b, "    E%d\n", i)
			continue
		}
		fmt.Fprintf(&b, "    E%d {\n", i)
		for range 1 + rng.Intn(5) {
			fmt.Fprintf(&b, "        string %s", words[rng.Intn(len(words))])
			if rng.Intn(3) == 0 {
				b.WriteString(" PK")
			}
			if rng.Intn(4) == 0 {
				b.WriteString(` "a comment"`)
			}
			b.WriteString("\n")
		}
		b.WriteString("    }\n")
	}
	left := []string{"||", "|o", "}o", "}|"}
	right := []string{"||", "o|", "o{", "|{"}
	for range rng.Intn(2 * n) {
		a, c := rng.Intn(n), rng.Intn(n)
		if a == c && rng.Intn(3) > 0 {
			continue
		}
		kind := "--"
		if rng.Intn(3) == 0 {
			kind = ".."
		}
		fmt.Fprintf(&b, "    E%d %s%s%s E%d : %s\n", a, left[rng.Intn(4)], kind, right[rng.Intn(4)], c, words[rng.Intn(len(words))])
	}
	return b.String()
}

func TestERLayoutRandom(t *testing.T) {
	dirs := []string{"TB", "BT", "LR", "RL"}
	for seed := int64(1); seed <= int64(*randomN); seed++ {
		src := randomER(seed, dirs[seed%4])
		d, el := erOf(t, src, fakeMeasure)
		checkER(t, fmt.Sprintf("seed %d", seed), d, el, fakeMeasure)
		if t.Failed() {
			t.Logf("source:\n%s", src)
			return
		}
	}
}

// TestRealERBends counts direction changes over the 11 real ER diagrams:
// a baseline like TestRealBends. The operator's first ER check marked a
// link that could have run straight (b6ffb9fc3c) and too many bends
// (0a1fd38f5f); letting a table's links use 80% of its face fixed both.
func TestRealERBends(t *testing.T) {
	fn, err := DefaultFont()
	if err != nil {
		t.Skipf("no system font: %v", err)
	}
	files, _ := filepath.Glob("../testdata/real/*/*.mmd")
	total := 0
	for _, file := range files {
		b, _ := os.ReadFile(file)
		d, err := mr.Parse(string(b))
		if err != nil {
			continue
		}
		e, ok := d.(*mr.ER)
		if !ok {
			continue
		}
		el, err := layoutER(e, fn.measureEm)
		if err != nil {
			t.Fatal(err)
		}
		for _, ed := range el.Edges {
			p := ed.Points
			for i := 1; i+1 < len(p); i++ {
				ax, ay := p[i].X-p[i-1].X, p[i].Y-p[i-1].Y
				bx, by := p[i+1].X-p[i].X, p[i+1].Y-p[i].Y
				if math.Abs(ax*by-ay*bx) > 1e-6 {
					total++
				}
			}
		}
	}
	t.Logf("ER bends %d", total)
	if total > erBendsBaseline {
		t.Errorf("ER bends %d (baseline %d)", total, erBendsBaseline)
	}
}

// erBendsBaseline: 46 after the first ER check (48 with a 60% face), 40
// after the second (pushRuns).
const erBendsBaseline = 40

// What is drawn matches the source: each end's marker has the parts of its
// own cardinality (next to the entity the maximum, then the minimum), and
// the line is dashed exactly for non-identifying relationships. The layout
// checks cannot see this: they end at the right entity whatever is drawn.
func TestERDrawnMarkers(t *testing.T) {
	fn, err := DefaultFont()
	if err != nil {
		t.Skipf("no system font: %v", err)
	}
	parts := map[mr.Cardinality]string{
		mr.ExactlyOne: "bar@0.45 bar@0.75",
		mr.ZeroOrOne:  "bar@0.45 circle@1.00",
		mr.ZeroOrMore: "foot circle@1.10",
		mr.OneOrMore:  "foot bar@1.05",
	}
	src := `erDiagram
    direction DIR
    A |o--o{ B : first
    B }|..|| C : second
    C ||--o| A : third
    A }o..|{ A : self`
	for _, dir := range []string{"TB", "BT", "LR", "RL"} {
		d, err := mr.Parse(strings.Replace(src, "DIR", dir, 1))
		if err != nil {
			t.Fatal(err)
		}
		el, err := layoutER(d.(*mr.ER), fn.measureEm)
		if err != nil {
			t.Fatal(err)
		}
		drawn := map[string]string{}
		if _, err := render(d, Options{Font: fn}, func(s string) {
			if k, v, ok := strings.Cut(s, ": "); ok {
				drawn[k] = v
			} else {
				drawn[s] = ""
			}
		}); err != nil {
			t.Fatal(err)
		}
		for _, e := range el.Edges {
			r := el.rels[e.Link]
			p, q := e.Points[0], e.Points[len(e.Points)-1]
			if got := drawn[fmt.Sprintf("marker at %.2f,%.2f", p.X, p.Y)]; got != parts[r.FromCard] {
				t.Errorf("%s %s: marker at %s is %q, want %s %q", dir, r.Label, r.From.Name, got, r.FromCard, parts[r.FromCard])
			}
			if got := drawn[fmt.Sprintf("marker at %.2f,%.2f", q.X, q.Y)]; got != parts[r.ToCard] {
				t.Errorf("%s %s: marker at %s is %q, want %s %q", dir, r.Label, r.To.Name, got, r.ToCard, parts[r.ToCard])
			}
			line := fmt.Sprintf("line dashed=%v %.2f,%.2f", !r.Identifying, p.X, p.Y)
			if _, ok := drawn[line]; !ok {
				t.Errorf("%s %s: no %q", dir, r.Label, line)
			}
		}
	}
}
