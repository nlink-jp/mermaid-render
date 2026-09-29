package raster

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mr "github.com/nlink-jp/mermaid-render"
)

// Every real flowchart draws as text art (mermaid-ascii drew 21 of 26 real
// flowchart and ER blocks).
func TestTextArtRealFlowcharts(t *testing.T) {
	files, _ := filepath.Glob("../testdata/real/*/*.mmd")
	n := 0
	for _, p := range files {
		b, _ := os.ReadFile(p)
		d, err := mr.Parse(string(b))
		if err != nil {
			continue
		}
		if _, ok := d.(*mr.Flowchart); !ok {
			continue
		}
		n++
		if _, err := RenderText(d, TextOptions{}); err != nil {
			t.Errorf("%s: %v", filepath.Base(p), err)
		}
	}
	if n != 22 {
		t.Errorf("%d real flowcharts, want 22", n)
	}
}

// Random flowcharts in every direction: nearly all draw; the rest are
// refused (the caller shows the source), never drawn wrong; the art is
// the same every time.
func TestTextArtRandomFlowcharts(t *testing.T) {
	ok, total := 0, 0
	for seed := int64(1); seed <= 300; seed++ {
		for _, dir := range []string{"TD", "LR", "BT", "RL"} {
			d, err := mr.Parse(randomFlowchart(seed, dir))
			if err != nil {
				t.Fatal(err)
			}
			total++
			art, err := RenderText(d, TextOptions{})
			var e *mr.Error
			if err != nil && !(errors.As(err, &e) && e.Kind == mr.LayoutFault) {
				t.Fatalf("seed %d %s: %v", seed, dir, err)
			}
			if err != nil {
				continue
			}
			ok++
			if again, _ := RenderText(d, TextOptions{}); again != art {
				t.Fatalf("seed %d %s: not deterministic", seed, dir)
			}
		}
	}
	if ok*100 < total*99 {
		t.Errorf("%d of %d random flowcharts drawn, want 99%%", ok, total)
	}
}

