package mermaidrender

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// dumpState prints a state diagram's scopes as a tree.
func dumpState(d *StateDiagram) string {
	var b strings.Builder
	if d.Title() != "" {
		fmt.Fprintf(&b, "title %q\n", d.Title())
	}
	var scope func(s *StateScope, indent string)
	scope = func(s *StateScope, indent string) {
		fmt.Fprintf(&b, "%sdirection %s\n", indent, s.Direction)
		for _, n := range s.States {
			fmt.Fprintf(&b, "%s%s %q", indent, n.Kind, n.ID)
			if n.Label != "" && n.Label != n.ID {
				fmt.Fprintf(&b, " label %q", n.Label)
			}
			for _, l := range n.Lines {
				fmt.Fprintf(&b, " line %q", l)
			}
			b.WriteString("\n")
			for i, r := range n.Regions {
				if len(n.Regions) > 1 {
					fmt.Fprintf(&b, "%s  region %d\n", indent, i+1)
				}
				scope(r, indent+"    ")
			}
		}
		for _, t := range s.Transitions {
			fmt.Fprintf(&b, "%s%q --> %q", indent, t.From, t.To)
			if t.Label != "" {
				fmt.Fprintf(&b, " : %q", t.Label)
			}
			b.WriteString("\n")
		}
		for _, n := range s.Notes {
			side := "right"
			if n.Left {
				side = "left"
			}
			fmt.Fprintf(&b, "%snote %s of %q %q\n", indent, side, n.State, n.Text)
		}
	}
	scope(d.Root, "")
	return b.String()
}

func mustState(t *testing.T, src string) *StateDiagram {
	t.Helper()
	d, err := Parse(src)
	if err != nil {
		t.Fatalf("%v\n%s", err, src)
	}
	s, ok := d.(*StateDiagram)
	if !ok {
		t.Fatalf("%T, want *StateDiagram", d)
	}
	return s
}

