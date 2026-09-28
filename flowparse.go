package mermaidrender

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The flowchart grammar follows the mermaid 12.0.0 documentation
// (syntax/flowchart.md). Where the documentation is silent — which
// characters an id may hold, how link symbols are read, subgraph
// membership — it follows the same version's parser/flow.jison and
// flowDb.ts, and the comment says so.

func parseFlowchart(lines []srcLine, title frontTitle) (Diagram, error) {
	p := &flowParser{f: &Flowchart{title: title.text, titleLine: title.line}, nodes: map[string]*Node{}}
	var stmts []srcLine
	for _, l := range lines {
		parts, err := splitStatements(l)
		if err != nil {
			return nil, err
		}
		stmts = append(stmts, parts...)
	}
	if err := p.header(stmts[0]); err != nil {
		return nil, err
	}
	for _, s := range stmts[1:] {
		if err := p.statement(s); err != nil {
			return nil, err
		}
	}
	if p.open != nil {
		return nil, errf(SyntaxError, p.open.sg.Line, "subgraph %q is not closed with end", p.open.sg.ID)
	}
	if err := p.finish(); err != nil {
		return nil, err
	}
	return p.f, nil
}

var reEntityTail = regexp.MustCompile(`#([A-Za-z][A-Za-z0-9]*|[0-9]+)$`)

// splitStatements splits a line at ";" outside quotes, brackets and |link
// text| (flow.jison: SEMI separates statements, as NEWLINE does). The ";"
// closing an entity code such as "#amp;" is not a separator: mermaid
// replaces entity codes before it parses (encodeEntities).
func splitStatements(l srcLine) ([]srcLine, error) {
	var out []srcLine
	depth, quoted, piped, start := 0, false, false, 0
	for i := 0; i < len(l.text); i++ {
		switch c := l.text[i]; {
		case c == '"':
			quoted = !quoted
		case quoted:
		case c == '|' && depth == 0:
			piped = !piped
		case piped:
		case c == ';' && reEntityTail.MatchString(l.text[:i]):
		case c == '[' || c == '(' || c == '{':
			depth++
		case c == ']' || c == ')' || c == '}':
			if depth > 0 {
				depth--
			}
		case c == ';' && depth == 0:
			if t := strings.TrimSpace(l.text[start:i]); t != "" {
				out = append(out, srcLine{t, l.no})
			}
			start = i + 1
		}
	}
	if quoted {
		return nil, errf(SyntaxError, l.no, "unclosed quote")
	}
	if t := strings.TrimSpace(l.text[start:]); t != "" {
		out = append(out, srcLine{t, l.no})
	}
	return out, nil
}

type openSubgraph struct {
	sg       *Subgraph
	mentions []string
}

type flowParser struct {
	f        *Flowchart
	nodes    map[string]*Node
	explicit map[string]bool // ids given a text or shape somewhere
	open     *openSubgraph
	closed   []*openSubgraph
	line     int
}

func (p *flowParser) header(s srcLine) error {
	words := strings.Fields(s.text)
	if len(words) > 2 {
		return errf(SyntaxError, s.no, "unexpected text after the direction: %q", strings.Join(words[2:], " "))
	}
	p.f.Direction = TB
	if len(words) == 2 {
		switch words[1] {
		case "TB", "TD":
			p.f.Direction = TB
		case "BT":
			p.f.Direction = BT
		case "LR":
			p.f.Direction = LR
		case "RL":
			p.f.Direction = RL
		default:
			return errf(SyntaxError, s.no, "unknown direction %q (TB, TD, BT, RL, LR)", words[1])
		}
	}
	return nil
}

