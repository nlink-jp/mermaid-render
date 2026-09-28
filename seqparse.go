package mermaidrender

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// The sequenceDiagram grammar of sequenceDiagram.jison (mermaid 12.0.0) and
// what sequenceDb.ts does with each statement. Refused as unsupported: the
// constructs the RFP leaves for later (create / destroy, half arrows,
// central connections), participant configs (@{ "type": ... }: a symbol the
// engine does not draw) and par_over (in the grammar, not the
// documentation). Ignored: rect's colour (its contents are drawn), link /
// links / properties / details (menus), wrap: prefixes, accTitle /
// accDescr.

// MaxEvents bounds a sequence diagram's messages, notes, activations and
// block lines together.
const MaxEvents = 2000

type seqParser struct {
	toks   []erToken
	i      int
	d      *Sequence
	parts  map[string]*Participant
	box    *Box // the box being read
	active map[*Participant]int
	// pending are participants an activation named before any statement
	// placed them: activation does not place a participant
	// (sequenceDiagram.jison: 'activate' actor yields no addParticipant).
	pending map[string]*Participant
	depth   int // open blocks
	// autonumber: mermaid numbers every arrow, shown or not.
	number     float64
	step       float64
	showNumber bool
}

func parseSequence(lines []srcLine, title frontTitle) (Diagram, error) {
	toks, err := lexSequence(lines)
	if err != nil {
		return nil, err
	}
	p := &seqParser{toks: toks, d: &Sequence{title: title.text, titleLine: title.line},
		parts: map[string]*Participant{}, active: map[*Participant]int{}, pending: map[string]*Participant{},
		number: 1, step: 1}
	for p.peek().kind == "NEWLINE" {
		p.i++
	}
	if _, err := p.expect("SD"); err != nil {
		return nil, err
	}
	if err := p.document(nil); err != nil {
		return nil, err
	}
	if t := p.peek(); t.kind != "EOF" {
		return nil, p.unexpected(t)
	}
	var first *Participant
	for _, pt := range p.pending {
		if first == nil || pt.Line < first.Line || pt.Line == first.Line && pt.ID < first.ID {
			first = pt
		}
	}
	if first != nil {
		// mermaid fails to draw an activation of a participant that
		// nothing places.
		return nil, errf(SyntaxError, first.Line, "activating %q, which is never a participant", first.ID)
	}
	// A box draws around neighbouring columns. A participant mentioned
	// before its box was declared keeps its earlier place, and a box around
	// columns that are not side by side would take in a participant it
	// does not hold (mermaid draws that too): refused, not drawn wrong.
	at := map[*Participant]int{}
	for i, pt := range p.d.Participants {
		at[pt] = i
	}
	for _, b := range p.d.Boxes {
		lo, hi := len(p.d.Participants), -1
		for _, pt := range b.Participants {
			lo, hi = min(lo, at[pt]), max(hi, at[pt])
		}
		if hi >= 0 && hi-lo+1 != len(b.Participants) {
			return nil, errf(UnsupportedConstruct, b.Line, "box %q holds participants that are not side by side", b.Title)
		}
	}
	return p.d, nil
}

func (p *seqParser) peek() erToken { return p.toks[p.i] }

func (p *seqParser) next() erToken {
	t := p.toks[p.i]
	if t.kind != "EOF" {
		p.i++
	}
	return t
}

func (p *seqParser) unexpected(t erToken) error {
	switch t.kind {
	case "EOF":
		return errf(SyntaxError, t.line, "unexpected end of diagram")
	case "NEWLINE":
		return errf(SyntaxError, t.line, "unexpected end of line")
	}
	return errf(SyntaxError, t.line, "unexpected %q", t.text)
}

func (p *seqParser) expect(kind string) (erToken, error) {
	t := p.next()
	if t.kind != kind {
		return t, p.unexpected(t)
	}
	return t, nil
}

// document reads lines until a token in stop (or the end).
func (p *seqParser) document(stop map[string]bool) error {
	for {
		t := p.peek()
		if t.kind == "EOF" || stop[t.kind] {
			return nil
		}
		switch t.kind {
		case "NEWLINE", "INVALID":
			// line: NEWLINE | INVALID — a stray character is dropped.
			p.i++
			continue
		}
		if err := p.statement(); err != nil {
			return err
		}
		if len(p.d.Events) > MaxEvents {
			return errf(UnsupportedConstruct, t.line, "more than %d messages, notes and blocks", MaxEvents)
		}
	}
}

