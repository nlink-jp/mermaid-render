package mermaidrender

import (
	"regexp"
	"sort"
	"strings"
)

// The erDiagram lexer follows erDiagram.jison (mermaid 12.0.0) rule by
// rule, in its order. A jison lexer takes the first rule that matches (not
// the longest), matches case-insensitively here (%options case-insensitive),
// and appends \b to a rule that ends in a word character. So keywords win
// over names ("one", "to", "end", "class", "style" are never names), and
// what looks like one name may lex as several tokens: the grammar decides
// what that means. Following the lexer, not the documentation's prose, is
// what makes this parser agree with mermaid on the edge cases.

// JavaScript's \s and . differ from Go's: \s takes the Unicode spaces too,
// and . stops at every line terminator.
const (
	jsSpace    = `\t\n\v\f\r \x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}`
	jsDot      = `[^\n\r\x{2028}\x{2029}]`
	jsNotSpace = `[^` + jsSpace + `]`
)

func lexRE(s string) *regexp.Regexp {
	s = strings.ReplaceAll(s, `\s`, `[`+jsSpace+`]`)
	s = strings.ReplaceAll(s, `(DOT)`, jsDot)
	s = strings.ReplaceAll(s, `(NOTSPACE)`, jsNotSpace)
	return regexp.MustCompile(`(?i)^(?:` + s + `)`)
}

type erToken struct {
	kind string // the jison token name, or the character itself
	text string
	line int
}

type lexRule struct {
	re *regexp.Regexp
	// match, when set instead of re, is a hand-written matcher: the
	// length it matches at the start of rest, or -1.
	match func(rest string) int
	kind  string // "" skips the match; "." is the character itself
	// pop leaves that many states, then push enters one.
	pop  int
	push string
	trim bool // the token's text is trimmed (yytext.trim())
	// fresh, when set, makes the rule's matcher for one run: a matcher
	// that caches what it learns about a line (see lineMatchers).
	fresh func() func(rest string) int
	// look is a hand-written lookahead (Go's regexp has none): the text
	// after the match must satisfy it.
	look func(rest string) bool
	// need: the rule is tried only on a line holding this text (lower
	// case). The rules that run to the end of the line would otherwise
	// make lexing a long line quadratic.
	need string
}

var (
	reLookLetter = lexRE(`\s+[A-Za-z_"']`)
	reLookDigit  = lexRE(`\s+[0-9]`)
)

func r(re, kind string) lexRule { return lexRule{re: lexRE(re), kind: kind} }

