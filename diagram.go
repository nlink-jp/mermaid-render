package mermaidrender

import "fmt"

// Diagram is the parsed form of one mermaid diagram. It says what the
// diagram means, never how it is drawn. The concrete types are *Flowchart,
// and later *Sequence and *ER.
type Diagram interface {
	// Title is the front-matter title, or "".
	Title() string
	diagram()
}

// ErrorKind tells why a source was not turned into a Diagram. Every kind
// means the same thing to a caller — show the source — but they are kept
// apart because the display treats them differently: an unsupported type
// passes through silently, a failed attempt gets a one-line note.
type ErrorKind int

const (
	// SyntaxError: the source is not valid mermaid.
	SyntaxError ErrorKind = iota
	// UnsupportedType: valid mermaid of a diagram type this engine does not draw.
	UnsupportedType
	// UnsupportedConstruct: a construct this engine does not draw, inside a
	// supported type.
	UnsupportedConstruct
)

func (k ErrorKind) String() string {
	switch k {
	case UnsupportedType:
		return "unsupported diagram type"
	case UnsupportedConstruct:
		return "unsupported construct"
	default:
		return "syntax error"
	}
}

// Error is the only error Parse returns. Line is 1-based in the source as
// given, front matter included; 0 means the error is not tied to a line.
type Error struct {
	Kind ErrorKind
	Line int
	Msg  string
}

func (e *Error) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("line %d: %s: %s", e.Line, e.Kind, e.Msg)
	}
	return fmt.Sprintf("%s: %s", e.Kind, e.Msg)
}

func errf(kind ErrorKind, line int, format string, a ...any) *Error {
	return &Error{Kind: kind, Line: line, Msg: fmt.Sprintf(format, a...)}
}
