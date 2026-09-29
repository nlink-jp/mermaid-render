package mermaidrender

// Mindmap is a mind map (mindmap, mermaid 12.0.0): a tree of nodes, read
// from an indented outline as MindmapDB builds it.
type Mindmap struct {
	title     string
	titleLine int
	// Nodes are in source order, which is the tree's preorder: a parent
	// comes before its children. Nodes[0] is the root; there are none in an
	// empty mind map.
	Nodes []*MindmapNode
}

func (*Mindmap) diagram() {}

// Title is the front matter's title, or "".
func (m *Mindmap) Title() string { return m.title }

// TitleLine is the source line of the title, or 0.
func (m *Mindmap) TitleLine() int { return m.titleLine }

// MindmapShape is a node's shape, from its opening delimiter (getType).
type MindmapShape int

const (
	MindmapDefault MindmapShape = iota // no delimiter, "(-" or "-)"
	MindmapRect                        // [ ]
	MindmapRounded                     // ( )
	MindmapCircle                      // (( ))
	MindmapCloud                       // ) ( — and ( closed by anything but )
	MindmapBang                        // )) ((
	MindmapHexagon                     // {{ }}
)

// MindmapNode is one node.
type MindmapNode struct {
	// Text is the label as drawn, before wrapping: its lines joined by
	// "\n", each trimmed, runs of spaces as one, entity codes decoded.
	Text  string
	Shape MindmapShape
	// Level is the depth: 0 for the root.
	Level int
	// Parent is the parent's index in Nodes, -1 for the root.
	Parent int
	// Children are indices in Nodes, in source order.
	Children []int
	// Section is the index of the root's child the node descends from,
	// mod 11 (MAX_SECTIONS - 1 in mindmapDb), or -1 for the root.
	Section int
	Line    int
}

// MaxMindmapNodes bounds a mind map, as the flowchart's nodes.
const MaxMindmapNodes = 300

// MindmapSections is how many section colours cycle (MAX_SECTIONS - 1).
const MindmapSections = 11
