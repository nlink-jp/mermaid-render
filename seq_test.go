package mermaidrender

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// The cases quote the mermaid 12.0.0 documentation
// (syntax/sequenceDiagram.md) section by section; where it is silent,
// sequenceDiagram.jison and sequenceDb.ts of the same version decide.

// dumpSeq writes a sequence diagram compactly, one line per participant,
// box and event.
func dumpSeq(d *Sequence) string {
	var b strings.Builder
	if d.Title() != "" {
		fmt.Fprintf(&b, "title %q\n", d.Title())
	}
	for _, p := range d.Participants {
		fmt.Fprintf(&b, "participant %s %q", p.ID, p.Label)
		if p.Actor {
			b.WriteString(" actor")
		}
		if p.Box != nil {
			fmt.Fprintf(&b, " box %q", p.Box.Title)
		}
		b.WriteString("\n")
	}
	for _, e := range d.Events {
		switch e.Kind {
		case Message:
			line := "solid"
			if e.Dotted {
				line = "dotted"
			}
			fmt.Fprintf(&b, "msg %s -> %s %s %s", e.From.ID, e.To.ID, line, e.Head)
			if e.BothEnds {
				b.WriteString(" both")
			}
			if e.Number != "" {
				fmt.Fprintf(&b, " #%s", e.Number)
			}
			fmt.Fprintf(&b, " %q\n", e.Text)
		case Note:
			fmt.Fprintf(&b, "note %s %s", e.Place, e.From.ID)
			if e.To != e.From {
				fmt.Fprintf(&b, ",%s", e.To.ID)
			}
			fmt.Fprintf(&b, " %q\n", e.Text)
		case Activate, Deactivate:
			fmt.Fprintf(&b, "%s %s\n", e.Kind, e.From.ID)
		case BlockStart, BlockSection:
			fmt.Fprintf(&b, "%s %s %q\n", e.Kind, e.Block, e.Text)
		case BlockEnd:
			fmt.Fprintf(&b, "%s %s\n", e.Kind, e.Block)
		}
	}
	return b.String()
}

func mustSeq(t *testing.T, src string) *Sequence {
	t.Helper()
	d, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	s, ok := d.(*Sequence)
	if !ok {
		t.Fatalf("Parse(%q) = %T, want *Sequence", src, d)
	}
	return s
}