var blockOf = map[string]BlockKind{"loop": Loop, "opt": Opt, "alt": Alt, "par": Par, "critical": Critical, "break": Break}

// sectionOf is the section keyword each block may hold.
var sectionOf = map[BlockKind]string{Alt: "else", Par: "and", Critical: "option"}

func (p *seqParser) statement() error {
	t := p.peek()
	switch t.kind {
	case "participant", "participant_actor":
		return p.participantStatement()
	case "create", "destroy":
		return errf(UnsupportedConstruct, t.line, "%s (creating and destroying participants)", t.text)
	case "box":
		return p.boxStatement()
	case "ACTOR":
		return p.signal()
	case "autonumber":
		return p.autonumber()
	case "activate", "deactivate":
		p.i++
		a, err := p.actor()
		if err != nil {
			return err
		}
		if _, err := p.expect("NEWLINE"); err != nil {
			return err
		}
		pt := p.parts[a]
		if pt == nil {
			if pt = p.pending[a]; pt == nil {
				pt = &Participant{ID: a, Line: t.line}
				p.pending[a] = pt
			}
		}
		if t.kind == "activate" {
			p.activate(pt, t.line)
			return nil
		}
		return p.deactivate(pt, t.line)
	case "note":
		return p.note()
	case "links", "link", "properties", "details":
		// Actor menus: interaction only, but they place their participant
		// (the grammar yields [$2, …]).
		p.i++
		a, err := p.actor()
		if err != nil {
			return err
		}
		if _, err := p.expect("TXT"); err != nil {
			return err
		}
		if _, err := p.expect("NEWLINE"); err != nil {
			return err
		}
		_, err = p.mention(a, t.line)
		return err
	case "title", "legacy_title":
		p.i++
		// "title" or "title:", then one whitespace character (a rune: it
		// may be U+3000 or U+00A0), then the text.
		rest := t.text[len("title"):]
		if t.kind == "legacy_title" {
			rest = rest[1:]
		}
		_, n := utf8.DecodeRuneInString(rest)
		text, err := p.text(rest[n:], t.line)
		if err != nil {
			return err
		}
		p.d.title, p.d.titleLine = text, t.line
		return nil
	case "acc_title", "acc_descr":
		p.i++
		if v := p.peek().kind; v == "acc_title_value" || v == "acc_descr_value" {
			p.i++
			return nil
		}
		return p.unexpected(p.peek())
	case "acc_descr_multiline_value":
		p.i++
		return nil
	case "rect":
		// A background colour: the frame is dropped, its contents drawn.
		p.i++
		if _, err := p.expect("restOfLine"); err != nil {
			return err
		}
		if err := p.document(map[string]bool{"end": true}); err != nil {
			return err
		}
		_, err := p.expect("end")
		return err
	case "par_over":
		return errf(UnsupportedConstruct, t.line, "par_over (not in the documentation)")
	}
	if kind, ok := blockOf[t.kind]; ok {
		return p.block(kind)
	}
	return p.unexpected(t)
}

// block reads kind restOfLine document (section restOfLine document)* end.
// MaxNesting bounds how deep blocks nest: the parser recurses per level.
const MaxNesting = 50

func (p *seqParser) block(kind BlockKind) error {
	t := p.next()
	if p.depth >= MaxNesting {
		return errf(UnsupportedConstruct, t.line, "blocks nested more than %d deep", MaxNesting)
	}
	p.depth++
	defer func() { p.depth-- }()
	title, err := p.expect("restOfLine")
	if err != nil {
		return err
	}
	text, err := p.message(title.text, t.line)
	if err != nil {
		return err
	}
	p.d.Events = append(p.d.Events, &Event{Kind: BlockStart, Block: kind, Text: text, Line: t.line})
	stop := map[string]bool{"end": true}
	section := sectionOf[kind]
	if section != "" {
		stop[section] = true
	}
	for {
		if err := p.document(stop); err != nil {
			return err
		}
		s := p.next()
		if s.kind == "end" {
			p.d.Events = append(p.d.Events, &Event{Kind: BlockEnd, Block: kind, Line: s.line})
			return nil
		}
		if s.kind != section || section == "" {
			return p.unexpected(s)
		}
		title, err := p.expect("restOfLine")
		if err != nil {
			return err
		}
		text, err := p.message(title.text, s.line)
		if err != nil {
			return err
		}
		p.d.Events = append(p.d.Events, &Event{Kind: BlockSection, Block: kind, Text: text, Line: s.line})
	}
}

