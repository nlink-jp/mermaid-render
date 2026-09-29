package mermaidrender

import (
	"strings"
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
// grammar reads: indentation, blank lines and trailing spaces.
//   - Directives (%%{ … }%%) are removed, leaving the rest of their lines.
//   - cleanupComments: /^\s*%%(?!{)[^\n]+\n?/gm removes a comment line
//     together with the whitespace-only lines right before it (\s* crosses
//     line ends), then trimStart.
func mindmapLines(src string) ([]srcLine, frontTitle, error) {
	raw, i, title, err := frontMatter(src)
	if err != nil {
		return nil, title, err
	}
	var lines []srcLine
	for ; i < len(raw); i++ {
		text, no := raw[i], i+1
		for k := strings.Index(text, "%%{"); k >= 0; k = strings.Index(text, "%%{") {
			// A directive may run over several lines; what is left of its
			// first and last lines is one line.
			start, rest := i, text[k:]
			for !strings.Contains(rest, "}%%") {
				i++
				if i >= len(raw) {
					return nil, title, errf(SyntaxError, start+1, "directive is not closed with }%%")
				}
				rest = raw[i]
			}
			text = text[:k] + rest[strings.Index(rest, "}%%")+3:]
		}
		lines = append(lines, srcLine{text: text, no: no})
	}
	// cleanupComments.
	var kept []srcLine
	for _, l := range lines {
		t := strings.TrimLeft(l.text, jsSpaceChars)
		if c, ok := strings.CutPrefix(t, "%%"); ok && c != "" && c[0] != '{' {
			for len(kept) > 0 && strings.Trim(kept[len(kept)-1].text, jsSpaceChars) == "" {
				kept = kept[:len(kept)-1]
			}
			continue
		}
		kept = append(kept, l)
	}
	// trimStart.
	for len(kept) > 0 && strings.Trim(kept[0].text, jsSpaceChars) == "" {
		kept = kept[1:]
	}
	if len(kept) > 0 {
		kept[0].text = strings.TrimLeft(kept[0].text, jsSpaceChars)
	}
	return kept, title, nil
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
