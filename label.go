package mermaidrender

import (
	"html"
	"regexp"
	"strings"
	"unicode"
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

// hasMdEmphasis reports whether markdown (marked, reading CommonMark
// delimiter runs) might draw emphasis in s: a run of * or _ that can open
// followed by one of the same character that can close. It errs toward
// yes, and a label wrongly refused shows its source where one wrongly
// drawn shows asterisks where mermaid draws italics. So it says yes when
// any of the readings it cannot tell apart would: whitespace as Go or as
// JavaScript's \s (U+0085 and U+FEFF differ), and an underscore that
// opens or closes by CommonMark's rule or by marked's, which only refuses
// one next to a letter or digit (a combining mark or a zero-width space
// beside it counts for CommonMark, not for marked). The pairing rules that
// would still leave a pair as text (the rule of three, code spans) are
// not applied.
func hasMdEmphasis(s string) bool {
	rs := []rune(s)
	for _, space := range []func(rune) bool{unicode.IsSpace, isJSSpace} {
		for _, markedUnderscore := range []bool{false, true} {
			if emphasisIn(rs, space, markedUnderscore) {
				return true
			}
		}
	}
	return false
}

func emphasisIn(rs []rune, space func(rune) bool, markedUnderscore bool) bool {
	var open [2]bool // * and _
	for i := 0; i < len(rs); {
		c := rs[i]
		if c != '*' && c != '_' {
			i++
			continue
		}
		j := i
		for j < len(rs) && rs[j] == c {
			j++
		}
		before, after := ' ', ' ' // the text's ends count as whitespace
		if i > 0 {
			before = rs[i-1]
		}
		if j < len(rs) {
			after = rs[j]
		}
		lf := !space(after) && (!mdPunct(after) || space(before) || mdPunct(before))
		rf := !space(before) && (!mdPunct(before) || space(after) || mdPunct(after))
		canOpen, canClose, k := lf, rf, 0
		if c == '_' {
			k = 1
			if markedUnderscore {
				canOpen = lf && !alnum(before)
				canClose = rf && !alnum(after)
			} else {
				canOpen = lf && (!rf || mdPunct(before))
				canClose = rf && (!lf || mdPunct(after))
			}
		}
		if canClose && open[k] {
			return true
		}
		if canOpen {
			open[k] = true
		}
		i = j
	}
	return false
}

func alnum(r rune) bool { return unicode.IsLetter(r) || unicode.IsNumber(r) }

// isJSSpace is JavaScript's \s.
func isJSSpace(r rune) bool { return strings.ContainsRune(jsSpaceChars, r) }

// mdPunct is CommonMark's punctuation: Unicode punctuation and symbols.
func mdPunct(r rune) bool { return unicode.IsPunct(r) || unicode.IsSymbol(r) }
