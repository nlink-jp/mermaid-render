package mermaidrender

import (
	"regexp"
	"strconv"
	"strings"
)

// MaxStateDepth is how deep composites may nest. mermaid has no limit; this
// one keeps the parser's and the layout's recursion bounded.
const MaxStateDepth = 20

// stStmt is one statement as stateDiagram.jison's actions build it.
type stStmt struct {
	kind  string // "state", "relation", "dir"
	id    string
	typ   string // "default", "fork", "join", "choice", "divider"
	desc  string // "" when none
	doc   []*stStmt
	isDoc bool // doc is set (possibly empty): a composite or a region
	note  *stNote
	s1    *stStmt // a relation's ends and label
	s2    *stStmt
	label string
	dir   Direction
	start *bool // set by translate for [*]
	line  int
}

type stNote struct {
	left bool
	text string
}

type stParser struct {
	toks  []erToken
	pos   int
	depth int
}

func (p *stParser) peek() erToken { return p.toks[p.pos] }
func (p *stParser) next() erToken {
	t := p.toks[p.pos]
	if p.pos < len(p.toks)-1 {
		p.pos++
	}
	return t
}

func (p *stParser) expect(kind string) (erToken, error) {
	t := p.next()
	if t.kind != kind {
		return t, p.unexpected(t)
	}
	return t, nil
}

func (p *stParser) unexpected(t erToken) error {
	if t.kind == "EOF" {
		return errf(SyntaxError, t.line, "unexpected end of the diagram")
	}
	return errf(SyntaxError, t.line, "unexpected %q", strings.TrimSpace(t.text))
}

// trimColon is stateDb.trimColon.
func trimColon(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, ":") {
		return strings.TrimSpace(s[1:])
	}
	return s
}

func parseState(lines []srcLine, front frontTitle) (*StateDiagram, error) {
	doc, err := parseStateDoc(lines)
	if err != nil {
		return nil, err
	}
	return buildState(doc, front)
}

// parseStateDoc reads the statements as the grammar does, before stateDb
// makes anything of them.
func parseStateDoc(lines []srcLine) ([]*stStmt, error) {
	toks, err := lexState(lines)
	if err != nil {
		return nil, err
	}
	p := &stParser{toks: toks}
	// start: NL* SD document
	for p.peek().kind == "NL" {
		p.next()
	}
	if _, err := p.expect("SD"); err != nil {
		return nil, err
	}
	doc, err := p.document(false)
	if err != nil {
		return nil, err
	}
	if t := p.next(); t.kind != "EOF" {
		return nil, p.unexpected(t)
	}
	return doc, nil
}

// document is the grammar's document: statements, and (outside a
// composite) NL tokens between them. Inside a composite it stops before
// the STRUCT_STOP.
func (p *stParser) document(inStruct bool) ([]*stStmt, error) {
	var doc []*stStmt
	for {
		switch t := p.peek(); t.kind {
		case "NL":
			p.next()
			continue
		case "EOF":
			if inStruct {
				return nil, errf(SyntaxError, t.line, "a composite state is not closed with }")
			}
			return doc, nil
		case "STRUCT_STOP":
			if !inStruct {
				return nil, p.unexpected(t)
			}
			return doc, nil
		}
		st, err := p.statement()
		if err != nil {
			return nil, err
		}
		if st != nil {
			doc = append(doc, st)
		}
	}
}

