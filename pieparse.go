package mermaidrender

import (
	"errors"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// The pie grammar is Langium's (pie.langium and common.langium, mermaid
// 12.0.0), lexed by Chevrotain: at each position the token types are tried
// in the order Langium builds them — whitespace, then the keywords longest
// first ("showData", "pie", ":"), then the terminals in grammar order — and
// the first that matches is taken. AbstractMermaidTokenBuilder lets
// nothing but a space, a line end or "%%" follow a keyword ("pie
// showDatatitle T" does not lex), checked after the match.
var pieTokens = []struct {
	kind string
	re   *regexp.Regexp
}{
	{"ws", regexp.MustCompile(`^[\t ]+`)},
	{"showData", regexp.MustCompile(`^showData`)},
	{"pie", regexp.MustCompile(`^pie`)},
	{":", regexp.MustCompile(`^:`)},
	// FLOAT_PIE and INT_PIE: (?!\.) after the number.
	{"number", regexp.MustCompile(`^-?[0-9]+\.[0-9]+`)},
	{"number", regexp.MustCompile(`^-?(?:0|[1-9][0-9]*)`)},
	{"accDescr", regexp.MustCompile(`^accDescr(?:[\t ]*:.*|\s*\{[^}]*\})`)},
	{"accTitle", regexp.MustCompile(`^accTitle[\t ]*:.*`)},
	{"title", regexp.MustCompile(`^title(?:[\t ].*|)`)},
	{"string", regexp.MustCompile(`^(?:"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*')`)},
	{"comment", regexp.MustCompile(`^%%.*`)},
}

type pieToken struct {
	kind, text string
}

// lexPie splits one line into tokens, dropping whitespace and comments.
func lexPie(s string, no int) ([]pieToken, error) {
	var out []pieToken
	for s != "" {
		matched := false
		for _, t := range pieTokens {
			m := t.re.FindString(s)
			if m == "" {
				continue
			}
			switch t.kind {
			case "pie", "showData":
				if rest := s[len(m):]; rest != "" && !strings.HasPrefix(rest, "%%") && rest[0] != ' ' && rest[0] != '\t' {
					continue
				}
			case "number":
				// (?!\.): a number followed by '.' is not this token.
				if strings.HasPrefix(s[len(m):], ".") {
					continue
				}
			case "accDescr", "accTitle", "title":
				// The single-line forms stop before a comment
				// ([^\n\r]*?(?=%%)); accDescr's { … } form does not.
				braces := t.kind == "accDescr" && !strings.HasPrefix(strings.TrimLeft(m[len("accDescr"):], "\t "), ":")
				if i := strings.Index(m, "%%"); i >= 0 && !braces {
					m = m[:i]
				}
			}
			if t.kind != "ws" && t.kind != "comment" {
				out = append(out, pieToken{t.kind, m})
			}
			s = s[len(m):]
			matched = true
			break
		}
		if !matched {
			return nil, errf(SyntaxError, no, "unexpected %q", firstRuneOrWord(s))
		}
	}
	return out, nil
}

func firstRuneOrWord(s string) string {
	if i := strings.IndexAny(s, " \t"); i > 0 {
		return s[:i]
	}
	return s
}

func parsePie(lines []srcLine, front frontTitle) (*Pie, error) {
	lines, err := joinAccDescr(lines)
	if err != nil {
		return nil, err
	}
	p := &Pie{title: front.text, titleLine: front.line}
	seen := map[string]bool{}
	sum := 0.0
	for k, ln := range lines {
		toks, err := lexPie(ln.text, ln.no)
		if err != nil {
			return nil, err
		}
		if k == 0 {
			// "pie" showData? — the rest of the line is the first statement.
			if len(toks) == 0 || toks[0].kind != "pie" {
				return nil, errf(SyntaxError, ln.no, "a pie chart starts with pie")
			}
			toks = toks[1:]
			if len(toks) > 0 && toks[0].kind == "showData" {
				p.ShowData = true
				toks = toks[1:]
			}
			if len(toks) == 0 {
				continue
			}
		}
		switch toks[0].kind {
		case "title":
			if len(toks) != 1 {
				return nil, errf(SyntaxError, ln.no, "a title ends its line")
			}
			// titleRegex: title([\t ][^\n\r]*|), trimmed, runs of blanks as
			// one; populateCommonDb sets only a title that is not empty.
			if v := collapseBlanks(strings.TrimSpace(strings.TrimPrefix(toks[0].text, "title"))); v != "" {
				p.title, p.titleLine = decodeEntitiesOnly(v), ln.no
			}
		case "accTitle", "accDescr":
			if len(toks) != 1 {
				return nil, errf(SyntaxError, ln.no, "%s ends its line", toks[0].kind)
			}
		case "string":
			if len(toks) != 3 || toks[1].kind != ":" || toks[2].kind != "number" {
				return nil, errf(SyntaxError, ln.no, "a pie item is \"label\" : number")
			}
			label := convertLangiumString(toks[0].text)
			// Number(): digits past a float's range are Infinity, which the
			// total check below refuses.
			v, err := strconv.ParseFloat(toks[2].text, 64)
			if err != nil && !errors.Is(err, strconv.ErrRange) {
				return nil, errf(SyntaxError, ln.no, "%q is not a number", toks[2].text)
			}
			if v < 0 {
				return nil, errf(SyntaxError, ln.no, "%q has the negative value %s", label, toks[2].text)
			}
			if seen[label] {
				continue // pieDb keeps the first value
			}
			seen[label] = true
			if len(p.Slices) == MaxSlices {
				return nil, errf(UnsupportedConstruct, ln.no, "more than %d pie items", MaxSlices)
			}
			sum += v
			if math.IsInf(sum, 0) {
				return nil, errf(UnsupportedConstruct, ln.no, "the values add up past what a number holds")
			}
			// encodeEntities runs on every diagram's source and the SVG is
			// decoded (Diagram.fromText, mermaidAPI.ts): #amp; is &.
			p.Slices = append(p.Slices, Slice{Label: svgText(decodeEntitiesOnly(label)), Value: v, Text: jsNumber(v), Line: ln.no})
		default:
			return nil, errf(SyntaxError, ln.no, "unexpected %q", toks[0].text)
		}
	}
	return p, nil
}

// convertLangiumString is Langium's ValueConverter.convertString (4.2.1):
// the quotes dropped, and a backslash escape undone — b f n r t v 0 name
// controls, anything else is itself.
func convertLangiumString(q string) string {
	var b strings.Builder
	rs := []rune(q)
	for i := 1; i < len(rs)-1; i++ {
		c := rs[i]
		if c != '\\' {
			b.WriteRune(c)
			continue
		}
		i++
		switch rs[i] {
		case 'b':
			b.WriteByte('\b')
		case 'f':
			b.WriteByte('\f')
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		case 'v':
			b.WriteByte('\v')
		case '0':
			b.WriteByte(0)
		default:
			b.WriteRune(rs[i])
		}
	}
	return b.String()
}

// svgText is a label as an SVG text element shows it (xml:space default):
// newlines removed, tabs as spaces, the ends trimmed and runs of spaces as
// one. Other controls do not show and are dropped.
func svgText(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\n' || r == '\r':
		case r == '\t':
			b.WriteByte(' ')
		case r < 0x20 || r == 0x7f:
		default:
			b.WriteRune(r)
		}
	}
	return collapseBlanks(strings.TrimSpace(b.String()))
}

