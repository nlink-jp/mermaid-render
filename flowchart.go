package mermaidrender

// Direction is the direction a flowchart's ranks run in.
type Direction int

const (
	TB Direction = iota // top to bottom (TB, TD, and the default)
	BT                  // bottom to top
	LR                  // left to right
	RL                  // right to left
)

func (d Direction) String() string {
	return [...]string{"TB", "BT", "LR", "RL"}[d]
}

// Shape is a flowchart node's shape: the 14 classic shapes of the mermaid
// documentation (12.0.0, "Node shapes").
type Shape int

const (
	Rect             Shape = iota // id[text], and a bare id
	Round                         // id(text)
	Stadium                       // id([text])
	Subroutine                    // id[[text]]
	Cylinder                      // id[(text)]
	Circle                        // id((text))
	Asymmetric                    // id>text]
	Rhombus                       // id{text}
	Hexagon                       // id{{text}}
	Parallelogram                 // id[/text/]
	ParallelogramAlt              // id[\text\]
	Trapezoid                     // id[/text\]
	TrapezoidAlt                  // id[\text/]
	DoubleCircle                  // id(((text)))
)

func (s Shape) String() string {
	return [...]string{"rect", "round", "stadium", "subroutine", "cylinder", "circle",
		"asymmetric", "rhombus", "hexagon", "parallelogram", "parallelogram-alt",
		"trapezoid", "trapezoid-alt", "double-circle"}[s]
}

// Stroke is how a link's line is drawn.
type Stroke int

const (
	Solid  Stroke = iota // -- (and ---, -->)
	Thick                // ==
	Dotted               // -.-
)

func (s Stroke) String() string { return [...]string{"solid", "thick", "dotted"}[s] }

// Head is what one end of a link carries.
type Head int

const (
	NoHead     Head = iota
	Arrow           // > or <
	CircleHead      // o
	CrossHead       // x
)

func (h Head) String() string { return [...]string{"none", "arrow", "circle", "cross"}[h] }

// Flowchart is a parsed flowchart or graph.
type Flowchart struct {
	title     string
	Direction Direction
	// Nodes in order of first appearance. Subgraph ids used as link
	// endpoints are not nodes.
	Nodes []*Node
	// Links in source order, each pair of a chain or an & group its own
	// link. Invisible links (~~~) are not kept: they only steer layout.
	Links []*Link
	// Subgraphs in source order.
	Subgraphs []*Subgraph
}

func (f *Flowchart) Title() string { return f.title }
func (*Flowchart) diagram()        {}

// Node is one flowchart node.
type Node struct {
	ID    string
	Label string // the last text given, or the id; "\n" breaks lines
	Shape Shape
	// Subgraph is the id of the subgraph the node belongs to, or "". A node
	// belongs to the first subgraph that mentions it; mentioning it at the
	// top level does not count (mermaid's flowDb.makeUniq).
	Subgraph string
	Line     int // where the node first appears
}

// Subgraph is one subgraph. Phase 1 has no nesting.
type Subgraph struct {
	ID    string // as written, or "subGraph<n>" when the title has spaces
	Title string
	Nodes []string // member node ids, in order of mention
	Line  int
}

// Endpoint is one end of a link: a node, or a whole subgraph.
type Endpoint struct {
	ID       string
	Subgraph bool
}

// Link is one flowchart link.
type Link struct {
	From, To   Endpoint
	Label      string
	Stroke     Stroke
	Start, End Head // the heads at From and at To
	// Length is the rank span mermaid asks for: 1 for -->, 2 for --->, and
	// so on ("Minimum length of a link").
	Length int
	Line   int
}
