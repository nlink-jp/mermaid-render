package mermaidrender

import (
	"regexp"
	"strings"
)

// The erDiagram grammar of erDiagram.jison (mermaid 12.0.0), statement by
// statement. What the grammar accepts and erDb stores is read; what only
// styles a diagram is checked for form and dropped; subgraphs and the
// undocumented "u" cardinality are refused as unsupported constructs.

type erParser struct {
	toks     []erToken
	i        int
	d        *ER
	entities map[string]*Entity
}

func parseER(lines []srcLine, title frontTitle) (Diagram, error) {
	toks, err := lexER(lines)
	if err != nil {
		return nil, err
	}
	p := &erParser{toks: toks, d: &ER{title: title.text, titleLine: title.line}, entities: map[string]*Entity{}}
	if p.peek().kind != "ER_DIAGRAM" {
		return nil, errf(SyntaxError, p.peek().line, "expected erDiagram")
	}
	p.i++
	for p.peek().kind != "EOF" {
		if err := p.statement(); err != nil {
			return nil, err
		}
	}
	if len(p.d.Relationships) > MaxLinks {
		return nil, errf(UnsupportedConstruct, 0, "%d relationships (at most %d)", len(p.d.Relationships), MaxLinks)
	}
	for _, e := range p.d.Entities {
		if err := p.finishEntity(e); err != nil {
			return nil, err
		}
	}
	for _, rel := range p.d.Relationships {
		if rel.Label, err = p.text(rel.Label, rel.Line, true); err != nil {
			return nil, err
		}
	}
	return p.d, nil
}

func (p *erParser) peek() erToken { return p.toks[p.i] }

func (p *erParser) next() erToken {
	t := p.toks[p.i]
	if t.kind != "EOF" {
		p.i++
	}
	return t
}

func (p *erParser) unexpected(t erToken) error {
	if t.kind == "EOF" {
		return errf(SyntaxError, t.line, "unexpected end of diagram")
	}
	return errf(SyntaxError, t.line, "unexpected %q", t.text)
}

func (p *erParser) expect(kind string) (erToken, error) {
	t := p.next()
	if t.kind != kind {
		return t, p.unexpected(t)
	}
	return t, nil
}

var entityNameKinds = map[string]bool{"ENTITY_NAME": true, "UNICODE_TEXT": true, "NUM": true, "DECIMAL_NUM": true, "ENTITY_ONE": true}

var cardinalityKinds = map[string]Cardinality{
	"ONLY_ONE": ExactlyOne, "ZERO_OR_ONE": ZeroOrOne, "ZERO_OR_MORE": ZeroOrMore, "ONE_OR_MORE": OneOrMore,
}

// entityName reads one name token: a quoted name loses every quote
// (entityName: ENTITY_NAME { $1.replace(/"/g, ”) }).
func (p *erParser) entityName() (string, bool) {
	t := p.peek()
	if !entityNameKinds[t.kind] {
		return "", false
	}
	p.i++
	if t.kind == "ENTITY_NAME" {
		return strings.ReplaceAll(t.text, `"`, ""), true
	}
	return t.text, true
}

func (p *erParser) statement() error {
	t := p.peek()
	switch {
	case t.kind == "NEWLINE":
		p.i++
		return nil
	case entityNameKinds[t.kind]:
		return p.entityStatement()
	case t.kind == "acc_title" || t.kind == "acc_descr":
		p.i++
		if v := p.peek().kind; v == "acc_title_value" || v == "acc_descr_value" {
			p.i++
			return nil
		}
		return p.unexpected(p.peek())
	case t.kind == "acc_descr_multiline_value":
		p.i++
		return nil
	case strings.HasPrefix(t.kind, "direction_"):
		p.i++
		p.d.Direction = map[string]Direction{"direction_tb": TB, "direction_bt": BT, "direction_rl": RL, "direction_lr": LR}[t.kind]
		return nil
	case t.kind == "CLASSDEF" || t.kind == "STYLE":
		p.i++
		return p.styleRest()
	case t.kind == "CLASS":
		p.i++
		if err := p.idList(); err != nil {
			return err
		}
		return p.idList()
	case t.kind == "SUBGRAPH":
		return errf(UnsupportedConstruct, t.line, "subgraph in an erDiagram")
	}
	return p.unexpected(t)
}

