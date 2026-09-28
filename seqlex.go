package mermaidrender

import (
	"strings"
	"unicode/utf8"
)

// The sequenceDiagram lexer follows sequenceDiagram.jison (mermaid 12.0.0)
// rule by rule, in its order, with jison's behaviour (see erlex.go). Its
// exclusive states: ID after participant / actor / activate / deactivate /
// destroy (an actor name up to the line's end, an alias or a config), ALIAS
// (before "as"), LINE (the rest of a line: block titles, alias text), CONFIG
// (@{ ... }). "#" starts a comment in every state but CONFIG, so message
// text stops at "#"; entity codes are placeholders by then.

// seqArrowStarts are the texts an actor name may not continue into: the
// negative lookahead in the ACTOR rule.
var seqArrowStarts = []string{
	`-x`, `--x`, `-)`, `--)`, `-|\`, `-\`, `-/`, `-//`, `-|/`, `/|-`, `\|-`, `//-`, `\\-`, `/|-`, `--|\`, `--`, `()`,
}

// matchSeqActor is the INITIAL-state ACTOR rule:
//
//	[^\/\\\+\()\+<\->\->:\n,;]+((?!(arrows))[\-]*[^\+<\->\->:\n,;]+)*
func matchSeqActor(s string) int {
	first := func(c byte) bool { return !strings.ContainsRune("/\\+()<->:\n,;", rune(c)) }
	later := func(c byte) bool { return !strings.ContainsRune("+<->:\n,;", rune(c)) }
	i := 0
	for i < len(s) && first(s[i]) {
		i++
	}
	if i == 0 {
		return -1
	}
	for i < len(s) {
		low := strings.ToLower(s[i:])
		stop := false
		for _, a := range seqArrowStarts {
			if strings.HasPrefix(low, a) {
				stop = true
				break
			}
		}
		if stop {
			break
		}
		j := i
		for j < len(s) && s[j] == '-' {
			j++
		}
		k := j
		for k < len(s) && later(s[k]) {
			k++
		}
		if k == j {
			break
		}
		i = k
	}
	return i
}

// seqNum is the NUM rule: ([0-9]+(\.[0-9]{1,2})?|\.[0-9]{1,2})(?=[ \n]+).
var reSeqNum = lexRE(`([0-9]+(\.[0-9]{1,2})?|\.[0-9]{1,2})`)

func followedBySpaceOrNewline(rest string) bool {
	return rest != "" && (rest[0] == ' ' || rest[0] == '\n')
}

// idLook: (?=\s*[\n;#]|$).
var reIDEnd = lexRE(`\s*[\n;#]`)

func idEnd(rest string) bool { return rest == "" || reIDEnd.MatchString(rest) }

// asLook: (?=\s+as\s).
var reAsLook = lexRE(`\s+as\s`)

func asFollows(rest string) bool { return reAsLook.MatchString(rest) }

