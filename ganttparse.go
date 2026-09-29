package mermaidrender

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

// MaxGanttTasks bounds a gantt chart, as the flowchart's links.
const MaxGanttTasks = MaxLinks

// ganttStmt is a statement of gantt.jison's grammar that ganttDb keeps.
type ganttStmt struct {
	kind string // the token kind
	text string
	data string // a task's data
	line int
}

func parseGantt(lines []srcLine, front frontTitle) (*Gantt, error) {
	toks, err := lexGantt(lines)
	if err != nil {
		return nil, err
	}
	p := &stParser{toks: toks}
	if _, err := p.expect("gantt"); err != nil {
		return nil, err
	}
	var stmts []ganttStmt
	for {
		t := p.next()
		switch t.kind {
		case "NL":
			continue
		case "EOF":
			if p.peek().kind == "EOF" && p.pos == len(p.toks)-1 {
				return compileGantt(stmts, front)
			}
			continue
		case "topAxis":
			// gantt.jison calls yy.TopAxis(), which ganttDb does not have.
			return nil, errf(SyntaxError, t.line, "topAxis (mermaid 12.0.0 fails on it; a top axis is configuration)")
		case "dateFormat", "inclusiveEndDates", "axisFormat", "tickInterval", "excludes",
			"includes", "todayMarker", "title", "section",
			"weekday_monday", "weekday_tuesday", "weekday_wednesday", "weekday_thursday",
			"weekday_friday", "weekday_saturday", "weekday_sunday", "weekend_friday", "weekend_saturday":
			stmts = append(stmts, ganttStmt{kind: t.kind, text: t.text, line: t.line})
		case "acc_title":
			if _, err := p.expect("acc_title_value"); err != nil {
				return nil, err
			}
		case "acc_descr":
			if _, err := p.expect("acc_descr_value"); err != nil {
				return nil, err
			}
		case "acc_descr_multiline_value":
		case "click":
			if err := p.ganttClick(); err != nil {
				return nil, err
			}
		case "taskTxt":
			d, err := p.expect("taskData")
			if err != nil {
				return nil, err
			}
			stmts = append(stmts, ganttStmt{kind: "task", text: t.text, data: d.text, line: t.line})
		default:
			return nil, p.unexpected(t)
		}
	}
}

// ganttClick reads the rest of a click statement: callbackname with its
// args and an href, in either order (dropped: interaction).
func (p *stParser) ganttClick() error {
	switch p.peek().kind {
	case "callbackname":
		p.next()
		if p.peek().kind == "callbackargs" {
			p.next()
		}
		if p.peek().kind == "href" {
			p.next()
		}
	case "href":
		p.next()
		if p.peek().kind == "callbackname" {
			p.next()
			if p.peek().kind == "callbackargs" {
				p.next()
			}
		}
	default:
		return p.unexpected(p.peek())
	}
	return nil
}

// gtStart is a raw task's start as parseData records it.
type gtStart struct {
	prevEnd bool   // prevTaskEnd
	prevID  string // the previous task's id ("" for none)
	hasPrev bool
	data    string // getStartDate's input
}

type gtTask struct {
	id, text, section string
	line              int
	start             gtStart
	endData           string
	active, done      bool
	crit, milestone   bool
	vert              bool
	order             int
	// compiled
	startT, endT jsDate
	hasStart     bool
	hasEnd       bool
	renderEnd    jsDate
	hasRender    bool
	processed    bool
	manualEnd    bool
}

type ganttDB struct {
	dateFormat         string
	axisFormat         string
	tickInterval       string
	excludes, includes []string
	weekday            string
	weekend            string
	inclusive          bool
	tasks              []*gtTask
	byID               map[string]int
	today              bool // a date took its fields from today
	todayLine          int
}

var ganttTags = []string{"active", "done", "crit", "milestone", "vert"}