func (p *erParser) entityStatement() error {
	line := p.peek().line
	name, _ := p.entityName()
	switch p.peek().kind {
	case "STYLE_SEPARATOR":
		p.i++
		if err := p.idList(); err != nil {
			return err
		}
		if _, ok := cardinalityKinds[p.peek().kind]; ok || p.peek().kind == "MD_PARENT" {
			return p.relationship(name, line)
		}
		if p.peek().kind == "BLOCK_START" {
			return p.block(p.entity(name, line))
		}
		p.entity(name, line)
		return nil
	case "BLOCK_START":
		return p.block(p.entity(name, line))
	case "SQS":
		p.i++
		alias, ok := p.entityName()
		if !ok {
			return p.unexpected(p.peek())
		}
		if _, err := p.expect("SQE"); err != nil {
			return err
		}
		e := p.entity(name, line)
		if e.Alias == "" {
			e.Alias = alias
		}
		if p.peek().kind == "STYLE_SEPARATOR" {
			p.i++
			if err := p.idList(); err != nil {
				return err
			}
		}
		if p.peek().kind == "BLOCK_START" {
			return p.block(e)
		}
		return nil
	}
	if _, ok := cardinalityKinds[p.peek().kind]; ok || p.peek().kind == "MD_PARENT" {
		return p.relationship(name, line)
	}
	p.entity(name, line)
	return nil
}

// entity finds or adds an entity (erDb.addEntity).
func (p *erParser) entity(name string, line int) *Entity {
	if e, ok := p.entities[name]; ok {
		return e
	}
	e := &Entity{Name: name, Line: line}
	p.entities[name] = e
	p.d.Entities = append(p.d.Entities, e)
	return e
}

func (p *erParser) cardinality() (Cardinality, error) {
	t := p.next()
	if t.kind == "MD_PARENT" {
		return 0, errf(UnsupportedConstruct, t.line, "the \"u\" cardinality (not in the documentation)")
	}
	c, ok := cardinalityKinds[t.kind]
	if !ok {
		return 0, p.unexpected(t)
	}
	return c, nil
}

// relationship reads relSpec entityName [::: idList] COLON role. The
// first cardinality is written next to the first entity.
func (p *erParser) relationship(from string, line int) error {
	c1, err := p.cardinality()
	if err != nil {
		return err
	}
	rt := p.next()
	if rt.kind != "IDENTIFYING" && rt.kind != "NON_IDENTIFYING" {
		return p.unexpected(rt)
	}
	c2, err := p.cardinality()
	if err != nil {
		return err
	}
	to, ok := p.entityName()
	if !ok {
		return p.unexpected(p.peek())
	}
	if p.peek().kind == "STYLE_SEPARATOR" {
		p.i++
		if err := p.idList(); err != nil {
			return err
		}
	}
	if _, err := p.expect("COLON"); err != nil {
		return err
	}
	role := p.next()
	switch role.kind {
	case "WORD", "ENTITY_NAME":
		role.text = strings.ReplaceAll(role.text, `"`, "")
	case "UNICODE_TEXT":
	default:
		return p.unexpected(role)
	}
	a, b := p.entity(from, line), p.entity(to, line)
	p.d.Relationships = append(p.d.Relationships, &Relationship{
		From: a, To: b, FromCard: c1, ToCard: c2,
		Identifying: rt.kind == "IDENTIFYING", Label: role.text, Line: line,
	})
	return nil
}

// block reads { attributes } for an entity; blocks for one entity add up.
func (p *erParser) block(e *Entity) error {
	p.i++ // BLOCK_START
	for {
		t := p.peek()
		if t.kind == "BLOCK_STOP" {
			p.i++
			return nil
		}
		if t.kind != "ATTRIBUTE_WORD" {
			return p.unexpected(t)
		}
		p.i++
		a := Attribute{Type: t.text, Line: t.line}
		if p.peek().kind == "?" {
			p.i++
			a.Type += "?"
		}
		n, err := p.expect("ATTRIBUTE_WORD")
		if err != nil {
			return err
		}
		a.Name = n.text
		if p.peek().kind == "ATTRIBUTE_KEY" {
			for {
				k := p.next()
				a.Keys = append(a.Keys, k.text) // as written: pk stays pk
				if p.peek().kind != "," {
					break
				}
				p.i++
				if p.peek().kind != "ATTRIBUTE_KEY" {
					return p.unexpected(p.peek())
				}
			}
		}
		if p.peek().kind == "COMMENT" {
			a.Comment = strings.ReplaceAll(p.next().text, `"`, "")
		}
		e.Attributes = append(e.Attributes, a)
		if len(e.Attributes) > MaxAttributes {
			return errf(UnsupportedConstruct, t.line, "entity %q has more than %d attributes", e.Name, MaxAttributes)
		}
	}
}

// MaxAttributes bounds one entity's table.
const MaxAttributes = 200

// idList: id (, id)*.
func (p *erParser) idList() error {
	for {
		t := p.next()
		if t.kind != "UNICODE_TEXT" && t.kind != "STYLE_TEXT" {
			return p.unexpected(t)
		}
		if p.peek().kind != "COMMA" {
			return nil
		}
		p.i++
	}
}

