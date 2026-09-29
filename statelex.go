package mermaidrender

import (
	"strings"
	"unicode/utf8"
)

// The stateDiagram lexer follows stateDiagram.jison (mermaid 12.0.0) rule
// by rule, in its order, with jison's behaviour (see erlex.go): the first
// rule that matches wins, case-insensitively, and a rule ending in a word
// character has \b appended. Its exclusive states: STATE after "state ",
// STATE_STRING / STATE_ID for `state "…" as id`, struct for a composite's
// body (no NL tokens there: statements run on across lines), NOTE /
// NOTE_ID / NOTE_TEXT and FLOATING_NOTE / FLOATING_NOTE_ID for notes,
// SCALE, CLASSDEF / CLASSDEFID, CLASS / CLASS_STYLE, STYLE /
// STYLEDEF_STYLES, and the acc_ states.

// Character classes the jison rules write with \s inside brackets, which
// lexRE cannot expand there.
const (
	stateIDClass   = `[^:\-\{` + jsSpace + `]+`                                                                    // [^:\n\s\-\{]+
	stateCompClass = `[^\{` + jsSpace + `]+`                                                                       // [^\n\s\{]+
	stateNoteID    = `\s*[^:\-` + jsSpace + `]+`                                                                   // \s*[^:\n\s\-]+
	stateLineSpace = `[\t\v\f\r \x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}]+` // ((?!\n)\s)+
)

// stateComment is \%\%(?!\{)[^\n]*.
func stateComment(rest string) int {
	if !strings.HasPrefix(rest, "%%") || strings.HasPrefix(rest, "%%{") {
		return -1
	}
	if i := strings.IndexByte(rest, '\n'); i >= 0 {
		return i
	}
	return len(rest)
}

// processID splits a rule's match at an inline %% (the jison processId):
// the token ends before it and the comment is lexed again. A match that
// starts with %% yields no token and leaves the state as it is; that half
// is idSkip.
func processID(re string) (id, skip func(string) int) {
	rx := lexRE(re)
	id = func(rest string) int {
		loc := rx.FindStringIndex(rest)
		if loc == nil {
			return -1
		}
		m := rest[:loc[1]]
		switch i := strings.Index(m, "%%"); {
		case i == 0:
			return -1 // idSkip's
		case i > 0:
			return i
		}
		return loc[1]
	}
	skip = func(rest string) int {
		loc := rx.FindStringIndex(rest)
		if loc == nil || loc[1] == 0 || !strings.HasPrefix(rest, "%%") {
			return -1
		}
		return loc[1]
	}
	return id, skip
}

// markerMatcher is <STATE>.*"<<fork>>" and its kin: greedy to the last
// marker on the line. The token is the text before it (the rule's
// slice(0,-n).trim()).
func markerMatcher(marker string) func(string) int {
	return func(rest string) int {
		end := len(rest)
		for i, r := range rest {
			if r == '\n' || r == '\r' || r == 0x2028 || r == 0x2029 {
				end = i
				break
			}
		}
		if i := lastIndexFoldASCII(rest[:end], marker); i >= 0 {
			return i + len(marker)
		}
		return -1
	}
}

// lastIndexFoldASCII is the last index of an ASCII marker in s, ignoring
// ASCII case (lowering s whole could change its byte offsets).
func lastIndexFoldASCII(s, marker string) int {
	lower := func(c byte) byte {
		if 'A' <= c && c <= 'Z' {
			return c + 'a' - 'A'
		}
		return c
	}
	for i := len(s) - len(marker); i >= 0; i-- {
		ok := true
		for j := 0; j < len(marker); j++ {
			if lower(s[i+j]) != lower(marker[j]) {
				ok = false
				break
			}
		}
		if ok {
			return i
		}
	}
	return -1
}

// twoWordsMatcher is <STATE>\w+\s+\w+.*?\{ (ASCII \w; \s takes line
// ends too, so the second word may be on a later line): a word, spaces, a
// word, and a { later on that word's line. The next { of a line is cached,
// as the lexer only moves forward: scanning the line's rest at every token
// would make a long line quadratic.
func twoWordsMatcher() func(string) int {
	braceD, lineEndD, fromD := -1, -1, -1 // distances to the source's end
	isWord := func(c byte) bool {
		return c == '_' || '0' <= c && c <= '9' || 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z'
	}
	return func(rest string) int {
		i := 0
		for i < len(rest) && isWord(rest[i]) {
			i++
		}
		if i == 0 {
			return -1
		}
		j := i
		for j < len(rest) {
			r, n := utf8.DecodeRuneInString(rest[j:])
			if !strings.ContainsRune(jsSpaceChars, r) {
				break
			}
			j += n
		}
		if j == i {
			return -1
		}
		k := j
		for k < len(rest) && isWord(rest[k]) {
			k++
		}
		if k == j {
			return -1
		}
		d := len(rest) - k // where the lazy .*? starts
		// The cache holds, for a stretch of one line from fromD on, the
		// first { in it (braceD, or -1 for none); a { behind d is stale.
		cached := d <= fromD && d > lineEndD && braceD <= d
		if !cached {
			// Find the line's end and its next { from here.
			tail := rest[k:]
			le := len(tail)
			for x, r := range tail {
				if r == '\n' || r == '\r' || r == 0x2028 || r == 0x2029 {
					le = x
					break
				}
			}
			fromD, lineEndD, braceD = d, d-le, -1
			if b := strings.IndexByte(tail[:le], '{'); b >= 0 {
				braceD = d - b
			}
		}
		if braceD < 0 {
			return -1
		}
		return len(rest) - braceD + 1
	}
}