func (p *flowParser) statement(s srcLine) error {
	p.line = s.no
	t := s.text
	switch {
	case t == "end":
		if p.open == nil {
			return errf(SyntaxError, s.no, "end without subgraph")
		}
		p.closed = append(p.closed, p.open)
		p.open = nil
		return nil
	case hasWord(t, "subgraph"):
		return p.subgraph(strings.TrimSpace(t[len("subgraph"):]), s.no)
	case isDirection(t):
		// Presentation: a subgraph's own direction. The top-level direction
		// is set by the header. flow.jison needs a direction word after
		// it; "direction --> B" is a link from a node called direction.
		return nil
	case hasWord(t, "classDef"), hasWord(t, "class"), hasWord(t, "style"),
		hasWord(t, "linkStyle"), hasWord(t, "click"):
		// Presentation. A keyword followed by a link is a keyword used as a
		// node id, which mermaid cannot parse either; dropping the line
		// would drop a link.
		if f := strings.Fields(t); len(f) > 1 && looksLikeLink(f[1]) {
			return errf(SyntaxError, s.no, "%q is a keyword and cannot be a node id", f[0])
		}
		return nil
	case strings.HasPrefix(t, "accTitle") && strings.HasPrefix(strings.TrimSpace(t[len("accTitle"):]), ":"),
		strings.HasPrefix(t, "accDescr") && strings.HasPrefix(strings.TrimSpace(t[len("accDescr"):]), ":"):
		return nil
	}
	return p.vertexStatement(t)
}

func isDirection(t string) bool {
	f := strings.Fields(t)
	if len(f) != 2 || f[0] != "direction" {
		return false
	}
	switch f[1] {
	case "TB", "TD", "BT", "RL", "LR":
		return true
	}
	return false
}

func (p *flowParser) subgraph(rest string, no int) error {
	if p.open != nil {
		return errf(UnsupportedConstruct, no, "nested subgraph")
	}
	sg := &Subgraph{Line: no}
	var id, title string
	autoID := false
	if i := strings.IndexByte(rest, '['); i >= 0 && !strings.HasPrefix(rest, "\"") {
		// subgraph id [title] / subgraph id["title"]
		j := strings.LastIndexByte(rest, ']')
		if j < i || strings.TrimSpace(rest[j+1:]) != "" {
			return errf(SyntaxError, no, "subgraph title is not closed with ]")
		}
		id = strings.TrimSpace(rest[:i])
		title = rest[i+1 : j]
		if id == "" {
			return errf(SyntaxError, no, "subgraph has a title but no id")
		}
	} else {
		// subgraph title — flowDb.addSubGraph: the text is the id too,
		// unless it holds whitespace.
		title = rest
		text := strings.TrimSpace(rest)
		if len(text) >= 2 && text[0] == '"' && text[len(text)-1] == '"' {
			text = text[1 : len(text)-1]
		}
		if text == "" || strings.ContainsAny(text, " \t") {
			autoID = true
		} else {
			id = text
		}
	}
	decoded, ok := decodeLabel(title)
	if !ok {
		return errf(UnsupportedConstruct, no, "HTML in a subgraph title")
	}
	if strings.HasPrefix(strings.TrimSpace(title), "\"`") {
		return errf(UnsupportedConstruct, no, "Markdown string")
	}
	if autoID {
		id = "subGraph" + strconv.Itoa(len(p.f.Subgraphs))
	}
	for _, other := range p.f.Subgraphs {
		if other.ID == id {
			return errf(UnsupportedConstruct, no, "subgraph id %q is used twice", id)
		}
	}
	sg.ID, sg.Title = id, decoded
	p.f.Subgraphs = append(p.f.Subgraphs, sg)
	p.open = &openSubgraph{sg: sg}
	return nil
}

// cursor walks one statement.
type cursor struct {
	s   string
	pos int
}

func (c *cursor) eof() bool { return c.pos >= len(c.s) }

func (c *cursor) skipSpace() {
	for c.pos < len(c.s) && (c.s[c.pos] == ' ' || c.s[c.pos] == '\t') {
		c.pos++
	}
}

func (c *cursor) rest() string { return c.s[c.pos:] }

func (p *flowParser) vertexStatement(t string) error {
	c := &cursor{s: t}
	prev, err := p.nodeGroup(c)
	if err != nil {
		return err
	}
	for {
		c.skipSpace()
		if c.eof() {
			return nil
		}
		l, visible, err := p.link(c)
		if err != nil {
			return err
		}
		c.skipSpace()
		next, err := p.nodeGroup(c)
		if err != nil {
			return err
		}
		if visible {
			if len(p.f.Links)+len(prev)*len(next) > MaxLinks {
				return errf(UnsupportedConstruct, p.line, "more than %d links (mermaid's own limit)", MaxLinks)
			}
			for _, a := range prev {
				for _, b := range next {
					ln := *l
					ln.From, ln.To = Endpoint{ID: a}, Endpoint{ID: b}
					p.f.Links = append(p.f.Links, &ln)
				}
			}
		}
		prev = next
	}
}