// collapseBlanks turns each run of spaces and tabs into one space.
func collapseBlanks(s string) string {
	return blanksRe.ReplaceAllString(s, " ")
}

var blanksRe = regexp.MustCompile(`[\t ]{2,}`)

// jsNumber is a number as JavaScript's String() prints it: shortest
// round-trip digits, an exponent from 1e21 up and below 1e-6 ("1e-7",
// "1e+21", no leading zero in it), and -0 as "0".
func jsNumber(v float64) string {
	if v == 0 {
		return "0"
	}
	if a := math.Abs(v); a >= 1e21 || a < 1e-6 {
		s := strconv.FormatFloat(v, 'e', -1, 64) // 1e-07, 1e+21
		s = strings.Replace(s, "e-0", "e-", 1)
		return strings.Replace(s, "e+0", "e+", 1)
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// joinAccDescr puts an accDescr { ... } block that runs over lines back on
// one line, as pie.langium's ACC_DESCR (\s*{[^}]*}) reads it across line
// ends — "accDescr" alone with its brace on the next line too. Whatever
// follows the closing brace stays on that line, where the grammar wants
// its end.
func joinAccDescr(lines []srcLine) ([]srcLine, error) {
	var out []srcLine
	for i := 0; i < len(lines); i++ {
		ln := lines[i]
		for {
			j := strings.LastIndex(ln.text, "accDescr")
			if j < 0 || strings.HasPrefix(strings.TrimLeft(ln.text[j+len("accDescr"):], " \t"), ":") {
				break
			}
			tail := ln.text[j+len("accDescr"):]
			open := strings.Index(tail, "{")
			if open >= 0 && strings.Contains(tail[open:], "}") {
				break // closed on this line
			}
			if open < 0 && strings.TrimSpace(tail) != "" {
				break // not a block; the lexer says what it is
			}
			if i+1 >= len(lines) {
				return nil, errf(SyntaxError, ln.no, "accDescr block is not closed with }")
			}
			i++
			ln.text += "\n" + lines[i].text
		}
		out = append(out, ln)
	}
	return out, nil
}