// participantStatement: (participant | actor) ACTOR [@{...}] [as text]
// NEWLINE.
func (p *seqParser) participantStatement() error {
	t := p.next()
	id, err := p.expect("ACTOR")
	if err != nil {
		return err
	}
	if p.peek().kind == "CONFIG_START" {
		return errf(UnsupportedConstruct, t.line, "participant configuration @{...} (typed participant symbols)")
	}
	var desc *string
	if p.peek().kind == "AS" {
		p.i++
		rest, err := p.expect("restOfLine")
		if err != nil {
			return err
		}
		text, err := p.message(rest.text, t.line)
		if err != nil {
			return err
		}
		desc = &text
	}
	if _, err := p.expect("NEWLINE"); err != nil {
		return err
	}
	return p.addParticipant(id.text, desc, t.kind == "participant_actor", t.line)
}

// addParticipant is sequenceDb.addActor: the first mention places a
// participant; a later one with a description replaces its label and kind,
// one without leaves it as it is. A participant stays in its first box.
func (p *seqParser) addParticipant(id string, desc *string, actor bool, line int) error {
	if pt := p.pending[id]; pt != nil {
		// Named by an activation first: placed now, where it is placed.
		delete(p.pending, id)
		label, err := p.text(id, line)
		if err != nil {
			return err
		}
		if desc != nil {
			label = *desc
		}
		pt.Label, pt.Actor, pt.Box, pt.Line = label, actor, p.box, line
		p.parts[id] = pt
		p.d.Participants = append(p.d.Participants, pt)
		if p.box != nil {
			p.box.Participants = append(p.box.Participants, pt)
		}
		return nil
	}
	old := p.parts[id]
	if old != nil {
		if p.box != nil && old.Box != nil && old.Box != p.box {
			return errf(SyntaxError, line, "participant %q is in two boxes", id)
		}
		if old.Box == nil && p.box != nil {
			old.Box = p.box
			p.box.Participants = append(p.box.Participants, old)
		}
		if desc == nil {
			return nil
		}
		old.Label, old.Actor = *desc, actor
		return nil
	}
	label, err := p.text(id, line)
	if err != nil {
		return err
	}
	if desc != nil {
		label = *desc
	}
	np := &Participant{ID: id, Label: label, Actor: actor, Box: p.box, Line: line}
	p.parts[id] = np
	p.d.Participants = append(p.d.Participants, np)
	if p.box != nil {
		p.box.Participants = append(p.box.Participants, np)
	}
	return nil
}

// mention is a participant named in a message, note or activation.
func (p *seqParser) mention(id string, line int) (*Participant, error) {
	if err := p.addParticipant(id, nil, false, line); err != nil {
		return nil, err
	}
	return p.parts[id], nil
}

func (p *seqParser) actor() (string, error) {
	t, err := p.expect("ACTOR")
	return t.text, err
}

// boxStatement: box restOfLine (participant statements)* end.
func (p *seqParser) boxStatement() error {
	t := p.next()
	rest, err := p.expect("restOfLine")
	if err != nil {
		return err
	}
	title, err := p.text(boxTitle(rest.text), t.line)
	if err != nil {
		return err
	}
	p.box = &Box{Title: title, Line: t.line}
	p.d.Boxes = append(p.d.Boxes, p.box)
	for {
		switch p.peek().kind {
		case "NEWLINE":
			p.i++
			continue
		case "participant", "participant_actor":
			if err := p.participantStatement(); err != nil {
				return err
			}
			continue
		case "end":
			p.i++
			p.box = nil
			return nil
		}
		return p.unexpected(p.peek())
	}
}

var arrowKinds = map[string]struct {
	dotted bool
	head   ArrowHead
	both   bool
}{
	"SOLID_OPEN_ARROW": {false, HeadNone, false}, "DOTTED_OPEN_ARROW": {true, HeadNone, false},
	"SOLID_ARROW": {false, HeadFilled, false}, "DOTTED_ARROW": {true, HeadFilled, false},
	"BIDIRECTIONAL_SOLID_ARROW": {false, HeadFilled, true}, "BIDIRECTIONAL_DOTTED_ARROW": {true, HeadFilled, true},
	"SOLID_CROSS": {false, HeadCross, false}, "DOTTED_CROSS": {true, HeadCross, false},
	"SOLID_POINT": {false, HeadOpen, false}, "DOTTED_POINT": {true, HeadOpen, false},
}

