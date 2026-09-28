package mermaidrender

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// The cases quote the mermaid 12.0.0 documentation (syntax/flowchart.md)
// section by section; the section is named in each case.

// dump writes a flowchart compactly: one line per node, then one per link.
//
//	node  id shape "label" [in sg]
//	link  from -> to stroke start/end len=n "label"   (sg: marks a subgraph)
func dump(f *Flowchart) string {
	var b strings.Builder
	fmt.Fprintf(&b, "dir %s\n", f.Direction)
	for _, n := range f.Nodes {
		fmt.Fprintf(&b, "node %s %s %q", n.ID, n.Shape, n.Label)
		if n.Subgraph != "" {
			fmt.Fprintf(&b, " in %s", n.Subgraph)
		}
		b.WriteString("\n")
	}
	ep := func(e Endpoint) string {
		if e.Subgraph {
			return "sg:" + e.ID
		}
		return e.ID
	}
	for _, l := range f.Links {
		fmt.Fprintf(&b, "link %s -> %s %s %s/%s len=%d", ep(l.From), ep(l.To), l.Stroke, l.Start, l.End, l.Length)
		if l.Label != "" {
			fmt.Fprintf(&b, " %q", l.Label)
		}
		b.WriteString("\n")
	}
	for _, s := range f.Subgraphs {
		fmt.Fprintf(&b, "subgraph %s %q %v\n", s.ID, s.Title, s.Nodes)
	}
	return b.String()
}

func mustFlow(t *testing.T, src string) *Flowchart {
	t.Helper()
	d, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	f, ok := d.(*Flowchart)
	if !ok {
		t.Fatalf("Parse(%q) = %T, want *Flowchart", src, d)
	}
	return f
}

func links(t *testing.T, src string) string {
	t.Helper()
	var out []string
	for _, line := range strings.Split(dump(mustFlow(t, src)), "\n") {
		if strings.HasPrefix(line, "link ") {
			out = append(out, strings.TrimPrefix(line, "link "))
		}
	}
	return strings.Join(out, "\n")
}

func nodes(t *testing.T, src string) string {
	t.Helper()
	var out []string
	for _, line := range strings.Split(dump(mustFlow(t, src)), "\n") {
		if strings.HasPrefix(line, "node ") {
			out = append(out, strings.TrimPrefix(line, "node "))
		}
	}
	return strings.Join(out, "\n")
}

func wantErr(t *testing.T, src string, kind ErrorKind, line int) {
	t.Helper()
	_, err := Parse(src)
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("Parse(%q) error = %v, want *Error of kind %s", src, err, kind)
	}
	if e.Kind != kind || (line > 0 && e.Line != line) {
		t.Fatalf("Parse(%q) = %v, want %s at line %d", src, e, kind, line)
	}
}

// "Direction", and "Instead of flowchart one can also use graph".
func TestDirection(t *testing.T) {
	for src, want := range map[string]Direction{
		"flowchart TD\n A": TB, "flowchart TB\n A": TB, "flowchart BT\n A": BT,
		"flowchart RL\n A": RL, "flowchart LR\n A": LR, "graph LR\n A": LR,
		"graph\n A": TB, "flowchart\n A": TB, "flowchart-elk TD\n A": TB,
	} {
		if got := mustFlow(t, src).Direction; got != want {
			t.Errorf("%q: direction %s, want %s", src, got, want)
		}
	}
	wantErr(t, "flowchart XY\n A", SyntaxError, 1)
}

// "Node shapes": the 14 classic shapes, with the documentation's examples.
func TestShapes(t *testing.T) {
	for src, want := range map[string]string{
		"id1[This is the text in the box]":        `id1 rect "This is the text in the box"`,
		"id1(This is the text in the box)":        `id1 round "This is the text in the box"`,
		"id1([This is the text in the box])":      `id1 stadium "This is the text in the box"`,
		"id1[[This is the text in the box]]":      `id1 subroutine "This is the text in the box"`,
		"id1[(Database)]":                         `id1 cylinder "Database"`,
		"id1((This is the text in the circle))":   `id1 circle "This is the text in the circle"`,
		"id1>This is the text in the box]":        `id1 asymmetric "This is the text in the box"`,
		"id1{This is the text in the box}":        `id1 rhombus "This is the text in the box"`,
		"id1{{This is the text in the box}}":      `id1 hexagon "This is the text in the box"`,
		"id1[/This is the text in the box/]":      `id1 parallelogram "This is the text in the box"`,
		"id1[\\This is the text in the box\\]":    `id1 parallelogram-alt "This is the text in the box"`,
		"A[/Christmas\\]":                         `A trapezoid "Christmas"`,
		"B[\\Go shopping/]":                       `B trapezoid-alt "Go shopping"`,
		"id1(((This is the text in the circle)))": `id1 double-circle "This is the text in the circle"`,
		// "A node (default)": the id is the text.
		"id": `id rect "id"`,
	} {
		if got := nodes(t, "flowchart LR\n    "+src); got != want {
			t.Errorf("%s\n got  %s\n want %s", src, got, want)
		}
	}
}