var (
	stateIDMatch, stateIDSkip         = processID(stateIDClass)
	stateAsIDMatch, stateAsIDSkip     = processID(`[^\n\{]*`)
	stateNoteIDMatch, stateNoteIDSkip = processID(stateNoteID)
	stateFloatIDMatch, stateFloatSkip = processID(`[^\n]*`)
)

func stateMarker(marker, kind string) lexRule {
	return lexRule{match: markerMatcher(marker), kind: kind, pop: 1, cut: len(marker), trim: true, need: marker}
}

var stateDirections = []lexRule{
	{fresh: directionMatcher("TB"), kind: "direction_tb", need: "direction"},
	{fresh: directionMatcher("BT"), kind: "direction_bt", need: "direction"},
	{fresh: directionMatcher("RL"), kind: "direction_rl", need: "direction"},
	{fresh: directionMatcher("LR"), kind: "direction_lr", need: "direction"},
}

var (
	stateSkipLineSpace = r(stateLineSpace, "")
	stateHashComment   = r(`#[^\n]*`, "")
	statePctComment    = lexRule{match: stateComment}
	stateDescr         = lexRule{re: lexRE(`\s*:(?:[^:\n;]|:[^:\n;])+`), kind: "DESCR", trim: true}
)

var stateRules = map[string][]lexRule{
	"INITIAL": rules([]lexRule{
		r(`click\b`, "CLICK"),
		r(`href\b`, "HREF"),
		r(`"[^"]*"`, "STRING"),
		r(`default\b`, "DEFAULT"),
	}, stateDirections, []lexRule{
		r(`[\n]+`, "NL"),
		r(`\s+`, ""),
		stateHashComment,
		statePctComment,
		{re: lexRE(`scale\s+`), kind: "scale", push: "SCALE"},
		{re: lexRE(`accTitle\s*:\s*`), kind: "acc_title", push: "acc_title"},
		{re: lexRE(`accDescr\s*:\s*`), kind: "acc_descr", push: "acc_descr"},
		{re: lexRE(`accDescr\s*\{\s*`), push: "acc_descr_multiline"},
		{re: lexRE(`classDef\s+`), kind: "classDef", push: "CLASSDEF"},
		{re: lexRE(`class\s+`), kind: "class", push: "CLASS"},
		{re: lexRE(`style\s+`), kind: "style", push: "STYLE"},
		{re: lexRE(`state\s+`), push: "STATE"},
		{re: lexRE(`\{`), kind: "STRUCT_START", pop: 1, push: "struct"},
		{re: lexRE(`note\s+`), kind: "note", push: "NOTE"},
		r(`stateDiagram\s+`, "SD"),
		r(`stateDiagram-v2\s+`, "SD"),
		r(`hide empty description\b`, "HIDE_EMPTY"),
		r(`\[\*\]`, "EDGE_STATE"),
		{match: stateIDSkip},
		{match: stateIDMatch, kind: "ID"},
		stateDescr,
		r(`-->`, "-->"),
		r(`:::`, "STYLE_SEPARATOR"),
		r(`(DOT)`, "INVALID"),
	}),
	"struct": rules([]lexRule{
		stateSkipLineSpace,
		stateHashComment,
		statePctComment,
		{re: lexRE(`classDef\s+`), kind: "classDef", push: "CLASSDEF"},
		{re: lexRE(`class\s+`), kind: "class", push: "CLASS"},
		{re: lexRE(`style\s+`), kind: "style", push: "STYLE"},
		{re: lexRE(`state\s+`), push: "STATE"},
	}, stateDirections, []lexRule{
		{re: lexRE(`\}`), kind: "STRUCT_STOP", pop: 1},
		r(`[\n]`, ""),
		{re: lexRE(`note\s+`), kind: "note", push: "NOTE"},
		r(`\[\*\]`, "EDGE_STATE"),
		{match: stateIDSkip},
		{match: stateIDMatch, kind: "ID"},
		stateDescr,
		r(`-->`, "-->"),
		r(`--`, "CONCURRENT"),
		r(`:::`, "STYLE_SEPARATOR"),
	}),
	"STATE": {
		stateSkipLineSpace,
		stateHashComment,
		statePctComment,
		stateMarker("<<fork>>", "FORK"),
		stateMarker("<<join>>", "JOIN"),
		stateMarker("<<choice>>", "CHOICE"),
		stateMarker("[[fork]]", "FORK"),
		stateMarker("[[join]]", "JOIN"),
		stateMarker("[[choice]]", "CHOICE"),
		{re: lexRE(`"`), push: "STATE_STRING"},
		{re: lexRE(`\s*as\s+`), kind: "AS", push: "STATE_ID"},
		// jison throws here: "State name must be a single word".
		{fresh: twoWordsMatcher, kind: "STATE_NAME_ERROR"},
		r(stateCompClass, "COMPOSIT_STATE"),
		{re: lexRE(`\n`), pop: 1},
		{re: lexRE(`\{`), kind: "STRUCT_START", pop: 1, push: "struct"},
	},
	"STATE_STRING": {
		{re: lexRE(`"`), pop: 1},
		r(`[^"]*`, "STATE_DESCR"),
	},
	"STATE_ID": {
		{match: stateAsIDSkip},
		{match: stateAsIDMatch, kind: "ID", pop: 1},
	},
	"SCALE": {
		r(`\d+`, "WIDTH"),
		{re: lexRE(`\s+width\b`), pop: 1},
	},
	"acc_title": {
		{re: lexRE(`[^\n]*`), kind: "acc_title_value", pop: 1},
	},
	"acc_descr": {
		{re: lexRE(`[^\n]*`), kind: "acc_descr_value", pop: 1},
	},
	"acc_descr_multiline": {
		{re: lexRE(`\}`), pop: 1},
		r(`[^\}]*`, "acc_descr_multiline_value"),
	},
	"CLASSDEF": {
		{re: lexRE(`DEFAULT\s+`), kind: "DEFAULT_CLASSDEF_ID", pop: 1, push: "CLASSDEFID"},
		{re: lexRE(`\w+\s+`), kind: "CLASSDEF_ID", pop: 1, push: "CLASSDEFID"},
	},
	"CLASSDEFID": {
		{re: lexRE(`[^\n]*`), kind: "CLASSDEF_STYLEOPTS", pop: 1},
	},
	"CLASS": {
		{re: lexRE(`(\w+)+((,\s*\w+)*)`), kind: "CLASSENTITY_IDS", pop: 1, push: "CLASS_STYLE"},
	},
	"CLASS_STYLE": {
		{re: lexRE(`[^\n]*`), kind: "STYLECLASS", pop: 1},
	},
	"STYLE": {
		{re: lexRE(`[\w,]+\s+`), kind: "STYLE_IDS", pop: 1, push: "STYLEDEF_STYLES"},
	},
	"STYLEDEF_STYLES": {
		{re: lexRE(`[^\n]*`), kind: "STYLEDEF_STYLEOPTS", pop: 1},
	},
	"NOTE": {
		{re: lexRE(`left of\b`), kind: "left_of", pop: 1, push: "NOTE_ID"},
		{re: lexRE(`right of\b`), kind: "right_of", pop: 1, push: "NOTE_ID"},
		{re: lexRE(`"`), pop: 1, push: "FLOATING_NOTE"},
	},
	"FLOATING_NOTE": {
		{re: lexRE(`\s*as\s*`), kind: "AS", pop: 1, push: "FLOATING_NOTE_ID"},
		r(`"`, ""),
		r(`[^"]*`, "NOTE_TEXT"),
	},
	"FLOATING_NOTE_ID": {
		{match: stateFloatSkip},
		{match: stateFloatIDMatch, kind: "ID", pop: 1},
	},
	"NOTE_ID": {
		{match: stateNoteIDSkip},
		{match: stateNoteIDMatch, kind: "ID", pop: 1, push: "NOTE_TEXT"},
	},
	"NOTE_TEXT": {
		// yytext.substr(2).trim(): the first two characters go, whatever
		// they are.
		{re: lexRE(`\s*:[^:\n;]+`), kind: "NOTE_TEXT", pop: 1, drop: 2, trim: true},
		{re: lexRE(`(?s:.)*?\n\s*end note\b`), kind: "NOTE_TEXT", pop: 1, cut: len("end note"), trim: true},
	},
}

func rules(parts ...[]lexRule) []lexRule {
	var out []lexRule
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// lexState turns the prepared lines into tokens. mermaid parses the source
// with a line end added (Diagram.fromText), so "stateDiagram" alone is a
// header and an empty diagram.
func lexState(lines []srcLine) ([]erToken, error) {
	last := 0
	if len(lines) > 0 {
		last = lines[len(lines)-1].no
	}
	return runLexer(append(lines[:len(lines):len(lines)], srcLine{text: "", no: last}), stateRules, "NL")
}
