package mermaidrender

import (
	"html"
	"regexp"
	"strings"
	"unicode/utf16"
)

// mmParser reads mindmap.jison's grammar (mermaid 12.0.0) over the tokens:
//
//	start      : spaceLines? mindMap
//	spaceLines : SPACELINE (SPACELINE | NL)*
//	mindMap    : MINDMAP NL? document
//	document   : (statement stop)+
//	stop       : (NL | EOF | SPACELINE) (NL | EOF)*
//	statement  : SPACELIST (node | ICON | CLASS)? | SPACELINE | node | ICON | CLASS
//	node       : NODE_ID (NODE_DSTART NODE_DESCR NODE_DEND)? | NODE_DSTART NODE_DESCR NODE_DEND
type mmParser struct {
	stParser
	db mmDB
}

// mmDB is the parse-time half of MindmapDB.
type mmDB struct {
	nodes []*MindmapNode
	// levels are the nodes' levels as MindmapDB keeps them: indentation
	// less the root's. A parent is found by level; depth is the tree's.
	levels    []int
	baseLevel int
	// raw keeps each label as the lexer read it, entity codes restored
	// (for comparing the reading with mermaid's own parser).
	raw bool
}

func parseMindmap(src string) (*Mindmap, error) { return readMindmap(src, false) }

func readMindmap(src string, raw bool) (*Mindmap, error) {
	lines, front, err := mindmapLines(src)
	if err != nil {
		return nil, err
	}
	toks, err := lexMindmap(lines)
	if err != nil {
		return nil, err
	}
	p := &mmParser{stParser: stParser{toks: toks}, db: mmDB{raw: raw}}
	if p.peek().kind == "SPACELINE" {
		p.next()
		for k := p.peek().kind; k == "SPACELINE" || k == "NL"; k = p.peek().kind {
			p.next()
		}
	}
	if _, err := p.expect("MINDMAP"); err != nil {
		return nil, err
	}
	if p.peek().kind == "NL" {
		p.next()
	}
	for {
		if err := p.statement(); err != nil {
			return nil, err
		}
		if err := p.stop(); err != nil {
			return nil, err
		}
		if p.peek().kind == "EOF" {
			break
		}
	}
	m := &Mindmap{title: front.text, titleLine: front.line, Nodes: p.db.nodes}
	for i, n := range m.Nodes {
		switch {
		case n.Parent < 0:
			n.Section = -1
		case n.Level == 1:
			n.Section = len(m.Nodes[0].Children[:indexOf(m.Nodes[0].Children, i)]) % MindmapSections
		default:
			n.Section = m.Nodes[n.Parent].Section
		}
	}
	return m, nil
}

func indexOf(s []int, v int) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}

func (p *mmParser) stop() error {
	switch t := p.next(); t.kind {
	case "NL", "SPACELINE":
	case "EOF":
		return nil
	default:
		return p.unexpected(t)
	}
	for k := p.peek().kind; k == "NL"; k = p.peek().kind {
		p.next()
	}
	return nil
}

func (p *mmParser) statement() error {
	level := 0
	switch t := p.peek(); t.kind {
	case "SPACELINE":
		p.next()
		return nil
	case "SPACELIST":
		p.next()
		level = len(utf16.Encode([]rune(t.text)))
	}
	switch t := p.peek(); t.kind {
	case "ICON", "CLASS":
		p.next()
		if len(p.db.nodes) == 0 {
			// decorateNode sets a property of nodes[-1]: mermaid's crash.
			return errf(SyntaxError, t.line, "%s before any node", strings.ToLower(t.kind))
		}
		return nil // presentation: read and dropped
	case "NODE_ID", "NODE_DSTART":
		return p.node(level)
	case "SPACELINE", "NL", "EOF":
		if level > 0 {
			return nil // SPACELIST alone
		}
	}
	return p.unexpected(p.peek())
}

func (p *mmParser) node(level int) error {
	first := p.next()
	descr, start, end := first.text, "", ""
	if first.kind == "NODE_ID" && p.peek().kind != "NODE_DSTART" {
		return p.db.add(level, descr, MindmapDefault, first.line)
	}
	if first.kind == "NODE_DSTART" {
		start = first.text
	} else {
		start = p.next().text
	}
	d, err := p.expect("NODE_DESCR")
	if err != nil {
		return err
	}
	e, err := p.expect("NODE_DEND")
	if err != nil {
		return err
	}
	descr, end = d.text, e.text
	return p.db.add(level, descr, mindmapShape(start, end), first.line)
}

// mindmapShape is MindmapDB.getType: the opening delimiter decides, and
// "(" only with its close.
func mindmapShape(start, end string) MindmapShape {
	switch start {
	case "[":
		return MindmapRect
	case "(":
		if end == ")" {
			return MindmapRounded
		}
		return MindmapCloud
	case "((":
		return MindmapCircle
	case ")":
		return MindmapCloud
	case "))":
		return MindmapBang
	case "{{":
		return MindmapHexagon
	}
	return MindmapDefault
}

