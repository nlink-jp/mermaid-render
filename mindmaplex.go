package mermaidrender

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// The mindmap lexer follows mindmap.jison (mermaid 12.0.0) rule by rule, in
// its order, with jison's behaviour (see erlex.go). A line's indentation is
// a SPACELIST token whose length is the node's level; a node is a NODE_ID,
// or a delimited label in the exclusive NODE state, optionally after one.
var mindmapRules = map[string][]lexRule{
	"INITIAL": {
		r(`\s*%%(DOT)*`, "SPACELINE"),
		r(`mindmap\b`, "MINDMAP"),
		{re: lexRE(`:::`), push: "CLASS"},
		{re: lexRE(`::icon\(`), push: "ICON"},
		r(`\s+\n`, "SPACELINE"), // [\s]+[\n]
		r(`\n+`, "NL"),
		{re: lexRE(`-\)`), kind: "NODE_DSTART", push: "NODE"},
		{re: lexRE(`\(-`), kind: "NODE_DSTART", push: "NODE"},
		{re: lexRE(`\)\)`), kind: "NODE_DSTART", push: "NODE"},
		{re: lexRE(`\)`), kind: "NODE_DSTART", push: "NODE"},
		{re: lexRE(`\(\(`), kind: "NODE_DSTART", push: "NODE"},
		{re: lexRE(`\{\{`), kind: "NODE_DSTART", push: "NODE"},
		{re: lexRE(`\(`), kind: "NODE_DSTART", push: "NODE"},
		{re: lexRE(`\[`), kind: "NODE_DSTART", push: "NODE"},
		r(`\s+`, "SPACELIST"),
		r(`[^\(\[\n\)\{\}]+`, "NODE_ID"),
	},
	"CLASS": {
		{re: lexRE(`(DOT)+`), kind: "CLASS", pop: 1},
		{re: lexRE(`\n`), pop: 1},
	},
	"ICON": {
		r(`[^\)]+`, "ICON"),
		{re: lexRE(`\)`), pop: 1},
	},
	"NODE": {
		{re: lexRE("[\"][`]"), push: "NSTR2"},
		{re: lexRE(`["]`), push: "NSTR"},
		{re: lexRE(`[\)]\)`), kind: "NODE_DEND", pop: 1},
		{re: lexRE(`[\)]`), kind: "NODE_DEND", pop: 1},
		{re: lexRE(`[\]]`), kind: "NODE_DEND", pop: 1},
		{re: lexRE(`\}\}`), kind: "NODE_DEND", pop: 1},
		{re: lexRE(`\(-`), kind: "NODE_DEND", pop: 1},
		{re: lexRE(`-\)`), kind: "NODE_DEND", pop: 1},
		{re: lexRE(`\(\(`), kind: "NODE_DEND", pop: 1},
		{re: lexRE(`\(`), kind: "NODE_DEND", pop: 1},
		r(`[^\)\]\(\}]+`, "NODE_DESCR"),
		// .+(?!\(\()  — .+ runs to the line's end, where "((" cannot follow.
		r(`(DOT)+`, "NODE_DESCR"),
	},
	"NSTR2": {
		r("[^`\"]+", "NODE_DESCR"),
		{re: lexRE("[`][\"]"), pop: 1},
	},
	"NSTR": {
		r(`[^"]+`, "NODE_DESCR"),
		{re: lexRE(`["]`), pop: 1},
	},
}

