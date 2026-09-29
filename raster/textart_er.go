package raster

import (
	"fmt"
	"strconv"
	"strings"

	mr "github.com/nlink-jp/mermaid-render"
)

// ER text art: the entities are tables, the relationships links between
// their left and right faces with the cardinality in mermaid's notation in
// the two cells next to each table. The diagram is always laid out left to
// right (right to left when written RL): cardinality reads along a line.

// textTable is an entity's table on the grid.
type textTable struct {
	name  string
	cols  []int      // column widths in cells, text only
	cells [][]string // a row per attribute
	w, h  int        // the box's size in cells, borders included
}

func measureTextTable(e *mr.Entity, tm *textMeasure) (*textTable, error) {
	if err := tm.check(e.Label, e.Line); err != nil {
		return nil, err
	}
	t := &textTable{name: e.Label}
	nw, nh := tm.size(e.Label)
	if len(e.Attributes) == 0 {
		t.w, t.h = nw+4, nh+2
		t.w += t.w % 2
		return t, nil
	}
	keys, comments := false, false
	for _, a := range e.Attributes {
		keys = keys || len(a.Keys) > 0
		comments = comments || a.Comment != ""
	}
	for _, a := range e.Attributes {
		row := []string{a.Type, a.Name}
		if keys {
			row = append(row, strings.Join(a.Keys, ","))
		}
		if comments {
			row = append(row, a.Comment)
		}
		for _, c := range row {
			if strings.Contains(c, "\n") {
				return nil, &mr.Error{Kind: mr.UnsupportedConstruct, Line: a.Line, Msg: "text art: a line break in an attribute"}
			}
			if err := tm.check(c, a.Line); err != nil {
				return nil, err
			}
		}
		t.cells = append(t.cells, row)
	}
	t.cols = make([]int, len(t.cells[0]))
	for _, row := range t.cells {
		for k, c := range row {
			t.cols[k] = max(t.cols[k], tm.cells(c))
		}
	}
	// │ col │ col │: each column padded a cell each side, a bar between.
	inner := len(t.cols) - 1
	for _, c := range t.cols {
		inner += c + 2
	}
	if nw+2 > inner {
		t.cols[len(t.cols)-1] += nw + 2 - inner
		inner = nw + 2
	}
	t.w = inner + 2
	if t.w%2 == 1 {
		t.cols[len(t.cols)-1]++
		t.w++
	}
	t.h = nh + 3 + len(t.cells) // border, name, rule, rows, border
	return t, nil
}

// erText lays out and draws an ER diagram as text art.
func erText(d *mr.ER, tm *textMeasure) (*tgrid, error) {
	if len(d.Entities) > MaxNodes || len(d.Relationships) > MaxLinks {
		return nil, &mr.Error{Kind: mr.UnsupportedConstruct, Msg: fmt.Sprintf(
			"too large to lay out: %d entities, %d relationships (limits %d, %d)",
			len(d.Entities), len(d.Relationships), MaxNodes, MaxLinks)}
	}
	dir := mr.LR
	if d.Direction == mr.RL {
		dir = mr.RL
	}
	f := &mr.Flowchart{Direction: dir}
	sizes := make([][2]float64, len(d.Entities))
	tables := make([]*textTable, len(d.Entities))
	id := map[*mr.Entity]string{}
	for i, e := range d.Entities {
		id[e] = "e" + strconv.Itoa(i)
		f.Nodes = append(f.Nodes, &mr.Node{ID: id[e], Label: e.Label, Shape: mr.Rect, Line: e.Line})
		t, err := measureTextTable(e, tm)
		if err != nil {
			return nil, err
		}
		for _, r := range d.Relationships {
			if r.From == e && r.To == e {
				t.h = max(t.h, 4)
			}
		}
		tables[i] = t
		sizes[i] = [2]float64{float64(t.w) / 2, float64(t.h)}
	}
	var marks [][2][2]rune
	for _, r := range d.Relationships {
		if err := tm.check(r.Label, r.Line); err != nil {
			return nil, err
		}
		stroke := mr.Solid
		if !r.Identifying {
			stroke = mr.Dotted
		}
		// Heads keep the room the markers take at both ends.
		f.Links = append(f.Links, &mr.Link{From: mr.Endpoint{ID: id[r.From]}, To: mr.Endpoint{ID: id[r.To]},
			Label: r.Label, Stroke: stroke, Start: mr.Arrow, End: mr.Arrow, Length: 1, Line: r.Line})
		marks = append(marks, [2][2]rune{cardRunes(r.FromCard), cardRunes(r.ToCard)})
	}
	measure := func(text string, bold bool) (float64, float64, error) {
		if text == "" {
			return 0, 0, nil
		}
		w, h := tm.size(text)
		return float64(w+w%2) / 2, float64(h), nil
	}
	// A mark takes two cells (one unit across) at each end; the room before
	// it keeps a label a cell of line away.
	lay, err := layoutGraph(f, measure, &layouter{sp: &gridSpacing, boxes: true, sizes: sizes,
		portGap: gridPortGap, rankGap: gridRankGap + 1, endRoom: gridEndRoom + 1, labelRoom: gridSpacing.trackIn + 1})
	if err != nil {
		return nil, err
	}
	tf := snapFlow(f, lay)
	tf.tables, tf.marks = tables, marks
	// A relationship of an entity to itself loops on the table's right
	// face (the layout loops it below, where marks cannot read across):
	// out and back over its two lowest interior rows.
	for i, lk := range f.Links {
		if lk.From != lk.To {
			continue
		}
		b := endBox(f, tf, lk.From)
		x := b.x1 + 1
		used := map[int]bool{}
		for j, p := range tf.paths {
			if j != i && f.Links[j].From != f.Links[j].To && len(p) > 0 {
				for _, q := range [][2]int{p[0], p[len(p)-1]} {
					if q[0] == x && q[1] > b.y0 && q[1] < b.y1 {
						used[q[1]] = true
					}
				}
			}
		}
		var free []int
		for y := b.y1 - 1; y > b.y0 && len(free) < 2; y-- {
			if !used[y] {
				free = append(free, y)
			}
		}
		if len(free) < 2 {
			return nil, &mr.Error{Kind: mr.LayoutFault, Msg: "text art: no room for a relationship to itself"}
		}
		ya, yb := free[1], free[0]
		tf.paths[i] = [][2]int{{x, ya}, {x + 4, ya}, {x + 4, yb}, {x, yb}}
		if lk.Label != "" {
			w, h := tm.size(lk.Label)
			tf.labels[i] = iRect{x + 6, yb - h + 1, x + 6 + w - 1, yb}
		}
		tf.w, tf.h = max(tf.w, x+8+tf.labels[i].x1-tf.labels[i].x0+1), max(tf.h, yb+1)
	}
	labelsOnLines(f, tf, 3)
	return drawFlowText(f, tf, tm)
}