func compileGantt(stmts []ganttStmt, front frontTitle) (*Gantt, error) {
	db := &ganttDB{weekday: "sunday", weekend: "saturday", byID: map[string]int{}}
	g := &Gantt{title: front.text, titleLine: front.line}
	section := ""
	lastID, hasLast := "", false
	taskCnt, order := 0, 0
	for _, st := range stmts {
		switch st.kind {
		case "dateFormat":
			db.dateFormat = st.text[11:]
		case "inclusiveEndDates":
			db.inclusive = true
		case "axisFormat":
			db.axisFormat = st.text[11:]
		case "tickInterval":
			db.tickInterval = st.text[13:]
		case "excludes":
			db.excludes = mergeTokens(db.excludes, st.text[9:])
		case "includes":
			db.includes = mergeTokens(db.includes, st.text[9:])
		case "todayMarker":
			// The today marker depends on the day: never drawn.
		case "title":
			g.title, g.titleLine = svgText(decodeEntitiesOnly(restoreEntities(st.text[6:]))), st.line
		case "section":
			section = st.text[8:]
		case "task":
			if len(db.tasks) >= MaxGanttTasks {
				return nil, errf(UnsupportedConstruct, st.line, "more than %d gantt tasks", MaxGanttTasks)
			}
			t := &gtTask{text: st.text, section: section, line: st.line}
			data := strings.Split(st.data[1:], ",")
			// getTaskTags: while a tag is the first item, take it.
			for found := true; found; {
				found = false
				for _, tag := range ganttTags {
					if len(data) == 0 {
						return nil, errf(SyntaxError, st.line, "a task with tags and no data (mermaid fails on it)")
					}
					if regexp.MustCompile(`^[` + jsSpace + `]*` + tag + `[` + jsSpace + `]*$`).MatchString(data[0]) {
						switch tag {
						case "active":
							t.active = true
						case "done":
							t.done = true
						case "crit":
							t.crit = true
						case "milestone":
							t.milestone = true
						case "vert":
							t.vert = true
						}
						data = data[1:]
						found = true
					}
				}
			}
			for i := range data {
				data[i] = strings.Trim(data[i], jsSpaceChars)
			}
			autoID := func() string { taskCnt++; return "task" + strconv.Itoa(taskCnt) }
			switch len(data) {
			case 1:
				t.id = autoID()
				t.start = gtStart{prevEnd: true, prevID: lastID, hasPrev: hasLast}
				t.endData = data[0]
			case 2:
				t.id = autoID()
				t.start = gtStart{data: data[0]}
				t.endData = data[1]
			case 3:
				t.id = data[0]
				t.start = gtStart{data: data[1]}
				t.endData = data[2]
			default:
				return nil, errf(SyntaxError, st.line, "a task with more than three items (mermaid fails on it)")
			}
			if t.vert {
				t.order = -1
			} else {
				t.order = order
				order++
			}
			db.tasks = append(db.tasks, t)
			db.byID[t.id] = len(db.tasks) - 1
			lastID, hasLast = t.id, true
		default:
			if w, ok := strings.CutPrefix(st.kind, "weekday_"); ok {
				db.weekday = w
			} else if w, ok := strings.CutPrefix(st.kind, "weekend_"); ok {
				db.weekend = w
			}
		}
	}
	if err := db.compile(); err != nil {
		return nil, err
	}
	return db.finish(g)
}

var reTickInterval = regexp.MustCompile(`^([1-9]\d*)(millisecond|second|minute|hour|day|week|month)$`)

// timeOnlyAxis: d3-time-format directives that print no part of a date.
var reAxisDirective = regexp.MustCompile(`%[-_0]?(.)`)

func axisPrintsDate(f string) bool {
	for _, m := range reAxisDirective.FindAllStringSubmatch(f, -1) {
		if !strings.Contains("HIMSLfpX%", m[1]) {
			return true
		}
	}
	return false
}