// signal: ACTOR arrow [+|-] ACTOR TXT NEWLINE.
func (p *seqParser) signal() error {
	from := p.next()
	if p.peek().kind == "()" {
		return errf(UnsupportedConstruct, from.line, "a central connection ()")
	}
	arrow := p.next()
	if arrow.kind == "HALF_ARROW" {
		return errf(UnsupportedConstruct, arrow.line, "a half arrow %q", arrow.text)
	}
	ak, ok := arrowKinds[arrow.kind]
	if !ok {
		return p.unexpected(arrow)
	}
	mod := ""
	switch p.peek().kind {
	case "+", "-":
		mod = p.next().kind
	case "()":
		return errf(UnsupportedConstruct, from.line, "a central connection ()")
	}
	to, err := p.expect("ACTOR")
	if err != nil {
		return err
	}
	txt, err := p.expect("TXT")
	if err != nil {
		return err
	}
	if _, err := p.expect("NEWLINE"); err != nil {
		return err
	}
	text, err := p.message(txt.text[1:], from.line)
	if err != nil {
		return err
	}
	a, err := p.mention(from.text, from.line)
	if err != nil {
		return err
	}
	b, err := p.mention(to.text, from.line)
	if err != nil {
		return err
	}
	e := &Event{Kind: Message, From: a, To: b, Dotted: ak.dotted, Head: ak.head, BothEnds: ak.both, Text: text, Line: from.line}
	if p.showNumber {
		e.Number = strconv.FormatFloat(p.number, 'f', -1, 64)
	}
	p.number = math.Round((p.number+p.step)*100) / 100
	p.d.Events = append(p.d.Events, e)
	switch mod {
	case "+":
		p.activate(b, from.line)
	case "-":
		return p.deactivate(a, from.line)
	}
	return nil
}

func (p *seqParser) activate(pt *Participant, line int) {
	p.active[pt]++
	p.d.Events = append(p.d.Events, &Event{Kind: Activate, From: pt, Line: line})
}

// deactivate fails for a participant with no activation, as sequenceDb's
// addSignal does ("Trying to inactivate an inactive participant").
func (p *seqParser) deactivate(pt *Participant, line int) error {
	if p.active[pt] < 1 {
		return errf(SyntaxError, line, "deactivating %q, which is not active", pt.ID)
	}
	p.active[pt]--
	p.d.Events = append(p.d.Events, &Event{Kind: Deactivate, From: pt, Line: line})
	return nil
}

// autonumber [start [step]] | autonumber off. A start or step of 0 keeps
// the one before (the renderer's "start || sequenceIndex").
func (p *seqParser) autonumber() error {
	p.i++
	switch p.peek().kind {
	case "off":
		p.i++
		p.showNumber = false
	case "NUM":
		start, _ := strconv.ParseFloat(p.next().text, 64)
		if start != 0 {
			p.number = start
		}
		if p.peek().kind == "NUM" {
			step, _ := strconv.ParseFloat(p.next().text, 64)
			if step != 0 {
				p.step = step
			}
		} else {
			// sequenceIndexStep: 1 in the grammar; the renderer keeps
			// "step || sequenceIndexStep", so 1 it is.
			p.step = 1
		}
		p.showNumber = true
	default:
		p.showNumber = true
	}
	_, err := p.expect("NEWLINE")
	return err
}

// note: note (left of | right of) ACTOR TXT | note over ACTOR [, ACTOR] TXT.
func (p *seqParser) note() error {
	t := p.next()
	var place NotePlace
	switch p.next().kind {
	case "left_of":
		place = LeftOf
	case "right_of":
		place = RightOf
	case "over":
		place = Over
	default:
		return p.unexpected(p.toks[p.i-1])
	}
	a, err := p.actor()
	if err != nil {
		return err
	}
	b := a
	if place == Over && p.peek().kind == "," {
		p.i++
		if b, err = p.actor(); err != nil {
			return err
		}
	}
	txt, err := p.expect("TXT")
	if err != nil {
		return err
	}
	if _, err := p.expect("NEWLINE"); err != nil {
		return err
	}
	pa, err := p.mention(a, t.line)
	if err != nil {
		return err
	}
	pb, err := p.mention(b, t.line)
	if err != nil {
		return err
	}
	text, err := p.message(txt.text[1:], t.line)
	if err != nil {
		return err
	}
	p.d.Events = append(p.d.Events, &Event{Kind: Note, From: pa, To: pb, Place: place, Text: text, Line: t.line})
	return nil
}

