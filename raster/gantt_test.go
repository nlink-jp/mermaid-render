package raster

import (
	"errors"
	"flag"
	"fmt"
	"math/rand"
	"strings"
	"testing"

	mr "github.com/nlink-jp/mermaid-render"
)

func ganttOf(t *testing.T, src string) (*mr.Gantt, *ganttLayout) {
	t.Helper()
	d, err := mr.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v\n%s", err, src)
	}
	g := d.(*mr.Gantt)
	gl, err := layoutGantt(g, fakeMeasure)
	if err != nil {
		t.Fatalf("layout: %v\n%s", err, src)
	}
	return g, gl
}

func checkGantt(t *testing.T, name string, g *mr.Gantt, gl *ganttLayout) {
	t.Helper()
	for _, f := range ganttFaults(g, gl, fakeMeasure) {
		t.Errorf("%s: %s", name, f)
	}
}

var ganttCases = map[string]string{
	"the documentation's syntax example": `gantt
    dateFormat  YYYY-MM-DD
    title       Adding GANTT diagram functionality to mermaid
    excludes    weekends
    section A section
    Completed task            :done,    des1, 2014-01-06,2014-01-08
    Active task               :active,  des2, 2014-01-09, 3d
    Future task               :         des3, after des2, 5d
    section Critical tasks
    Completed task in the critical line :crit, done, 2014-01-06,24h
    Create tests for parser             :crit, active, 3d
    Add to mermaid                      :until isadded
    Functionality added                 :milestone, isadded, 2014-01-25, 0d`,
	"milestones and verts in time": `gantt
    dateFormat HH:mm
    axisFormat %H:%M
    Initial milestone : milestone, m1, 17:49, 2m
    Task A : 10m
    Initial vert : vert, v1, 17:30, 2m
    Close vert : vert, v2, 17:31, 2m
    Task B : 5m`,
	"a tall section title":              "gantt\n  dateFormat YYYY-MM-DD\n  section One<br>two<br>three<br>four\n  A :2024-01-01, 1d",
	"a repeated section":                "gantt\n  dateFormat YYYY-MM-DD\n  section A\n  a1 :2024-01-01, 1d\n  section B\n  b1 :2024-01-02, 1d\n  section A\n  a2 :2024-01-03, 1d",
	"all at one instant":                "gantt\n  dateFormat YYYY-MM-DD\n  M :milestone, m, 2024-01-01, 0d",
	"a daily tick interval over a year": "gantt\n  dateFormat YYYY-MM-DD\n  tickInterval 1day\n  A :2024-01-01, 1y",
	"weekly ticks from Monday":          "gantt\n  dateFormat YYYY-MM-DD\n  tickInterval 1week\n  weekday monday\n  A :2024-01-03, 30d",
}

func TestGanttLayoutCases(t *testing.T) {
	for name, src := range ganttCases {
		g, gl := ganttOf(t, src)
		checkGantt(t, name, g, gl)
	}
}

// randomGantt is a gantt chart of 1-25 tasks: sections, tags, durations
// across scales, after chains, excluded days, verts, texts short and long.
func randomGantt(seed int64) string {
	r := rand.New(rand.NewSource(seed))
	var b strings.Builder
	b.WriteString("gantt\n  dateFormat YYYY-MM-DD\n")
	if r.Intn(3) == 0 {
		b.WriteString("  excludes weekends\n")
	}
	if r.Intn(4) == 0 {
		fmt.Fprintf(&b, "  tickInterval %d%s\n", 1+r.Intn(3), []string{"day", "week", "month"}[r.Intn(3)])
	}
	if r.Intn(4) == 0 {
		b.WriteString("  axisFormat " + []string{"%d %b", "%a %e", "%m/%d", "%Y"}[r.Intn(4)] + "\n")
	}
	n := 1 + r.Intn(25)
	for i := range n {
		if r.Intn(5) == 0 {
			fmt.Fprintf(&b, "  section %s\n", []string{"Plan", "Build<br>phase", "Ship", "長い節の名前"}[r.Intn(4)])
		}
		var items []string
		for _, tag := range []string{"crit", "done", "active", "milestone", "vert"} {
			if r.Intn(9) == 0 {
				items = append(items, tag)
			}
		}
		start := fmt.Sprintf("2024-%02d-%02d", 1+r.Intn(6), 1+r.Intn(28))
		if i > 0 && r.Intn(3) == 0 {
			start = fmt.Sprintf("after t%d", r.Intn(i))
		}
		dur := []string{"1d", "3d", "2w", "12h", "1M", "0d", "5d"}[r.Intn(7)]
		items = append(items, fmt.Sprintf("t%d", i), start, dur)
		text := []string{"x", "Task", "A much longer task name that needs room", "設計", "Review & sign-off"}[r.Intn(5)]
		fmt.Fprintf(&b, "  %s %d :%s\n", text, i, strings.Join(items, ", "))
	}
	return b.String()
}