// The glyphs: shapes by family, strokes, heads, a label breaking its line.
func TestTextArtGlyphs(t *testing.T) {
	src := "flowchart TD\n    A([開始]) --> B{判定}\n    B -- はい --> C[完了]\n    B -.-> D(保留)\n    C ==> E[終了]\n    D --o E"
	got, err := RenderTextSource(src, TextOptions{})
	if err != nil {
		t.Fatal(err)
	}
	want := `   ╭──────╮
   │ 開始 │
   ╰───┬──╯
       │
       ▼
◇───────────◇
│   判定    │
◇──────┬───┬◇
       │   ┆
       │   └┄┄┄┄┄┄┄┄┄┐
       │             ┆
     はい            ┆
       ▼             ▼
   ┌──────┐      ╭──────╮
   │ 完了 │      │ 保留 │
   └───┬──┘      ╰───┬──╯
       ┃             │
       ┃             │
       ┃   ┌─────────┘
       ▼   ○
┌───────────┐
│   終了    │
└───────────┘`
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// Labels the grid cannot hold cell by cell are refused; the caller's
// widths are used.
func TestTextArtLabels(t *testing.T) {
	for _, label := range []string{"a\x1b[31mred", "tab\there", "e\u0301", "👨\u200d👩", "a\u202eb"} {
		f := &mr.Flowchart{Nodes: []*mr.Node{{ID: "a", Label: label, Line: 2}}}
		_, err := RenderText(f, TextOptions{})
		var e *mr.Error
		if !errors.As(err, &e) || e.Kind != mr.UnsupportedConstruct {
			t.Errorf("%q: %v, want unsupported", label, err)
		}
	}
	narrow := func(rune) int { return 1 }
	wide, _ := RenderTextSource("flowchart TD\n    a[日本語]", TextOptions{})
	thin, _ := RenderTextSource("flowchart TD\n    a[日本語]", TextOptions{Width: narrow})
	// 日本語 is 6 cells wide, or 3; a box is its text and 4, even.
	if !strings.Contains(wide, "┌────────┐") || !strings.Contains(thin, "┌──────┐\n") {
		t.Errorf("widths not used:\n%s\n%s", wide, thin)
	}
}

// Every render checks the art on the grid: each corruption is named by its
// own check.
func TestTextArtFaults(t *testing.T) {
	src := "flowchart TD\n    subgraph S [枠]\n        A[一] --> B[二]\n    end\n    B -->|ラベル| C[三]\n    A --> C"
	for _, c := range []struct {
		name, want string
		corrupt    func(*textFlow)
	}{
		{"boxes overlapping", "overlap", func(tf *textFlow) { tf.boxes[1] = tf.boxes[0] }},
		{"a box too small", "smaller than its text", func(tf *textFlow) { tf.boxes[0].x1 = tf.boxes[0].x0 + 3 }},
		{"a frame not holding a member", "does not hold", func(tf *textFlow) { tf.frames[0].y1 = tf.frames[0].y0 + 2 }},
		{"a line through a box", "runs through", func(tf *textFlow) {
			// B moved onto A --> C's column.
			x, b := tf.paths[2][0][0], &tf.boxes[1]
			b.x0, b.x1 = x-2, x+3
		}},
		{"a line not ending beside its box", "does not end beside", func(tf *textFlow) {
			p := tf.paths[0]
			p[len(p)-1][1] -= 1
		}},
		{"a diagonal", "not a right angle", func(tf *textFlow) { tf.paths[0][len(tf.paths[0])-1][0]++ }},
		{"two links sharing a run", "meet", func(tf *textFlow) {
			// A --> C ends down B --> C's last run.
			p, q := tf.paths[2], tf.paths[1]
			a, b, c := p[0], q[len(q)-2], q[len(q)-1]
			tf.paths[2] = [][2]int{a, {a[0], b[1]}, b, c}
		}},
		{"a label over a box", "lies over", func(tf *textFlow) { tf.labels[1] = tf.boxes[2] }},
		{"a head after a corner", "does not end a straight run", func(tf *textFlow) {
			// A --> B arriving across onto the cell above B: its head turns.
			p := tf.paths[0]
			last := p[len(p)-1]
			tf.paths[0] = [][2]int{p[0], {p[0][0], last[1] - 2}, {last[0] - 3, last[1] - 2}, {last[0] - 3, last[1]}, last}
		}},
		{"a label too small", "smaller than its text", func(tf *textFlow) { tf.labels[1].x1 = tf.labels[1].x0 }},
		{"a title with no room", "no room for its title", func(tf *textFlow) {
			// The frame hugs its members: no row above or below them.
			fr := &tf.frames[0]
			top, bottom := tf.boxes[0].y0, tf.boxes[1].y1
			fr.y0, fr.y1 = top-1, bottom+1
			tf.titles[0].y0, tf.titles[0].y1 = top, top
		}},
	} {
		textProbe = c.corrupt
		debugArt = true
		d, _ := mr.Parse(src)
		_, err := RenderText(d, TextOptions{})
		textProbe, debugArt = nil, false
		var e *mr.Error
		if !errors.As(err, &e) || e.Kind != mr.LayoutFault || !strings.Contains(e.Msg, c.want) {
			t.Errorf("%s: %v, want a fault naming %q", c.name, err, c.want)
		}
	}
	// Uncorrupted, it draws.
	if _, err := RenderTextSource(src, TextOptions{}); err != nil {
		t.Fatal(err)
	}
	_ = fmt.Sprint
}

// Every real ER diagram draws as text art.
func TestTextArtRealER(t *testing.T) {
	files, _ := filepath.Glob("../testdata/real/*/*.mmd")
	n := 0
	for _, p := range files {
		b, _ := os.ReadFile(p)
		d, err := mr.Parse(string(b))
		if err != nil {
			continue
		}
		if _, ok := d.(*mr.ER); !ok {
			continue
		}
		n++
		if _, err := RenderText(d, TextOptions{}); err != nil {
			t.Errorf("%s: %v", filepath.Base(p), err)
		}
	}
	if n != 11 {
		t.Errorf("%d real ER diagrams, want 11", n)
	}
}

// Random ER diagrams: most draw (dense random ones are refused more often
// than flowcharts: a self-relationship's label can find no room), and
// the art is the same every time.
func TestTextArtRandomER(t *testing.T) {
	ok, total := 0, 0
	for seed := int64(1); seed <= 200; seed++ {
		for _, dir := range []string{"TD", "LR", "BT", "RL"} {
			d, err := mr.Parse(randomER(seed, dir))
			if err != nil {
				t.Fatal(err)
			}
			total++
			art, err := RenderText(d, TextOptions{})
			var e *mr.Error
			if err != nil && !(errors.As(err, &e) && e.Kind == mr.LayoutFault) {
				t.Fatalf("seed %d %s: %v", seed, dir, err)
			}
			if err != nil {
				continue
			}
			ok++
			if again, _ := RenderText(d, TextOptions{}); again != art {
				t.Fatalf("seed %d %s: not deterministic", seed, dir)
			}
		}
	}
	if ok*100 < total*93 {
		t.Errorf("%d of %d random ER diagrams drawn, want 93%%", ok, total)
	}
}

// ER: tables, cardinality in mermaid's notation on each side (mirrored for
// a table on the left), a self-relationship looping on the right face.
func TestTextArtER(t *testing.T) {
	src := "erDiagram\n    CUSTOMER ||--o{ ORDER : places\n    ORDER ||--|{ LINE_ITEM : contains\n    CUSTOMER {\n        string name PK\n        string email\n    }\n    EMPLOYEE |o..o{ EMPLOYEE : manages"
	got, err := RenderTextSource(src, TextOptions{})
	if err != nil {
		t.Fatal(err)
	}
	want := `┌──────────────────────┐
│       CUSTOMER       │
├────────┬───────┬─────┤              ┌────────┐                ┌────────────┐
│ string │ name  │ PK  │||──places──o{│ ORDER  │||──contains──|{│ LINE_ITEM  │
│ string │ email │     │              └────────┘                └────────────┘
└────────┴───────┴─────┘



      ┌──────────┐
      │ EMPLOYEE │|o┄┄┐
      │          │}o┄┄┘ manages
      └──────────┘`
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	for c, want := range map[mr.Cardinality][2]string{
		mr.ExactlyOne: {"||", "||"}, mr.ZeroOrOne: {"o|", "|o"}, mr.ZeroOrMore: {"o{", "}o"}, mr.OneOrMore: {"|{", "}|"},
	} {
		r := cardRunes(c)
		if m := mirrored(r); string(r[:]) != want[0] || string(m[:]) != want[1] {
			t.Errorf("%v: %q / %q, want %q", c, string(r[:]), string(m[:]), want)
		}
	}
}

// A mark must stand on a run across into its table.
func TestTextArtERMarkFault(t *testing.T) {
	textProbe = func(tf *textFlow) {
		// The link into ORDER turned to come down onto its top face.
		p := tf.paths[0]
		end := p[len(p)-1]
		b := tf.boxes[1]
		tf.paths[0] = [][2]int{p[0], {b.x0 + 2, p[0][1]}, {b.x0 + 2, b.y0 - 1}}
		_ = end
	}
	debugArt = true
	_, err := RenderTextSource("erDiagram\n    A ||--o{ ORDER : x", TextOptions{})
	textProbe, debugArt = nil, false
	var e *mr.Error
	if !errors.As(err, &e) || e.Kind != mr.LayoutFault || !strings.Contains(e.Msg, "cardinality mark") {
		t.Errorf("%v, want a fault naming the cardinality mark", err)
	}
}

// Every real sequence diagram draws as text art, Japanese labels included
// (mermaid-ascii refused them).
func TestTextArtRealSequences(t *testing.T) {
	files, _ := filepath.Glob("../testdata/real/*/*.mmd")
	n := 0
	for _, p := range files {
		b, _ := os.ReadFile(p)
		d, err := mr.Parse(string(b))
		if err != nil {
			continue
		}
		if _, ok := d.(*mr.Sequence); !ok {
			continue
		}
		n++
		if _, err := RenderText(d, TextOptions{}); err != nil {
			t.Errorf("%s: %v", filepath.Base(p), err)
		}
	}
	if n != 10 {
		t.Errorf("%d real sequence diagrams, want 10", n)
	}
}

func TestTextArtRandomSequences(t *testing.T) {
	ok := 0
	for seed := int64(1); seed <= 600; seed++ {
		d, err := mr.Parse(randomSequence(seed))
		if err != nil {
			t.Fatal(err)
		}
		art, err := RenderText(d, TextOptions{})
		var e *mr.Error
		if err != nil && !(errors.As(err, &e) && e.Kind == mr.LayoutFault) {
			t.Fatalf("seed %d: %v", seed, err)
		}
		if err != nil {
			continue
		}
		ok++
		if again, _ := RenderText(d, TextOptions{}); again != art {
			t.Fatalf("seed %d: not deterministic", seed)
		}
	}
	if ok*100 < 600*99 {
		t.Errorf("%d of 600 random sequence diagrams drawn, want 99%%", ok)
	}
}

// Sequence: a group, autonumber, activation, blocks with a section, notes,
// a message to self, the head kinds.
func TestTextArtSequence(t *testing.T) {
	src := `sequenceDiagram
    autonumber
    box 社内
    actor U as 利用者
    participant A as エージェント
    end
    participant T as ツール
    U->>A: 調査して
    activate A
    loop 3回まで
        A->>T: 問い合わせ
        alt 成功
            T-->>A: 結果
        else 失敗
            T--xA: エラー
        end
    end
    Note over A,T: 集計
    A->>A: 要約
    A-)U: 報告
    deactivate A`
	got, err := RenderTextSource(src, TextOptions{})
	if err != nil {
		t.Fatal(err)
	}
	want := `┌─社内─────────────────────────┐
│┌────────┐    ┌──────────────┐│   ┌────────┐
││ 利用者 │    │ エージェント ││   │ ツール │
│└────┬───┘    └───────┬──────┘│   └────┬───┘
│     │                │       │        │
│     │   1. 調査して  │       │        │
│     │───────────────►│       │        │
│     │                │       │        │
│     │           ┌─[loop] 3回まで──────┼────┐
│     │           │    ┃       │        │    │
│     │           │    ┃  2. 問い合わせ │    │
│     │           │    ┃───────┼───────►│    │
│     │           │    ┃       │        │    │
│     │           │  ┌─[alt] 成功───────┼─┐  │
│     │           │  │ ┃       │        │ │  │
│     │           │  │ ┃     3. 結果    │ │  │
│     │           │  │ ┃◄┈┈┈┈┈┈┼┈┈┈┈┈┈┈┈│ │  │
│     │           │  │ ┃       │        │ │  │
│     │           │  ├┈[else] 失敗┈┈┈┈┈┈┼┈┤  │
│     │           │  │ ┃       │        │ │  │
│     │           │  │ ┃    4. エラー   │ │  │
│     │           │  │ ┃×┈┈┈┈┈┈┼┈┈┈┈┈┈┈┈│ │  │
│     │           │  │ ┃       │        │ │  │
│     │           │  └─┼────────────────┼─┘  │
│     │           │    ┃       │        │    │
│     │           └────┼────────────────┼────┘
│     │                ┃       │        │
│     │               ┌┴────────────────┴┐
│     │               │       集計       │
│     │               └┬────────────────┬┘
│     │                ┃       │        │
│     │                ┃─┐ 5. 要約      │
│     │                ┃◄┘     │        │
│     │                ┃       │        │
│     │     6. 報告    ┃       │        │
│     │(───────────────┃       │        │
│     │                ┃       │        │
│┌────┴───┐    ┌───────┴──────┐│   ┌────┴───┐
││ 利用者 │    │ エージェント ││   │ ツール │
│└────────┘    └──────────────┘│   └────────┘
└──────────────────────────────┘`
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// The gate: what may cover what.
func TestTextArtSequenceGate(t *testing.T) {
	g, _ := newGrid(4, 1)
	a := &seqArt{g: g, tm: &textMeasure{width: eastAsianWidth}, kind: [][]seqCell{make([]seqCell, 4)}}
	a.kind[0][0], a.kind[0][1], a.kind[0][2] = scLifeline, scFrame, scGroup
	a.put(0, 0, '─', scArrow) // an arrow crosses a lifeline
	a.put(2, 0, '─', scArrow) // and a group's side
	if len(a.fault) != 0 || g.cells[0][0].r != '┼' || g.cells[0][2].r != '┼' {
		t.Errorf("crossings: %v %q %q", a.fault, g.cells[0][0].r, g.cells[0][2].r)
	}
	a.put(1, 0, 'x', scText) // text over a block's frame
	a.put(0, 0, 'y', scBox)  // a box over an arrow
	if len(a.fault) != 2 {
		t.Errorf("faults %v, want two", a.fault)
	}
	// A frame's label that does not fit is a fault, not a clipped label.
	g2, _ := newGrid(8, 1)
	b := &seqArt{g: g2, tm: &textMeasure{width: eastAsianWidth}, kind: [][]seqCell{make([]seqCell, 8)}}
	b.frameRow(0, 7, 0, '┌', '┐', '─', "[loop] long")
	if len(b.fault) != 1 || !strings.Contains(b.fault[0], "does not fit") {
		t.Errorf("faults %v, want the label not fitting", b.fault)
	}
}

// Snapping is by identity: values equal up to rounding error land on one
// grid line, so a right angle never breaks at a half-cell tie (the case
// the specification's verification measured).
func TestSnapperIdentity(t *testing.T) {
	s := &snapper{scale: 2}
	s.add(3.7499999999999964)
	s.add(3.75)
	s.done()
	if a, b := s.at(3.7499999999999964), s.at(3.75); a != b {
		t.Errorf("columns %d and %d for one coordinate", a, b)
	}
}