// mergeTokens is ganttDb's: lower case, split at spaces and commas, the
// new ones appended once.
func mergeTokens(existing []string, txt string) []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range existing {
		if !seen[t] {
			seen[t], out = true, append(out, t)
		}
	}
	for _, t := range regexp.MustCompile(`[`+jsSpace+`,]+`).Split(strings.ToLower(txt), -1) {
		if t != "" && !seen[t] {
			seen[t], out = true, append(out, t)
		}
	}
	return out
}

// compile is ganttDb.getTasks: up to 11 passes of compileTasks, so that
// after may name a later task.
func (db *ganttDB) compile() error {
	all, err := db.compileTasks()
	for it := 0; err == nil && !all && it < 10; it++ {
		all, err = db.compileTasks()
	}
	return err
}

func (db *ganttDB) find(id string) *gtTask {
	if i, ok := db.byID[id]; ok {
		return db.tasks[i]
	}
	return nil
}

func (db *ganttDB) compileTasks() (bool, error) {
	all := true
	for _, t := range db.tasks {
		if t.start.prevEnd {
			key := t.start.prevID
			if !t.start.hasPrev {
				key = "undefined" // taskDb[undefined]
			}
			prev := db.find(key)
			if prev == nil {
				return false, errf(SyntaxError, t.line, "the first task has no start (mermaid fails on it)")
			}
			t.startT, t.hasStart = prev.endT, prev.hasEnd
		} else {
			st, ok, err := db.startDate(t.start.data, t.line)
			if err != nil {
				return false, err
			}
			if ok {
				t.startT, t.hasStart = st, true
			}
		}
		if t.hasStart {
			end, ok, err := db.endDate(t.startT, t.endData, t.line)
			if err != nil {
				return false, err
			}
			if ok {
				t.endT, t.hasEnd = end, true
				t.processed = true
				d, _, err := djParseStrict(t.endData, "YYYY-MM-DD")
				if err != nil {
					return false, err
				}
				t.manualEnd = d.valid
				if err := db.checkTaskDates(t); err != nil {
					return false, err
				}
			}
		}
		all = all && t.processed
	}
	return all, nil
}

var (
	reAfter    = regexp.MustCompile(`^after[` + jsSpace + `]+([\d\w\- ]+)`)
	reUntil    = regexp.MustCompile(`^until[` + jsSpace + `]+([\d\w\- ]+)`)
	reDigits   = regexp.MustCompile(`^\d+$`)
	reDuration = regexp.MustCompile(`^(\d+(?:\.\d+)?)([Mdhmswy]|ms)$`)
	// The ECMAScript date-time string format, which every browser's Date
	// reads alike.
	reISODate = regexp.MustCompile(`^([+-]\d{6}|\d{4})(?:-(\d{2})(?:-(\d{2}))?)?(?:T(\d{2}):(\d{2})(?::(\d{2})(?:\.(\d{1,3}))?)?(Z|[+-]\d{2}:\d{2})?)?$`)
)

// startDate is getStartDate: ok false when it names a task not placed yet.
func (db *ganttDB) startDate(str string, line int) (jsDate, bool, error) {
	str = strings.Trim(str, jsSpaceChars)
	f := strings.Trim(db.dateFormat, jsSpaceChars)
	if (f == "x" || f == "X") && reDigits.MatchString(str) {
		return jsTimeClip(jsNumberOf(str)), true, nil
	}
	if m := reAfter.FindStringSubmatch(str); m != nil {
		var latest *gtTask
		for _, id := range strings.Split(m[1], " ") {
			t := db.find(id)
			if t == nil {
				continue
			}
			if latest == nil || t.hasEnd && latest.hasEnd && t.endT.valid && latest.endT.valid && t.endT.ms > latest.endT.ms {
				latest = t
			}
		}
		if latest == nil {
			return jsInvalid, false, errf(UnsupportedConstruct, line, "after %s names no task (mermaid then starts at today)", strings.TrimSpace(m[1]))
		}
		return latest.endT, latest.hasEnd, nil
	}
	d, fill, err := djParseStrict(str, f)
	if err != nil {
		return jsInvalid, false, errf(SyntaxError, line, "dateFormat %q (mermaid fails on it)", f)
	}
	if d.valid {
		db.noteToday(fill, line)
		return d, true, nil
	}
	// mermaid hands the rest to the browser's Date.
	iso, ok := isoDate(str)
	if !ok {
		return jsInvalid, false, errf(UnsupportedConstruct, line, "the date %q does not match dateFormat %q (mermaid then reads it with the browser's own Date)", str, f)
	}
	return iso, true, nil
}