var ganttRandomN = flag.Int("ganttrandom", 400, "number of random gantt charts TestGanttLayoutRandom lays out")

func TestGanttLayoutRandom(t *testing.T) {
	laid := 0
	for seed := int64(1); seed <= int64(*ganttRandomN); seed++ {
		src := randomGantt(seed)
		d, err := mr.Parse(src)
		if err != nil {
			t.Fatalf("seed %d: parse: %v\n%s", seed, err, src)
		}
		g := d.(*mr.Gantt)
		gl, err := layoutGantt(g, fakeMeasure)
		if err != nil {
			var e *mr.Error
			if errors.As(err, &e) {
				continue // a limit
			}
			t.Fatalf("seed %d: %v\n%s", seed, err, src)
		}
		before := t.Failed()
		checkGantt(t, fmt.Sprintf("seed %d", seed), g, gl)
		if t.Failed() && !before {
			t.Logf("source of seed %d:\n%s", seed, src)
			return
		}
		laid++
	}
	if laid < *ganttRandomN*9/10 {
		t.Errorf("only %d of %d random gantt charts were laid out", laid, *ganttRandomN)
	}
}

// Seeds that exposed a defect: a milestone at the scale's edge outside the
// picture (341) and over a section title (1223).
func TestGanttLayoutRegressions(t *testing.T) {
	for _, seed := range []int64{341, 1223} {
		g, gl := ganttOf(t, randomGantt(seed))
		checkGantt(t, fmt.Sprintf("seed %d", seed), g, gl)
	}
}

// What is drawn is what the source says: each task in its row, its text
// inside or beside, each vert, each section title, the axis labels as
// axisFormat prints d3's ticks.
func TestGanttDrawn(t *testing.T) {
	fn := systemFont(t)
	d, err := mr.Parse(`gantt
  dateFormat YYYY-MM-DD
  axisFormat %m/%d
  section Plan<br>A
  Long enough to hold its own text inside :a, 2024-01-01, 20d
  Beside :b, after a, 1d
  Freeze : vert, v, 2024-01-10, 0d
  section Ship
  Go live :milestone, m, 2024-01-22, 0d`)
	if err != nil {
		t.Fatal(err)
	}
	var trace []string
	if _, err := render(d, Options{Font: fn}, probe{trace: func(s string) { trace = append(trace, s) }}); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(trace, "\n")
	for _, want := range []string{"task a row 0 inside true", "task b row 1 inside false", "task m row 2 inside false",
		"vert v", "section Plan|A", "section Ship", "tick 01/01", "tick 01/11", "tick 01/21"} {
		if !strings.Contains(got, want) {
			t.Errorf("trace lacks %q:\n%s", want, got)
		}
	}
}