// Read as stateDiagram.jison, stateDb and dataFetcher read them (mermaid
// 12.0.0).
func TestParseState(t *testing.T) {
	for _, c := range []struct{ name, src, want string }{
		{"the documentation's first example", `stateDiagram-v2
    [*] --> Still
    Still --> [*]

    Still --> Moving
    Moving --> Still
    Moving --> Crash
    Crash --> [*]`, `direction TB
start "root_start"
state "Still"
end "root_end"
state "Moving"
state "Crash"
"root_start" --> "Still"
"Still" --> "root_end"
"Still" --> "Moving"
"Moving" --> "Still"
"Moving" --> "Crash"
"Crash" --> "root_end"
`},
		{"both headers", "stateDiagram\n  a --> b", "direction TB\nstate \"a\"\nstate \"b\"\n\"a\" --> \"b\"\n"},
		{"descriptions: the id, then the first, then a title and lines", `stateDiagram-v2
    s1
    s2 : This is a state description
    state "Another description" as s3
    s4 : first
    s4 : second
    s4 : third`, `direction TB
state "s1"
state "s2" label "This is a state description"
state "s3" label "Another description"
state "s4" label "first" line "second" line "third"
`},
		{"transition labels, and : inside them", `stateDiagram-v2
    s1 --> s2: A transition
    s2 --> s3 : ratio 1:2`, `direction TB
state "s1"
state "s2"
state "s3"
"s1" --> "s2" : "A transition"
"s2" --> "s3" : "ratio 1:2"
`},
		{"composites nest; [*] is per scope", `stateDiagram-v2
    [*] --> First
    state First {
        [*] --> Second
        state Second {
            [*] --> second
            second --> [*]
        }
    }`, `direction TB
start "root_start"
composite "First"
    direction TB
    start "First_start"
    composite "Second"
        direction TB
        start "Second_start"
        state "second"
        end "Second_end"
        "Second_start" --> "second"
        "second" --> "Second_end"
    "First_start" --> "Second"
"root_start" --> "First"
`},
		{"a composite's description is its title", `stateDiagram-v2
    NamedComposite: Another Composite
    state NamedComposite {
        namedSimple: Another simple
    }`, `direction TB
composite "NamedComposite" label "Another Composite"
    direction TB
    state "namedSimple" label "Another simple"
`},
		{"state \"…\" as id { }", "stateDiagram-v2\n  state \"Long title\" as L {\n    a\n  }", `direction TB
composite "L" label "Long title"
    direction TB
    state "a"
`},
		{"the brace may be on the next line", "stateDiagram-v2\n  state X\n  {\n    a\n  }", `direction TB
composite "X"
    direction TB
    state "a"
`},
		{"choice, fork and join; kind from the first mention", `stateDiagram-v2
    state if_state <<choice>>
    state fork_state [[fork]]
    state join_state <<JOIN>>
    IsPositive --> late
    state late <<choice>>
    if_state --> False: if n < 0`, `direction TB
choice "if_state"
fork "fork_state"
join "join_state"
state "IsPositive"
state "late"
state "False"
"IsPositive" --> "late"
"if_state" --> "False" : "if n < 0"
`},
		{"a description makes a fork a plain state", "stateDiagram-v2\n  state f <<fork>>\n  f : forked", "direction TB\nstate \"f\" label \"forked\"\n"},
		{"concurrent regions, each with its own start", `stateDiagram-v2
    state Active {
        [*] --> NumLockOff
        NumLockOff --> NumLockOn : EvNumLockPressed
        --
        [*] --> CapsLockOff
    }`, `direction TB
composite "Active"
  region 1
    direction TB
    start "divider\n1_start"
    state "NumLockOff"
    state "NumLockOn"
    "divider\n1_start" --> "NumLockOff"
    "NumLockOff" --> "NumLockOn" : "EvNumLockPressed"
  region 2
    direction TB
    start "divider\n2_start"
    state "CapsLockOff"
    "divider\n2_start" --> "CapsLockOff"
`},
		{"direction per scope, last wins, not inherited", `stateDiagram
    direction LR
    [*] --> A
    state B {
      direction LR
      a --> b
      direction BT
    }
    state C {
      c
    }
    direction RL`, `direction RL
start "root_start"
state "A"
composite "B"
    direction BT
    state "a"
    state "b"
    "a" --> "b"
composite "C"
    direction TB
    state "c"
"root_start" --> "A"
`},
		{"a line holding direction LR is a direction statement", "stateDiagram-v2\n  A --> B : turn direction LR now", "direction LR\n"},
		{"direction TD is two states", "stateDiagram-v2\n  direction TD", "direction TB\nstate \"direction\"\nstate \"TD\"\n"},
		{"notes: one line, and until end note", `stateDiagram-v2
        State1: The state with a note
        note right of State1
            Important information! You can write
            notes.
        end note
        State1 --> State2
        note left of State2 : This is the note to the left.`, `direction TB
state "State1" label "The state with a note"
state "State2"
"State1" --> "State2"
note right of "State1" "Important information! You can write\nnotes."
note left of "State2" "This is the note to the left."
`},
		{"a one-line note loses two characters after the id; LEFT OF is a right note", "stateDiagram-v2\n  A\n  B\n  C\n  note left of A :text\n  note left of B:text\n  NOTE LEFT OF C : x", `direction TB
state "A"
state "B"
state "C"
note left of "A" "text"
note left of "B" "ext"
note right of "C" "x"
`},
		{"a note first, then a description: the description gives the shape", "stateDiagram-v2\n  note right of A : n\n  A : described", `direction TB
state "A" label "described"
note right of "A" "n"
`},
		{"a title and lines are not markdown", "stateDiagram-v2\n  A : **title**\n  A : one\\ntwo", `direction TB
state "A" label "**title**" line "one\ntwo"
`},
		{"dropped and ignored statements", `stateDiagram
   accTitle: This is the accessible title
   accDescr: This is an accessible description
   classDef movement font-style:italic;
   classDef badBadEvent fill:#f00,color:white
   [*] --> Still:::movement
   Crash:::badBadEvent --> [*]
   class Moving, Crash movement
   style Still fill:#fff
   hide empty description
   scale 350 width
   state Lonely
   note "floating" as N1
   click Still "https://example.com" "tip"
   accDescr {
     many
     lines
   }`, `direction TB
start "root_start"
state "Still"
state "Crash"
end "root_end"
"root_start" --> "Still"
"Crash" --> "root_end"
`},
		{"comments, inline and alone", "stateDiagram-v2\n  %% alone\n  A --> B %% inline\n  C%%glued\n  # hash", "direction TB\nstate \"A\"\nstate \"B\"\nstate \"C\"\n\"A\" --> \"B\"\n"},
		{"a state belongs to the last composite that mentions it", `stateDiagram-v2
    top --> X
    state X {
        shared
    }
    state Y {
        shared --> y
    }
    shared : desc`, `direction TB
state "top"
composite "X"
    direction TB
composite "Y"
    direction TB
    state "shared" label "desc"
    state "y"
    "shared" --> "y"
"top" --> "X"
`},
		{"inside a composite, statements run on across lines", "stateDiagram-v2\n  state X {\n    a\n    : described\n    b\n    --> c\n  }", `direction TB
composite "X"
    direction TB
    state "a" label "described"
    state "b"
    state "c"
    "b" --> "c"
`},
		{"a composite named default is fine inside", "stateDiagram-v2\n  state X {\n    default --> click\n  }", `direction TB
composite "X"
    direction TB
    state "default"
    state "click"
    "default" --> "click"
`},
		{"entities and <br>", "stateDiagram-v2\n  A : Tom #amp; Jerry<br>again\n  A --> B : #quot;go#quot;", `direction TB
state "A" label "Tom & Jerry\nagain"
state "B"
"A" --> "B" : "\"go\""
`},
		{"; and } are id characters, as mermaid lexes them", "stateDiagram-v2\n  A : x; y\n  }", `direction TB
state "A" label "x"
state ";"
state "y"
state "}"
`},
		{"a mention in a later region moves a state there", "stateDiagram-v2\n  state X {\n    a\n    --\n    b\n    a --> b\n  }", `direction TB
composite "X"
  region 1
    direction TB
  region 2
    direction TB
    state "a"
    state "b"
    "a" --> "b"
`},
		{"Japanese ids", "stateDiagram-v2\n  [*] --> 注文作成\n  注文作成 --> 支払い待ち : 確定", `direction TB
start "root_start"
state "注文作成"
state "支払い待ち"
"root_start" --> "注文作成"
"注文作成" --> "支払い待ち" : "確定"
`},
		{":: at a description's start stays; %% inside stays", "stateDiagram-v2\n  A :: y\n  B : 50%% off", `direction TB
state "A" label ": y"
state "B" label "50%% off"
`},
		{"direction ending a line joins LR on the next", "stateDiagram-v2\n  A --> direction\n  LR --> B\n  C", "direction LR\nstate \"C\"\n"},
		{"top-level keywords are states inside a composite", "stateDiagram-v2\n  state X {\n    hide empty description\n  }", `direction TB
composite "X"
    direction TB
    state "hide"
    state "empty"
    state "description"
`},
		{"a non-ASCII word before the name does not count", "stateDiagram-v2\n  state 状態 X {\n    a\n  }", `direction TB
composite "X"
    direction TB
    state "a"
`},
		{"a marker takes everything before it as the id", "stateDiagram-v2\n  state my fork <<fork>>", "direction TB\nfork \"my fork\"\n"},
		{"the header alone is an empty diagram", "stateDiagram", "direction TB\n"},
		{"a title in front matter", "---\ntitle: Simple sample\n---\nstateDiagram-v2\n  a", "title \"Simple sample\"\ndirection TB\nstate \"a\"\n"},
	} {
		d := mustState(t, c.src)
		if got := dumpState(d); got != c.want {
			t.Errorf("%s:\n got\n%s\n want\n%s", c.name, got, c.want)
		}
	}
}