var erRules = map[string][]lexRule{
	"INITIAL": {
		{re: lexRE(`accTitle\s*:\s*`), kind: "acc_title", push: "acc_title"},
		{re: lexRE(`accDescr\s*:\s*`), kind: "acc_descr", push: "acc_descr"},
		{re: lexRE(`accDescr\s*\{\s*`), push: "acc_descr_multiline"},
		{fresh: directionMatcher("TB"), kind: "direction_tb", need: "direction"},
		{fresh: directionMatcher("BT"), kind: "direction_bt", need: "direction"},
		{fresh: directionMatcher("RL"), kind: "direction_rl", need: "direction"},
		{fresh: directionMatcher("LR"), kind: "direction_lr", need: "direction"},
		r(`[ \t\r]+`, ""),
		r(`[\n]+`, "NEWLINE"),
		r(`"[^"%\r\n\v\x08\\]+"`, "ENTITY_NAME"),
		r(`"[^"]*"`, "WORD"),
		r(`erDiagram\b`, "ER_DIAGRAM"),
		{re: lexRE(`\{`), kind: "BLOCK_START", push: "block"},
		r(`#`, "BRKT"),
		r(`,`, "COMMA"),
		r(`:::`, "STYLE_SEPARATOR"),
		r(`:`, "COLON"),
		r(`\[`, "SQS"),
		r(`\]`, "SQE"),
		{re: lexRE(`style\b`), kind: "STYLE", push: "style"},
		{re: lexRE(`classDef\b`), kind: "CLASSDEF", push: "style"},
		r(`class\b`, "CLASS"),
		r(`subgraph\b`, "SUBGRAPH"),
		r(`end\b\s*`, "END"),
		r(`one or zero\b`, "ZERO_OR_ONE"),
		r(`one or more\b`, "ONE_OR_MORE"),
		r(`one or many\b`, "ONE_OR_MORE"),
		r(`1\+`, "ONE_OR_MORE"),
		r(`\|o\b`, "ZERO_OR_ONE"),
		r(`zero or one\b`, "ZERO_OR_ONE"),
		r(`zero or more\b`, "ZERO_OR_MORE"),
		r(`zero or many\b`, "ZERO_OR_MORE"),
		r(`0\+`, "ZERO_OR_MORE"),
		r(`\}o\b`, "ZERO_OR_MORE"),
		r(`many\(0\)`, "ZERO_OR_MORE"),
		r(`many\(1\)`, "ONE_OR_MORE"),
		r(`many\b`, "ZERO_OR_MORE"),
		r(`\}\|`, "ONE_OR_MORE"),
		r(`one\b`, "ONLY_ONE"),
		r(`only one\b`, "ONLY_ONE"),
		r(`[0-9]+\.[0-9]+`, "DECIMAL_NUM"),
		{re: lexRE(`1`), kind: "ONLY_ONE", look: func(s string) bool { return reLookLetter.MatchString(s) }},
		{re: lexRE(`1`), kind: "ONLY_ONE", look: func(s string) bool { return reLookDigit.MatchString(s) }},
		{re: lexRE(`1`), kind: "ONLY_ONE", look: func(s string) bool {
			return strings.HasPrefix(s, "--") || strings.HasPrefix(s, "..") || strings.HasPrefix(s, ".-") || strings.HasPrefix(s, "-.")
		}},
		r(`1\b`, "ENTITY_ONE"),
		r(`[0-9]+`, "NUM"),
		r(`\|\|`, "ONLY_ONE"),
		r(`o\|`, "ZERO_OR_ONE"),
		r(`o\{`, "ZERO_OR_MORE"),
		r(`\|\{`, "ONE_OR_MORE"),
		{re: lexRE(`u`), kind: "MD_PARENT", look: func(s string) bool {
			return s != "" && strings.ContainsRune(".-|", rune(s[0]))
		}},
		r(`\.\.`, "NON_IDENTIFYING"),
		r(`--`, "IDENTIFYING"),
		r(`to\b`, "IDENTIFYING"),
		r(`optionally to\b`, "NON_IDENTIFYING"),
		r(`\.-`, "NON_IDENTIFYING"),
		r(`-\.`, "NON_IDENTIFYING"),
		r(`([^\x00-\x7F]|\w|-|\*|\.)+`, "UNICODE_TEXT"),
		r(`(DOT)`, "."), // the character itself
	},
	"block": {
		r(`\s+`, ""),
		r(`\b(PK|FK|UK)\b`, "ATTRIBUTE_KEY"),
		{fresh: tildeMatcher, kind: "ATTRIBUTE_WORD", need: "~"},
		r(`[\*A-Za-z_\x{C0}-\x{10FFFF}][A-Za-z0-9\-_\[\]\(\)\.,\x{C0}-\x{10FFFF}\*]*`, "ATTRIBUTE_WORD"),
		{re: lexRE("`"), push: "block_bq"},
		r(`"[^"]*"`, "COMMENT"),
		r(`[\n]+`, ""),
		{re: lexRE(`\}`), kind: "BLOCK_STOP", pop: 1},
		r(`(DOT)`, "."),
	},
	"block_bq": {
		r("[^`]+", "ATTRIBUTE_WORD"),
		{re: lexRE("`"), pop: 1},
	},
	"style": {
		{re: lexRE(`[\n]+`), kind: "NEWLINE", pop: 1},
		r(`\s+`, ""),
		r(`:`, "COLON"),
		r(`,`, "COMMA"),
		r(`#`, "BRKT"),
		r(`([^\x00-\x7F]|\w|-|\*)+`, "STYLE_TEXT"),
		r(`;`, "SEMI"),
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

// Mermaid replaces entity codes before any diagram is parsed
// (utils.encodeEntities), so "#quot;" lexes as non-ASCII text, and puts
// them back when drawing. The same placeholders are used here, so a code
// is accepted exactly where mermaid accepts one (in a name, not inside an
// attribute word, whose characters stop short of ° and ¶).
var (
	reStyleSemi    = regexp.MustCompile(`style.*:\S*#.*;`)
	reClassDefSemi = regexp.MustCompile(`classDef.*:\S*#.*;`)
	reEntityCode   = regexp.MustCompile(`#\w+;`)
	reIntCode      = regexp.MustCompile(`^\+?\d+$`)
	rePlaceholder  = regexp.MustCompile(`ﬂ°°?(\+?\w+)¶ß`)
)

func encodeEntities(s string) string {
	dropSemi := func(m string) string { return m[:len(m)-1] }
	s = reStyleSemi.ReplaceAllStringFunc(s, dropSemi)
	s = reClassDefSemi.ReplaceAllStringFunc(s, dropSemi)
	return reEntityCode.ReplaceAllStringFunc(s, func(m string) string {
		inner := m[1 : len(m)-1]
		if reIntCode.MatchString(inner) {
			return "ﬂ°°" + inner + "¶ß"
		}
		return "ﬂ°" + inner + "¶ß"
	})
}

// restoreEntities turns placeholders back into the codes as written.
func restoreEntities(s string) string {
	return rePlaceholder.ReplaceAllString(s, "#$1;")
}

// lexER turns the prepared lines into tokens. Tokens carry the source line
// they start on.
func lexER(lines []srcLine) ([]erToken, error) {
	return runLexer(lines, erRules, "")
}

// runLexer runs a jison-style lexer: in the current state, the first rule
// that matches wins. At the end it emits eofKind (if any) in the INITIAL
// state, as a <<EOF>> rule would, then EOF.
func runLexer(lines []srcLine, rules map[string][]lexRule, eofKind string) ([]erToken, error) {
	texts := make([]string, len(lines))
	for i, l := range lines {
		texts[i] = l.text
	}
	// encodeEntities never adds or removes a newline, so the n-th line of
	// src is still lines[n].
	src := encodeEntities(strings.Join(texts, "\n"))
	ln := 0 // index of the line pos is on
	lineOf := func() int { return lines[min(ln, len(lines)-1)].no }
	lowLine, lowFor := "", -1
	lineLower := func(rest string) string {
		if lowFor != ln {
			end := strings.IndexByte(rest, '\n')
			if end < 0 {
				end = len(rest)
			}
			lowLine, lowFor = strings.ToLower(rest[:end]), ln
		}
		return lowLine
	}
	// Matchers that cache per line are made anew for each run.
	own := map[string][]lexRule{}
	for st, rs := range rules {
		cp := append([]lexRule(nil), rs...)
		for i := range cp {
			if cp[i].fresh != nil {
				cp[i].match = cp[i].fresh()
			}
		}
		own[st] = cp
	}
	rules = own
	var out []erToken
	state := []string{"INITIAL"}
	pos := 0
	for pos < len(src) {
		cur := state[len(state)-1]
		rest := src[pos:]
		matched := false
		for _, rl := range rules[cur] {
			if rl.need != "" && !strings.Contains(lineLower(rest), rl.need) {
				continue
			}
			n := -1
			if rl.match != nil {
				n = rl.match(rest)
			} else if loc := rl.re.FindStringIndex(rest); loc != nil {
				n = loc[1]
			}
			if n < 0 {
				continue
			}
			if rl.look != nil && !rl.look(rest[n:]) {
				continue
			}
			if n == 0 && rl.kind == "" && rl.push == "" && rl.pop == 0 {
				continue // an empty skip would never advance
			}
			text := rest[:n]
			if rl.kind != "" {
				kind := rl.kind
				if kind == "." {
					kind = text
				}
				t := text
				if rl.trim {
					t = strings.TrimSpace(t)
				}
				out = append(out, erToken{kind: kind, text: t, line: lineOf()})
			}
			for range rl.pop {
				if len(state) > 1 {
					state = state[:len(state)-1]
				}
			}
			if rl.push != "" {
				state = append(state, rl.push)
			}
			pos += n
			ln += strings.Count(text, "\n")
			matched = true
			break
		}
		if !matched {
			return nil, errf(SyntaxError, lineOf(), "unrecognized text %q", firstRunes(rest, 12))
		}
	}
	if eofKind != "" && len(state) == 1 {
		out = append(out, erToken{kind: eofKind, line: lineOf()})
	}
	out = append(out, erToken{kind: "EOF", line: lineOf()})
	return out, nil
}

func firstRunes(s string, n int) string {
	for i := range s {
		if n == 0 {
			return s[:i]
		}
		n--
	}
	return s
}

// The ER lexer's two rules that run to a line's end are, as regular
// expressions, quadratic on a long line: tried at every token, each scans
// the rest of the line (a 40 KB line took 106 s). These matchers answer the
// same question from facts gathered once per line. The lexer only moves
// forward, so a line is first met at its earliest queried position;
// positions are kept as distances to the source's end.

// directionMatcher is (DOT)*direction\s+XX[^\n]*: it matches at a position
// that has "direction", whitespace and XX (any case) later on its line, and
// takes the rest of the line.
func directionMatcher(dir string) func() func(string) int {
	occ := regexp.MustCompile(`(?i)direction[` + jsSpace + `]+` + dir)
	// "direction" that only whitespace follows to the line's end: \s+ may
	// run on across the newline to XX on a later line.
	tail := regexp.MustCompile(`(?i)direction[` + jsSpace + `]*$`)
	cross := lexRE(`direction\s+` + dir + `[^\n]*`)
	return func() func(string) int {
		var base, lineEnd int
		var starts, stops []int // byte offsets from the line's first queried position
		tailAt, crossEnd := -1, -1
		valid := false
		return func(rest string) int {
			d := len(rest)
			if !valid || d <= lineEnd {
				le := strings.IndexByte(rest, '\n')
				if le < 0 {
					le = len(rest)
				}
				base, lineEnd, valid = d, d-le, true
				starts, stops = starts[:0], stops[:0]
				for _, loc := range occ.FindAllStringIndex(rest[:le], -1) {
					starts = append(starts, loc[0])
				}
				for i, r := range rest[:le] {
					if r == '\r' || r == 0x2028 || r == 0x2029 {
						stops = append(stops, i) // (DOT)* stops here
					}
				}
				stops = append(stops, le)
				// Once per line: does the line's last "direction" run on to
				// XX on a later line, and where does that match end?
				tailAt, crossEnd = -1, -1
				if loc := tail.FindStringIndex(rest[:le]); loc != nil {
					if m := cross.FindStringIndex(rest[loc[0]:]); m != nil {
						tailAt, crossEnd = loc[0], loc[0]+m[1]
					}
				}
			}
			i0 := base - d
			next := stops[sort.SearchInts(stops, i0)]
			// (DOT)* is greedy: the last reachable "direction" decides, and
			// the one ending the line is the last.
			if tailAt >= i0 && tailAt < next {
				return crossEnd - i0
			}
			if k := sort.SearchInts(starts, i0); k < len(starts) && starts[k] < next {
				return d - lineEnd
			}
			return -1
		}
	}
}

// tildeMatcher is <block>([^\s]*)[~](DOT)*[~]([^\s]*) as JavaScript
// backtracks it: from here a run of non-spaces holding a ~ (the last one
// that still has another ~ after it on the line), the line's last ~, then
// non-spaces.
func tildeMatcher() func(string) int {
	var base, lineEnd int
	var tildes, spaces, stops []int // byte offsets from the line's first queried position
	valid := false
	return func(rest string) int {
		d := len(rest)
		if !valid || d <= lineEnd {
			le := strings.IndexByte(rest, '\n')
			if le < 0 {
				le = len(rest)
			}
			base, lineEnd, valid = d, d-le, true
			tildes, spaces, stops = tildes[:0], spaces[:0], stops[:0]
			for i, r := range rest[:le] {
				switch {
				case r == '~':
					tildes = append(tildes, i)
				case strings.ContainsRune(jsSpaceChars, r):
					spaces = append(spaces, i)
					if r == '\r' || r == 0x2028 || r == 0x2029 {
						stops = append(stops, i) // . stops here
					}
				}
			}
			spaces = append(spaces, le)
			stops = append(stops, le)
		}
		i0 := base - d
		after := func(xs []int, i int) int { return xs[sort.SearchInts(xs, i)] }
		runEnd := after(spaces, i0)
		stop := after(stops, i0)
		// b: the last ~ before the dot stops.
		bi := sort.SearchInts(tildes, stop) - 1
		if bi < 0 || tildes[bi] <= i0 {
			return -1
		}
		b := tildes[bi]
		// a: the last ~ in the run that is before b.
		lim := min(runEnd, b)
		ai := sort.SearchInts(tildes, lim) - 1
		if ai < 0 || tildes[ai] < i0 {
			return -1
		}
		return after(spaces, b+1) - i0
	}
}
