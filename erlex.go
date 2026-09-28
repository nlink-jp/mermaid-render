package mermaidrender

import (
	"regexp"
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
	re   *regexp.Regexp
	kind string // "" skips the match
	// push / pop change the lexer state.
	push string
	pop  bool
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
		{re: lexRE(`(DOT)*direction\s+TB[^\n]*`), kind: "direction_tb", need: "direction"},
		{re: lexRE(`(DOT)*direction\s+BT[^\n]*`), kind: "direction_bt", need: "direction"},
		{re: lexRE(`(DOT)*direction\s+RL[^\n]*`), kind: "direction_rl", need: "direction"},
		{re: lexRE(`(DOT)*direction\s+LR[^\n]*`), kind: "direction_lr", need: "direction"},
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
		{re: lexRE(`((NOTSPACE)*)[~](DOT)*[~]((NOTSPACE)*)`), kind: "ATTRIBUTE_WORD", need: "~"},
		r(`[\*A-Za-z_\x{C0}-\x{10FFFF}][A-Za-z0-9\-_\[\]\(\)\.,\x{C0}-\x{10FFFF}\*]*`, "ATTRIBUTE_WORD"),
		{re: lexRE("`"), push: "block_bq"},
		r(`"[^"]*"`, "COMMENT"),
		r(`[\n]+`, ""),
		{re: lexRE(`\}`), kind: "BLOCK_STOP", pop: true},
		r(`(DOT)`, "."),
	},
	"block_bq": {
		r("[^`]+", "ATTRIBUTE_WORD"),
		{re: lexRE("`"), pop: true},
	},
	"style": {
		{re: lexRE(`[\n]+`), kind: "NEWLINE", pop: true},
		r(`\s+`, ""),
		r(`:`, "COLON"),
		r(`,`, "COMMA"),
		r(`#`, "BRKT"),
		r(`([^\x00-\x7F]|\w|-|\*)+`, "STYLE_TEXT"),
		r(`;`, "SEMI"),
	},
	"acc_title": {
		{re: lexRE(`[^\n]*`), kind: "acc_title_value", pop: true},
	},
	"acc_descr": {
		{re: lexRE(`[^\n]*`), kind: "acc_descr_value", pop: true},
	},
	"acc_descr_multiline": {
		{re: lexRE(`\}`), pop: true},
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
	var out []erToken
	state := []string{"INITIAL"}
	pos := 0
	for pos < len(src) {
		cur := state[len(state)-1]
		rest := src[pos:]
		matched := false
		for _, rl := range erRules[cur] {
			if rl.need != "" && !strings.Contains(lineLower(rest), rl.need) {
				continue
			}
			loc := rl.re.FindStringIndex(rest)
			if loc == nil {
				continue
			}
			n := loc[1]
			if rl.look != nil && !rl.look(rest[n:]) {
				continue
			}
			if n == 0 && rl.kind == "" && rl.push == "" && !rl.pop {
				continue // an empty skip would never advance
			}
			text := rest[:n]
			if rl.kind != "" {
				kind := rl.kind
				if kind == "." {
					kind = text
				}
				out = append(out, erToken{kind: kind, text: text, line: lineOf()})
			}
			if rl.push != "" {
				state = append(state, rl.push)
			}
			if rl.pop && len(state) > 1 {
				state = state[:len(state)-1]
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
	out = append(out, erToken{kind: "EOF", line: lines[len(lines)-1].no})
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