// reWrap is sequenceDb.extractWrap's prefix, case-sensitive as there.
var reWrap = regexp.MustCompile(`^:?(?:no)?wrap:`)

// message is sequenceDb.parseMessage: trimmed, a wrap: or nowrap: prefix
// dropped (wrapping is presentation), then the label text.
func (p *seqParser) message(s string, line int) (string, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimSpace(reWrap.ReplaceAllString(s, ""))
	return p.text(s, line)
}

// text: entity codes back, <br> as a break, other HTML refused.
func (p *seqParser) text(s string, line int) (string, error) {
	out, ok := decodeLabel(restoreEntities(s))
	if !ok {
		return "", errf(UnsupportedConstruct, line, "HTML in %q", s)
	}
	return out, nil
}

// boxTitle is sequenceDb.parseBoxData without the colour: the first word,
// or an rgb()/rgba()/hsl()/hsla() value, is a colour when CSS reads it as
// one, and the rest is the title; otherwise the whole line is the title.
func boxTitle(s string) string {
	m := reBoxData.FindStringSubmatch(s)
	color, title := strings.TrimSpace(m[1]), strings.TrimSpace(m[2])
	if !isCSSColor(color) {
		return strings.TrimSpace(reWrap.ReplaceAllString(strings.TrimSpace(s), ""))
	}
	return strings.TrimSpace(reWrap.ReplaceAllString(title, ""))
}

var (
	reBoxData  = regexp.MustCompile(`^((?:rgba?|hsla?)\s*\(.*\)|\w*)(.*)$`)
	reColorFun = regexp.MustCompile(`(?i)^(rgba?|hsla?)\s*\(\s*[-+0-9.%\s,/a-z]*\)$`)
)

func isCSSColor(s string) bool {
	if s == "" {
		return false
	}
	if reColorFun.MatchString(s) {
		return true
	}
	return cssColorNames[strings.ToLower(s)]
}

// cssColorNames: the CSS named colours, and the keywords CSS.supports
// accepts as a colour.
var cssColorNames = func() map[string]bool {
	m := map[string]bool{}
	for _, n := range strings.Fields(`aliceblue antiquewhite aqua aquamarine azure beige bisque black
	blanchedalmond blue blueviolet brown burlywood cadetblue chartreuse chocolate coral cornflowerblue
	cornsilk crimson cyan darkblue darkcyan darkgoldenrod darkgray darkgreen darkgrey darkkhaki
	darkmagenta darkolivegreen darkorange darkorchid darkred darksalmon darkseagreen darkslateblue
	darkslategray darkslategrey darkturquoise darkviolet deeppink deepskyblue dimgray dimgrey
	dodgerblue firebrick floralwhite forestgreen fuchsia gainsboro ghostwhite gold goldenrod gray
	green greenyellow grey honeydew hotpink indianred indigo ivory khaki lavender lavenderblush
	lawngreen lemonchiffon lightblue lightcoral lightcyan lightgoldenrodyellow lightgray lightgreen
	lightgrey lightpink lightsalmon lightseagreen lightskyblue lightslategray lightslategrey
	lightsteelblue lightyellow lime limegreen linen magenta maroon mediumaquamarine mediumblue
	mediumorchid mediumpurple mediumseagreen mediumslateblue mediumspringgreen mediumturquoise
	mediumvioletred midnightblue mintcream mistyrose moccasin navajowhite navy oldlace olive
	olivedrab orange orangered orchid palegoldenrod palegreen paleturquoise palevioletred papayawhip
	peachpuff peru pink plum powderblue purple rebeccapurple red rosybrown royalblue saddlebrown
	salmon sandybrown seagreen seashell sienna silver skyblue slateblue slategray slategrey snow
	springgreen steelblue tan teal thistle tomato turquoise violet wheat white whitesmoke yellow
	yellowgreen transparent currentcolor inherit initial unset revert
	accentcolor accentcolortext activetext buttonborder buttonface buttontext canvas canvastext
	field fieldtext graytext highlight highlighttext linktext mark marktext selecteditem
	selecteditemtext visitedtext activeborder activecaption appworkspace background
	buttonhighlight buttonshadow captiontext inactiveborder inactivecaption inactivecaptiontext
	infobackground infotext menu menutext scrollbar threeddarkshadow threedface threedhighlight
	threedlightshadow threedshadow window windowframe windowtext`) {
		m[n] = true
	}
	return m
}()