// "A node with text": the last text given is the one used, and a later
// mention without text keeps it.
func TestLastTextWins(t *testing.T) {
	got := nodes(t, "flowchart LR\n A[first] --> B\n A(second)\n A --> C")
	want := "A round \"second\"\nB rect \"B\"\nC rect \"C\""
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// "Unicode text" and "Special characters that break syntax": quoted text is
// literal, parentheses included (gem-agent review 4 pinned the same trap).
func TestQuotedText(t *testing.T) {
	for src, want := range map[string]string{
		`id["This ❤ Unicode"]`:                 `id rect "This ❤ Unicode"`,
		`id1["This is the (text) in the box"]`: `id1 rect "This is the (text) in the box"`,
		`A["read_file(path)"]`:                 `A rect "read_file(path)"`,
		`A("a ] and a ) inside")`:              `A round "a ] and a ) inside"`,
		`A["gem-agent ($ open .)"]`:            `A rect "gem-agent ($ open .)"`,
		`クライアント["クライアント層"]`:                    `クライアント rect "クライアント層"`,
	} {
		if got := nodes(t, "flowchart LR\n "+src); got != want {
			t.Errorf("%s\n got  %s\n want %s", src, got, want)
		}
	}
}

// "Entity codes to escape characters", and <br> as a line break.
func TestLabelText(t *testing.T) {
	got := nodes(t, "flowchart LR\n A[\"A double quote:#quot;\"] --> B[\"A dec char:#9829;\"]\n C[one<br>two<br/>three] --> D[a < b]")
	want := "A rect \"A double quote:\\\"\"\nB rect \"A dec char:♥\"\nC rect \"one\\ntwo\\nthree\"\nD rect \"a < b\""
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	wantErr(t, "flowchart LR\n A[<b>bold</b>]", UnsupportedConstruct, 2)
}

// "Links between nodes": every example of the section.
func TestLinks(t *testing.T) {
	for src, want := range map[string]string{
		"A-->B":                       "A -> B solid none/arrow len=1",
		"A --- B":                     "A -> B solid none/none len=1",
		"A-- This is the text! ---B":  `A -> B solid none/none len=1 "This is the text!"`,
		"A---|This is the text|B":     `A -> B solid none/none len=1 "This is the text"`,
		"A-->|text|B":                 `A -> B solid none/arrow len=1 "text"`,
		"A-- text -->B":               `A -> B solid none/arrow len=1 "text"`,
		"A-.->B;":                     "A -> B dotted none/arrow len=1",
		"A-. text .-> B":              `A -> B dotted none/arrow len=1 "text"`,
		"A ==> B":                     "A -> B thick none/arrow len=1",
		"A == text ==> B":             `A -> B thick none/arrow len=1 "text"`,
		"A --o B":                     "A -> B solid none/circle len=1",
		"A --x B":                     "A -> B solid none/cross len=1",
		"A o--o B":                    "A -> B solid circle/circle len=1",
		"B <--> C":                    "B -> C solid arrow/arrow len=1",
		"C x--x D":                    "C -> D solid cross/cross len=1",
		"A --> |spaced| B":            `A -> B solid none/arrow len=1 "spaced"`,
		`A -->|"quoted | pipe"| B`:    `A -> B solid none/arrow len=1 "quoted | pipe"`,
		`A -- "has --> inside" --> B`: `A -> B solid none/arrow len=1 "has --> inside"`,
		// flowDb.destructEndLink: the line "---" minus one.
		"A---oB":     "A -> B solid none/circle len=2",
		"A---xB":     "A -> B solid none/cross len=2",
		"dev--- ops": "dev -> ops solid none/none len=1",
		"dev---Ops":  "dev -> Ops solid none/none len=1",
	} {
		if got := links(t, "flowchart LR\n    "+src); got != want {
			t.Errorf("%s\n got  %s\n want %s", src, got, want)
		}
	}
}

// A start mark counts only when the end mark is the same kind
// (flowDb.destructEndLink / destructLink, mermaid 12.0.0).
func TestStartMarks(t *testing.T) {
	for src, want := range map[string]string{
		"A <---> B":        "A -> B solid arrow/arrow len=2",
		"A <--- B":         "A -> B solid none/none len=2",
		"A o--> B":         "A -> B solid none/arrow len=2",
		"A <==> B":         "A -> B thick arrow/arrow len=1",
		"A <-.-> B":        "A -> B dotted arrow/arrow len=1",
		"A <-- text --> B": `A -> B solid arrow/arrow len=1 "text"`,
		"A x-- text --x B": `A -> B solid cross/cross len=1 "text"`,
		"A -- text --o B":  `A -> B solid none/circle len=1 "text"`,
	} {
		if got := links(t, "flowchart LR\n "+src); got != want {
			t.Errorf("%s\n got  %s\n want %s", src, got, want)
		}
	}
	// "x-- xyz -->  - not supported" (destructLink returns INVALID).
	wantErr(t, "flowchart LR\n A <-- text --- B", SyntaxError, 2)
	wantErr(t, "flowchart LR\n A x-- text --> B", SyntaxError, 2)
}

// "An invisible link": it steers layout only, so no link is kept, but the
// nodes exist.
func TestInvisibleLink(t *testing.T) {
	f := mustFlow(t, "flowchart LR\n A ~~~ B")
	if len(f.Links) != 0 || len(f.Nodes) != 2 {
		t.Errorf("got %s", dump(f))
	}
}

// "Minimum length of a link": the documentation's table.
func TestLinkLength(t *testing.T) {
	for link, want := range map[string]int{
		"---": 1, "----": 2, "-----": 3,
		"-->": 1, "--->": 2, "---->": 3,
		"===": 1, "====": 2, "=====": 3,
		"==>": 1, "===>": 2, "====>": 3,
		"-.-": 1, "-..-": 2, "-...-": 3,
		"-.->": 1, "-..->": 2, "-...->": 3,
	} {
		f := mustFlow(t, "flowchart LR\n A "+link+" B")
		if len(f.Links) != 1 || f.Links[0].Length != want {
			t.Errorf("%s: %s", link, dump(f))
		}
	}
	// "the extra dashes must be added on the right side of the link"
	if got := links(t, "flowchart LR\n B -- No ----> E[End]"); got != `B -> E solid none/arrow len=3 "No"` {
		t.Errorf("text link length: %s", got)
	}
	if got := links(t, "flowchart LR\n B ---->|No| E[End]"); got != `B -> E solid none/arrow len=3 "No"` {
		t.Errorf("pipe link length: %s", got)
	}
}

// "Chaining of links".
func TestChaining(t *testing.T) {
	for src, want := range map[string]string{
		"A -- text --> B -- text2 --> C": "A -> B solid none/arrow len=1 \"text\"\nB -> C solid none/arrow len=1 \"text2\"",
		"a --> b & c--> d":               "a -> b solid none/arrow len=1\na -> c solid none/arrow len=1\nb -> d solid none/arrow len=1\nc -> d solid none/arrow len=1",
		"A & B--> C & D":                 "A -> C solid none/arrow len=1\nA -> D solid none/arrow len=1\nB -> C solid none/arrow len=1\nB -> D solid none/arrow len=1",
	} {
		if got := links(t, "flowchart LR\n   "+src); got != want {
			t.Errorf("%s\n got\n%s\n want\n%s", src, got, want)
		}
	}
}

// ";" separates statements (flow.jison SEMI). gem-agent review 4: the text
// art drew a phantom node "B[b]; B".
func TestSemicolons(t *testing.T) {
	f := mustFlow(t, "graph TD;A[a]-->B[b]; B-->C[c];")
	if got := dump(f); got != "dir TB\nnode A rect \"a\"\nnode B rect \"b\"\nnode C rect \"c\"\nlink A -> B solid none/arrow len=1\nlink B -> C solid none/arrow len=1\n" {
		t.Errorf("got\n%s", got)
	}
}

// Ids that start with a keyword are ids (gem-agent review 4); lowercase
// "end" is not ("If you are using the word end ... capitalize").
func TestKeywordsAndIds(t *testing.T) {
	if got := links(t, "graph LR\n direction_check[Check] --> B[b]\n subgraph_x[Sub] --> B\n endpoint --> B\n classA --> B"); strings.Count(got, "\n")+1 != 4 {
		t.Errorf("keyword-prefixed ids: %s", got)
	}
	if got := nodes(t, "graph LR\n A --> End\n B --> END"); got != "A rect \"A\"\nEnd rect \"End\"\nB rect \"B\"\nEND rect \"END\"" {
		t.Errorf("capitalised end: %s", got)
	}
	wantErr(t, "graph LR\n A --> end", SyntaxError, 2)
	// A keyword used as a node id in a link cannot be dropped as a style line.
	wantErr(t, "graph LR\n style --> B", SyntaxError, 2)
	// Without whitespace after it, "class" is an id, not the keyword.
	if got := links(t, "graph LR\n class-.->B"); got != "class -> B dotted none/arrow len=1" {
		t.Errorf("class as an id: %s", got)
	}
}

// "Subgraphs", with the documentation's examples.
func TestSubgraphs(t *testing.T) {
	src := `flowchart TB
    c1-->a2
    subgraph one
    a1-->a2
    end
    subgraph two
    b1-->b2
    end
    subgraph three
    c1-->c2
    end`
	want := `dir TB
node c1 rect "c1" in three
node a2 rect "a2" in one
node a1 rect "a1" in one
node b1 rect "b1" in two
node b2 rect "b2" in two
node c2 rect "c2" in three
link c1 -> a2 solid none/arrow len=1
link a1 -> a2 solid none/arrow len=1
link b1 -> b2 solid none/arrow len=1
link c1 -> c2 solid none/arrow len=1
subgraph one "one" [a1 a2]
subgraph two "two" [b1 b2]
subgraph three "three" [c1 c2]
`
	if got := dump(mustFlow(t, src)); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	// "You can also set an explicit id for the subgraph."
	f := mustFlow(t, "flowchart TB\n    c1-->a2\n    subgraph ide1 [one]\n    a1-->a2\n    end")
	if s := f.Subgraphs[0]; s.ID != "ide1" || s.Title != "one" {
		t.Errorf("explicit id: %+v", s)
	}
	// "edges to and from subgraphs"
	got := links(t, src+"\n    one --> two\n    three --> two\n    two --> c2")
	if !strings.HasSuffix(got, "sg:one -> sg:two solid none/arrow len=1\nsg:three -> sg:two solid none/arrow len=1\nsg:two -> c2 solid none/arrow len=1") {
		t.Errorf("subgraph endpoints:\n%s", got)
	}
	if n := nodes(t, src+"\n one --> two"); strings.Contains(n, "one rect") {
		t.Errorf("a subgraph id became a node:\n%s", n)
	}
}

// Title forms seen in the real data, and flowDb.addSubGraph's id rule: a
// title with whitespace gets a generated id.
func TestSubgraphTitles(t *testing.T) {
	for src, want := range map[string][2]string{
		"subgraph ClientLayer [Client Zone]":  {"ClientLayer", "Client Zone"},
		`subgraph Client["クライアント層"]`:          {"Client", "クライアント層"},
		"subgraph ドメイン調査":                     {"ドメイン調査", "ドメイン調査"},
		"subgraph External Registries & APIs": {"subGraph0", "External Registries & APIs"},
		`subgraph "Quoted title"`:             {"subGraph0", "Quoted title"},
		"subgraph":                            {"subGraph0", ""},
	} {
		f := mustFlow(t, "flowchart LR\n "+src+"\n  A\n end")
		if s := f.Subgraphs[0]; s.ID != want[0] || s.Title != want[1] || len(s.Nodes) != 1 {
			t.Errorf("%s: got id %q title %q nodes %v", src, s.ID, s.Title, s.Nodes)
		}
	}
}

// Membership: a node belongs to the first subgraph that mentions it, and a
// top-level mention does not count (flowDb.makeUniq). The documentation's
// own first example depends on it (c1 and a2 above); this is the shape of
// the real-data flowchart that put a node in the wrong box when misread.
func TestSubgraphMembership(t *testing.T) {
	src := `flowchart TD
    Start --> Check{種別判定}
    Check -->|Domain| StepDomain[ドメイン帰属調査]
    Check -->|IP| StepIP[IP帰属調査]
    subgraph DomainFlow [ドメイン調査]
        StepDomain --> D1[WHOIS]
    end
    subgraph IPFlow [IP調査]
        StepIP --> I1[ASN]
        D1 --> I1
    end
    DomainFlow --> Correlate
    IPFlow --> Correlate`
	f := mustFlow(t, src)
	in := map[string]string{}
	for _, n := range f.Nodes {
		in[n.ID] = n.Subgraph
	}
	want := map[string]string{"Start": "", "Check": "", "StepDomain": "DomainFlow", "D1": "DomainFlow",
		"StepIP": "IPFlow", "I1": "IPFlow", "Correlate": ""}
	for id, sg := range want {
		if in[id] != sg {
			t.Errorf("%s in %q, want %q", id, in[id], sg)
		}
	}
	if got := f.Subgraphs[1].Nodes; strings.Join(got, ",") != "StepIP,I1" {
		t.Errorf("IPFlow members %v (D1 was taken by DomainFlow first)", got)
	}
}

// "Direction in subgraphs" is presentation; nesting is phase 2.
func TestSubgraphLimits(t *testing.T) {
	f := mustFlow(t, "flowchart LR\n subgraph A\n direction TB\n x --> y\n end")
	if len(f.Subgraphs[0].Nodes) != 2 {
		t.Errorf("direction inside a subgraph: %s", dump(f))
	}
	wantErr(t, "flowchart LR\n subgraph A\n subgraph B\n x\n end\n end", UnsupportedConstruct, 3)
	wantErr(t, "flowchart LR\n subgraph A\n x\n", SyntaxError, 2)
	wantErr(t, "flowchart LR\n x\n end", SyntaxError, 3)
	// A subgraph mentioned inside another subgraph would nest it.
	wantErr(t, "flowchart LR\n subgraph A\n x\n end\n subgraph B\n y --> A\n end", UnsupportedConstruct, 5)
	// An id that is both a node with a text and a subgraph.
	wantErr(t, "flowchart LR\n one[Node] --> x\n subgraph one\n y\n end", UnsupportedConstruct, 2)
	wantErr(t, "flowchart LR\n subgraph one\n y\n end\n subgraph one\n z\n end", UnsupportedConstruct, 5)
}

// Presentation lines are ignored: "Comments", "Styling and classes",
// "Interaction", accessibility, init directives.
func TestIgnored(t *testing.T) {
	plain := dump(mustFlow(t, "flowchart LR\n A[a] --> B[b]"))
	styled := dump(mustFlow(t, `%%{init: {"theme": "dark"}}%%
flowchart LR
%% this is a comment A -- text --> B{node}
    accTitle: A title
    accDescr: A description
    accDescr {
        Several lines
        of description
    }
    A[a]:::someclass --> B[b]
    classDef someclass fill:#f96
    class A someclass
    style B fill:#f9f,stroke:#333,stroke-width:4px
    linkStyle 0 stroke:#ff3,stroke-width:4px
    click A callback "Tooltip"
    click B "https://www.github.com" _blank`))
	if plain != styled {
		t.Errorf("styled differs:\n%s\nplain:\n%s", styled, plain)
	}
}

// Documented constructs outside phase 1, and undocumented shapes.
func TestUnsupported(t *testing.T) {
	for src, line := range map[string]int{
		"flowchart LR\n A@{ shape: rect }":                 2,
		"flowchart LR\n A e1@--> B":                        2,
		"flowchart LR\n A[\"`**bold**`\"]":                 2,
		"flowchart LR\n A -->|\"`md`\"| B":                 2,
		"flowchart LR\n A(-ellipse-)":                      2,
		"flowchart LR\n A[|field:value|text]":              2,
		"flowchart LR\n subgraph \"`md title`\"\n A\n end": 2,
	} {
		wantErr(t, src, UnsupportedConstruct, line)
	}
}

func TestSyntaxErrors(t *testing.T) {
	for src, line := range map[string]int{
		"flowchart LR\n A -->":           2,
		"flowchart LR\n A[unclosed":      2,
		"flowchart LR\n A --> B C":       2,
		"flowchart LR\n A[\"open]":       2,
		"flowchart LR\n A -- text":       2,
		"flowchart LR\n A -->|open B":    2,
		"flowchart LR\n A -- t -->|u| B": 2,
		"flowchart LR\n , A":             2,
	} {
		wantErr(t, src, SyntaxError, line)
	}
}

// The front matter's title is kept, and line numbers count its lines.
func TestFrontMatter(t *testing.T) {
	src := "---\ntitle: Node\nconfig:\n  look: classic\n---\nflowchart LR\n    id\n    id --> end"
	wantErr(t, src, SyntaxError, 8)
	f := mustFlow(t, "---\ntitle: \"Node with text\"\n---\nflowchart LR\n    id1[This is the text in the box]")
	if f.Title() != "Node with text" {
		t.Errorf("title %q", f.Title())
	}
	wantErr(t, "---\ntitle: x\nflowchart LR\n A", SyntaxError, 1)
}

// Findings of the step-2 review, each against mermaid 12.0.0's flow.jison /
// flowDb.ts.
func TestReviewParserFindings(t *testing.T) {
	// "direction" not followed by a direction word is a node (the jison
	// rule is direction\s+(TB|BT|RL|LR|TD)).
	if got := links(t, "flowchart LR\n direction --> B\n A --> C"); got != "direction -> B solid none/arrow len=1\nA -> C solid none/arrow len=1" {
		t.Errorf("direction as an id: %s", got)
	}
	// NODE_STRING holds "&": only a spaced or leading & groups nodes.
	if got := links(t, "flowchart LR\n API&DB --> Cache\n A & B --> C"); got != "API&DB -> Cache solid none/arrow len=1\nA -> C solid none/arrow len=1\nB -> C solid none/arrow len=1" {
		t.Errorf("& inside an id: %s", got)
	}
	// An entity code's ";" and a ";" in |link text| do not end a statement.
	f := mustFlow(t, "flowchart LR\n subgraph 監視 #amp; 通知\n X --> Y\n end\n A -->|a;b| B; B --> C")
	if s := f.Subgraphs[0]; s.Title != "監視 & 通知" || len(s.Nodes) != 2 {
		t.Errorf("entity in a title: %+v", s)
	}
	if got := links(t, "flowchart LR\n A -->|a;b| B; B --> C"); got != "A -> B solid none/arrow len=1 \"a;b\"\nB -> C solid none/arrow len=1" {
		t.Errorf("; in link text: %s", got)
	}
	if got := links(t, `flowchart LR
 A -- "say #quot;hi#quot;" --> B`); got != `A -> B solid none/arrow len=1 "say \"hi\""` {
		t.Errorf("entity in link text: %s", got)
	}
	// mermaid stops a link's length at 10.
	if got := links(t, "flowchart LR\n A "+strings.Repeat("-", 40)+"> B"); got != "A -> B solid none/arrow len=10" {
		t.Errorf("long link: %s", got)
	}
	// mermaid's own edge limit, checked before an & product is built.
	var group []string
	for i := range 30 {
		group = append(group, fmt.Sprintf("a%d", i))
	}
	g := strings.Join(group, " & ")
	wantErr(t, "flowchart LR\n "+g+" --> "+g, UnsupportedConstruct, 2)
}

func TestReviewSourceFindings(t *testing.T) {
	// A CR alone ends a line; a directive may span lines.
	if _, err := Parse("flowchart LR\r A --> B\r"); err != nil {
		t.Errorf("CR line ends: %v", err)
	}
	if got := links(t, "%%{init: {\n  \"theme\": \"dark\"\n}}%%\nflowchart LR\n A --> B"); got != "A -> B solid none/arrow len=1" {
		t.Errorf("multi-line directive: %s", got)
	}
	wantErr(t, "%%{init: {\nflowchart LR\n A", SyntaxError, 1)
	// The title is a YAML scalar, and its line is kept.
	for src, want := range map[string]string{
		"title: 'it''s'":      "it's",
		`title: "a\"b"`:       `a"b`,
		"title: hello # note": "hello",
		"title: plain":        "plain",
	} {
		f := mustFlow(t, "---\n"+src+"\n---\nflowchart LR\n A")
		if f.Title() != want || f.TitleLine() != 2 {
			t.Errorf("%s: title %q line %d, want %q line 2", src, f.Title(), f.TitleLine(), want)
		}
	}
}
