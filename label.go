package mermaidrender

import (
	"html"
	"regexp"
	"strings"
)

var (
	// "#quot;" and "#9829;" — mermaid's entity codes ("Entity codes to
	// escape characters": base-10 numbers and HTML character names).
	reEntity = regexp.MustCompile(`#([A-Za-z][A-Za-z0-9]*|[0-9]+);`)
	reBreak  = regexp.MustCompile(`(?i)<br\s*/?>`)
	// Any other HTML tag. "a < b" and "x<5" are not tags.
	reTag = regexp.MustCompile(`</?[A-Za-z][A-Za-z0-9]*(\s[^<>]*)?/?>`)
)

// decodeLabel turns label text as written into the text to draw: quotes
// removed, entity codes decoded, <br> as a line break. A label holding any
// other HTML tag is refused (ok=false): drawing the tag as text would show
// something the author did not write as text, and rendering HTML is out of
// scope.
func decodeLabel(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
	}
	s = reBreak.ReplaceAllString(s, "\n")
	if reTag.MatchString(s) {
		return "", false
	}
	s = decodeEntityCodes(s)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(l)
	}
	return strings.Join(lines, "\n"), true
}

// decodeEntityCodes decodes mermaid's entity codes; an unknown name stays
// as written.
func decodeEntityCodes(s string) string {
	return reEntity.ReplaceAllStringFunc(s, func(m string) string {
		name := m[1 : len(m)-1]
		var ref string
		if name[0] >= '0' && name[0] <= '9' {
			ref = "&#" + name + ";"
		} else {
			ref = "&" + name + ";"
		}
		if out := html.UnescapeString(ref); out != ref {
			return out
		}
		return m
	})
}
