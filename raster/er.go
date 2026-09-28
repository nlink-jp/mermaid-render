package raster

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	mr "github.com/nlink-jp/mermaid-render"
)

// An ER diagram is laid out as a flowchart whose nodes are the entities'
// tables and whose links are the relationships, with wider spacing: every
// relationship ends in a cardinality marker at both ends, and a crow's foot
// spreads across the line. Each table is a header with the entity's name,
// then a row per attribute: type, name, keys, comment — the last two columns
// only when some attribute has them (as mermaid's erBox draws).

// ER spacing, in em.
const (
	erPortGap = 1.4 // two crow's feet side by side keep 0.6 em apart (1.2 looked crowded)
	// Between layers: the longest marker (1.32) and 0.9 em more, so a
	// label between two entities clears both markers (1.6 put labels
	// against the lower marker: the operator's second ER check).
	erRankGap = 2.2
	erEndRoom = 1.6 // a bent link runs straight this far into its end
	// A table is wide: its links may use 80% of a face (a shape's 60%
	// kept a link from standing under the box it goes to).
	erFaceSpread = 0.8
	erLabelRoom  = 1.2 // a label to a bend below it (0.45 looked cramped)
	cellPadX     = 0.5
	cellPadY     = 0.22
	hdrPadY      = 0.4
	// markerReach is how far the longest marker (zero or more: a crow's
	// foot and a circle) runs along its line from the entity.
	markerReach = 1.32
)

// erTable is one entity's table, measured.
type erTable struct {
	hdrH  float64
	cols  []float64 // column widths
	rows  []float64 // row heights
	cells [][]string
}

func (t *erTable) size(labelW float64) (w, h float64) {
	for _, c := range t.cols {
		w += c
	}
	w = math.Max(w, labelW+2*padX)
	h = t.hdrH
	for _, r := range t.rows {
		h += r
	}
	return w, h
}

// erLayout is a placed ER diagram: the layout (Nodes in entity order, one
// Edge per relationship), each entity's table, and the relationship behind
// each placed link.
type erLayout struct {
	*Layout
	tables []erTable // nil entry: an entity without attributes
	rels   map[*mr.Link]*mr.Relationship
	graph  *mr.Flowchart // what was laid out
}

func layoutER(d *mr.ER, m measurer) (*erLayout, error) {
	if len(d.Entities) > MaxNodes || len(d.Relationships) > MaxLinks {
		return nil, &mr.Error{Kind: mr.UnsupportedConstruct, Msg: fmt.Sprintf(
			"too large to lay out: %d entities, %d relationships (limits %d, %d)",
			len(d.Entities), len(d.Relationships), MaxNodes, MaxLinks)}
	}
	f := &mr.Flowchart{Direction: d.Direction}
	var sizes [][2]float64
	tables := make([]erTable, len(d.Entities))
	id := map[*mr.Entity]string{}
	for i, e := range d.Entities {
		id[e] = "e" + strconv.Itoa(i)
		f.Nodes = append(f.Nodes, &mr.Node{ID: id[e], Label: e.Label, Shape: mr.Rect, Line: e.Line})
		lw, lh, err := m(e.Label, true)
		if err != nil {
			return nil, glyphErr(err, e.Line)
		}
		if len(e.Attributes) == 0 {
			w, h := nodeSize(mr.Rect, lw, lh)
			sizes = append(sizes, [2]float64{w, h})
			continue
		}
		t, err := measureTable(e, m)
		if err != nil {
			return nil, err
		}
		t.hdrH = lh + 2*hdrPadY
		tables[i] = t
		w, h := t.size(lw)
		sizes = append(sizes, [2]float64{w, h})
	}
	rels := map[*mr.Link]*mr.Relationship{}
	for _, r := range d.Relationships {
		stroke := mr.Solid
		if !r.Identifying {
			stroke = mr.Dotted
		}
		// Heads stand for the markers: the layout keeps room before a
		// head at both ends.
		lk := &mr.Link{From: mr.Endpoint{ID: id[r.From]}, To: mr.Endpoint{ID: id[r.To]},
			Label: r.Label, Stroke: stroke, Start: mr.Arrow, End: mr.Arrow, Length: 1, Line: r.Line}
		f.Links = append(f.Links, lk)
		rels[lk] = r
	}
	lay, err := layoutGraph(f, m, &layouter{portGap: erPortGap, rankGap: erRankGap, endRoom: erEndRoom,
		faceSpread: erFaceSpread, labelRoom: erLabelRoom, pushSteps: true, sizes: sizes})
	if err != nil {
		return nil, err
	}
	return &erLayout{Layout: lay, tables: tables, rels: rels, graph: f}, nil
}

// measureTable measures an entity's rows. Keys join with "," as erBox
// joins them.
func measureTable(e *mr.Entity, m measurer) (erTable, error) {
	keys, comments := false, false
	for _, a := range e.Attributes {
		keys = keys || len(a.Keys) > 0
		comments = comments || a.Comment != ""
	}
	var t erTable
	for _, a := range e.Attributes {
		row := []string{a.Type, a.Name}
		if keys {
			k := ""
			for i, s := range a.Keys {
				if i > 0 {
					k += ","
				}
				k += s
			}
			row = append(row, k)
		}
		if comments {
			row = append(row, a.Comment)
		}
		t.cells = append(t.cells, row)
	}
	t.cols = make([]float64, len(t.cells[0]))
	for ri, row := range t.cells {
		line := e.Attributes[ri].Line
		rh := 0.0
		for j, s := range row {
			if utf8.RuneCountInString(s) > MaxLabel {
				return t, &mr.Error{Kind: mr.UnsupportedConstruct, Line: line, Msg: fmt.Sprintf("an attribute longer than %d characters", MaxLabel)}
			}
			w, h, err := m(s, false)
			if err != nil {
				return t, glyphErr(err, line)
			}
			if s == "" {
				_, h, _ = m("x", false)
			}
			t.cols[j] = math.Max(t.cols[j], w+2*cellPadX)
			rh = math.Max(rh, h+2*cellPadY)
		}
		t.rows = append(t.rows, rh)
	}
	return t, nil
}