func (db *ganttDB) noteToday(fill djFill, line int) {
	if fill.any() && !db.today {
		db.today, db.todayLine = true, line
		if !(fill.year && fill.month && fill.day) {
			db.todayLine = -line // part of a date from today: always refused
		}
	}
}

// isoDate reads the ECMAScript date-time string format: date only is UTC,
// date and time without an offset local — UTC here as well.
func isoDate(s string) (jsDate, bool) {
	m := reISODate.FindStringSubmatch(s)
	if m == nil {
		return jsInvalid, false
	}
	y, _ := strconv.ParseInt(m[1], 10, 64)
	if m[1] == "-000000" {
		return jsInvalid, false
	}
	get := func(i int, def float64) float64 {
		if m[i] == "" {
			return def
		}
		v, _ := strconv.ParseFloat(m[i], 64)
		return v
	}
	mon, d := get(2, 1), get(3, 1)
	h, mi, sec := get(4, 0), get(5, 0), get(6, 0)
	ms := 0.0
	if m[7] != "" {
		ms = get(7, 0) * math.Pow(10, float64(3-len(m[7])))
	}
	if mon < 1 || mon > 12 || d < 1 || d > float64(daysInMonth(y, int(mon)-1)) || h > 24 || mi > 59 || sec > 59 ||
		h == 24 && (mi != 0 || sec != 0 || ms != 0) {
		return jsInvalid, false
	}
	days := float64(daysFromCivil(y, int(mon), int(d)))
	t := days*86400000 + h*3600000 + mi*60000 + sec*1000 + ms
	if z := m[8]; z != "" && z != "Z" {
		oh, _ := strconv.ParseFloat(z[1:3], 64)
		om, _ := strconv.ParseFloat(z[4:6], 64)
		off := (oh*60 + om) * 60000
		if z[0] == '+' {
			t -= off
		} else {
			t += off
		}
	}
	r := jsTimeClip(t)
	if !r.valid {
		return r, false
	}
	if yr := r.fields().y; yr < -10000 || yr > 10000 {
		return jsInvalid, false
	}
	return r, true
}

// endDate is getEndDate: ok false when until names a task not placed yet.
func (db *ganttDB) endDate(start jsDate, str string, line int) (jsDate, bool, error) {
	str = strings.Trim(str, jsSpaceChars)
	if m := reUntil.FindStringSubmatch(str); m != nil {
		var earliest *gtTask
		for _, id := range strings.Split(m[1], " ") {
			t := db.find(id)
			if t == nil {
				continue
			}
			if earliest == nil || t.hasStart && earliest.hasStart && t.startT.valid && earliest.startT.valid && t.startT.ms < earliest.startT.ms {
				earliest = t
			}
		}
		if earliest == nil {
			return jsInvalid, false, errf(UnsupportedConstruct, line, "until %s names no task (mermaid then ends at today)", strings.TrimSpace(m[1]))
		}
		return earliest.startT, earliest.hasStart, nil
	}
	f := strings.Trim(db.dateFormat, jsSpaceChars)
	d, fill, err := djParseStrict(str, f)
	if err != nil {
		return jsInvalid, false, errf(SyntaxError, line, "dateFormat %q (mermaid fails on it)", f)
	}
	if d.valid {
		db.noteToday(fill, line)
		if db.inclusive {
			d = djAdd(d, 1, "d")
		}
		return d, true, nil
	}
	end := start
	if m := reDuration.FindStringSubmatch(str); m != nil {
		v, _ := strconv.ParseFloat(m[1], 64)
		if n := djAdd(start, v, m[2]); n.valid {
			end = n
		}
	}
	return end, true, nil
}