// nodeGroup reads "node (& node)*" and returns the ids.
func (p *flowParser) nodeGroup(c *cursor) ([]string, error) {
	var ids []string
	for {
		c.skipSpace()
		id, err := p.node(c)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
		save := c.pos
		c.skipSpace()
		if !c.eof() && c.s[c.pos] == '&' {
			c.pos++
			continue
		}
		c.pos = save
		return ids, nil
	}
}

// idRune reports whether r may appear in a node id at s[i:]. flow.jison's
// NODE_STRING: ASCII letters, digits and !#$%'*+.?\_/ , "-" unless a link
// starts there, "=" unless "==" does, and Unicode letters. We also stop at
// "." when "-" follows, which starts a dotted link.
func idRune(s string, i int, r rune) bool {
	next := byte(0)
	if i+1 < len(s) {
		next = s[i+1]
	}
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return true
	case strings.ContainsRune("!#$%'*+?\\_/", r):
		return true
	case r == '-':
		return next != '-' && next != '>' && next != '.'
	case r == '=':
		return next != '='
	case r == '.':
		return next != '-'
	case r >= utf8.RuneSelf:
		return unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r)
	}
	return false
}

type shapeSpec struct {
	open  string
	close []string // alternatives, matched in order
	shape []Shape  // the shape each alternative gives
	bad   string   // non-empty: an undocumented form, refused
}

// Longest opener first.
var shapeSpecs = []shapeSpec{
	{open: "(((", close: []string{")))"}, shape: []Shape{DoubleCircle}},
	{open: "((", close: []string{"))"}, shape: []Shape{Circle}},
	{open: "([", close: []string{"])"}, shape: []Shape{Stadium}},
	{open: "(-", bad: "the ellipse shape (-text-) is not in the documentation"},
	{open: "(", close: []string{")"}, shape: []Shape{Round}},
	{open: "[[", close: []string{"]]"}, shape: []Shape{Subroutine}},
	{open: "[(", close: []string{")]"}, shape: []Shape{Cylinder}},
	{open: "[/", close: []string{"/]", "\\]"}, shape: []Shape{Parallelogram, Trapezoid}},
	{open: "[\\", close: []string{"\\]", "/]"}, shape: []Shape{ParallelogramAlt, TrapezoidAlt}},
	{open: "[|", bad: "the [|field:value|text] form is not in the documentation"},
	{open: "[", close: []string{"]"}, shape: []Shape{Rect}},
	{open: "{{", close: []string{"}}"}, shape: []Shape{Hexagon}},
	{open: "{", close: []string{"}"}, shape: []Shape{Rhombus}},
	{open: ">", close: []string{"]"}, shape: []Shape{Asymmetric}},
}

// node reads "id", "id<shape>" and an optional ":::class".
func (p *flowParser) node(c *cursor) (string, error) {
	start := c.pos
	for c.pos < len(c.s) {
		r, w := utf8.DecodeRuneInString(c.s[c.pos:])
		// flow.jison's NODE_STRING holds "&": "API&DB" is one id. Only an
		// "&" that starts a token is the AMP of a node group.
		if !idRune(c.s, c.pos, r) && !(r == '&' && c.pos > start) {
			break
		}
		c.pos += w
	}
	id := c.s[start:c.pos]
	if id == "" {
		if c.eof() {
			return "", errf(SyntaxError, p.line, "a link must end at a node")
		}
		return "", errf(SyntaxError, p.line, "expected a node id at %q", clip(c.rest()))
	}
	if id == "end" {
		// flow.jison lexes lowercase "end" as the keyword wherever it
		// stands; the documentation says it "will break the Flowchart".
		return "", errf(SyntaxError, p.line, `"end" in lowercase cannot be a node id`)
	}
	if !c.eof() && c.s[c.pos] == '@' {
		if strings.HasPrefix(c.rest(), "@{") {
			return "", errf(UnsupportedConstruct, p.line, "node metadata %s@{...}", id)
		}
		return "", errf(UnsupportedConstruct, p.line, "edge id %s@", id)
	}
	label, shape, has, err := p.shape(c)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(c.rest(), ":::") {
		c.pos += 3
		for c.pos < len(c.s) {
			r, w := utf8.DecodeRuneInString(c.s[c.pos:])
			if !idRune(c.s, c.pos, r) {
				break
			}
			c.pos += w
		}
	}
	n := p.nodes[id]
	if n == nil {
		n = &Node{ID: id, Label: id, Line: p.line}
		p.nodes[id] = n
		p.f.Nodes = append(p.f.Nodes, n)
	}
	if has {
		// "it is the last text found for the node that will be used"
		n.Label, n.Shape = label, shape
		if p.explicit == nil {
			p.explicit = map[string]bool{}
		}
		p.explicit[id] = true
	}
	if p.open != nil {
		p.open.mentions = append(p.open.mentions, id)
	}
	return id, nil
}