// add is MindmapDB.addNode.
func (db *mmDB) add(level int, descr string, shape MindmapShape, line int) error {
	if len(db.nodes) >= MaxMindmapNodes {
		return errf(UnsupportedConstruct, line, "more than %d mindmap nodes", MaxMindmapNodes)
	}
	text := restoreEntities(descr)
	if !db.raw {
		var err error
		if text, err = mindmapLabel(descr, line); err != nil {
			return err
		}
	}
	n := &MindmapNode{Text: text, Shape: shape, Parent: -1, Line: line}
	if len(db.nodes) == 0 {
		db.baseLevel = level
		db.nodes, db.levels = append(db.nodes, n), append(db.levels, 0)
		return nil
	}
	level -= db.baseLevel
	for i := len(db.nodes) - 1; i >= 0; i-- {
		if db.levels[i] < level {
			n.Parent = i
			break
		}
	}
	if n.Parent < 0 {
		return errf(SyntaxError, line, "there can be only one root: no parent for %q", strings.TrimSpace(restoreEntities(descr)))
	}
	parent := db.nodes[n.Parent]
	n.Level = parent.Level + 1
	parent.Children = append(parent.Children, len(db.nodes))
	db.nodes, db.levels = append(db.nodes, n), append(db.levels, level)
	return nil
}

// mindmapLabel turns a node's text as the lexer read it into the text
// drawn. mermaid draws every mindmap label as markdown in HTML
// (markdownToHTML): emphasis is formatting, which this engine refuses, as
// it does in ER and state; every other markdown token shows as written.
func mindmapLabel(descr string, line int) (string, error) {
	s := restoreEntities(descr)
	refuse := func(what string) (string, error) {
		return "", errf(UnsupportedConstruct, line, "%s in %q", what, s)
	}
	// With a "<" in it, DOMPurify parses the label and decodes its entity
	// codes before markdown reads it.
	forms := []string{s}
	if strings.Contains(s, "<") {
		forms = append(forms, html.UnescapeString(entityRefs(s)))
	}
	for _, f := range forms {
		switch {
		case hasMdEmphasis(f):
			return refuse("markdown formatting")
		case reMdEscape.MatchString(f):
			return refuse("a markdown escape")
		case reIconText.MatchString(f):
			return refuse("an icon")
		case reKatex.MatchString(f):
			return refuse("math")
		}
	}
	if lines := strings.Split(s, "\n"); len(lines) > 1 {
		// A block's raw text, and a hard break's, show with their line ends
		// as spaces.
		for k, l := range lines {
			if reMdBlockLine.MatchString(l) {
				return refuse("a markdown block")
			}
			if k < len(lines)-1 && (strings.HasSuffix(l, "  ") || strings.HasSuffix(l, "\\")) {
				return refuse("a markdown hard break")
			}
		}
	}
	s = reMmBreak.ReplaceAllString(s, "\n")
	// A "<" before a letter opens a tag, which swallows the rest.
	if reTagStart.MatchString(s) {
		return refuse("HTML")
	}
	s = html.UnescapeString(entityRefs(s))
	var out []string
	for _, l := range strings.Split(s, "\n") {
		// HTML collapses ASCII whitespace only.
		if l = strings.Join(strings.FieldsFunc(l, isHTMLSpace), " "); l != "" {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n"), nil
}

// entityRefs turns entity codes into HTML references, as mermaid's
// decodeEntities does: "#quot;" is "&quot;", "#35;" is "&#35;", and a
// name HTML does not know stays a reference the browser shows as written.
func entityRefs(s string) string {
	return reEntity.ReplaceAllStringFunc(s, func(m string) string {
		name := m[1 : len(m)-1]
		if name[0] >= '0' && name[0] <= '9' {
			return "&#" + name + ";"
		}
		return "&" + name + ";"
	})
}

func isHTMLSpace(r rune) bool { return r == ' ' || r == '\t' || r == '\f' || r == '\r' || r == '\n' }

var (
	reMmBreak  = regexp.MustCompile(`(?i)</?br\s*/?>`)
	reTagStart = regexp.MustCompile(`<[A-Za-z/!?]`)
	// reIconText is an icon written in a label (replaceIconSubstring).
	reIconText = regexp.MustCompile(`fa[bklrs]?:fa-[\w-]+`)
	// reKatex is math (katexRegex).
	reKatex = regexp.MustCompile(`\$\$.*?\$\$`)
	// reMdEscape is a markdown backslash escape, which mermaid drops.
	reMdEscape = regexp.MustCompile(`\\[!-/:-@\[-` + "`" + `{-~]`)
	// reMdBlockLine is a line that starts a markdown block (or underlines
	// one): list item, heading, quote, fence, thematic break, setext
	// underline, table delimiter row.
	reMdBlockLine = regexp.MustCompile(`^[ \t]*(?:#{1,6}(?:[ \t]|$)|>|[-+*](?:[ \t]|$)|\d{1,9}[.)](?:[ \t]|$)|` +
		"```|~~~|" + `[-_*=]{3,}[ \t]*$|=+[ \t]*$|-+[ \t]*$|\|?[ \t]*:?-+:?[ \t]*(?:\|[ \t]*:?-+:?[ \t]*)+\|?[ \t]*$)`)
)
