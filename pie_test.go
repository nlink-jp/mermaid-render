package mermaidrender

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// dumpPie is a pie chart as the tests read it.
func dumpPie(p *Pie) string {
	var b strings.Builder
	if p.Title() != "" {
		fmt.Fprintf(&b, "title %q\n", p.Title())
	}
	if p.ShowData {
		b.WriteString("showData\n")
	}
	for _, s := range p.Slices {
		fmt.Fprintf(&b, "slice %q %s\n", s.Label, s.Text)
	}
	return b.String()
}

func mustPie(t *testing.T, src string) *Pie {
	t.Helper()
	d, err := Parse(src)
	if err != nil {
		t.Fatalf("%v\n%s", err, src)
	}
	p, ok := d.(*Pie)
	if !ok {
		t.Fatalf("%T, want *Pie", d)
	}
	return p
}

// Read as pie.langium, common.langium and pieDb read them (mermaid 12.0.0).
func TestParsePie(t *testing.T) {
	for _, c := range []struct{ name, src, want string }{
		{"the documentation's example", `pie title Pets adopted by volunteers
    "Dogs" : 386
    "Cats" : 85
    "Rats" : 15`, `title "Pets adopted by volunteers"
slice "Dogs" 386
slice "Cats" 85
slice "Rats" 15
`},
		{"showData, a title line, floats, single quotes", `pie showData
    title Key elements in Product X
    "Calcium" : 42.96
    'Potassium' : 50.05
    "Magnesium" : 10.01
    "Iron" :  5`, `title "Key elements in Product X"
showData
slice "Calcium" 42.96
slice "Potassium" 50.05
slice "Magnesium" 10.01
slice "Iron" 5
`},
		{"pieDb keeps a repeated label's first value", "pie\n\"a\" : 1\n\"a\" : 2\n\"b\" : 3", `slice "a" 1
slice "b" 3
`},
		{"JavaScript's number printing; zero and minus zero", "pie showData\n\"a\" : 10.50\n\"b\" : 0\n\"c\" : -0\n\"d\" : 0.10", `showData
slice "a" 10.5
slice "b" 0
slice "c" 0
slice "d" 0.1
`},
		{"Langium's escapes, SVG's whitespace", `pie
    "say \"hi\"" : 1
    "tab\there" : 2
    "line\nbreak" : 3
    "  spaced   out  " : 4
    'it\'s \q' : 5`, `slice "say \"hi\"" 1
slice "tab here" 2
slice "linebreak" 3
slice "spaced out" 4
slice "it's q" 5
`},
		{"a comment after an item; a title stops at a comment", "pie title T %% note\n\"a\" : 1 %% why\n%% whole line", `title "T"
slice "a" 1
`},
		{"an item on the header's line; the last title wins; front matter under it", "---\ntitle: Front\n---\npie \"a\" : 1\ntitle One\ntitle   Two   words", `title "Two words"
slice "a" 1
`},
		{"front matter's title stands without a body title", "---\ntitle: Front\n---\npie\n\"a\": 1", `title "Front"
slice "a" 1
`},
		{"a comment right after the keyword", "pie%%c\n\"a\" : 1", `slice "a" 1
`},
		{"accTitle and accDescr are ignored", "pie\naccTitle: x\naccDescr: y %% z\naccDescr {\n  many\n  lines\n}\n\"a\" : 1", `slice "a" 1
`},
	} {
		if got := dumpPie(mustPie(t, c.src)); got != c.want {
			t.Errorf("%s:\n got\n%s want\n%s", c.name, got, c.want)
		}
	}
}

func TestParsePieErrors(t *testing.T) {
	for _, c := range []struct {
		name, src string
		kind      ErrorKind
		line      int
	}{
		{"a negative value", "pie\n\"a\" : -1", SyntaxError, 2},
		{"a leading zero", "pie\n\"a\" : 01", SyntaxError, 2},
		{"a number followed by a dot", "pie\n\"a\" : 1.5.", SyntaxError, 2},
		{"a bare label", "pie\na : 1", SyntaxError, 2},
		{"no colon", "pie\n\"a\" 1", SyntaxError, 2},
		{"two items on a line", "pie\n\"a\" : 1 \"b\" : 2", SyntaxError, 2},
		{"a word glued to the keyword", "pie showDatax\n\"a\" : 1", SyntaxError, 1},
		{"showData on a later line", "pie\nshowData", SyntaxError, 2},
		{"an exponent", "pie\n\"a\" : 1e3", SyntaxError, 2},
		{"too many items", "pie\n" + strings.Repeat("\"x\" : 1\n", 1) + func() string {
			var b strings.Builder
			for i := range MaxSlices + 1 {
				fmt.Fprintf(&b, "\"s%d\" : 1\n", i)
			}
			return b.String()
		}(), UnsupportedConstruct, MaxSlices + 2},
		{"a total past a float", "pie\n\"a\" : " + strings.Repeat("9", 400) + "\n\"b\" : 1", UnsupportedConstruct, 2},
	} {
		_, err := Parse(c.src)
		var e *Error
		if !errors.As(err, &e) || e.Kind != c.kind || e.Line != c.line {
			t.Errorf("%s: %v, want %v at line %d", c.name, err, c.kind, c.line)
		}
	}
}