// checkTaskDates lengthens a task over excluded days (not one whose end is
// written YYYY-MM-DD).
func (db *ganttDB) checkTaskDates(t *gtTask) error {
	if len(db.excludes) == 0 || t.manualEnd {
		return nil
	}
	start := djAdd(t.startT, 1, "d")
	end := t.endT
	maxEnd := djAdd(end, 10000, "d")
	invalid := false
	var render jsDate
	hasRender := false
	for start.valid && end.valid && start.ms <= end.ms {
		if !invalid {
			render, hasRender = end, true
		}
		bad, err := db.isInvalidDate(start)
		if err != nil {
			return err
		}
		invalid = bad
		if invalid {
			end = djAdd(end, 1, "d")
			if end.ms > maxEnd.ms {
				return errf(SyntaxError, t.line, "no day that excludes leaves (mermaid gives up after 10,000)")
			}
		}
		start = djAdd(start, 1, "d")
	}
	t.endT = end
	t.renderEnd, t.hasRender = render, hasRender
	return nil
}

var weekendStart = map[string]int{"friday": 5, "saturday": 6}

// isInvalidDate is ganttDb's: an excluded day that includes does not
// bring back.
func (db *ganttDB) isInvalidDate(d jsDate) (bool, error) {
	formatted, err := djFormat(d, strings.Trim(db.dateFormat, jsSpaceChars))
	if err != nil {
		return false, err
	}
	dateOnly, _ := djFormat(d, "YYYY-MM-DD")
	has := func(list []string, s string) bool {
		for _, v := range list {
			if v == s {
				return true
			}
		}
		return false
	}
	if has(db.includes, formatted) || has(db.includes, dateOnly) {
		return false, nil
	}
	f := d.fields()
	iso := f.weekday
	if iso == 0 {
		iso = 7
	}
	if has(db.excludes, "weekends") && (iso == weekendStart[db.weekend] || iso == weekendStart[db.weekend]+1) {
		return true, nil
	}
	if has(db.excludes, strings.ToLower(djWeekdays[f.weekday])) {
		return true, nil
	}
	return has(db.excludes, formatted) || has(db.excludes, dateOnly), nil
}