func TestSequenceDocumentation(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"participants", `sequenceDiagram
    participant Alice
    participant Bob
    Bob->>Alice: Hi Alice
    Alice->>Bob: Hi Bob`, `participant Alice "Alice"
participant Bob "Bob"
msg Bob -> Alice solid filled "Hi Alice"
msg Alice -> Bob solid filled "Hi Bob"
`},
		{"actors", `sequenceDiagram
    actor Alice
    actor Bob
    Alice->>Bob: Hi Bob
    Bob->>Alice: Hi Alice`, `participant Alice "Alice" actor
participant Bob "Bob" actor
msg Alice -> Bob solid filled "Hi Bob"
msg Bob -> Alice solid filled "Hi Alice"
`},
		{"implicit participants, in order of mention", `sequenceDiagram
    Alice->>John: Hello John, how are you?
    John-->>Alice: Great!`, `participant Alice "Alice"
participant John "John"
msg Alice -> John solid filled "Hello John, how are you?"
msg John -> Alice dotted filled "Great!"
`},
		{"external alias", `sequenceDiagram
    participant A as Alice
    participant J as John
    A->>J: Hello John, how are you?
    J->>A: Great!`, `participant A "Alice"
participant J "John"
msg A -> J solid filled "Hello John, how are you?"
msg J -> A solid filled "Great!"
`},
		{"grouping / box", `sequenceDiagram
    box Purple Alice & John
    participant A
    participant J
    end
    box Another Group
    participant B
    participant C
    end
    A->>J: Hello John, how are you?
    B->>C: Hello Charley, how are you?`, `participant A "A" box "Alice & John"
participant J "J" box "Alice & John"
participant B "B" box "Another Group"
participant C "C" box "Another Group"
msg A -> J solid filled "Hello John, how are you?"
msg B -> C solid filled "Hello Charley, how are you?"
`},
		{"box colours", `sequenceDiagram
    box rgb(33,66,99) Colored
    participant A
    end
    box transparent Aqua
    participant B
    end
    box Group without description
    participant C
    end`, `participant A "A" box "Colored"
participant B "B" box "Aqua"
participant C "C" box "Group without description"
`},
		{"supported arrow types", `sequenceDiagram
    A->B: 1
    A-->B: 2
    A->>B: 3
    A-->>B: 4
    A<<->>B: 5
    A<<-->>B: 6
    A-xB: 7
    A--xB: 8
    A-)B: 9
    A--)B: 10`, `participant A "A"
participant B "B"
msg A -> B solid none "1"
msg A -> B dotted none "2"
msg A -> B solid filled "3"
msg A -> B dotted filled "4"
msg A -> B solid filled both "5"
msg A -> B dotted filled both "6"
msg A -> B solid cross "7"
msg A -> B dotted cross "8"
msg A -> B solid open "9"
msg A -> B dotted open "10"
`},
		{"activations", `sequenceDiagram
    Alice->>John: Hello John, how are you?
    activate John
    John-->>Alice: Great!
    deactivate John`, `participant Alice "Alice"
participant John "John"
msg Alice -> John solid filled "Hello John, how are you?"
activate John
msg John -> Alice dotted filled "Great!"
deactivate John
`},
		{"activation shortcuts, stacked", `sequenceDiagram
    Alice->>+John: Hello John, how are you?
    Alice->>+John: John, can you hear me?
    John-->>-Alice: Hi Alice, I can hear you!
    John-->>-Alice: I feel great!`, `participant Alice "Alice"
participant John "John"
msg Alice -> John solid filled "Hello John, how are you?"
activate John
msg Alice -> John solid filled "John, can you hear me?"
activate John
msg John -> Alice dotted filled "Hi Alice, I can hear you!"
deactivate John
msg John -> Alice dotted filled "I feel great!"
deactivate John
`},
		{"notes", `sequenceDiagram
    participant John
    Note right of John: Text in note
    Note left of John: Left
    Alice->John: Hello John, how are you?
    Note over Alice,John: A typical interaction`, `participant John "John"
participant Alice "Alice"
note right-of John "Text in note"
note left-of John "Left"
msg Alice -> John solid none "Hello John, how are you?"
note over Alice,John "A typical interaction"
`},
		{"line breaks", `sequenceDiagram
    participant Alice as Alice<br/>Johnson
    Alice->John: Hello John,<br/>how are you?
    Note over Alice,John: A typical interaction<br/>But now in two lines`, `participant Alice "Alice\nJohnson"
participant John "John"
msg Alice -> John solid none "Hello John,\nhow are you?"
note over Alice,John "A typical interaction\nBut now in two lines"
`},
		{"loops", `sequenceDiagram
    Alice->John: Hello John, how are you?
    loop Every minute
        John-->Alice: Great!
    end`, `participant Alice "Alice"
participant John "John"
msg Alice -> John solid none "Hello John, how are you?"
block-start loop "Every minute"
msg John -> Alice dotted none "Great!"
block-end loop
`},
		{"alt and opt", `sequenceDiagram
    Alice->>Bob: Hello Bob, how are you?
    alt is sick
        Bob->>Alice: Not so good :(
    else is well
        Bob->>Alice: Feeling fresh like a daisy
    end
    opt Extra response
        Bob->>Alice: Thanks for asking
    end`, `participant Alice "Alice"
participant Bob "Bob"
msg Alice -> Bob solid filled "Hello Bob, how are you?"
block-start alt "is sick"
msg Bob -> Alice solid filled "Not so good :("
block-section alt "is well"
msg Bob -> Alice solid filled "Feeling fresh like a daisy"
block-end alt
block-start opt "Extra response"
msg Bob -> Alice solid filled "Thanks for asking"
block-end opt
`},
		{"nested parallel", `sequenceDiagram
    par Alice to Bob
        Alice->>Bob: Go help John
    and Alice to John
        Alice->>John: I want this done today
        par John to Charlie
            John->>Charlie: Can we do this today?
        and John to Diana
            John->>Diana: Can you help us today?
        end
    end`, `participant Alice "Alice"
participant Bob "Bob"
participant John "John"
participant Charlie "Charlie"
participant Diana "Diana"
block-start par "Alice to Bob"
msg Alice -> Bob solid filled "Go help John"
block-section par "Alice to John"
msg Alice -> John solid filled "I want this done today"
block-start par "John to Charlie"
msg John -> Charlie solid filled "Can we do this today?"
block-section par "John to Diana"
msg John -> Diana solid filled "Can you help us today?"
block-end par
block-end par
`},
		{"critical region", `sequenceDiagram
    critical Establish a connection to the DB
        Service-->DB: connect
    option Network timeout
        Service-->Service: Log error
    end`, `participant Service "Service"
participant DB "DB"
block-start critical "Establish a connection to the DB"
msg Service -> DB dotted none "connect"
block-section critical "Network timeout"
msg Service -> Service dotted none "Log error"
block-end critical
`},
		{"break", `sequenceDiagram
    Consumer-->API: Book something
    break when the booking process fails
        API-->Consumer: show failure
    end`, `participant Consumer "Consumer"
participant API "API"
msg Consumer -> API dotted none "Book something"
block-start break "when the booking process fails"
msg API -> Consumer dotted none "show failure"
block-end break
`},
		{"background highlighting: the rect is dropped, its contents kept", `sequenceDiagram
    participant Alice
    participant John
    rect rgb(191, 223, 255)
    note right of Alice: Alice calls John.
    Alice->>+John: Hello John, how are you?
    rect rgb(200, 150, 255)
    Alice->>+John: John, can you hear me?
    John-->>-Alice: Hi Alice, I can hear you!
    end
    John-->>-Alice: I feel great!
    end`, `participant Alice "Alice"
participant John "John"
note right-of Alice "Alice calls John."
msg Alice -> John solid filled "Hello John, how are you?"
activate John
msg Alice -> John solid filled "John, can you hear me?"
activate John
msg John -> Alice dotted filled "Hi Alice, I can hear you!"
deactivate John
msg John -> Alice dotted filled "I feel great!"
deactivate John
`},
		{"entity codes", `sequenceDiagram
    A->>B: I #9829; you!
    B->>A: I #9829; you #infin; times more!`, `participant A "A"
participant B "B"
msg A -> B solid filled "I ♥ you!"
msg B -> A solid filled "I ♥ you ∞ times more!"
`},
		{"sequence numbers", `sequenceDiagram
    autonumber
    Alice->>John: Hello John, how are you?
    loop HealthCheck
        John->>John: Fight against hypochondria
    end
    Note right of John: Rational thoughts!
    John-->>Alice: Great!`, `participant Alice "Alice"
participant John "John"
msg Alice -> John solid filled #1 "Hello John, how are you?"
block-start loop "HealthCheck"
msg John -> John solid filled #2 "Fight against hypochondria"
block-end loop
note right-of John "Rational thoughts!"
msg John -> Alice dotted filled #3 "Great!"
`},
		{"start and increment", `sequenceDiagram
    A->>B: before
    autonumber 10 5
    A->>B: x
    A->>B: y
    autonumber off
    A->>B: z
    autonumber 1.5 0.25
    A->>B: w
    A->>B: v`, `participant A "A"
participant B "B"
msg A -> B solid filled "before"
msg A -> B solid filled #10 "x"
msg A -> B solid filled #15 "y"
msg A -> B solid filled "z"
msg A -> B solid filled #1.5 "w"
msg A -> B solid filled #1.75 "v"
`},
		{"renderer: plain autonumber counts the messages before it", `sequenceDiagram
    A->>B: one
    A->>B: two
    autonumber
    A->>B: three`, `participant A "A"
participant B "B"
msg A -> B solid filled "one"
msg A -> B solid filled "two"
msg A -> B solid filled #3 "three"
`},
		{"actor menus: ignored", `sequenceDiagram
    participant Alice
    link Alice: Dashboard @ https://dashboard.contoso.com/alice
    links Alice: {"Dashboard": "https://dashboard.contoso.com/alice"}
    Alice->>Alice: hi`, `participant Alice "Alice"
msg Alice -> Alice solid filled "hi"
`},
		{"title statement, semicolons, wrap prefix", `sequenceDiagram
    title My flow
    A->>B: one; B->>A: wrap: two`, `title "My flow"
participant A "A"
participant B "B"
msg A -> B solid filled "one"
msg B -> A solid filled "two"
`},
		{"jison: # ends the text", `sequenceDiagram
    A->>B: issue #12 fixed`, `participant A "A"
participant B "B"
msg A -> B solid filled "issue"
`},
		{"jison: names with spaces and hyphens", `sequenceDiagram
    Web Client->>auth-service: login
    auth-service-->>Web Client: ok`, `participant Web Client "Web Client"
participant auth-service "auth-service"
msg Web Client -> auth-service solid filled "login"
msg auth-service -> Web Client dotted filled "ok"
`},
		{"sequenceDb: a later declaration with an alias relabels", `sequenceDiagram
    A->>B: hi
    actor A as Alice
    participant B`, `participant A "Alice" actor
participant B "B"
msg A -> B solid filled "hi"
`},
		{"jison: spaces around the arrow and colon", `sequenceDiagram
    Alice ->> Bob : hi`, `participant Alice "Alice"
participant Bob "Bob"
msg Alice -> Bob solid filled "hi"
`},
		{"jison: a name starting with digits is a name", `sequenceDiagram
    3DS Server->>ACS: auth`, `participant 3DS Server "3DS Server"
participant ACS "ACS"
msg 3DS Server -> ACS solid filled "auth"
`},
		{"jison: keywords only as whole words", `sequenceDiagram
    Endpoint->>Looper: a
    Notes->>Participants: b
    Optimizer->>Alternative: c`, `participant Endpoint "Endpoint"
participant Looper "Looper"
participant Notes "Notes"
participant Participants "Participants"
participant Optimizer "Optimizer"
participant Alternative "Alternative"
msg Endpoint -> Looper solid filled "a"
msg Notes -> Participants solid filled "b"
msg Optimizer -> Alternative solid filled "c"
`},
		{"jison: a comment after end, autonumber as the last line", `sequenceDiagram
    loop x
    A->>B: y
    end %% done
    autonumber 10 5`, `participant A "A"
participant B "B"
block-start loop "x"
msg A -> B solid filled "y"
block-end loop
`},
		{"legacy title, and a title after a full-width space", `sequenceDiagram
    title: Legacy
    title　売上
    A->>B: x`, `title "売上"
participant A "A"
participant B "B"
msg A -> B solid filled "x"
`},
		{"renderer: autonumber N resets the step; rounding to hundredths", `sequenceDiagram
    autonumber 10 5
    A->>B: a
    autonumber 3
    A->>B: b
    A->>B: c
    autonumber 1 0.1
    A->>B: d
    A->>B: e
    A->>B: f`, `participant A "A"
participant B "B"
msg A -> B solid filled #10 "a"
msg A -> B solid filled #3 "b"
msg A -> B solid filled #4 "c"
msg A -> B solid filled #1 "d"
msg A -> B solid filled #1.1 "e"
msg A -> B solid filled #1.2 "f"
`},
		{"sequenceDb: wrap: is dropped only in lower case", `sequenceDiagram
    A->>B: Wrap: the result
    A->>B: wrap: kept`, `participant A "A"
participant B "B"
msg A -> B solid filled "Wrap: the result"
msg A -> B solid filled "kept"
`},
		{"jison: activate does not place a participant", `sequenceDiagram
    activate B
    A->>B: hi
    deactivate B`, `participant A "A"
participant B "B"
activate B
msg A -> B solid filled "hi"
deactivate B
`},
		{"jison: menus place their participant", `sequenceDiagram
    properties Z: {"a": "b"}
    A->>B: x`, `participant Z "Z"
participant A "A"
participant B "B"
msg A -> B solid filled "x"
`},
		{"jison: a comment after a declared name", `sequenceDiagram
    participant Alice # main: user
    Alice->>Bob: x`, `participant Alice "Alice"
participant Bob "Bob"
msg Alice -> Bob solid filled "x"
`},
		{"sequenceDb: a system colour opens a box line", `sequenceDiagram
    box Window Services
    participant A
    end`, `participant A "A" box "Services"
`},
		{"a byte order mark", "\uFEFFsequenceDiagram\n    A->>B: x", `participant A "A"
participant B "B"
msg A -> B solid filled "x"
`},
	}
	for _, c := range cases {
		s := mustSeq(t, c.src)
		if got := dumpSeq(s); got != c.want {
			t.Errorf("%s:\n got\n%s\n want\n%s", c.name, got, c.want)
		}
	}
}

