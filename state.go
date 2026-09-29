package mermaidrender

// StateDiagram is a parsed stateDiagram or stateDiagram-v2. Every scope —
// the top level, a composite's inside, each concurrent region — is laid out
// on its own, so the diagram is a tree of scopes.
type StateDiagram struct {
	title     string
	titleLine int
	Root      *StateScope
}

func (s *StateDiagram) Title() string { return s.title }

// TitleLine is the source line of the front matter's title, or 0.
func (s *StateDiagram) TitleLine() int { return s.titleLine }
func (*StateDiagram) diagram()         {}

// StateScope is the states laid out together, and the transitions and notes
// between them.
type StateScope struct {
	// Direction is the last direction statement in the scope's own
	// statements, else TB: a nested scope does not inherit its parent's
	// (stateCommon's DEFAULT_NESTED_DOC_DIR).
	Direction Direction
	// States in the order mermaid first meets them (dataFetcher's walk:
	// the source order, a composite's inside right after it).
	States      []*StateNode
	Transitions []*Transition
	Notes       []*StateNote
}

// StateKind is what a state is drawn as.
type StateKind int

const (
	StatePlain     StateKind = iota // a rounded box
	StateStart                      // [*] first in a transition
	StateEnd                        // [*] second in a transition
	StateChoice                     // <<choice>>
	StateFork                       // <<fork>>
	StateJoin                       // <<join>>
	StateComposite                  // state id { … }
)

func (k StateKind) String() string {
	return [...]string{"state", "start", "end", "choice", "fork", "join", "composite"}[k]
}

// StateNode is one state.
type StateNode struct {
	ID   string
	Kind StateKind
	// Label is a plain state's first description, or its id; a
	// composite's title. Start, end, choice, fork and join show none. "\n"
	// breaks lines.
	Label string
	// Lines are a plain state's descriptions after the first, drawn under
	// its label (mermaid's rectWithTitle).
	Lines []string
	// Regions are a composite's inside: one scope, or one per concurrent
	// region (-- splits them).
	Regions []*StateScope
	Line    int // where the state first appears
}

// Transition is one transition between two states of the same scope.
type Transition struct {
	From, To string
	Label    string
	Line     int
}

// StateNote is a note beside a state of the scope.
type StateNote struct {
	State string
	// Left: "note left of". mermaid's note edge runs note -> state for a
	// left note and state -> note for a right one.
	Left bool
	Text string
	Line int
}
