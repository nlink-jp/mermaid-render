package mermaidrender

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// dumpGantt prints a gantt chart's settings and tasks, times in UTC.
func dumpGantt(g *Gantt) string {
	var b strings.Builder
	if g.Title() != "" {
		fmt.Fprintf(&b, "title %q\n", g.Title())
	}
	fmt.Fprintf(&b, "axis %q", g.AxisFormat)
	if g.TickEvery > 0 {
		fmt.Fprintf(&b, " every %d %s", g.TickEvery, g.TickUnit)
	}
	b.WriteString("\n")
	day := func(ms int64) string {
		t := time.UnixMilli(ms).UTC()
		if t.Hour() == 0 && t.Minute() == 0 && t.Second() == 0 && t.Nanosecond() == 0 {
			return t.Format("2006-01-02")
		}
		return t.Format("2006-01-02T15:04:05.000")
	}
	for k := 0; k+1 < len(g.Excluded); k += 2 {
		fmt.Fprintf(&b, "excluded %s..%s\n", day(g.Excluded[k]), day(g.Excluded[k+1]+1))
	}
	for _, t := range g.Tasks {
		fmt.Fprintf(&b, "%q %q [%s] %s..%s", t.Section, t.Text, t.ID, day(t.Start), day(t.End))
		if t.Bar != t.End {
			fmt.Fprintf(&b, " bar..%s", day(t.Bar))
		}
		for _, kv := range []struct {
			on bool
			k  string
		}{{t.Active, "active"}, {t.Done, "done"}, {t.Crit, "crit"}, {t.Milestone, "milestone"}, {t.Vert, "vert"}} {
			if kv.on {
				b.WriteString(" " + kv.k)
			}
		}
		fmt.Fprintf(&b, " row %d\n", t.Row)
	}
	return b.String()
}

func mustGantt(t *testing.T, src string) *Gantt {
	t.Helper()
	d, err := Parse(src)
	if err != nil {
		t.Fatalf("%v\n%s", err, src)
	}
	g, ok := d.(*Gantt)
	if !ok {
		t.Fatalf("%T, want *Gantt", d)
	}
	return g
}

