package mermaidrender

// ER is an entity relationship diagram (erDiagram, mermaid 12.0.0).
type ER struct {
	title     string
	titleLine int
	// Direction is TB unless a direction statement says otherwise.
	Direction     Direction
	Entities      []*Entity // in order of first appearance
	Relationships []*Relationship
}

func (*ER) diagram() {}

// Title is the front-matter title, or "".
func (e *ER) Title() string { return e.title }

// TitleLine is the source line of the front-matter title, or 0.
func (e *ER) TitleLine() int { return e.titleLine }

// Entity is one entity box.
type Entity struct {
	Name  string // as written, quotes removed: the key relationships use
	Alias string // name[alias]; "" when none
	// Label is what the box shows: the alias if there is one, else the
	// name, with entity codes decoded and <br> as a line break.
	Label      string
	Attributes []Attribute
	Line       int // where the entity is first mentioned
}

// Attribute is one row of an entity's table.
type Attribute struct {
	Type    string // generics written with ~ are shown with < >: List~int~ is List<int>
	Name    string
	Keys    []string // PK, FK, UK in the order written
	Comment string
	Line    int
}

// Cardinality is how many of an entity one of the other relates to.
type Cardinality int

const (
	ExactlyOne Cardinality = iota // || , 1, one, only one
	ZeroOrOne                     // |o o| , zero or one, one or zero
	ZeroOrMore                    // }o o{ , zero or more, zero or many, many, many(0), 0+
	OneOrMore                     // }| |{ , one or more, one or many, many(1), 1+
)

func (c Cardinality) String() string {
	return [...]string{"exactly-one", "zero-or-one", "zero-or-more", "one-or-more"}[c]
}

// Relationship is a line between two entities.
type Relationship struct {
	From, To *Entity
	// FromCard is written next to From ("|o" in "A |o--o{ B"), ToCard next
	// to To; each is drawn at its own end.
	FromCard, ToCard Cardinality
	// Identifying relationships (--, to) are drawn solid, the others (..,
	// .-, -., optionally to) dashed.
	Identifying bool
	Label       string
	Line        int
}