// Every render checks a gantt chart's layout: a corrupted one is a layout
// fault.
func TestRenderRefusesGanttFaults(t *testing.T) {
	fn := systemFont(t)
	src := `gantt
  dateFormat YYYY-MM-DD
  section Plan
  Long enough to hold its own text inside :a, 2024-01-01, 20d
  Beside :b, after a, 1d
  Freeze : vert, v, 2024-01-10, 0d
  section Ship
  Go live :milestone, m, 2024-01-22, 0d`
	for name, corrupt := range map[string]func(*ganttLayout){
		"a bar off its time": func(gl *ganttLayout) { gl.bars[0].box.X0 += 1 },
		"a milestone off its time": func(gl *ganttLayout) {
			b := &gl.bars[2] // its text moves with it
			b.box.X0, b.box.X1, b.text.X0, b.text.X1 = b.box.X0-1, b.box.X1-1, b.text.X0-1, b.text.X1-1
		},
		"a bar lost": func(gl *ganttLayout) { gl.bars = gl.bars[1:] },
		"rows out of order": func(gl *ganttLayout) {
			// Listed and drawn in the wrong order, each bar at its own times.
			a, b := gl.bars[0], gl.bars[1]
			dy := b.box.Y0 - a.box.Y0
			a.box.Y0, a.box.Y1, a.text.Y0, a.text.Y1 = a.box.Y0+dy, a.box.Y1+dy, a.text.Y0+dy, a.text.Y1+dy
			b.box.Y0, b.box.Y1, b.text.Y0, b.text.Y1 = b.box.Y0-dy, b.box.Y1-dy, b.text.Y0-dy, b.text.Y1-dy
			gl.bars[0], gl.bars[1] = b, a
		},
		"text out of its bar":  func(gl *ganttLayout) { gl.bars[0].text.X1 = gl.bars[0].box.X1 + 1 },
		"text left of its bar": func(gl *ganttLayout) { gl.bars[1].text.X0 = gl.bars[1].box.X0 - 1 },
		"text too small":       func(gl *ganttLayout) { gl.bars[1].text.X1 = gl.bars[1].text.X0 + 0.1 },
		"an axis label over a bar": func(gl *ganttLayout) {
			// Over the part of b's row that holds only its bar.
			gl.ticks[0].box = gl.bars[1].box
		},
		"a section title off its run": func(gl *ganttLayout) {
			// Just past its run's end, above the next title.
			s := &gl.sections[0]
			h := s.text.H()
			s.text.Y1 = s.run.Y1 + 0.2
			s.text.Y0 = s.text.Y1 - h
		},
		"axis labels overlapping":     func(gl *ganttLayout) { gl.ticks[1].box = gl.ticks[0].box },
		"ticks out of order":          func(gl *ganttLayout) { gl.ticks[1].x = gl.ticks[0].x - 1 },
		"a marker off its time":       func(gl *ganttLayout) { gl.verts[0].x += 1 },
		"a label outside the picture": func(gl *ganttLayout) { gl.ticks[0].box.X0 = -5 },
	} {
		d, _ := mr.Parse(src)
		_, err := render(d, Options{Font: fn}, probe{corruptGantt: corrupt})
		var e *mr.Error
		if !errors.As(err, &e) || e.Kind != mr.LayoutFault {
			t.Errorf("%s: %v, want a layout fault", name, err)
		}
	}
}

// A milestone's diamond reaches half a bar past its time: at the scale's
// end it widens the picture, text or none. (A parsed task always has text;
// this one is built by hand.)
func TestGanttMilestoneAtTheEnd(t *testing.T) {
	// Labels of two characters, the last tick an hour before the end: no
	// label reaches past the diamond.
	const h = 3600000
	g := &mr.Gantt{AxisFormat: "%H", Tasks: []*mr.GanttTask{
		{ID: "a", Text: "A", Start: 0, End: 25 * h, Bar: 25 * h, Row: 0},
		{ID: "m", Milestone: true, Start: 25 * h, End: 25 * h, Bar: 25 * h, Row: 1},
	}}
	gl, err := layoutGantt(g, fakeMeasure)
	if err != nil {
		t.Fatal(err)
	}
	checkGantt(t, "a milestone with no text at the end", g, gl)
}