// styleRest reads the rest of a style or classDef statement: idList, then
// styles (components separated by commas), then a separator.
func (p *erParser) styleRest() error {
	if err := p.idList(); err != nil {
		return err
	}
	component := func(k string) bool {
		return k == "STYLE_TEXT" || k == "NUM" || k == "COLON" || k == "BRKT"
	}
	for {
		if !component(p.peek().kind) {
			return p.unexpected(p.peek())
		}
		for component(p.peek().kind) {
			p.i++
		}
		if p.peek().kind != "COMMA" {
			break
		}
		p.i++
	}
	switch t := p.next(); t.kind {
	case "SEMI", "NEWLINE", "EOF":
		return nil
	default:
		return p.unexpected(t)
	}
}

// finishEntity sets the label and turns attribute text into what is shown.
func (p *erParser) finishEntity(e *Entity) error {
	// Names are keys while parsing, placeholders and all; afterwards they
	// read as written.
	e.Name, e.Alias = restoreEntities(e.Name), restoreEntities(e.Alias)
	label := e.Name
	if e.Alias != "" {
		label = e.Alias
	}
	var err error
	if e.Label, err = p.text(label, e.Line, true); err != nil {
		return err
	}
	for i := range e.Attributes {
		a := &e.Attributes[i]
		for _, s := range []*string{&a.Type, &a.Name, &a.Comment} {
			if *s, err = p.text(*s, e.Line, false); err != nil {
				return err
			}
		}
	}
	return nil
}

// text turns written text into shown text: entity codes back, generics
// (parseGenericTypes), and for labels drawn as markdown, <br> as a break;
// markdown formatting is refused rather than shown as marks.
func (p *erParser) text(s string, line int, markdown bool) (string, error) {
	s = restoreEntities(s)
	if markdown && mdMarkup(s) {
		return "", errf(UnsupportedConstruct, line, "markdown formatting in %q", s)
	}
	if !markdown {
		return decodeEntitiesOnly(parseGenericTypes(s)), nil
	}
	out, ok := decodeLabel(parseGenericTypes(s))
	if !ok {
		return "", errf(UnsupportedConstruct, line, "HTML in %q", s)
	}
	return out, nil
}

// mdMarkup finds what mermaid's markdown labels might draw as formatting
// (entity names and relationship labels are markdown in erDb): emphasis
// (hasMdEmphasis), code spans, and a line that starts a heading, quote or
// list.
func mdMarkup(s string) bool { return hasMdEmphasis(s) || reMarkdown.MatchString(s) }

var reMarkdown = regexp.MustCompile("`[^`]+`" + `|^\s*(?:#{1,6}\s|>|[-+*]\s|\d+[.)]\s)`)

func decodeEntitiesOnly(s string) string {
	return reEntity.ReplaceAllStringFunc(s, func(m string) string {
		out, _ := decodeLabel(m)
		return out
	})
}

// parseGenericTypes is common.ts parseGenericTypes (mermaid 12.0.0):
// pairs of ~ become < and >, "~K, V~" split at the comma included.
func parseGenericTypes(input string) string {
	sets := splitKeepComma(input)
	var out []string
	for i := 0; i < len(sets); i++ {
		this := sets[i]
		if this == "," && i > 0 && i+1 < len(sets) {
			prev, next := sets[i-1], sets[i+1]
			if strings.Count(prev, "~") == 1 && strings.Count(next, "~") == 1 {
				this = prev + "," + next
				i++
				out = out[:len(out)-1]
			}
		}
		out = append(out, processTildes(this))
	}
	return strings.Join(out, "")
}

func splitKeepComma(s string) []string {
	var out []string
	for {
		i := strings.IndexByte(s, ',')
		if i < 0 {
			return append(out, s)
		}
		out = append(out, s[:i], ",")
		s = s[i+1:]
	}
}

func processTildes(input string) string {
	n := strings.Count(input, "~")
	if n <= 1 {
		return input
	}
	starting := false
	if n%2 != 0 && strings.HasPrefix(input, "~") {
		input = input[1:]
		starting = true
	}
	chars := []rune(input)
	index := func(last bool) int {
		pos := -1
		for i, c := range chars {
			if c == '~' {
				pos = i
				if !last {
					return i
				}
			}
		}
		return pos
	}
	first, last := index(false), index(true)
	for first != -1 && last != -1 && first != last {
		chars[first], chars[last] = '<', '>'
		first, last = index(false), index(true)
	}
	out := string(chars)
	if starting {
		out = "~" + out
	}
	return out
}
