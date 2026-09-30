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
	for label, why := range map[string]string{
		"a\x1b[31mred": "control character", "tab\there": "control character", "del\x7f": "control character",
		"c1\u0085": "control character", "line\u2028sep": "control character", "para\u2029sep": "control character",
		"a\u202eb": "bidi control", "a\u2066b": "bidi control",
		"e\u0301": "joins", "👨\u200d👩": "joins", "🇯🇵": "joins", "👍🏽": "joins", "no\ufe0f": "joins",
		"कि\u093e": "joins", "\u1100\u1161\u11a8": "joins", "\u0e01\u0e33": "joins", "\u0e81\u0eb3": "joins",
	} {
		f := &mr.Flowchart{Nodes: []*mr.Node{{ID: "a", Label: label, Line: 2}}}
		_, err := RenderText(f, TextOptions{})
		var e *mr.Error
		if !errors.As(err, &e) || e.Kind != mr.UnsupportedConstruct || !strings.Contains(e.Msg, why) {
			t.Errorf("%q: %v, want unsupported: %s", label, err, why)
		}
	}
	// Precomposed Hangul, Thai without SARA AM, and ambiguous characters
	// are one cell or two, and draw.
	for label, cells := range map[string]int{"한국": 4, "\u0e01\u0e32": 2, "α±→": 3} {
		tm := &textMeasure{width: eastAsianWidth}
		if err := tm.check(label, 1); err != nil || tm.cells(label) != cells {
			t.Errorf("%q: %v, %d cells, want %d", label, err, tm.cells(label), cells)
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
		empty := len(d.(*mr.Sequence).Participants) == 0 && len(d.(*mr.Sequence).Events) > 0
		if err != nil && !(errors.As(err, &e) && (e.Kind == mr.LayoutFault || empty && e.Kind == mr.UnsupportedConstruct)) {
			t.Fatalf("seed %d: %v", seed, err)
		}
		if err != nil {
			continue
		}
		if empty || art == "" && len(d.(*mr.Sequence).Events) > 0 {
			t.Fatalf("seed %d: blocks with no participant drew %q", seed, art)
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
│     │───────────────►┃       │        │
│     │                ┃       │        │
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
│     │                │       │        │
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

// A labelled link bends twice where two bends reach its target, the label
// on the straight run (the operator's check, round 1: Reject and No bent
// four times).
func TestTextArtStraighten(t *testing.T) {
	for id, label := range map[string]string{"beebfdda4d": "Reject", "f3f8dd01f6": "No"} {
		ms, _ := filepath.Glob("../testdata/real/*/" + id + ".mmd")
		b, _ := os.ReadFile(ms[0])
		d, _ := mr.Parse(string(b))
		f := d.(*mr.Flowchart)
		tm := &textMeasure{width: eastAsianWidth}
		sizes := make([][2]float64, len(f.Nodes))
		for i, n := range f.Nodes {
			w, h := tm.size(n.Label)
			cols := w + 4
			cols += cols % 2
			sizes[i] = [2]float64{float64(cols) / 2, float64(h + 2)}
		}
		measure := func(text string, bold bool) (float64, float64, error) {
			if text == "" {
				return 0, 0, nil
			}
			w, h := tm.size(text)
			return float64(w+w%2) / 2, float64(h), nil
		}
		lay, err := layoutGraph(f, measure, &layouter{sp: &gridSpacing, boxes: true, sizes: sizes,
			portGap: gridPortGap, rankGap: gridRankGap, endRoom: gridEndRoom, labelRoom: gridSpacing.trackIn})
		if err != nil {
			t.Fatal(err)
		}
		tf := flowGrid(f, lay, tm)
		for i, lk := range f.Links {
			if lk.Label == label && len(tf.paths[i]) > 4 {
				t.Errorf("%s: %s bends %d times", id, label, len(tf.paths[i])-2)
			}
		}
		// A leaf moves so that its one link runs straight (round 3: Archive).
		for i, lk := range f.Links {
			if lk.Label == "Archive" && len(tf.paths[i]) != 2 {
				t.Errorf("%s: Archive bends %d times", id, len(tf.paths[i])-2)
			}
		}
		if _, err := RenderText(d, TextOptions{}); err != nil {
			t.Errorf("%s: %v", id, err)
		}
	}
}

// A label stands on its own line: both resolves_to labels break their
// lines (the operator's check, round 2: one stood a row above its line).
func TestTextArtLabelOnLine(t *testing.T) {
	ms, _ := filepath.Glob("../testdata/real/*/0a1fd38f5f.mmd")
	b, _ := os.ReadFile(ms[0])
	art, err := RenderTextSource(string(b), TextOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(art, "─resolves_to─"); n != 2 {
		t.Errorf("%d resolves_to labels on their lines, want 2:\n%s", n, art)
	}
}

// A frame whose title the links crossing it leave no room for widens into
// free columns beside it, a cell from each side (the field flowchart
// gem-agent's box art drew before this engine).
func TestTextArtTitleWidensItsFrame(t *testing.T) {
	src := "flowchart TD\n    Start([Investigation Target]) --> CheckType{Target Type?}\n    subgraph Domain_Flow[Domain Attribution]\n        CheckType -->|Domain / FQDN| D1[WHOIS / RDAP Lookup]\n        D1 --> D2[DNS / DoH Resolution]\n    end\n    subgraph IP_Flow[IP Attribution]\n        CheckType -->|IP / CIDR| I1[ASN & GeoIP Lookup]\n        I1 --> I2[Tor / Relay Check]\n    end\n    D2 --> R[Report]\n    I2 --> R\n"
	art, err := RenderTextSource(src, TextOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(art, "║ IP Attribution │") {
		t.Errorf("title not a cell clear of the frame and the link:\n%s", art)
	}
}

// titleRoom widens by the fewest columns, on the side that is free (the
// right when both are) a cell clear of what is beside it, on the grid,
// not past a frame around it, and not at all when the title already has
// room. The blockers stand beside the frame's lower rows, clear of the
// title's own.
func TestTitleRoom(t *testing.T) {
	f := &mr.Flowchart{Subgraphs: []*mr.Subgraph{{ID: "s", Title: "abcdef"}, {ID: "p"}}}
	tm := &textMeasure{width: eastAsianWidth}
	wide, fr := iRect{-10, -2, 90, 11}, iRect{10, 1, 20, 8}
	for _, c := range []struct {
		name  string
		frame iRect
		link  int
		boxes []iRect
		label iRect
		outer iRect
		want  iRect
	}{
		{"both free", fr, 15, nil, iRect{}, wide, iRect{10, 1, 24, 8}},
		{"left blocked", fr, 15, []iRect{{2, 6, 9, 8}}, iRect{}, wide, iRect{10, 1, 24, 8}},
		{"right blocked", fr, 15, []iRect{{21, 6, 30, 8}}, iRect{}, wide, iRect{6, 1, 20, 8}},
		{"right blocked by a label", fr, 15, nil, iRect{21, 6, 30, 8}, wide, iRect{6, 1, 20, 8}},
		{"right would abut", fr, 15, []iRect{{25, 6, 30, 8}}, iRect{}, wide, iRect{6, 1, 20, 8}},
		{"left would abut", fr, 15, []iRect{{1, 6, 5, 8}, {21, 6, 30, 8}}, iRect{}, wide, fr},
		{"both blocked", fr, 15, []iRect{{2, 6, 9, 8}, {21, 6, 30, 8}}, iRect{}, wide, fr},
		{"the frame around", fr, 15, []iRect{{2, 6, 9, 8}}, iRect{}, iRect{-10, -2, 24, 11}, fr},
		{"the grid's edge", iRect{3, 1, 13, 8}, 8, []iRect{{14, 6, 30, 8}}, iRect{}, wide, iRect{3, 1, 13, 8}},
		{"room already", fr, 18, nil, iRect{}, wide, fr},
	} {
		tf := &textFlow{
			boxes:  c.boxes,
			frames: []iRect{c.frame, c.outer},
			titles: []iRect{{c.frame.x0 + 1, 2, c.frame.x1 - 1, 2}, {}},
			paths:  [][][2]int{{{c.link, 0}, {c.link, 9}}},
			labels: []iRect{c.label},
			w:      31, h: 10,
		}
		titleRoom(f, tf, tm)
		if tf.frames[0] != c.want || tf.w <= tf.frames[0].x1 {
			t.Errorf("%s: frame %v in width %d, want %v", c.name, tf.frames[0], tf.w, c.want)
		}
	}
}

// A message to self keeps its arrowheads: none for ->, one back into the
// lifeline for ->>, both ends for <<->>, a cross for -x.
func TestTextArtSelfMessageHeads(t *testing.T) {
	got, err := RenderTextSource("sequenceDiagram\n A->A: plain\n A-->A: dotted\n A->>A: head\n A<<->>A: both\n A-xA: cross", TextOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"│─┐ plain\n   │─┘", "│┈┐ dotted\n   │┈┘", "│─┐ head\n   │◄┘", "│◄┐ both\n   │◄┘", "│─┐ cross\n   │×┘"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
}

// Blocks with no participant have no columns: refused, never empty art.
func TestTextArtBlocksWithNoParticipant(t *testing.T) {
	art, err := RenderTextSource("sequenceDiagram\nloop x\ncritical y\noption z\nend\nend", TextOptions{})
	var e *mr.Error
	if !errors.As(err, &e) || e.Kind != mr.UnsupportedConstruct || art != "" {
		t.Errorf("got %q, %v", art, err)
	}
	if art, err := RenderTextSource("sequenceDiagram", TextOptions{}); err != nil || art != "" {
		t.Errorf("an empty diagram: %q, %v", art, err)
	}
}

// A group of one participant widens past its box to hold its title, and
// keeps the next participant clear of it.
func TestTextArtGroupOfOne(t *testing.T) {
	for _, src := range []string{
		"sequenceDiagram\n box A long group title\n participant A\n end\n A->>B: x",
		"sequenceDiagram\n participant A\n box B long group title\n participant B\n end\n A->>B: x",
		"sequenceDiagram\n participant A\n box B long group title\n participant B\n end\n participant C\n A->>C: x",
	} {
		art, err := RenderTextSource(src, TextOptions{})
		if err != nil || !strings.Contains(art, "─B long group title─┐") && !strings.Contains(art, "─A long group title─┐") {
			t.Errorf("%q: %v\n%s", src, err, art)
		}
	}
}

// Block labels are measured with the caller's widths, brackets included.
func TestTextArtBlockLabelWidths(t *testing.T) {
	two := TextOptions{Width: func(rune) int { return 2 }}
	art, err := RenderTextSource("sequenceDiagram\n A->>B: x\n alt yes\n B->>A: y\n else no\n A->>B: z\n end", two)
	if err != nil || !strings.Contains(art, "[alt] yes") || !strings.Contains(art, "[else] no") {
		t.Errorf("%v\n%s", err, art)
	}
}

// An activation a message opens or closes starts or ends at its arrow, as
// in the picture.
func TestTextArtActivationAtTheArrow(t *testing.T) {
	art, err := RenderTextSource("sequenceDiagram\n A->>+B: go\n B-->>-A: done\n A->>B: again", TextOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"   │   go   │\n   │───────►┃\n", "   │  done  ┃\n   │◄┈┈┈┈┈┈┈┃\n   │        │\n"} {
		if !strings.Contains(art, want) {
			t.Errorf("missing %q in\n%s", want, art)
		}
	}
}

// Two relationships of an entity to itself loop over rows of their own.
func TestTextArtTwoSelfRelationships(t *testing.T) {
	art, err := RenderTextSource("erDiagram\n A ||--o{ A : parent\n A }o--|| A : child", TextOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"│}o──┐\n", "│||──┘ child\n", "│||──┐\n", "│}o──┘ parent\n"} {
		if !strings.Contains(art, want) {
			t.Errorf("missing %q in\n%s", want, art)
		}
	}
}

// A refusal names the same fault every run.
func TestTextArtFaultIsDeterministic(t *testing.T) {
	src := "erDiagram\n A ||--o{ A : r\n A ||--|| A : s\n A }o--o{ A : t\n A ||--o{ B : u"
	_, first := RenderTextSource(src, TextOptions{})
	if first == nil {
		t.Fatal("the source draws now: the property needs one that is refused")
	}
	for k := 0; k < 50; k++ {
		if _, err := RenderTextSource(src, TextOptions{}); err == nil || err.Error() != first.Error() {
			t.Fatalf("run %d: %v, first %v", k, err, first)
		}
	}
}