// cardRunes is a cardinality in mermaid's notation as written on the
// right of the dashes (touching the entity to its right): ||, o|, o{, |{.
// Mirrored for an entity on the left: ||, |o, }o, }|.
func cardRunes(c mr.Cardinality) [2]rune {
	switch c {
	case mr.ZeroOrOne:
		return [2]rune{'o', '|'}
	case mr.ZeroOrMore:
		return [2]rune{'o', '{'}
	case mr.OneOrMore:
		return [2]rune{'|', '{'}
	}
	return [2]rune{'|', '|'}
}

// mirrored is a marker for an entity on its left.
func mirrored(m [2]rune) [2]rune {
	flip := func(r rune) rune {
		if r == '{' {
			return '}'
		}
		return r
	}
	return [2]rune{flip(m[1]), flip(m[0])}
}

// drawTable draws an entity's table in its box: the name, a rule, a row
// per attribute in columns.
func drawTable(g *tgrid, b iRect, t *textTable, own int, tm *textMeasure) {
	if len(t.cells) == 0 {
		drawBorder(g, b, [6]rune{'┌', '─', '┐', '│', '└', '┘'}, own)
		fillOwn(g, b, own)
		centreText(g, iRect{b.x0 + 1, b.y0 + 1, b.x1 - 1, b.y1 - 1}, t.name, own, tm)
		return
	}
	_, nh := tm.size(t.name)
	rule := b.y0 + nh + 1
	drawBorder(g, b, [6]rune{'┌', '─', '┐', '│', '└', '┘'}, own)
	fillOwn(g, b, own)
	centreText(g, iRect{b.x0 + 1, b.y0 + 1, b.x1 - 1, rule - 1}, t.name, own, tm)
	for x := b.x0 + 1; x < b.x1; x++ {
		g.cells[rule][x] = tcell{r: '─', own: own}
	}
	g.cells[rule][b.x0] = tcell{r: '├', own: own}
	g.cells[rule][b.x1] = tcell{r: '┤', own: own}
	// Column bars, and the joints on the rule and the bottom border.
	x := b.x0
	for k, c := range t.cols {
		for r, row := range t.cells {
			g.text(x+2, rule+1+r, row[k], own, tm)
		}
		x += c + 3
		if k < len(t.cols)-1 {
			for y := rule + 1; y < b.y1; y++ {
				g.cells[y][x] = tcell{r: '│', own: own}
			}
			g.cells[rule][x] = tcell{r: '┬', own: own}
			g.cells[b.y1][x] = tcell{r: '┴', own: own}
		}
	}
}

func fillOwn(g *tgrid, b iRect, own int) {
	for y := b.y0 + 1; y < b.y1; y++ {
		for x := b.x0 + 1; x < b.x1; x++ {
			g.cells[y][x] = tcell{own: own}
		}
	}
}
