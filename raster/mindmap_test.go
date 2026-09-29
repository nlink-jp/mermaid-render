package raster

import (
	"errors"
	"flag"
	"fmt"
	"image/color"
	"math"
	"math/rand"
	"strings"
	"testing"

	mr "github.com/nlink-jp/mermaid-render"
)

func mindmapOf(t *testing.T, src string) (*mr.Mindmap, *mindmapLayout) {
	t.Helper()
	d, err := mr.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v\n%s", err, src)
	}
	m := d.(*mr.Mindmap)
	ml, err := layoutMindmap(m, fakeMeasure)
	if err != nil {
		t.Fatalf("layout: %v\n%s", err, src)
	}
	return m, ml
}

func checkMindmap(t *testing.T, name string, m *mr.Mindmap, ml *mindmapLayout) {
	t.Helper()
	for _, f := range mindmapFaults(m, ml, fakeMeasure) {
		t.Errorf("%s: %s", name, f)
	}
}

var mindmapCases = map[string]string{
	"the documentation's example": `mindmap
  root((mindmap))
    Origins
      Long history
      ::icon(fa fa-book)
      Popularisation
        British popular psychology author Tony Buzan
    Research
      On effectiveness<br/>and features
      On Automatic creation
        Uses
            Creative techniques
            Strategic planning
            Argument mapping
    Tools
      Pen and paper
      Mermaid`,
	"every shape":         "mindmap\n  root\n    a[rect]\n    b(rounded)\n    c((circle))\n    d)cloud(\n    e))bang((\n    f{{hexagon}}\n    g",
	"a root alone":        "mindmap\n  root",
	"one child":           "mindmap\n  root\n    only",
	"a tall parent":       "mindmap\n  root\n    a[one<br>two<br>three<br>four<br>five]\n      x\n    b",
	"a long label wraps":  "mindmap\n  root\n    A label long enough to wrap onto several lines of text\n    長い日本語のラベルは十二・五文字幅を超えると折り返される、はず",
	"an unbreakable word": "mindmap\n  root\n    Supercalifragilisticexpialidocious_and_more",
	"deep":                "mindmap\n  r\n    a\n      b\n        c\n          d\n            e\n              f",
}

func TestMindmapLayoutCases(t *testing.T) {
	for name, src := range mindmapCases {
		m, ml := mindmapOf(t, src)
		checkMindmap(t, name, m, ml)
	}
}

// randomMindmap is a mind map of 1-60 nodes: random depths, every shape,
// labels short, long, multi-line and Japanese.
func randomMindmap(seed int64) string {
	r := rand.New(rand.NewSource(seed))
	words := []string{"a", "node", "データ", "設計と実装", "x y z", "Long label text", "言語", "Go", "ひらがなとカタカナ", "A<br>B", "12345"}
	shapes := [][2]string{{"", ""}, {"[", "]"}, {"(", ")"}, {"((", "))"}, {")", "("}, {"))", "(("}, {"{{", "}}"}}
	var b strings.Builder
	b.WriteString("mindmap\n")
	n := 1 + r.Intn(60)
	depth := 0
	for i := range n {
		if i > 0 {
			depth = 1 + r.Intn(min(depth+1, 6))
		}
		label := words[r.Intn(len(words))]
		for range r.Intn(4) {
			label += " " + words[r.Intn(len(words))]
		}
		s := shapes[r.Intn(len(shapes))]
		id := ""
		if s[0] != "" {
			id = fmt.Sprintf("n%d", i)
		}
		fmt.Fprintf(&b, "%s%s%s%s%s\n", strings.Repeat("  ", depth+1), id, s[0], label, s[1])
	}
	return b.String()
}

var mindmapRandomN = flag.Int("mindmaprandom", 400, "number of random mind maps TestMindmapLayoutRandom lays out")

