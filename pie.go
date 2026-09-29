package mermaidrender

// Pie is a pie chart (pie, mermaid 12.0.0).
type Pie struct {
	title     string
	titleLine int
	// ShowData puts each value beside its label in the legend.
	ShowData bool
	// Slices are the items in the order written, a repeated label dropped
	// (pieDb keeps the first value).
	Slices []Slice
}

func (*Pie) diagram() {}

// Title is the diagram's title — the body's `title` line over the front
// matter's, as mermaid applies them — or "".
func (p *Pie) Title() string { return p.title }

// TitleLine is the source line of the title, or 0.
func (p *Pie) TitleLine() int { return p.titleLine }

// Slice is one item of a pie chart.
type Slice struct {
	// Label is what the legend shows: the quoted label with its escapes
	// undone and its whitespace handled as SVG text shows it.
	Label string
	Value float64
	// Text is the value as JavaScript prints it, which showData's legend
	// shows: 10.50 is "10.5", -0 is "0".
	Text string
	Line int
}

// MaxSlices bounds a pie chart's items.
const MaxSlices = 100