func TestSequenceRefusals(t *testing.T) {
	cases := []struct {
		name, src string
		kind      ErrorKind
		line      int
	}{
		{"create", `sequenceDiagram
    Alice->>Bob: Hello
    create participant Carl
    Alice->>Carl: Hi Carl!`, UnsupportedConstruct, 3},
		{"destroy", `sequenceDiagram
    Alice->>Bob: Hello
    destroy Bob
    Bob->>Alice: I agree`, UnsupportedConstruct, 3},
		{"half arrows", `sequenceDiagram
    Alice-|\Bob: top half`, UnsupportedConstruct, 2},
		{"central connections", `sequenceDiagram
    Alice->>()John: Hello John`, UnsupportedConstruct, 2},
		{"typed participants", `sequenceDiagram
    participant Alice@{ "type" : "database" }
    Alice->>Bob: q`, UnsupportedConstruct, 2},
		{"par_over", `sequenceDiagram
    par_over A
    A->>B: x
    end`, UnsupportedConstruct, 2},
		{"a message without text", `sequenceDiagram
    A->>B`, SyntaxError, 2},
		{"deactivating an inactive participant", `sequenceDiagram
    A->>B: hi
    deactivate B`, SyntaxError, 3},
		{"a shortcut deactivating an inactive participant", `sequenceDiagram
    A->>-B: hi`, SyntaxError, 2},
		{"a participant in two boxes", `sequenceDiagram
    box One
    participant A
    end
    box Two
    participant A
    end`, SyntaxError, 6},
		{"a message inside a box", `sequenceDiagram
    box One
    A->>B: hi
    end`, SyntaxError, 3},
		{"else outside alt", `sequenceDiagram
    loop x
    A->>B: hi
    else y
    end`, SyntaxError, 4},
		{"an unclosed block", `sequenceDiagram
    loop x
    A->>B: hi`, SyntaxError, 3},
		{"jison: a name stops before --", `sequenceDiagram
    A--B->>C: x`, SyntaxError, 2},
		{"a box around participants not side by side", `sequenceDiagram
    A->>B: hi
    box Grp
    participant A
    participant C
    end`, UnsupportedConstruct, 3},
		{"note left of two participants", `sequenceDiagram
    Note left of A,B: x`, SyntaxError, 2},
		{"activating a participant nothing places", `sequenceDiagram
    A->>B: x
    activate C`, SyntaxError, 3},
		{"blocks nested too deep", "sequenceDiagram\n" + strings.Repeat("opt\n", MaxNesting+1), UnsupportedConstruct, MaxNesting + 2},
		{"HTML in a message", `sequenceDiagram
    A->>B: <b>bold</b>`, UnsupportedConstruct, 2},
	}
	for _, c := range cases {
		_, err := Parse(c.src)
		var pe *Error
		if !errors.As(err, &pe) {
			t.Errorf("%s: err = %v, want *Error", c.name, err)
			continue
		}
		if pe.Kind != c.kind || pe.Line != c.line {
			t.Errorf("%s: %v (kind %v line %d), want kind %v line %d", c.name, pe, pe.Kind, pe.Line, c.kind, c.line)
		}
	}
}

func TestSequenceEventCap(t *testing.T) {
	var b strings.Builder
	b.WriteString("sequenceDiagram\n")
	for i := 0; i <= MaxEvents; i++ {
		b.WriteString("A->>B: x\n")
	}
	_, err := Parse(b.String())
	var pe *Error
	if !errors.As(err, &pe) || pe.Kind != UnsupportedConstruct {
		t.Fatalf("%d messages: %v, want an unsupported-construct error", MaxEvents+1, err)
	}
}
