package mermaidrender

// Sequence is a sequence diagram (sequenceDiagram, mermaid 12.0.0).
type Sequence struct {
	title     string
	titleLine int
	// Participants in order of first mention: a declaration, a message,
	// a note or an activation.
	Participants []*Participant
	Boxes        []*Box
	// Events in source order. Blocks are flat: a BlockStart, sections
	// (BlockSection), a BlockEnd, and the events between them.
	Events []*Event
}

func (*Sequence) diagram() {}

// Title is the front-matter title, or a title statement's.
func (s *Sequence) Title() string { return s.title }

// TitleLine is the line the title came from, or 0.
func (s *Sequence) TitleLine() int { return s.titleLine }

// Participant is one lifeline.
type Participant struct {
	ID    string
	Label string // the alias (as ...), else the id; "\n" breaks lines
	Actor bool   // declared with actor: drawn as a figure
	Box   *Box
	Line  int
}

// Box groups participants under a title (its colour is presentation).
type Box struct {
	Title        string
	Participants []*Participant
	Line         int
}

// EventKind says what an Event is.
type EventKind int

const (
	Message      EventKind = iota // From -> To (From == To: to itself)
	Note                          // over From..To, or left or right of From
	Activate                      // From's activation starts
	Deactivate                    // From's activation ends
	BlockStart                    // Block opens with Text
	BlockSection                  // else / and / option, with Text
	BlockEnd                      // the innermost open block closes
)

func (k EventKind) String() string {
	return [...]string{"message", "note", "activate", "deactivate", "block-start", "block-section", "block-end"}[k]
}

// ArrowHead is what a message's line ends in.
type ArrowHead int

const (
	HeadNone   ArrowHead = iota // -> and -->
	HeadFilled                  // ->> and -->>
	HeadCross                   // -x and --x
	HeadOpen                    // -) and --) (async)
)

func (h ArrowHead) String() string { return [...]string{"none", "filled", "cross", "open"}[h] }

// NotePlace is where a note stands.
type NotePlace int

const (
	LeftOf NotePlace = iota
	RightOf
	Over
)

func (p NotePlace) String() string { return [...]string{"left-of", "right-of", "over"}[p] }

// BlockKind names a block.
type BlockKind int

const (
	Loop BlockKind = iota
	Opt
	Alt
	Par
	Critical
	Break
)

func (b BlockKind) String() string {
	return [...]string{"loop", "opt", "alt", "par", "critical", "break"}[b]
}

// Event is one step of a sequence diagram.
type Event struct {
	Kind     EventKind
	From, To *Participant
	// Message fields.
	Dotted bool
	Head   ArrowHead
	// BothEnds: <<->> and <<-->>, a filled head at each end.
	BothEnds bool
	// Number is the autonumber shown on the message, or "".
	Number string
	Text   string // message, note, block and section text
	Place  NotePlace
	Block  BlockKind // BlockStart, BlockSection, BlockEnd
	Line   int
}