func TestParseStateErrors(t *testing.T) {
	for _, c := range []struct {
		name, src string
		kind      ErrorKind
		line      int
	}{
		{"a header glued to a statement", "stateDiagram[*] --> A", SyntaxError, 1},
		{"accDescr { } inside a composite is not a keyword", "stateDiagram-v2\n  state X {\n    accDescr {\n      x\n    }\n  }", SyntaxError, 3},
		{"default at the top level", "stateDiagram-v2\n  default --> A", SyntaxError, 2},
		{"a quoted string at the top level", "stateDiagram-v2\n  \"A\" --> B", SyntaxError, 2},
		{"a two-word composite name", "stateDiagram-v2\n  state Foo Bar {\n  }", SyntaxError, 2},
		{"two words before { across a line end", "stateDiagram-v2\n  state a\n  state b {\n  }", SyntaxError, 2},
		{"a :: inside a description", "stateDiagram-v2\n  A : x::y", SyntaxError, 2},
		{"a note first-met state with a transition only", "stateDiagram-v2\n  note left of A : x\n  A --> B", SyntaxError, 2},
		{"a note beside a choice in TB", "stateDiagram-v2\n  state c <<choice>>\n  B --> c\n  note right of c : x", UnsupportedConstruct, 4},
		{"a note beside a state looping to itself in BT", "stateDiagram-v2\n  direction BT\n  A --> A\n  note left of A : x", UnsupportedConstruct, 4},
		{"a state named root", "stateDiagram-v2\n  A --> root", UnsupportedConstruct, 2},
		{"a composite named root", "stateDiagram-v2\n  state root {\n    A\n  }", UnsupportedConstruct, 2},
		{"bold with __", "stateDiagram-v2\n  __init__ --> running", UnsupportedConstruct, 2},
		{"an unclosed composite", "stateDiagram-v2\n  state X {\n    a", SyntaxError, 3},
		{"-- at the top level", "stateDiagram-v2\n  a\n  --\n  b", SyntaxError, 3},
		{"a hyphen in an id", "stateDiagram-v2\n  a-b --> c", SyntaxError, 2},
		{"classDef default", "stateDiagram-v2\n  classDef default fill:#f00", SyntaxError, 2},
		{"a note with neither side nor quotes", "stateDiagram-v2\n  note over A : x", SyntaxError, 2},
		{"a composite with two descriptions", "stateDiagram-v2\n  X : one\n  X : two\n  state X {\n    a\n  }", SyntaxError, 3},
		{"a colon in the id of state … as", "stateDiagram-v2\n  state \"d\" as a:b", UnsupportedConstruct, 2},
		{"a transition into a composite from outside", "stateDiagram-v2\n  state X {\n    inner\n  }\n  outer --> inner", UnsupportedConstruct, 5},
		{"a transition between regions", "stateDiagram-v2\n  state X {\n    a --> b\n    --\n    b\n  }", UnsupportedConstruct, 3},
		{"a composite to its own inner state", "stateDiagram-v2\n  state X {\n    X --> a\n  }", UnsupportedConstruct, 2},
		{"a composite ending with --", "stateDiagram-v2\n  state X {\n    a\n    --\n  }", UnsupportedConstruct, 2},
		{"a leading --", "stateDiagram-v2\n  state X {\n    --\n    a\n  }", UnsupportedConstruct, 3},
		{"markdown in a description", "stateDiagram-v2\n  A : **bold**", UnsupportedConstruct, 2},
		{"markdown in a note line", "stateDiagram-v2\n  A\n  note right of A\n    text\n    - item\n  end note", UnsupportedConstruct, 3},
		{"a state first met in a note", "stateDiagram-v2\n  note right of A : n\n  A --> B", SyntaxError, 2},
		{"HTML in a label", "stateDiagram-v2\n  A --> B : <b>x</b>", UnsupportedConstruct, 2},
		{"nesting past the limit", "stateDiagram-v2\n" + strings.Repeat("state X {\n", MaxStateDepth+1) + strings.Repeat("}\n", MaxStateDepth+1), UnsupportedConstruct, MaxStateDepth + 2},
	} {
		_, err := Parse(c.src)
		var e *Error
		if !errors.As(err, &e) || e.Kind != c.kind || e.Line != c.line {
			t.Errorf("%s: %v, want %s at line %d", c.name, err, c.kind, c.line)
		}
	}
}

// A trailing -- is refused for itself: mermaid leaves the composite unsplit
// with a stray divider in it, which no other check stands for.
func TestParseStateTrailingDivider(t *testing.T) {
	_, err := Parse("stateDiagram-v2\n  state X {\n    a\n    --\n  }")
	if err == nil || !strings.Contains(err.Error(), "ends with --") {
		t.Errorf("%v, want the trailing -- refused", err)
	}
}

// Where a note can stand on its side it is read: beside a choice in LR.
func TestParseStateNoteOnChoiceLR(t *testing.T) {
	d := mustState(t, "stateDiagram-v2\n  direction LR\n  state c <<choice>>\n  B --> c\n  note right of c : x")
	if len(d.Root.Notes) != 1 {
		t.Errorf("notes %v", d.Root.Notes)
	}
}

// Messages name a [*] as written, not as the id it became.
func TestParseStateMessagesShowStart(t *testing.T) {
	_, err := Parse("stateDiagram-v2\n  state X {\n    a\n    --\n    [*] --> b\n  }\n  state Y {\n    b\n  }")
	if err == nil || !strings.Contains(err.Error(), "[*] --> b") || strings.Contains(err.Error(), "\n") {
		t.Errorf("%v", err)
	}
}