func TestMindmapLayoutRandom(t *testing.T) {
	for seed := int64(1); seed <= int64(*mindmapRandomN); seed++ {
		src := randomMindmap(seed)
		m, ml := mindmapOf(t, src)
		before := t.Failed()
		checkMindmap(t, fmt.Sprintf("seed %d", seed), m, ml)
		if t.Failed() && !before {
			t.Logf("source of seed %d:\n%s", seed, src)
			return
		}
	}
}

// What is drawn is the tree: every node with its text, every line from
// parent to child.
func TestMindmapDrawn(t *testing.T) {
	fn := systemFont(t)
	d, err := mr.Parse("mindmap\n  root((中心))\n    左[L]\n      葉\n    右(R)")
	if err != nil {
		t.Fatal(err)
	}
	var trace []string
	if _, err := render(d, Options{Font: fn}, probe{trace: func(s string) { trace = append(trace, s) }}); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(trace, "\n")
	for _, want := range []string{`node 0 "中心"`, `node 1 "L"`, `node 2 "葉"`, `node 3 "R"`, "edge 0 1", "edge 1 2", "edge 0 3"} {
		if !strings.Contains(got, want) {
			t.Errorf("trace lacks %q:\n%s", want, got)
		}
	}
	_, ml := mindmapOf(t, "mindmap\n  root((中心))\n    左[L]\n      葉\n    右(R)")
	if ml.nodes[1].side != -1 || ml.nodes[2].side != -1 || ml.nodes[3].side != 1 {
		t.Errorf("sides %d %d %d, want the first child and its child left, the second right", ml.nodes[1].side, ml.nodes[2].side, ml.nodes[3].side)
	}
}

// A label is bounded as the flowchart's are.
func TestMindmapLabelLimit(t *testing.T) {
	d, err := mr.Parse("mindmap\n  " + strings.Repeat("あ", MaxLabel+1))
	if err != nil {
		t.Fatal(err)
	}
	_, err = layoutMindmap(d.(*mr.Mindmap), fakeMeasure)
	var e *mr.Error
	if !errors.As(err, &e) || e.Kind != mr.UnsupportedConstruct {
		t.Errorf("a label of %d characters: %v", MaxLabel+1, err)
	}
}

// Wrapping breaks at spaces and between CJK characters, never before
// closing punctuation, and keeps an unbreakable run whole.
func TestWrapLabel(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"short", "short"},
		{"one two three four five six seven", "one two three four\nfive six seven"},
		{"あいうえおかきくけこさしすせそ", "あいうえおかきくけこさし\nすせそ"},
		{"あいうえおかきくけこさし。すせそ", "あいうえおかきくけこさ\nし。すせそ"},
		{"Supercalifragilisticexpialidocious", "Supercalifragilisticexpialidocious"},
		{"a\nb", "a\nb"},
		{"state-of-the-art-machine-learning", "state-of-the-art-\nmachine-learning"},
		{"version 1.2-3456789012345", "version\n1.2-3456789012345"},
	} {
		got, err := wrapLabel(c.in, mmWrap, fakeMeasure)
		if err != nil || got != c.want {
			t.Errorf("%q: %q, %v; want %q", c.in, got, err, c.want)
		}
	}
}

// Every render checks a mind map's layout: a corrupted one is a layout
// fault.
func TestRenderRefusesMindmapFaults(t *testing.T) {
	fn := systemFont(t)
	d, _ := mr.Parse(mmFaultSrc)
	_, err := render(d, Options{Font: fn}, probe{corruptMindmap: func(ml *mindmapLayout) { ml.nodes[5].side = -1 }})
	var e *mr.Error
	if !errors.As(err, &e) || e.Kind != mr.LayoutFault {
		t.Errorf("%v, want a layout fault", err)
	}
}

const mmFaultSrc = "mindmap\n  root((中心))\n    左[L]\n      葉一\n      あいうえおかきくけこさしすせそ\n    右(R)\n      枝\n    下\n"