// statement reads one statement. What the grammar reads and mermaid keeps
// nothing of (styling, accessibility, a floating note, scale, a bare
// `state id`) is nil.
func (p *stParser) statement() (*stStmt, error) {
	t := p.next()
	switch t.kind {
	case "classDef":
		// classDef CLASSDEF_ID CLASSDEF_STYLEOPTS; "classDef default"
		// lexes as DEFAULT_CLASSDEF_ID, which the grammar has no rule for.
		if _, err := p.expect("CLASSDEF_ID"); err != nil {
			return nil, err
		}
		_, err := p.expect("CLASSDEF_STYLEOPTS")
		return nil, err
	case "style":
		if _, err := p.expect("STYLE_IDS"); err != nil {
			return nil, err
		}
		_, err := p.expect("STYLEDEF_STYLEOPTS")
		return nil, err
	case "class":
		if _, err := p.expect("CLASSENTITY_IDS"); err != nil {
			return nil, err
		}
		_, err := p.expect("STYLECLASS")
		return nil, err
	case "ID", "EDGE_STATE":
		s1, err := p.idStatement(t)
		if err != nil {
			return nil, err
		}
		switch p.peek().kind {
		case "DESCR":
			s1.desc = trimColon(p.next().text)
			return s1, nil
		case "-->":
			p.next()
			t2 := p.next()
			if t2.kind != "ID" && t2.kind != "EDGE_STATE" {
				return nil, p.unexpected(t2)
			}
			s2, err := p.idStatement(t2)
			if err != nil {
				return nil, err
			}
			rel := &stStmt{kind: "relation", s1: s1, s2: s2, line: t.line}
			if p.peek().kind == "DESCR" {
				rel.label = trimColon(p.next().text)
			}
			return rel, nil
		}
		return s1, nil
	case "HIDE_EMPTY":
		return nil, nil
	case "scale":
		_, err := p.expect("WIDTH")
		return nil, err
	case "COMPOSIT_STATE":
		if p.peek().kind != "STRUCT_START" {
			return nil, nil // `state id` alone: the grammar keeps the token, stateDb nothing
		}
		p.next()
		return p.composite(strings.TrimSpace(t.text), "", t.line)
	case "STATE_DESCR":
		if _, err := p.expect("AS"); err != nil {
			return nil, err
		}
		idt, err := p.expect("ID")
		if err != nil {
			return nil, err
		}
		if p.peek().kind == "STRUCT_START" {
			p.next()
			return p.composite(strings.TrimSpace(idt.text), strings.TrimSpace(t.text), t.line)
		}
		if strings.Contains(idt.text, ":") {
			// The action splits the id at ":" into an id and a second
			// description, which dataFetcher then nests as an array.
			return nil, errf(UnsupportedConstruct, t.line, "a description after the id in state %q as %s", t.text, strings.TrimSpace(idt.text))
		}
		return &stStmt{kind: "state", id: strings.TrimSpace(idt.text), typ: "default", desc: strings.TrimSpace(t.text), line: t.line}, nil
	case "FORK", "JOIN", "CHOICE":
		return &stStmt{kind: "state", id: t.text, typ: strings.ToLower(t.kind), line: t.line}, nil
	case "CONCURRENT":
		return &stStmt{kind: "state", typ: "divider", line: t.line}, nil
	case "note":
		switch pos := p.next(); pos.kind {
		case "left_of", "right_of":
			idt, err := p.expect("ID")
			if err != nil {
				return nil, err
			}
			txt, err := p.expect("NOTE_TEXT")
			if err != nil {
				return nil, err
			}
			// The statement has no type (the grammar gives none), and
			// dataFetcher compares the position with 'left of' as
			// written: "LEFT OF" lexes and is a right note.
			return &stStmt{kind: "state", id: strings.TrimSpace(idt.text),
				note: &stNote{left: strings.TrimSpace(pos.text) == "left of", text: strings.TrimSpace(txt.text)}, line: t.line}, nil
		case "NOTE_TEXT":
			// A floating note: note NOTE_TEXT AS ID, which mermaid keeps
			// nothing of.
			if _, err := p.expect("AS"); err != nil {
				return nil, err
			}
			_, err := p.expect("ID")
			return nil, err
		default:
			return nil, p.unexpected(pos)
		}
	case "direction_tb", "direction_bt", "direction_rl", "direction_lr":
		d := map[string]Direction{"direction_tb": TB, "direction_bt": BT, "direction_rl": RL, "direction_lr": LR}[t.kind]
		return &stStmt{kind: "dir", dir: d, line: t.line}, nil
	case "acc_title":
		_, err := p.expect("acc_title_value")
		return nil, err
	case "acc_descr":
		_, err := p.expect("acc_descr_value")
		return nil, err
	case "acc_descr_multiline_value":
		return nil, nil
	case "CLICK":
		// CLICK idStatement (STRING STRING | HREF STRING) NL
		t2 := p.next()
		if t2.kind != "ID" && t2.kind != "EDGE_STATE" {
			return nil, p.unexpected(t2)
		}
		if _, err := p.idStatement(t2); err != nil {
			return nil, err
		}
		if u := p.next(); u.kind != "STRING" && u.kind != "HREF" {
			return nil, p.unexpected(u)
		}
		if _, err := p.expect("STRING"); err != nil {
			return nil, err
		}
		_, err := p.expect("NL")
		return nil, err
	case "STATE_NAME_ERROR":
		return nil, errf(SyntaxError, t.line, "a state name must be a single word: %q", strings.TrimSpace(t.text))
	}
	return nil, p.unexpected(t)
}