// finish checks what this engine can draw and builds the chart.
func (db *ganttDB) finish(g *Gantt) (*Gantt, error) {
	switch {
	case db.axisFormat != "":
		g.AxisFormat = db.axisFormat
	case db.dateFormat == "D":
		g.AxisFormat = "%d"
	default:
		g.AxisFormat = "%Y-%m-%d"
	}
	if m := reTickInterval.FindStringSubmatch(db.tickInterval); m != nil {
		g.TickEvery, _ = strconv.Atoi(m[1])
		g.TickUnit = m[2]
	}
	g.Weekday = db.weekday
	var lo, hi jsDate
	for _, t := range db.tasks {
		if !t.processed || !t.hasStart || !t.hasEnd {
			return nil, errf(UnsupportedConstruct, t.line, "task %q is never placed (after or until in a cycle)", strings.TrimSpace(t.text))
		}
		if !t.startT.valid || !t.endT.valid {
			return nil, errf(UnsupportedConstruct, t.line, "task %q has no valid date", strings.TrimSpace(t.text))
		}
		if !lo.valid || t.startT.ms < lo.ms {
			lo = t.startT
		}
		if !hi.valid || t.endT.ms > hi.ms {
			hi = t.endT
		}
		bar := t.endT
		if t.hasRender && t.renderEnd.valid {
			bar = t.renderEnd
		}
		row := t.order
		g.Tasks = append(g.Tasks, &GanttTask{
			ID: t.id, Text: svgText(decodeEntitiesOnly(restoreEntities(t.text))),
			Section: t.section, SectionTitle: sectionTitle(t.section),
			Start: t.startT.ms, End: t.endT.ms, Bar: bar.ms,
			Active: t.active, Done: t.done, Crit: t.crit, Milestone: t.milestone, Vert: t.vert,
			Row: row, Line: t.line,
		})
	}
	if db.today {
		line := db.todayLine
		if line < 0 {
			return nil, errf(UnsupportedConstruct, -line, "dateFormat %q has no year: mermaid takes it from today", db.dateFormat)
		}
		// A format of times alone: the date is today's in mermaid, which
		// must not show — not on the axis, not through excluded days or week
		// and month ticks. Two days at most also rules out a month's length:
		// a duration in months or years spans more.
		switch {
		case len(db.excludes) > 0 || len(db.includes) > 0:
			return nil, errf(UnsupportedConstruct, line, "dateFormat %q has no date, and excluded days depend on it", db.dateFormat)
		case axisPrintsDate(g.AxisFormat):
			return nil, errf(UnsupportedConstruct, line, "dateFormat %q has no date, and the axis %q would print today's", db.dateFormat, g.AxisFormat)
		case g.TickUnit == "week" || g.TickUnit == "month" || hi.ms-lo.ms >= 2*86400000:
			return nil, errf(UnsupportedConstruct, line, "dateFormat %q has no date, and week or month ticks depend on it", db.dateFormat)
		}
	}
	if len(g.Tasks) > 0 && (len(db.excludes) > 0 || len(db.includes) > 0) && djDiffYears(hi, lo) <= 5 {
		var run []int64
		flush := func() {
			if run != nil {
				g.Excluded = append(g.Excluded, run[0], run[1])
				run = nil
			}
		}
		for d := lo; d.valid && d.ms <= hi.ms; d = djAdd(d, 1, "d") {
			bad, err := db.isInvalidDate(d)
			if err != nil {
				return nil, err
			}
			day := floorDiv(d.ms, 86400000) * 86400000
			switch {
			case !bad:
				flush()
			case run == nil:
				run = []int64{day, day + 86400000 - 1}
			default:
				run[1] = day + 86400000 - 1
			}
		}
		// mermaid drops a run still open at the last day; it is shaded here.
		flush()
	}
	return g, nil
}

// sectionTitle is a section name as vertLabels draws it: split at <br>
// into tspans, each shown as SVG text shows it.
func sectionTitle(name string) string {
	lines := reBreak.Split(restoreEntities(name), -1)
	for i, l := range lines {
		lines[i] = svgText(decodeEntitiesOnly(l))
	}
	return strings.Trim(strings.Join(lines, "\n"), "\n")
}

// djDiffYears is dayjs's max.diff(min, 'year'): whole years, by months.
func djDiffYears(a, b jsDate) int64 {
	m := djMonthDiff(a, b)
	return int64(m / 12) // absFloor of a truncation toward zero
}

func djMonthDiff(a, b jsDate) float64 {
	fa, fb := a.fields(), b.fields()
	if fa.d < fb.d {
		return -djMonthDiff(b, a)
	}
	whole := (fb.y-fa.y)*12 + int64(fb.mon-fa.mon)
	anchor := djAdd(a, float64(whole), "M")
	c := b.ms-anchor.ms < 0
	step := 1.0
	if c {
		step = -1
	}
	anchor2 := djAdd(a, float64(whole)+step, "M")
	var den float64
	if c {
		den = float64(anchor.ms - anchor2.ms)
	} else {
		den = float64(anchor2.ms - anchor.ms)
	}
	v := -(float64(whole) + float64(b.ms-anchor.ms)/den)
	if math.IsNaN(v) {
		return 0
	}
	return v
}