// Each check catches what it is for: every corruption must be named by its
// own check (another check catching it too does not guard this one).
func TestMindmapFaultsCatch(t *testing.T) {
	fn := systemFont(t)
	move := func(n *mmNode, dx, dy float64) {
		n.box, n.tbox, n.band = n.box.shifted(dx, dy), n.tbox.shifted(dx, dy), n.band.shifted(dx, dy)
		n.cy += dy
	}
	retext := func(n *mmNode, text string) {
		w, h, _ := fn.measureEm(text, false)
		c := n.tbox.Center()
		n.text, n.tbox = text, rect{c.X - w/2, c.Y - h/2, c.X + w/2, c.Y + h/2}
	}
	for _, c := range []struct {
		name, want string
		corrupt    func(*mindmapLayout)
	}{
		{"a node lost", "placed", func(ml *mindmapLayout) { ml.nodes = ml.nodes[1:] }},
		{"a line lost", "placed", func(ml *mindmapLayout) { ml.edges = ml.edges[1:] }},
		{"text out of its shape", "leaves its shape", func(ml *mindmapLayout) { ml.nodes[1].tbox = ml.nodes[1].tbox.shifted(3, 0) }},
		{"text box too small", "text's size", func(ml *mindmapLayout) { ml.nodes[1].tbox.X1 -= 0.3 }},
		{"text lost in wrapping", "is not", func(ml *mindmapLayout) { retext(&ml.nodes[2], "葉") }},
		{"a line too wide", "could break", func(ml *mindmapLayout) {
			retext(&ml.nodes[3], strings.ReplaceAll(ml.nodes[3].text, "\n", ""))
		}},
		{"a node on the wrong side", "is on side", func(ml *mindmapLayout) { ml.nodes[5].side = -1 }},
		{"a child too far out", "one gap out", func(ml *mindmapLayout) { move(&ml.nodes[5], 1, 0) }},
		{"siblings out of order", "out of order", func(ml *mindmapLayout) {
			a, b := &ml.nodes[2], &ml.nodes[3]
			move(a, 0, b.band.Y1-a.band.Y1)
			move(b, 0, a.band.Y0-b.band.Y0)
		}},
		{"a parent off its children's middle", "not between its children", func(ml *mindmapLayout) { move(&ml.nodes[1], 0, 0.1) }},
		{"a node outside the picture", "outside the picture", func(ml *mindmapLayout) { ml.W, ml.nodes[0].band.X1 = ml.W-1, ml.W-1 }},
		// 下, shrunk into the gap between 右 and 枝: inside 右's band, and
		// between a parent and its child.
		{"a node in another's band", "lies in node", func(ml *mindmapLayout) { ml.nodes[6].box = mmGapBox(ml) }},
		{"a node between parent and child", "lies between", func(ml *mindmapLayout) { ml.nodes[6].box = mmGapBox(ml) }},
		{"a band not holding its subtree", "does not hold", func(ml *mindmapLayout) { ml.nodes[1].band.Y1 = ml.nodes[1].band.Y0 + 0.1 }},
		{"nodes overlapping", "overlap", func(ml *mindmapLayout) { move(&ml.nodes[6], 0, ml.nodes[1].cy-ml.nodes[6].cy) }},
		{"a line not from its parent's side", "does not run from", func(ml *mindmapLayout) { ml.edges[0].pts[0].X += 0.5 }},
		{"a line straying", "leaves the space", func(ml *mindmapLayout) { ml.edges[0].pts[5].Y -= 20 }},
		{"a line joining the wrong nodes", "joins", func(ml *mindmapLayout) { ml.edges[1].from = 0 }},
	} {
		d, _ := mr.Parse(mmFaultSrc)
		m := d.(*mr.Mindmap)
		ml, err := layoutMindmap(m, fn.measureEm)
		if err != nil {
			t.Fatal(err)
		}
		if fs := mindmapFaults(m, ml, fn.measureEm); len(fs) > 0 {
			t.Fatalf("the uncorrupted layout: %v", fs)
		}
		c.corrupt(ml)
		fs := mindmapFaults(m, ml, fn.measureEm)
		found := false
		for _, f := range fs {
			found = found || strings.Contains(f, c.want)
		}
		if !found {
			t.Errorf("%s: faults %q, want one naming %q", c.name, fs, c.want)
		}
	}
}