// Read and placed as gantt.jison, ganttDb and dayjs place them (mermaid
// 12.0.0, a browser in UTC). Expected values were checked against the real
// ganttDb under node.
func TestParseGantt(t *testing.T) {
	for _, c := range []struct{ name, src, want string }{
		{"the documentation's first example", `gantt
    title A Gantt Diagram
    dateFormat YYYY-MM-DD
    section Section
        A task          :a1, 2014-01-01, 30d
        Another task    :after a1, 20d
    section Another
        Task in Another :2014-01-12, 12d
        another task    :24d`, `title "A Gantt Diagram"
axis "%Y-%m-%d"
"Section" "A task" [a1] 2014-01-01..2014-01-31 row 0
"Section" "Another task" [task1] 2014-01-31..2014-02-20 row 1
"Another" "Task in Another" [task2] 2014-01-12..2014-01-24 row 2
"Another" "another task" [task3] 2014-01-24..2014-02-17 row 3
`},
		{"after takes the first named until the others are placed", `gantt
  dateFormat YYYY-MM-DD
  A :a, 2024-01-01, 1d
  C :c, after a b, 1d
  B :b, 2024-02-01, 1d
  D :d, after b a, 1d`, `axis "%Y-%m-%d"
"" "A" [a] 2024-01-01..2024-01-02 row 0
"" "C" [c] 2024-01-02..2024-01-03 row 1
"" "B" [b] 2024-02-01..2024-02-02 row 2
"" "D" [d] 2024-02-02..2024-02-03 row 3
`},
		{"after keeps a first-named task not placed yet, and waits for it", `gantt
  dateFormat YYYY-MM-DD
  A :a, 2024-01-01, 1d
  E :e, after b a, 1d
  B :b, 2024-02-01, 1d`, `axis "%Y-%m-%d"
"" "A" [a] 2024-01-01..2024-01-02 row 0
"" "E" [e] 2024-02-02..2024-02-03 row 1
"" "B" [b] 2024-02-01..2024-02-02 row 2
`},
		{"weekend friday: Friday and Saturday are the weekend", "gantt\n  dateFormat YYYY-MM-DD\n  excludes weekends\n  weekend friday\n  A :a, 2024-01-04, 2d", `axis "%Y-%m-%d"
excluded 2024-01-05..2024-01-07
"" "A" [a] 2024-01-04..2024-01-08 row 0
`},
		{"excluded weekends lengthen a task; the bar stops before", `gantt
  dateFormat YYYY-MM-DD
  excludes weekends
  A :a, 2024-01-05, 3d
  B :b, 2024-01-06, 1d
  C :c, 2024-01-05, 2024-01-10`, `axis "%Y-%m-%d"
excluded 2024-01-06..2024-01-08
"" "A" [a] 2024-01-05..2024-01-10 row 0
"" "B" [b] 2024-01-06..2024-01-08 bar..2024-01-07 row 1
"" "C" [c] 2024-01-05..2024-01-10 row 2
`},
		{"durations: decimals, months clamped, invalid is zero", `gantt
  dateFormat YYYY-MM-DD
  A :a, 2024-01-31, 1M
  B :b, 2024-01-01, 1.5d
  C :c, 2024-01-01, 1.5w
  D :d, 2024-01-01, 3dX`, `axis "%Y-%m-%d"
"" "A" [a] 2024-01-31..2024-02-29 row 0
"" "B" [b] 2024-01-01..2024-01-03 row 1
"" "C" [c] 2024-01-01..2024-01-12 row 2
"" "D" [d] 2024-01-01..2024-01-01 row 3
`},
		{"no dateFormat: starts by the browser's Date (ISO), end dates not read", "gantt\n  A :a, 2014-01-01, 2014-01-05\n  B :b, 2014-01-01, 2d",
			`axis "%Y-%m-%d"
"" "A" [a] 2014-01-01..2014-01-01 row 0
"" "B" [b] 2014-01-01..2014-01-03 row 1
`},
		{"a time-only format", `gantt
    dateFormat HH:mm
    axisFormat %H:%M
    Initial milestone : milestone, m1, 17:49, 2m
    Task A : 10m
    Initial vert : vert, v1, 17:30, 2m
    Task B : 5m`, `axis "%H:%M"
"" "Initial milestone" [m1] 2000-01-01T17:49:00.000..2000-01-01T17:51:00.000 milestone row 0
"" "Task A" [task1] 2000-01-01T17:51:00.000..2000-01-01T18:01:00.000 row 1
"" "Initial vert" [v1] 2000-01-01T17:30:00.000..2000-01-01T17:32:00.000 vert row -1
"" "Task B" [task2] 2000-01-01T17:32:00.000..2000-01-01T17:37:00.000 row 2
`},
		{"tags, a tick interval, inclusive end dates, the D axis", `gantt
  dateFormat D
  tickInterval 1week
  weekday monday
  inclusiveEndDates
  A :crit, done, a, 1, 3`, "", /* checked below */
		},
		{"formats dayjs reads", "gantt\n  dateFormat Do MMM YYYY\n  A :a, 1st Jan 2014, 3d", `axis "%Y-%m-%d"
"" "A" [a] 2014-01-01..2014-01-04 row 0
`},
		{"keywords, comments, entities", "gantt\n  title Plan #amp; do\n  %% c\n  Topaxis2 x : 2024-01-01, 1d\n  Fish #amp; chips :2024-01-01,1d\n  5% done", `title "Plan & do"
axis "%Y-%m-%d"
"" "Topaxis2 x" [task1] 2024-01-01..2024-01-02 row 0
"" "Fish & chips" [task2] 2024-01-01..2024-01-02 row 1
`},
		{"a section name broken at <br>", "gantt\n  dateFormat YYYY-MM-DD\n  section Phase<br>one\n  A :2024-01-01, 1d", `axis "%Y-%m-%d"
"Phase<br>one" "A" [task1] 2024-01-01..2024-01-02 row 0
`},
		{"a duplicate id is the last", "gantt\n  dateFormat YYYY-MM-DD\n  A :x, 2024-01-01, 1d\n  B :x, 2024-03-01, 5d\n  C :after x, 1d", `axis "%Y-%m-%d"
"" "A" [x] 2024-01-01..2024-01-02 row 0
"" "B" [x] 2024-03-01..2024-03-06 row 1
"" "C" [task1] 2024-03-06..2024-03-07 row 2
`},
	} {
		if c.want == "" {
			continue
		}
		g := mustGantt(t, c.src)
		if got := dumpGantt(g); got != c.want {
			t.Errorf("%s:\n got\n%s\n want\n%s", c.name, got, c.want)
		}
	}
	g := mustGantt(t, "gantt\n  dateFormat YYYY-MM-DD\n  tickInterval 1week\n  weekday monday\n  inclusiveEndDates\n  A :crit, done, a, 2024-01-01, 2024-01-03")
	if got := dumpGantt(g); got != `axis "%Y-%m-%d" every 1 week
"" "A" [a] 2024-01-01..2024-01-04 done crit row 0
` || g.Weekday != "monday" {
		t.Errorf("tick interval, inclusive:\n%s weekday %s", got, g.Weekday)
	}
	// dateFormat D: the axis prints the day (renderer); starts in ISO go to
	// the browser's Date, so nothing comes from today.
	if g := mustGantt(t, "gantt\n  dateFormat D\n  A :a, 2024-01-01, 1d"); g.AxisFormat != "%d" {
		t.Errorf("axis %q", g.AxisFormat)
	}
	// Only an end written YYYY-MM-DD, literally, escapes excluded days.
	g2 := mustGantt(t, "gantt\n  dateFormat DD/MM/YYYY\n  excludes weekends\n  A :a, 05/01/2024, 10/01/2024\n  B :b, 05/01/2024, 2024-01-10")
	if got := dumpGantt(g2); got != `axis "%Y-%m-%d"
excluded 2024-01-06..2024-01-08
"" "A" [a] 2024-01-05..2024-01-12 row 0
"" "B" [b] 2024-01-05..2024-01-05 row 1
` {
		t.Errorf("a written end:\n%s", got)
	}
	// A run of excluded days still open at the last day is shaded (mermaid
	// drops it).
	g3 := mustGantt(t, "gantt\n  dateFormat YYYY-MM-DD\n  excludes weekends\n  A :a, 2024-01-05, 2024-01-07")
	if len(g3.Excluded) != 2 {
		t.Errorf("excluded %v", g3.Excluded)
	}
	if g := mustGantt(t, "gantt\n  dateFormat YYYY-MM-DD\n  tickInterval 1day \n  A :a, 2024-01-01, 1d"); g.TickEvery != 0 {
		t.Errorf("a tick interval with a trailing space was read: %d", g.TickEvery)
	}
	if g := mustGantt(t, "gantt\n  dateFormat YYYY-MM-DD\n  section X\n  A :a, 2024-01-01, 1d"); g.Tasks[0].SectionTitle != "X" {
		t.Errorf("section title %q", g.Tasks[0].SectionTitle)
	}
	if g := mustGantt(t, "gantt\n  dateFormat YYYY-MM-DD\n  section A<br>#amp; B\n  A :a, 2024-01-01, 1d"); g.Tasks[0].SectionTitle != "A\n& B" {
		t.Errorf("section title %q", g.Tasks[0].SectionTitle)
	}
}

