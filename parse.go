package mermaidrender

import (
	"strconv"
	"strings"
)

// Parse reads one mermaid diagram. The only error it returns is *Error.
func Parse(src string) (Diagram, error) {
	// mermaid's maxTextSize (mermaidAPI.ts, 12.0.0): a longer source is
	// not drawn, and here it bounds every parser's cost as well.
	if n := utf16Len(src); n > MaxTextSize {
		return nil, errf(UnsupportedConstruct, 0, "the source is %d characters (mermaid draws at most %d)", n, MaxTextSize)
	}
	// JavaScript's trimStart, which mermaid applies, removes a BOM too.
	src = strings.TrimPrefix(src, "\uFEFF")
	lines, title, err := prepare(src)
	if err != nil {
		return nil, err
	}
	if len(lines) == 0 {
		return nil, errf(SyntaxError, 0, "no diagram")
	}
	head := lines[0]
	kw := firstWord(head.text)
	switch kw {
	case "flowchart", "graph", "flowchart-elk":
		return parseFlowchart(lines, title)
	case "erDiagram":
		return parseER(lines, title)
	case "sequenceDiagram":
		return parseSequence(lines, title)
	}
	if unsupportedTypes[kw] {
		return nil, errf(UnsupportedType, head.no, "%s", kw)
	}
	return nil, errf(SyntaxError, head.no, "%q does not start a mermaid diagram", kw)
}

// unsupportedTypes are the diagram keywords of mermaid 12.0.0 that this
// engine does not draw.
var unsupportedTypes = map[string]bool{
	"stateDiagram": true, "stateDiagram-v2": true, "classDiagram": true, "classDiagram-v2": true,
	"gantt": true, "pie": true, "mindmap": true, "journey": true, "gitGraph": true,
	"timeline": true, "quadrantChart": true, "requirementDiagram": true,
	"C4Context": true, "C4Container": true, "C4Component": true, "C4Dynamic": true, "C4Deployment": true,
	"xychart-beta": true, "xychart": true, "sankey-beta": true, "sankey": true,
	"block-beta": true, "block": true, "packet-beta": true, "packet": true,
	"architecture-beta": true, "architecture": true, "kanban": true,
	"radar-beta": true, "radar": true, "treemap-beta": true, "treemap": true,
	"zenuml": true, "swimlane-beta": true,
}

// MaxTextSize is mermaid's maxTextSize default, in UTF-16 units as
// JavaScript counts a string's length.
const MaxTextSize = 50000

func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		n++
		if r > 0xFFFF {
			n++
		}
	}
	return n
}

// MaxLinks is mermaid's own default edge limit (flowDb maxEdges, 12.0.0):
// a source with more links is refused before they are built.
const MaxLinks = 500

// MaxLinkLength is where mermaid stops a link's length (flowDb.ts: "length
// > 10 ? 10").
const MaxLinkLength = 10

// frontTitle is the front matter's title and the line it is on.
type frontTitle struct {
	text string
	line int
}

type srcLine struct {
	text string // trimmed
	no   int    // 1-based line number in the source
}

// prepare splits src into meaningful lines: it removes the front matter
// (keeping its title), comment and directive lines (%%, %%{...}%%), blank
// lines, and accDescr { ... } blocks.
func prepare(src string) ([]srcLine, frontTitle, error) {
	src = strings.ReplaceAll(src, "\r\n", "\n")
	src = strings.ReplaceAll(src, "\r", "\n") // a CR alone ends a line too
	raw := strings.Split(src, "\n")
	i := 0
	for i < len(raw) && strings.TrimSpace(raw[i]) == "" {
		i++
	}
	var title frontTitle
	if i < len(raw) && strings.TrimSpace(raw[i]) == "---" {
		start := i
		i++
		closed := false
		for ; i < len(raw); i++ {
			t := strings.TrimSpace(raw[i])
			if t == "---" {
				closed = true
				i++
				break
			}
			// Only a top-level "title:" key matters; config and the rest are
			// presentation.
			if !strings.HasPrefix(raw[i], " ") && !strings.HasPrefix(raw[i], "\t") {
				if v, ok := strings.CutPrefix(t, "title:"); ok {
					title = frontTitle{unquoteYAML(strings.TrimSpace(v)), i + 1}
				}
			}
		}
		if !closed {
			return nil, title, errf(SyntaxError, start+1, "front matter is not closed with ---")
		}
	}
	var out []srcLine
	for ; i < len(raw); i++ {
		t := strings.TrimSpace(raw[i])
		if strings.HasPrefix(t, "%%{") && !strings.Contains(t, "}%%") {
			// A directive may run over several lines.
			start := i
			for !strings.Contains(raw[i], "}%%") {
				i++
				if i >= len(raw) {
					return nil, title, errf(SyntaxError, start+1, "directive is not closed with }%%")
				}
			}
			continue
		}
		if t == "" || strings.HasPrefix(t, "%%") {
			continue
		}
		if rest, ok := strings.CutPrefix(t, "accDescr"); ok && strings.HasPrefix(strings.TrimSpace(rest), "{") {
			start := i
			for !strings.Contains(raw[i], "}") {
				i++
				if i >= len(raw) {
					return nil, title, errf(SyntaxError, start+1, "accDescr block is not closed with }")
				}
			}
			continue
		}
		out = append(out, srcLine{t, i + 1})
	}
	return out, title, nil
}

// unquoteYAML reads a YAML scalar as a title: single quotes with ” for a
// quote, double quotes with backslash escapes, or plain text up to a
// " #" comment.
func unquoteYAML(v string) string {
	switch {
	case len(v) >= 2 && v[0] == '\'' && v[len(v)-1] == '\'':
		return strings.ReplaceAll(v[1:len(v)-1], "''", "'")
	case len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"':
		if u, err := strconv.Unquote(v); err == nil {
			return u
		}
		return v[1 : len(v)-1]
	}
	if i := strings.Index(v, " #"); i >= 0 {
		v = strings.TrimSpace(v[:i])
	}
	return v
}

func firstWord(s string) string {
	if i := strings.IndexAny(s, " \t;"); i >= 0 {
		return s[:i]
	}
	return s
}

// hasWord reports whether s is kw, or kw followed by whitespace.
func hasWord(s, kw string) bool {
	if !strings.HasPrefix(s, kw) {
		return false
	}
	if len(s) == len(kw) {
		return true
	}
	c := s[len(kw)]
	return c == ' ' || c == '\t'
}
