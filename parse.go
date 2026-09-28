package mermaidrender

import (
	"strings"
)

// Parse reads one mermaid diagram. The only error it returns is *Error.
func Parse(src string) (Diagram, error) {
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
	}
	if unsupportedTypes[kw] {
		return nil, errf(UnsupportedType, head.no, "%s", kw)
	}
	return nil, errf(SyntaxError, head.no, "%q does not start a mermaid diagram", kw)
}

// unsupportedTypes are the diagram keywords of mermaid 12.0.0 that this
// engine does not draw. sequenceDiagram and erDiagram leave this list when
// their parsers land.
var unsupportedTypes = map[string]bool{
	"sequenceDiagram": true, "erDiagram": true,
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

type srcLine struct {
	text string // trimmed
	no   int    // 1-based line number in the source
}

// prepare splits src into meaningful lines: it removes the front matter
// (keeping its title), comment and directive lines (%%, %%{...}%%), blank
// lines, and accDescr { ... } blocks.
func prepare(src string) ([]srcLine, string, error) {
	raw := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")
	i := 0
	for i < len(raw) && strings.TrimSpace(raw[i]) == "" {
		i++
	}
	title := ""
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
					title = unquoteYAML(strings.TrimSpace(v))
				}
			}
		}
		if !closed {
			return nil, "", errf(SyntaxError, start+1, "front matter is not closed with ---")
		}
	}
	var out []srcLine
	for ; i < len(raw); i++ {
		t := strings.TrimSpace(raw[i])
		if t == "" || strings.HasPrefix(t, "%%") {
			continue
		}
		if rest, ok := strings.CutPrefix(t, "accDescr"); ok && strings.HasPrefix(strings.TrimSpace(rest), "{") {
			start := i
			for !strings.Contains(raw[i], "}") {
				i++
				if i >= len(raw) {
					return nil, "", errf(SyntaxError, start+1, "accDescr block is not closed with }")
				}
			}
			continue
		}
		out = append(out, srcLine{t, i + 1})
	}
	return out, title, nil
}

func unquoteYAML(v string) string {
	if len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"' || v[0] == '\'' && v[len(v)-1] == '\'') {
		return v[1 : len(v)-1]
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
