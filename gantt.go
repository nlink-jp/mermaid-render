package mermaidrender

// Gantt is a parsed gantt chart, its tasks placed in time as ganttDb
// places them (mermaid 12.0.0) in a browser set to UTC.
type Gantt struct {
	title     string
	titleLine int
	// DateFormat is the dateFormat as written ("" when none).
	DateFormat string
	// AxisFormat is the axis labels' d3-time-format: the axisFormat as
	// written, "%d" for a dateFormat of "D", else "%Y-%m-%d".
	AxisFormat string
	// TickEvery and TickUnit are a valid tickInterval ("1week": 1, "week"),
	// or 0 and "" for d3's automatic ticks.
	TickEvery int
	TickUnit  string
	// Weekday is the first day of week ticks ("sunday" by default).
	Weekday string
	TopAxis bool
	// Excluded are the days drawn shaded, as ms since the epoch of each
	// excluded day's start, in order.
	Excluded []int64
	Tasks    []*GanttTask
}

func (g *Gantt) Title() string { return g.title }

// TitleLine is the source line of the title, or 0.
func (g *Gantt) TitleLine() int { return g.titleLine }
func (*Gantt) diagram()         {}

// GanttTask is one task, in source order.
type GanttTask struct {
	ID      string
	Text    string // as written, trimmed, entity codes decoded
	Section string // the section's name as written ("" before any section)
	// SectionTitle is the section's name as drawn: broken at <br>, each
	// line as SVG text shows it.
	SectionTitle string
	// Start and End are ms since the epoch. Bar is where the bar ends
	// (ganttDb's renderEndTime when excluded days lengthened the task).
	Start, End, Bar                     int64
	Active, Done, Crit, Milestone, Vert bool
	// Row is the task's row, in source order; -1 for a vert marker.
	Row  int
	Line int
}