// Each shape is the one its bracket names: a rectangle's corners, a
// hexagon's six points, a circle's constant radius, the bang's spikes and
// the cloud's bumps standing out of their inner ellipse, the rounded
// rectangle rounder than the default shape.
func TestMindmapShapeForms(t *testing.T) {
	box := rect{0, 0, 10, 6}
	radii := func(s mr.MindmapShape, box rect) (lo, hi float64) {
		c := box.Center()
		lo, hi = math.Inf(1), 0
		for _, q := range mmOutline(s, box) {
			// On the unit circle of the box's ellipse.
			r := math.Hypot((q.X-c.X)/(box.W()/2), (q.Y-c.Y)/(box.H()/2))
			lo, hi = math.Min(lo, r), math.Max(hi, r)
		}
		return
	}
	if p := mmOutline(mr.MindmapRect, box); len(p) != 4 || polyArea(p) != 60 {
		t.Errorf("rectangle: %v", p)
	}
	if p := mmOutline(mr.MindmapHexagon, box); len(p) != 6 {
		t.Errorf("hexagon: %d points", len(p))
	}
	if lo, hi := radii(mr.MindmapCircle, rect{0, 0, 6, 6}); math.Abs(lo-hi) > 1e-9 {
		t.Errorf("circle radii %v..%v", lo, hi)
	}
	if lo, hi := radii(mr.MindmapBang, box); math.Abs(hi/lo-mmSpikeOut) > 1e-9 {
		t.Errorf("bang: spikes %v times their feet, want %v", hi/lo, mmSpikeOut)
	}
	if lo, hi := radii(mr.MindmapCloud, box); hi/lo < mmBumpOut-1e-3 {
		t.Errorf("cloud: bumps %v times the ellipse, want %v", hi/lo, mmBumpOut)
	}
	if polyArea(mmOutline(mr.MindmapRounded, box)) >= polyArea(mmOutline(mr.MindmapDefault, box)) {
		t.Error("the rounded rectangle is no rounder than the default shape")
	}
}

// Every shape is inside its box and holds its text box.
func TestMindmapShapes(t *testing.T) {
	for s := mr.MindmapDefault; s <= mr.MindmapHexagon; s++ {
		for _, tb := range [][2]float64{{1, 1}, {12, 1.2}, {3, 6}, {0, 1.2}} {
			w, h := mmShapeSize(s, tb[0], tb[1])
			box := rect{0, 0, w, h}
			poly := mmOutline(s, box)
			if !within(box, polyBounds(poly)) {
				t.Errorf("shape %d around %v leaves its box", s, tb)
			}
			c := box.Center()
			tr := rect{c.X - tb[0]/2, c.Y - tb[1]/2, c.X + tb[0]/2, c.Y + tb[1]/2}
			if !rectInPolygon(poly, tr) {
				t.Errorf("shape %d does not hold a %v text", s, tb)
			}
		}
	}
}

// mmGapBox is a small box in the gap between 右 and 枝 (mmFaultSrc).
func mmGapBox(ml *mindmapLayout) rect {
	p := ml.edges[4].pts[0] // 右 to 枝
	return rect{p.X + 0.5, p.Y - 0.2, p.X + 0.9, p.Y + 0.2}
}

// A label wraps at its shape's width: 7.5 em in the rectangle, rounded
// rectangle and hexagon (mermaid's 120 px), 12.5 em in the others (200 px).
func TestMindmapWrapWidths(t *testing.T) {
	label := "one two three four five six seven eight"
	for _, c := range []struct {
		open, close string
		width       float64
	}{{"[", "]", mmWrapNarrow}, {"(", ")", mmWrapNarrow}, {"{{", "}}", mmWrapNarrow}, {"", "", mmWrap}, {"((", "))", mmWrap}, {")", "(", mmWrap}, {"))", "((", mmWrap}} {
		id := ""
		if c.open != "" {
			id = "n"
		}
		_, ml := mindmapOf(t, "mindmap\n  "+id+c.open+label+c.close)
		want, _ := wrapLabel(label, c.width, fakeMeasure)
		if got := ml.nodes[0].text; got != want {
			t.Errorf("%s…%s: %q, want %q", c.open, c.close, got, want)
		}
	}
}

