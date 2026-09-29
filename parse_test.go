package mermaidrender

import "testing"

func TestDiagramTypes(t *testing.T) {
	for src, kind := range map[string]ErrorKind{
		"classDiagram\n A <|-- B": UnsupportedType,
		"timeline\n  title x":     UnsupportedType,
		"journey\n title x":       UnsupportedType,
		"hello world":             SyntaxError,
	} {
		wantErr(t, src, kind, 0)
	}
	wantErr(t, "", SyntaxError, 0)
	wantErr(t, "%% only a comment\n\n", SyntaxError, 0)
	// The type keyword may follow comments and blank lines; its line is reported.
	wantErr(t, "\n%% c\njourney\n title x", UnsupportedType, 3)
	if _, err := Parse("\r\nflowchart LR\r\n A --> B\r\n"); err != nil {
		t.Errorf("CRLF source: %v", err)
	}
}

func TestErrorText(t *testing.T) {
	e := &Error{Kind: UnsupportedConstruct, Line: 3, Msg: "nested subgraph"}
	if got := e.Error(); got != "line 3: unsupported construct: nested subgraph" {
		t.Errorf("got %q", got)
	}
	e = &Error{Kind: SyntaxError, Msg: "no diagram"}
	if got := e.Error(); got != "syntax error: no diagram" {
		t.Errorf("got %q", got)
	}
}