func (p *flowParser) shape(c *cursor) (label string, shape Shape, has bool, err error) {
	rest := c.rest()
	for _, sp := range shapeSpecs {
		if !strings.HasPrefix(rest, sp.open) {
			continue
		}
		if sp.bad != "" {
			return "", 0, false, errf(UnsupportedConstruct, p.line, "%s", sp.bad)
		}
		body := rest[len(sp.open):]
		var text string
		var after string
		trimmed := strings.TrimLeft(body, " \t")
		if strings.HasPrefix(trimmed, "\"`") {
			return "", 0, false, errf(UnsupportedConstruct, p.line, "Markdown string")
		}
		if strings.HasPrefix(trimmed, "\"") {
			// A quoted text is literal; the closer follows the quote.
			q := strings.IndexByte(trimmed[1:], '"')
			if q < 0 {
				return "", 0, false, errf(SyntaxError, p.line, "unclosed quote")
			}
			text = trimmed[:q+2]
			after = strings.TrimLeft(trimmed[q+2:], " \t")
			for k, cl := range sp.close {
				if strings.HasPrefix(after, cl) {
					c.pos = len(c.s) - len(after) + len(cl)
					return p.label(text, sp.shape[k])
				}
			}
			return "", 0, false, errf(SyntaxError, p.line, "%s%s is not closed with %s", sp.open, text, sp.close[0])
		}
		best, which := -1, 0
		for k, cl := range sp.close {
			if i := strings.Index(body, cl); i >= 0 && (best < 0 || i < best) {
				best, which = i, k
			}
		}
		if best < 0 {
			return "", 0, false, errf(SyntaxError, p.line, "%s is not closed with %s", sp.open, sp.close[0])
		}
		text = body[:best]
		c.pos += len(sp.open) + best + len(sp.close[which])
		return p.label(text, sp.shape[which])
	}
	return "", Rect, false, nil
}

func (p *flowParser) label(text string, shape Shape) (string, Shape, bool, error) {
	l, ok := decodeLabel(text)
	if !ok {
		return "", 0, false, errf(UnsupportedConstruct, p.line, "HTML in a label: %q", clip(text))
	}
	return l, shape, true, nil
}

// Link tokens, from flow.jison (mermaid 12.0.0):
//
//	LINK        \s*[xo<]?\-\-+[-xo>]\s*      and the == and dotted forms
//	START_LINK  \s*[xo<]?\-\-\s*             text follows, up to a LINK
var (
	reInvisible = regexp.MustCompile(`^~~~+`)
	reSolid     = regexp.MustCompile(`^([xo<]?)(-{2,})([-xo>])`)
	reThick     = regexp.MustCompile(`^([xo<]?)(={2,})([=xo>])`)
	reDotted    = regexp.MustCompile(`^([xo<]?)-?(\.+)-([xo>]?)`)
	reSolidOpen = regexp.MustCompile(`^([xo<]?)--`)
	reThickOpen = regexp.MustCompile(`^([xo<]?)==`)
	reDotOpen   = regexp.MustCompile(`^([xo<]?)-\.`)
	// The end of a text link, searched for inside the rest.
	reSolidEnd  = regexp.MustCompile(`-{2,}[-xo>]`)
	reThickEnd  = regexp.MustCompile(`={2,}[=xo>]`)
	reDottedEnd = regexp.MustCompile(`\.+-[xo>]?`)
	reEdgeID    = regexp.MustCompile(`^[^\s"]+@`)
)