var seqRules = map[string][]lexRule{
	"INITIAL": append([]lexRule{
		r(`[\n]+`, "NEWLINE"),
		r(`\s+`, ""),
		r(`#[^\n]*`, ""),
		// \%%(?!\{)[^\n]* and [^\}]\%\%[^\n]*: comments.
		{match: func(s string) int {
			if strings.HasPrefix(s, "%%") && !strings.HasPrefix(s, "%%{") {
				return lineLen(s)
			}
			return -1
		}},
		{match: func(s string) int {
			c, n := utf8.DecodeRuneInString(s)
			if n > 0 && c != '}' && c != '\n' && strings.HasPrefix(s[n:], "%%") {
				return lineLen(s)
			}
			return -1
		}},
		{re: reSeqNum, kind: "NUM", look: followedBySpaceOrNewline},
		{re: lexRE(`box\b`), kind: "box", push: "LINE"},
		{re: lexRE(`participant\b`), kind: "participant", push: "ID"},
		{re: lexRE(`actor\b`), kind: "participant_actor", push: "ID"},
		r(`create\b`, "create"),
		{re: lexRE(`destroy\b`), kind: "destroy", push: "ID"},
		{re: lexRE(`loop\b`), kind: "loop", push: "LINE"},
		{re: lexRE(`rect\b`), kind: "rect", push: "LINE"},
		{re: lexRE(`opt\b`), kind: "opt", push: "LINE"},
		{re: lexRE(`alt\b`), kind: "alt", push: "LINE"},
		{re: lexRE(`else\b`), kind: "else", push: "LINE"},
		{re: lexRE(`par\b`), kind: "par", push: "LINE"},
		{re: lexRE(`par_over\b`), kind: "par_over", push: "LINE"},
		{re: lexRE(`and\b`), kind: "and", push: "LINE"},
		{re: lexRE(`critical\b`), kind: "critical", push: "LINE"},
		{re: lexRE(`option\b`), kind: "option", push: "LINE"},
		{re: lexRE(`break\b`), kind: "break", push: "LINE"},
		r(`end\b`, "end"),
		r(`left of\b`, "left_of"),
		r(`right of\b`, "right_of"),
		r(`links\b`, "links"),
		r(`link\b`, "link"),
		r(`properties\b`, "properties"),
		r(`details\b`, "details"),
		r(`over\b`, "over"),
		r(`note\b`, "note"),
		{re: lexRE(`activate\b`), kind: "activate", push: "ID"},
		{re: lexRE(`deactivate\b`), kind: "deactivate", push: "ID"},
		r(`title\s[^#\n;]+`, "title"),
		r(`title:\s[^#\n;]+`, "legacy_title"),
		{re: lexRE(`accTitle\s*:\s*`), kind: "acc_title", push: "acc_title"},
		{re: lexRE(`accDescr\s*:\s*`), kind: "acc_descr", push: "acc_descr"},
		{re: lexRE(`accDescr\s*\{\s*`), push: "acc_descr_multiline"},
		r(`sequenceDiagram\b`, "SD"),
		r(`autonumber\b`, "autonumber"),
		r(`off\b`, "off"),
		r(`,`, ","),
		r(`;`, "NEWLINE"),
		{match: matchSeqActor, kind: "ACTOR", trim: true},
	}, seqArrowRules...),
	"ID": {
		r(`[`+jsSpaceNoNL+`]+`, ""),
		r(`#[^\n]*`, ""),
		{re: lexRE(`@\{`), kind: "CONFIG_START", push: "CONFIG"},
		{re: lexRE(`[^<\->:\n,;@` + jsSpace + `]+`), kind: "ACTOR", trim: true, look: func(s string) bool { return strings.HasPrefix(s, "@{") }},
		{re: lexRE(`[^<>:\n,;@` + jsSpace + `]+`), kind: "ACTOR", trim: true, push: "ALIAS", look: asFollows},
		{re: lexRE(`[^<>:\n,;@]+`), kind: "ACTOR", trim: true, pop: 1, look: idEnd},
		{re: lexRE(`[^<>:\n,;@]*<[^\n]*`), kind: "INVALID", pop: 1},
		{re: lexRE(`[^\n]+`), kind: "INVALID", trim: true, pop: 1},
	},
	"ALIAS": {
		r(`[`+jsSpaceNoNL+`]+`, ""),
		r(`#[^\n]*`, ""),
		{re: lexRE(`as\b`), kind: "AS", pop: 2, push: "LINE"},
		{match: func(string) int { return 0 }, kind: "NEWLINE", pop: 2},
	},
	"LINE": {
		r(`[`+jsSpaceNoNL+`]+`, ""),
		r(`#[^\n]*`, ""),
		{re: lexRE(`(?:[:]?(?:no)?wrap:)?[^#\n;]*`), kind: "restOfLine", pop: 1},
	},
	"CONFIG": {
		r(`[^\}]+`, "CONFIG_CONTENT"),
		{re: lexRE(`\}`), kind: "CONFIG_END", pop: 1, push: "ALIAS", look: asFollows},
		{re: lexRE(`\}`), kind: "CONFIG_END", pop: 2},
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
}

// seqArrowRules are the message arrows, then text and the last INITIAL
// rules, in the grammar's order.
var seqArrowRules = []lexRule{
	r(`->>`, "SOLID_ARROW"),
	r(`<<->>`, "BIDIRECTIONAL_SOLID_ARROW"),
	r(`-->>`, "DOTTED_ARROW"),
	r(`<<-->>`, "BIDIRECTIONAL_DOTTED_ARROW"),
	r(`->`, "SOLID_OPEN_ARROW"),
	r(`-->`, "DOTTED_OPEN_ARROW"),
	r(`-[x]`, "SOLID_CROSS"),
	r(`--[x]`, "DOTTED_CROSS"),
	r(`-[\)]`, "SOLID_POINT"),
	r(`--[\)]`, "DOTTED_POINT"),
	r(`--\|\\`, "HALF_ARROW"),
	r(`--\|/`, "HALF_ARROW"),
	r(`--\\\\`, "HALF_ARROW"),
	r(`--//`, "HALF_ARROW"),
	r(`/\|--`, "HALF_ARROW"),
	r(`\\\|--`, "HALF_ARROW"),
	r(`//--`, "HALF_ARROW"),
	r(`\\\\--`, "HALF_ARROW"),
	r(`-\|\\`, "HALF_ARROW"),
	r(`-\|/`, "HALF_ARROW"),
	r(`-\\\\`, "HALF_ARROW"),
	r(`-//`, "HALF_ARROW"),
	r(`/\|-`, "HALF_ARROW"),
	r(`\\\|-`, "HALF_ARROW"),
	r(`//-`, "HALF_ARROW"),
	r(`\\\\-`, "HALF_ARROW"),
	r(`:(?:(?:no)?wrap:)?[^#\n;]*`, "TXT"),
	r(`:`, "TXT"),
	r(`\+`, "+"),
	r(`-`, "-"),
	r(`\(\)`, "()"),
	r(`(DOT)`, "INVALID"),
}

func lineLen(s string) int {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return i
	}
	return len(s)
}

// jsSpaceNoNL is JavaScript's \s without the newline: ((?!\n)\s).
const jsSpaceNoNL = `\t\v\f\r \x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}`

// lexSequence lexes the prepared lines. The source is given a final
// newline, as a source file ends in one: NUM needs a space or newline after
// it, and "autonumber 1 2" is often the last word on its line.
func lexSequence(lines []srcLine) ([]erToken, error) {
	lines = append(append([]srcLine(nil), lines...), srcLine{"", lines[len(lines)-1].no})
	return runLexer(lines, seqRules, "NEWLINE")
}