// fit stretches a table to its placed box: the columns share the extra
// width, the rows the extra height (a node grows to hold its links).
func (t *erTable) fit(box Rect) (cols, rows []float64) {
	w, h := 0.0, t.hdrH
	for _, c := range t.cols {
		w += c
	}
	for _, r := range t.rows {
		h += r
	}
	cols = append([]float64(nil), t.cols...)
	rows = append([]float64(nil), t.rows...)
	for j := range cols {
		cols[j] += (box.W() - w) / float64(len(cols))
	}
	for j := range rows {
		rows[j] += (box.H() - h) / float64(len(rows))
	}
	return cols, rows
}

var colRowAlt = colFrame // every other attribute row

// Marker geometry, in em along the line from the entity (the tip).
const (
	markHalf  = 0.4  // a bar's and a crow's foot's half width
	footLen   = 0.8  // a crow's foot's prongs meet the line here
	markCircR = 0.22 // a zero's circle
)

// marker draws a cardinality in crow's foot notation at tip, the line
// running on towards from. Next to the entity is the maximum (a bar for
// one, a crow's foot for many); further out the minimum (a bar for one, a
// circle for zero).
func (c *canvas) marker(card mr.Cardinality, tip, from Pt) {
	dx, dy := from.X-tip.X, from.Y-tip.Y
	l := math.Hypot(dx, dy)
	if l == 0 {
		return
	}
	ux, uy := dx/l, dy/l // along the line, away from the entity
	nx, ny := -uy, ux    // across it
	at := func(t, s float64) Pt { return Pt{tip.X + ux*t + nx*s, tip.Y + uy*t + ny*s} }
	var parts []string
	defer func() { c.tracef("marker at %.2f,%.2f: %s", tip.X, tip.Y, strings.Join(parts, " ")) }()
	bar := func(t float64) {
		parts = append(parts, fmt.Sprintf("bar@%.2f", t))
		c.segment(at(t, -markHalf), at(t, markHalf), lineW*1.2, colEdge)
	}
	circle := func(t float64) {
		parts = append(parts, fmt.Sprintf("circle@%.2f", t))
		ctr := at(t, 0)
		ring := ellipse(ctr.X, ctr.Y, markCircR, markCircR, 24)
		c.fill(ring, colBG)
		c.polyline(append(ring, ring[0]), lineW, false, colEdge)
	}
	foot := func() {
		parts = append(parts, "foot")
		c.segment(at(footLen, 0), at(0, -markHalf), lineW, colEdge)
		c.segment(at(footLen, 0), at(0, markHalf), lineW, colEdge)
	}
	switch card {
	case mr.ExactlyOne:
		bar(0.45)
		bar(0.75)
	case mr.ZeroOrOne:
		bar(0.45)
		circle(1.0)
	case mr.ZeroOrMore:
		foot()
		circle(footLen + 0.3)
	case mr.OneOrMore:
		foot()
		bar(1.05)
	}
}

// entity draws an entity: a box with its name, or a table (a header with
// the name, then a row per attribute).
func (c *canvas) entity(n NodeBox, t erTable, fn *Font) error {
	b := n.Box
	text := func(x, y float64, s string, bold bool) error {
		return fn.drawText(c.img, (x+c.offX)*c.em, (y+c.offY)*c.em, s, bold, c.em, colText)
	}
	if t.cols == nil {
		c.outlineShape(roundRect(b, 0), colNode, colStroke, lineW, false)
		return text(b.Center().X, b.Center().Y, n.Label, true)
	}
	cols, rows := t.fit(b)
	hdr := Rect{b.X0, b.Y0, b.X1, b.Y0 + t.hdrH}
	c.fill(roundRect(hdr, 0), colNode)
	y := hdr.Y1
	for i, rh := range rows {
		col := colBG
		if i%2 == 1 {
			col = colRowAlt
		}
		c.fill(roundRect(Rect{b.X0, y, b.X1, y + rh}, 0), col)
		x := b.X0
		for j, s := range t.cells[i] {
			if s != "" {
				w, _, err := fn.measureEm(s, false)
				if err != nil {
					return err
				}
				if err := text(x+cellPadX+w/2, y+rh/2, s, false); err != nil {
					return err
				}
			}
			x += cols[j]
		}
		y += rh
	}
	// Grid: the header's rule, the row rules, the column rules below the
	// header.
	thin := lineW * 0.6
	c.segment(Pt{b.X0, hdr.Y1}, Pt{b.X1, hdr.Y1}, lineW, colStroke)
	y = hdr.Y1
	for _, rh := range rows[:len(rows)-1] {
		y += rh
		c.segment(Pt{b.X0, y}, Pt{b.X1, y}, thin, colFrameSt)
	}
	x := b.X0
	for _, cw := range cols[:len(cols)-1] {
		x += cw
		c.segment(Pt{x, hdr.Y1}, Pt{x, b.Y1}, thin, colFrameSt)
	}
	c.outlineShape(roundRect(b, 0), nil, colStroke, lineW, false)
	return text(b.Center().X, hdr.Center().Y, n.Label, true)
}