// destructEnd reads a link token as flowDb.destructEndLink does (mermaid
// 12.0.0): the last character is the end mark; a first character counts as
// a start mark only when it is the same kind (<...>, o...o, x...x), which
// makes the link two-headed; the length is the rest of the line minus one,
// or the number of dots for a dotted link. An unmatched first character
// stays part of the line — "<---" is a plain line of length 2.
func destructEnd(tok string) (start, end Head, stroke Stroke, length int) {
	line := tok[:len(tok)-1]
	var pair byte
	switch tok[len(tok)-1] {
	case 'x':
		end, pair = CrossHead, 'x'
	case '>':
		end, pair = Arrow, '<'
	case 'o':
		end, pair = CircleHead, 'o'
	}
	if end != NoHead && tok[0] == pair {
		start = end
		line = line[1:]
	}
	length = len(line) - 1
	if strings.HasPrefix(line, "=") {
		stroke = Thick
	}
	if dots := strings.Count(line, "."); dots > 0 {
		stroke, length = Dotted, dots
	}
	length = min(length, MaxLinkLength)
	return
}

// startMark is the head a text link's opening mark asks for.
func startMark(tok string) Head {
	switch tok[0] {
	case '<':
		return Arrow
	case 'x':
		return CrossHead
	case 'o':
		return CircleHead
	}
	return NoHead
}

// link reads one link and its optional |text|. visible is false for ~~~.
func (p *flowParser) link(c *cursor) (*Link, bool, error) {
	rest := c.rest()
	if m := reInvisible.FindString(rest); m != "" {
		c.pos += len(m)
		return &Link{}, false, nil
	}
	l := &Link{Line: p.line}
	var tok string
	switch {
	case reThick.MatchString(rest):
		tok = reThick.FindString(rest)
	case reSolid.MatchString(rest):
		tok = reSolid.FindString(rest)
	case reDotted.MatchString(rest):
		tok = reDotted.FindString(rest)
	case reThickOpen.MatchString(rest):
		return p.textLink(c, l, reThickOpen, reThickEnd)
	case reSolidOpen.MatchString(rest):
		return p.textLink(c, l, reSolidOpen, reSolidEnd)
	case reDotOpen.MatchString(rest):
		return p.textLink(c, l, reDotOpen, reDottedEnd)
	case reEdgeID.MatchString(rest):
		return nil, false, errf(UnsupportedConstruct, p.line, "edge id %q", clip(reEdgeID.FindString(rest)))
	default:
		return nil, false, errf(SyntaxError, p.line, "expected a link at %q", clip(rest))
	}
	l.Start, l.End, l.Stroke, l.Length = destructEnd(tok)
	c.pos += len(tok)
	// Optional |text| after the link.
	save := c.pos
	c.skipSpace()
	if !c.eof() && c.s[c.pos] == '|' {
		end := pipeEnd(c.s, c.pos+1)
		if end < 0 {
			return nil, false, errf(SyntaxError, p.line, "link text is not closed with |")
		}
		if strings.HasPrefix(strings.TrimSpace(c.s[c.pos+1:end]), "\"`") {
			return nil, false, errf(UnsupportedConstruct, p.line, "Markdown string")
		}
		text, ok := decodeLabel(c.s[c.pos+1 : end])
		if !ok {
			return nil, false, errf(UnsupportedConstruct, p.line, "HTML in link text")
		}
		l.Label = text
		c.pos = end + 1
	} else {
		c.pos = save
	}
	return l, true, nil
}

// pipeEnd finds the closing | from i, skipping a quoted text.
func pipeEnd(s string, i int) int {
	t := strings.TrimLeft(s[i:], " \t")
	off := len(s) - len(t)
	if strings.HasPrefix(t, "\"") {
		q := strings.IndexByte(t[1:], '"')
		if q < 0 {
			return -1
		}
		j := strings.IndexByte(t[q+2:], '|')
		if j < 0 {
			return -1
		}
		return off + q + 2 + j
	}
	j := strings.IndexByte(s[i:], '|')
	if j < 0 {
		return -1
	}
	return i + j
}