func TestParseGanttErrors(t *testing.T) {
	for _, c := range []struct {
		name, src string
		kind      ErrorKind
		line      int
	}{
		{"after naming no task", "gantt\n  dateFormat YYYY-MM-DD\n  A :a, after zz, 1d", UnsupportedConstruct, 3},
		{"until naming no task", "gantt\n  dateFormat YYYY-MM-DD\n  A :a, 2024-01-01, until zz", UnsupportedConstruct, 3},
		{"a start the browser would read", "gantt\n  dateFormat YYYY-MM-DD\n  A :a, 01/02/2014, 3d", UnsupportedConstruct, 3},
		{"a cycle", "gantt\n  dateFormat YYYY-MM-DD\n  A :a, after b, 1d\n  B :b, after a, 1d", UnsupportedConstruct, 3},
		{"a first task with no start", "gantt\n  A : 3d", SyntaxError, 2},
		{"tags alone", "gantt\n  dateFormat YYYY-MM-DD\n  A :crit", SyntaxError, 3},
		{"four items", "gantt\n  dateFormat YYYY-MM-DD\n  A :a, b, 2024-01-01, 1d", SyntaxError, 3},
		{"topAxis", "gantt\n  topAxis\n  A :a, 2024-01-01, 1d", SyntaxError, 2},
		{"a line starting with a date", "gantt\n  2024-01-01 kickoff : 1d", SyntaxError, 2},
		{"call at a line's start", "gantt\n  call mom : 1d", SyntaxError, 2},
		{"a year-less date format", "gantt\n  dateFormat MM-DD\n  A :a, 01-05, 1d", UnsupportedConstruct, 3},
		{"times alone with a date on the axis", "gantt\n  dateFormat HH:mm\n  A :a, 10:00, 1h", UnsupportedConstruct, 3},
		{"times alone with excluded days", "gantt\n  dateFormat HH:mm\n  axisFormat %H:%M\n  excludes weekends\n  A :a, 10:00, 1h", UnsupportedConstruct, 5},
		{"times alone over two days", "gantt\n  dateFormat HH:mm\n  axisFormat %H:%M\n  A :a, 10:00, 3d", UnsupportedConstruct, 4},
		{"times alone with a month duration", "gantt\n  dateFormat HH:mm\n  axisFormat %H:%M\n  A :a, 10:00, 1M", UnsupportedConstruct, 4},
		{"a year-less format, even with times alone on the axis", "gantt\n  dateFormat MM-DD HH:mm\n  axisFormat %H:%M\n  A :a, 01-05 10:00, 1h", UnsupportedConstruct, 4},
		{"times alone with week ticks", "gantt\n  dateFormat HH:mm\n  axisFormat %H:%M\n  tickInterval 1week\n  A :a, 10:00, 1h", UnsupportedConstruct, 5},
		{"a week token dayjs throws on (week 0 formats back)", "gantt\n  dateFormat YYYY ww\n  A :a, 2024 00, 1d", SyntaxError, 3},
		{"a week token read by the browser", "gantt\n  dateFormat YYYY ww\n  A :a, 2024 03, 1d", UnsupportedConstruct, 3},
	} {
		_, err := Parse(c.src)
		var e *Error
		if !errors.As(err, &e) || e.Kind != c.kind || e.Line != c.line {
			t.Errorf("%s: %v, want %s at line %d", c.name, err, c.kind, c.line)
		}
	}
}