// The text check is exact for non-convex outlines: a text whose corners
// are inside a bang but whose edge crosses a spike's foot is outside.
func TestRectInPolygon(t *testing.T) {
	bang := mmOutline(mr.MindmapBang, rect{0, 0, 10, 6})
	c := rect{0, 0, 10, 6}.Center()
	inner := 3 / mmSpikeOut // the spikes' feet on the short axis
	if !rectInPolygon(bang, rect{c.X - 1, c.Y - 1, c.X + 1, c.Y + 1}) {
		t.Error("a small text at the middle is outside")
	}
	// Corners inside the tips' reach, an edge through a foot.
	if rectInPolygon(bang, rect{c.X - 3, c.Y - inner - 0.3, c.X + 3, c.Y + inner + 0.3}) {
		t.Error("a text crossing the spikes' feet is inside")
	}
}

// What is drawn, node by node and line by line, is what the layout and
// the colours say: each node filled with its section's colour, its text
// in the colour that reads on it at its box's middle, its shape's outline;
// each line in its child's colour, thinner with depth, from middle to
// middle.
func TestMindmapDrawnDetails(t *testing.T) {
	fn := systemFont(t)
	src := "mindmap\n  root((中心))\n    a[四角]\n      aa(角丸)\n        aaa\n    b))爆発((\n      bb)雲(\n    c{{六角}}\n"
	for k := range 10 {
		src += fmt.Sprintf("    n%d\n", k)
	}
	d, err := mr.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	m := d.(*mr.Mindmap)
	ml, err := layoutMindmap(m, fn.measureEm)
	if err != nil {
		t.Fatal(err)
	}
	var trace []string
	if _, err := render(d, Options{Font: fn}, probe{trace: func(s string) { trace = append(trace, s) }}); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(trace, "\n")
	fill := func(i int) color.RGBA {
		if sec := m.Nodes[i].Section; sec >= 0 {
			return mmPalette[sec%len(mmPalette)]
		}
		return mmRootColor
	}
	for i, n := range ml.nodes {
		poly := mmOutline(m.Nodes[i].Shape, n.box)
		c := n.tbox.Center()
		want := fmt.Sprintf("node %d %q fill=%s text=%s at=%.3f,%.3f poly=%d area=%.3f", i, n.text, hexColor(fill(i)), hexColor(textOn(fill(i))), c.X, c.Y, len(poly), polyArea(poly))
		if !strings.Contains(got, want) {
			t.Errorf("trace lacks %q", want)
		}
	}
	width := map[int]float64{}
	for _, e := range ml.edges {
		a, b := ml.nodes[e.from].box.Center(), ml.nodes[e.to].box.Center()
		want := fmt.Sprintf("edge %d %d col=%s w=%.3f from=%.3f,%.3f to=%.3f,%.3f", e.from, e.to, hexColor(fill(e.to)), e.w, a.X, a.Y, b.X, b.Y)
		if !strings.Contains(got, want) {
			t.Errorf("trace lacks %q", want)
		}
		width[m.Nodes[e.from].Level] = e.w
	}
	if !(width[0] > width[1] && width[1] > width[2]) {
		t.Errorf("line widths by depth %v do not fall", width)
	}
	// Sections 0 and 11 share a colour; 0 and 1 do not; the text on the
	// dark root is light.
	if fill(ml.sides[0][0]) == fill(ml.sides[1][0]) || textOn(mmRootColor) == colText {
		t.Error("colours do not tell the sections apart, or the root's text does not read")
	}
}