// mindmapLines prepares a mind map's source as mermaid does before its
// parser sees it (preprocessDiagram, mermaid 12.0.0), keeping what the
// grammar reads: indentation, blank lines and trailing spaces. The steps
// work on the text, as mermaid's regular expressions do, each byte keeping
// its source line:
//   - removeDirectives: directiveRegex, wherever it matches (directiveEnd);
//   - cleanupComments: /^\s*%%(?!{)[^\n]+\n?/gm, which takes the
//     whitespace-only lines before a comment line with it (\s* crosses
//     line ends), then trimStart.
func mindmapLines(src string) ([]srcLine, frontTitle, error) {
	raw, i, title, err := frontMatter(src)
	if err != nil {
		return nil, title, err
	}
	var t lineText
	for k := i; k < len(raw); k++ {
		if k > i {
			t.add("\n", k)
		}
		t.add(raw[k], k+1)
	}
	var cuts [][]int
	for p := strings.Index(t.s, "%%{"); p >= 0; {
		next := p + 1
		if end := directiveEnd(t.s, p); end > p {
			cuts = append(cuts, []int{p, end})
			next = end
		}
		q := strings.Index(t.s[next:], "%%{")
		if q < 0 {
			break
		}
		p = next + q
	}
	t = t.cut(cuts)
	t = t.cut(reCommentLine.FindAllStringIndex(t.s, -1))
	start := len(t.s) - len(strings.TrimLeft(t.s, jsSpaceChars))
	t = t.cut([][]int{{0, start}})
	var lines []srcLine
	if t.s == "" {
		return nil, title, nil
	}
	from := 0
	for k, text := range strings.Split(t.s, "\n") {
		no := t.line[min(from, len(t.line)-1)]
		if k > 0 && from == len(t.s) {
			no = t.line[from-1] // the empty line after a final line end
		}
		lines = append(lines, srcLine{text: text, no: no})
		from += len(text) + 1
	}
	return lines, title, nil
}

// reCommentLine is cleanupComments' expression, \s as JavaScript's.
var reCommentLine = regexp.MustCompile(`(?m)^[` + jsSpace + `]*%%[^{\n][^\n]*\n?`)

// lineText is text whose every byte knows its source line.
type lineText struct {
	s    string
	line []int
}

func (t *lineText) add(s string, no int) {
	t.s += s
	for range len(s) {
		t.line = append(t.line, no)
	}
}

// cut removes the byte ranges, which are in order and do not overlap.
func (t lineText) cut(spans [][]int) lineText {
	var out lineText
	last := 0
	var b strings.Builder
	for _, c := range spans {
		b.WriteString(t.s[last:c[0]])
		out.line = append(out.line, t.line[last:c[0]]...)
		last = c[1]
	}
	b.WriteString(t.s[last:])
	out.line = append(out.line, t.line[last:]...)
	out.s = b.String()
	return out
}

// directiveEnd is where mermaid's directiveRegex, matched at p, ends, or
// p when it does not match there:
//
//	%{2}{\s*(?:(\w+)\s*:|(\w+))\s*(?:(\w+)|((?:(?!}%{2}).|\r?\n)*))?\s*(?:}%{2})?
//
// A word must follow the brace, and the closing }%% is optional: without
// it the directive runs to a line separator or the end of the text.
func directiveEnd(s string, p int) int {
	space := func(q int) int {
		for q < len(s) {
			r, n := utf8.DecodeRuneInString(s[q:])
			if !strings.ContainsRune(jsSpaceChars, r) {
				break
			}
			q += n
		}
		return q
	}
	word := func(q int) int {
		for q < len(s) && (s[q] == '_' || s[q] >= '0' && s[q] <= '9' || s[q]|0x20 >= 'a' && s[q]|0x20 <= 'z') {
			q++
		}
		return q
	}
	q := space(p + 3)
	w := word(q)
	if w == q {
		return p
	}
	q = w
	if r := space(w); r < len(s) && s[r] == ':' {
		q = r + 1
	}
	q = space(q)
	if w := word(q); w > q {
		q = w
	} else {
		for q < len(s) && !strings.HasPrefix(s[q:], "}%%") {
			r, n := utf8.DecodeRuneInString(s[q:])
			if r == '\r' || r == '\u2028' || r == '\u2029' {
				break // . stops at every line terminator; \r?\n takes \n
			}
			q += n
		}
	}
	q = space(q)
	if strings.HasPrefix(s[q:], "}%%") {
		q += 3
	}
	return q
}

// lexMindmap turns the prepared lines into tokens, with the line end
// mermaid adds (Diagram.fromText).
func lexMindmap(lines []srcLine) ([]erToken, error) {
	last := 0
	if len(lines) > 0 {
		last = lines[len(lines)-1].no
	}
	return runLexer(append(lines[:len(lines):len(lines)], srcLine{text: "", no: last}), mindmapRules, "")
}