// textLink reads "-- text -->" and its thick and dotted forms: the text
// runs to the first place the closing symbol matches. The heads follow
// flowDb.destructLink: an unmarked opening takes the end's head; a marked
// one must match it and makes the link two-headed.
func (p *flowParser) textLink(c *cursor, l *Link, open, end *regexp.Regexp) (*Link, bool, error) {
	rest := c.rest()
	openTok := open.FindString(rest)
	body := rest[len(openTok):]
	skip := 0
	if t := strings.TrimLeft(body, " \t"); strings.HasPrefix(t, "\"") {
		// A quoted text may hold the link symbols.
		q := strings.IndexByte(t[1:], '"')
		if q < 0 {
			return nil, false, errf(SyntaxError, p.line, "unclosed quote")
		}
		skip = len(body) - len(t) + q + 2
	}
	loc := end.FindStringIndex(body[skip:])
	if loc == nil {
		return nil, false, errf(SyntaxError, p.line, "link text is not closed at %q", clip(rest))
	}
	loc[0], loc[1] = loc[0]+skip, loc[1]+skip
	raw := body[:loc[0]]
	if strings.HasPrefix(strings.TrimSpace(raw), "\"`") {
		return nil, false, errf(UnsupportedConstruct, p.line, "Markdown string")
	}
	text, ok := decodeLabel(raw)
	if !ok {
		return nil, false, errf(UnsupportedConstruct, p.line, "HTML in link text")
	}
	if text == "" {
		return nil, false, errf(SyntaxError, p.line, "empty link text at %q", clip(rest))
	}
	l.Label = text
	_, l.End, l.Stroke, l.Length = destructEnd(body[loc[0]:loc[1]])
	if sm := startMark(openTok); sm != NoHead {
		if sm != l.End {
			return nil, false, errf(SyntaxError, p.line, "a link cannot open with %q and end with %q", openTok, body[loc[0]:loc[1]])
		}
		l.Start = sm
	}
	c.pos += len(openTok) + loc[1]
	c.skipSpace()
	if !c.eof() && c.s[c.pos] == '|' {
		return nil, false, errf(SyntaxError, p.line, "a link has both inline text and |text|")
	}
	return l, true, nil
}

func clip(s string) string {
	if len(s) > 24 {
		return s[:24] + "…"
	}
	return s
}

// finish resolves subgraph endpoints and membership.
func (p *flowParser) finish() error {
	sgs := map[string]*Subgraph{}
	for _, sg := range p.f.Subgraphs {
		sgs[sg.ID] = sg
	}
	// A link endpoint named like a subgraph is the subgraph.
	for _, l := range p.f.Links {
		for _, e := range []*Endpoint{&l.From, &l.To} {
			if sgs[e.ID] != nil {
				e.Subgraph = true
			}
		}
	}
	for _, n := range p.f.Nodes {
		if sgs[n.ID] != nil && p.explicit[n.ID] {
			return errf(UnsupportedConstruct, n.Line, "%q is both a node with a text and a subgraph", n.ID)
		}
	}
	// Drop subgraph ids from the node list.
	kept := p.f.Nodes[:0]
	for _, n := range p.f.Nodes {
		if sgs[n.ID] == nil {
			kept = append(kept, n)
		}
	}
	p.f.Nodes = kept
	// Membership: in the order subgraphs close, each takes the nodes it
	// mentions that no earlier one took (flowDb.makeUniq). A subgraph id
	// mentioned inside another subgraph would nest it.
	for _, o := range p.closed {
		for _, id := range o.mentions {
			if sgs[id] != nil {
				if id == o.sg.ID {
					continue
				}
				return errf(UnsupportedConstruct, o.sg.Line, "subgraph %q inside subgraph %q (nested subgraph)", id, o.sg.ID)
			}
			n := p.nodes[id]
			if n.Subgraph != "" {
				continue
			}
			n.Subgraph = o.sg.ID
			o.sg.Nodes = append(o.sg.Nodes, id)
		}
	}
	return nil
}

func looksLikeLink(s string) bool {
	for _, re := range []*regexp.Regexp{reInvisible, reThick, reSolid, reDotted, reThickOpen, reSolidOpen, reDotOpen} {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}