// idStatement is ID or EDGE_STATE, with an optional ::: class (dropped).
func (p *stParser) idStatement(t erToken) (*stStmt, error) {
	s := &stStmt{kind: "state", id: strings.TrimSpace(t.text), typ: "default", line: t.line}
	if p.peek().kind == "STYLE_SEPARATOR" {
		p.next()
		if _, err := p.expect("ID"); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (p *stParser) composite(id, desc string, line int) (*stStmt, error) {
	if p.depth++; p.depth > MaxStateDepth {
		return nil, errf(UnsupportedConstruct, line, "composite states nested more than %d deep", MaxStateDepth)
	}
	doc, err := p.document(true)
	if err != nil {
		return nil, err
	}
	if _, err := p.expect("STRUCT_STOP"); err != nil {
		return nil, err
	}
	p.depth--
	return &stStmt{kind: "state", id: id, typ: "default", desc: desc, doc: doc, isDoc: true, line: line}, nil
}

// stBuild turns the statements into the diagram as stateDb and
// dataFetcher do.
type stBuild struct {
	nodes    map[string]*stInfo
	order    []*stInfo
	edges    []stEdge
	notes    []stNoteAt
	dividers int
	starts   map[string]bool // ids a [*] became
}

// shown is an id as an error message names it: [*] as written.
func (b *stBuild) shown(id string) string {
	if b.starts[id] {
		return "[*]"
	}
	return id
}

type stInfo struct {
	id      string
	kind    string // "default", "start", "end", "fork", "join", "choice", "divider"
	descs   []string
	parent  string // the last composite or region that mentions it; "" for none
	group   bool
	dir     Direction
	line    int
	descLns []int
}

type stEdge struct {
	from, to, label string
	line            int
}

type stNoteAt struct {
	state string
	note  *stNote
	line  int
}

const stRoot = "root"

func buildState(doc []*stStmt, front frontTitle) (*StateDiagram, error) {
	b := &stBuild{nodes: map[string]*stInfo{}, starts: map[string]bool{}}
	root := &stStmt{kind: "root", id: stRoot, doc: doc, isDoc: true}
	if err := b.translate(root, root, true); err != nil {
		return nil, err
	}
	for _, it := range root.doc {
		b.visitItem("", it)
	}
	return b.assemble(root, front)
}

// translate is stateDb.docTranslator: [*] becomes its scope's start or
// end, and a document holding -- becomes one region per part.
func (b *stBuild) translate(parent, node *stStmt, first bool) error {
	if node.kind == "relation" {
		if err := b.translate(parent, node.s1, true); err != nil {
			return err
		}
		return b.translate(parent, node.s2, false)
	}
	if node.kind == "state" && node.id == "[*]" {
		if first {
			node.id = parent.id + "_start"
		} else {
			node.id = parent.id + "_end"
		}
		f := first
		node.start = &f
		b.starts[node.id] = true
	}
	if !node.isDoc {
		return nil
	}
	var regions, cur []*stStmt
	divided := false
	for _, st := range node.doc {
		if st.kind == "state" && st.typ == "divider" {
			divided = true
			if len(cur) == 0 {
				return errf(UnsupportedConstruct, st.line, "an empty concurrent region (-- with nothing before it)")
			}
			regions = append(regions, b.region(cur, st.line))
			cur = nil
			continue
		}
		cur = append(cur, st)
	}
	if divided {
		if len(cur) == 0 {
			// mermaid then leaves the -- in place as a state of its own.
			return errf(UnsupportedConstruct, node.line, "composite %q ends with --", node.id)
		}
		node.doc = append(regions, b.region(cur, cur[0].line))
	}
	for _, st := range node.doc {
		if err := b.translate(node, st, true); err != nil {
			return err
		}
	}
	return nil
}

// region makes a concurrent region. Its id holds a newline, which no id
// in the source can.
func (b *stBuild) region(doc []*stStmt, line int) *stStmt {
	b.dividers++
	return &stStmt{kind: "state", id: "divider\n" + strconv.Itoa(b.dividers), typ: "divider", doc: doc, isDoc: true, line: line}
}

// visitItem is setupDoc for one statement of a document whose composite
// (or region) is parent ("" for the top level).
func (b *stBuild) visitItem(parent string, it *stStmt) {
	switch it.kind {
	case "state":
		b.visit(parent, it)
	case "relation":
		b.visit(parent, it.s1)
		b.visit(parent, it.s2)
		b.edges = append(b.edges, stEdge{from: it.s1.id, to: it.s2.id, label: it.label, line: it.line})
	}
}

// visit is dataFetcher for one state item.
func (b *stBuild) visit(parent string, it *stStmt) {
	n := b.nodes[it.id]
	if n == nil {
		kind := "default"
		switch {
		case it.start != nil && *it.start:
			kind = "start"
		case it.start != nil:
			kind = "end"
		}
		if it.typ != "default" {
			kind = it.typ // "" for a note: mermaid then has no shape
		}
		n = &stInfo{id: it.id, kind: kind, line: it.line}
		b.nodes[it.id] = n
		b.order = append(b.order, n)
	}
	if it.desc != "" {
		n.descs = append(n.descs, it.desc)
		n.descLns = append(n.descLns, it.line)
	}
	if !n.group && it.isDoc {
		n.group = true
		n.dir = TB
		for _, st := range it.doc {
			if st.kind == "dir" {
				n.dir = st.dir
			}
		}
	}
	if parent != "" {
		n.parent = parent // Object.assign: the last parent wins
	}
	if it.note != nil {
		b.notes = append(b.notes, stNoteAt{state: it.id, note: it.note, line: it.line})
	}
	for _, st := range it.doc {
		b.visitItem(it.id, st)
	}
}

// assemble checks what this engine can lay out and builds the scopes.
func (b *stBuild) assemble(root *stStmt, front frontTitle) (*StateDiagram, error) {
	// dataFetcher makes no state of the id "root" and keeps its inside at
	// the top level; a transition to it has no end.
	if n := b.nodes[stRoot]; n != nil {
		return nil, errf(UnsupportedConstruct, n.line, "a state named root (mermaid draws no state for it)")
	}
	// Every parent chain must end at the top level.
	for _, n := range b.order {
		seen := map[string]bool{n.id: true}
		for p := n.parent; p != ""; p = b.nodes[p].parent {
			if seen[p] {
				return nil, errf(UnsupportedConstruct, n.line, "state %q is inside itself", n.id)
			}
			seen[p] = true
		}
	}
	scopes := map[string]*StateScope{"": {Direction: TB}}
	for _, st := range root.doc {
		if st.kind == "dir" {
			scopes[""].Direction = st.dir
		}
	}
	for _, n := range b.order {
		if n.group {
			scopes[n.id] = &StateScope{Direction: n.dir}
		}
	}
	nodes := map[string]*StateNode{}
	var composites []*stInfo
	for _, n := range b.order {
		if n.kind == "divider" {
			continue // a region is a scope, not a state
		}
		sn := &StateNode{ID: n.id, Line: n.line}
		switch {
		case n.group:
			sn.Kind = StateComposite
			if len(n.descs) > 1 {
				// stateDb.extract: "Group nodes can only have label".
				return nil, errf(SyntaxError, n.descLns[1], "composite state %q has more than one description", n.id)
			}
			composites = append(composites, n)
		case len(n.descs) > 0 || n.kind == "default":
			sn.Kind = StatePlain
		case n.kind == "":
			// dataFetcher took the shape from a note statement, which has
			// no type, and the renderer throws "No such shape: undefined".
			return nil, errf(SyntaxError, n.line, "state %q is first met in a note (mermaid: no such shape)", n.id)
		default:
			sn.Kind = map[string]StateKind{"start": StateStart, "end": StateEnd,
				"choice": StateChoice, "fork": StateFork, "join": StateJoin}[n.kind]
		}
		if sn.Kind == StatePlain || sn.Kind == StateComposite {
			texts := n.descs
			lns := n.descLns
			if len(texts) == 0 {
				texts, lns = []string{n.id}, []int{n.line}
			}
			// A state with a title and lines is drawn by createLabel
			// without markdown (rectWithTitle): its texts are plain.
			text := stateText
			if sn.Kind == StatePlain && len(texts) > 1 {
				text = statePlainText
			}
			for i, s := range texts {
				out, err := text(s, lns[i])
				if err != nil {
					return nil, err
				}
				if i == 0 {
					sn.Label = out
				} else {
					sn.Lines = append(sn.Lines, out)
				}
			}
		}
		nodes[n.id] = sn
		scopes[n.parent].States = append(scopes[n.parent].States, sn)
	}
	// A composite's regions, in order; one holding both regions and states
	// of its own was written twice, once with -- and once without.
	for _, n := range b.order {
		if n.kind != "divider" {
			continue
		}
		owner := nodes[n.parent]
		if owner == nil || owner.Kind != StateComposite {
			return nil, errf(UnsupportedConstruct, n.line, "a concurrent region outside a composite state")
		}
		owner.Regions = append(owner.Regions, scopes[n.id])
	}
	for _, c := range composites {
		sn := nodes[c.id]
		inside := scopes[c.id]
		if len(sn.Regions) > 0 {
			if len(inside.States) > 0 {
				return nil, errf(UnsupportedConstruct, c.line, "composite %q has both concurrent regions and states outside them", c.id)
			}
			continue
		}
		sn.Regions = []*StateScope{inside}
	}
	scopeOf := func(id string) string { return b.nodes[id].parent }
	for _, e := range b.edges {
		sf, st := scopeOf(e.from), scopeOf(e.to)
		if sf != st {
			return nil, errf(UnsupportedConstruct, e.line, "the transition %s --> %s crosses a composite state's frame", b.shown(e.from), b.shown(e.to))
		}
		label, err := stateText(e.label, e.line)
		if err != nil {
			return nil, err
		}
		scopes[sf].Transitions = append(scopes[sf].Transitions, &Transition{From: e.from, To: e.to, Label: label, Line: e.line})
	}
	selfLoop := map[string]bool{}
	for _, e := range b.edges {
		if e.from == e.to {
			selfLoop[e.from] = true
		}
	}
	for _, nt := range b.notes {
		text, err := stateText(nt.note.text, nt.line)
		if err != nil {
			return nil, err
		}
		// In a top-down scope a note stands beside its state, which takes
		// a state that can stretch to the note's height and has no loop
		// out of its side; elsewhere it could not be on the side it names.
		if d := scopes[scopeOf(nt.state)].Direction; d == TB || d == BT {
			switch n := nodes[nt.state]; {
			case n.Kind != StatePlain && n.Kind != StateComposite:
				return nil, errf(UnsupportedConstruct, nt.line, "a note beside a %s in a top-down diagram", n.Kind)
			case selfLoop[nt.state]:
				return nil, errf(UnsupportedConstruct, nt.line, "a note beside %s, which has a transition to itself, in a top-down diagram", b.shown(nt.state))
			}
		}
		s := scopes[scopeOf(nt.state)]
		s.Notes = append(s.Notes, &StateNote{State: nt.state, Left: nt.note.left, Text: text, Line: nt.line})
	}
	return &StateDiagram{title: front.text, titleLine: front.line, Root: scopes[""]}, nil
}

// statePlainText is a label drawn without markdown (nonMarkdownToHTML):
// "\\n" as written, a line end and <br> break lines; HTML other than <br>
// is refused.
func statePlainText(s string, line int) (string, error) {
	s = strings.ReplaceAll(restoreEntities(s), `\n`, "\n")
	s = reBreak.ReplaceAllString(s, "\n")
	if reTag.MatchString(s) {
		return "", errf(UnsupportedConstruct, line, "HTML in %q", s)
	}
	return joinTextLines(decodeEntityCodes(s)), nil
}

// stateText is a state label as mermaid's markdown draws it
// (markdownToHTML): each line trimmed and its runs of spaces one, blank
// lines gone, <br> a break, entity codes decoded. Formatting is refused as
// for ER; so is HTML other than <br>.
func stateText(s string, line int) (string, error) {
	s = restoreEntities(s)
	for _, l := range strings.Split(s, "\n") {
		if mdMarkup(l) {
			return "", errf(UnsupportedConstruct, line, "markdown formatting in %q", s)
		}
	}
	s = reBreak.ReplaceAllString(s, "\n")
	if reTag.MatchString(s) {
		return "", errf(UnsupportedConstruct, line, "HTML in %q", s)
	}
	return joinTextLines(decodeEntityCodes(s)), nil
}

// joinTextLines is text as HTML shows it: each line trimmed, runs of
// spaces one, empty lines gone.
func joinTextLines(s string) string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(reSpaceRun.ReplaceAllString(l, " ")); l != "" {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

var reSpaceRun = regexp.MustCompile(`[\t\v\f\r \x{a0}]+`)
