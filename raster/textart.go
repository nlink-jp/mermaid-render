package raster

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode"

	"golang.org/x/text/width"

	mr "github.com/nlink-jp/mermaid-render"
)

// Text art: a diagram drawn in box-drawing characters on a tgrid of
// terminal cells, for terminals that cannot draw images (RFP, "Text art
// (phase 2e)"). Flowcharts and ER diagrams run the layered layout the
// picture uses, in grid units — one unit is one row down and two columns
// across — and are snapped to the grid; sequence diagrams have a column
// layout of their own. Every render is checked on the grid.

// TextOptions configure RenderText.
type TextOptions struct {
	// Width is the columns a character takes on the caller's terminal. Nil
	// uses Unicode East Asian Width: wide and fullwidth 2, others 1.
	Width func(r rune) int
}

// MaxTextCells bounds text art: time and memory, not looks.
const MaxTextCells = 2_000_000

// RenderTextSource parses src and renders it as text art.
func RenderTextSource(src string, opts TextOptions) (string, error) {
	d, err := mr.Parse(src)
	if err != nil {
		return "", err
	}
	return RenderText(d, opts)
}

// RenderText draws a flowchart, sequence or ER diagram as text art. Every
// error means the caller should show the source instead; a *mr.Error
// tells why.
func RenderText(d mr.Diagram, opts TextOptions) (string, error) {
	wf := opts.Width
	if wf == nil {
		wf = eastAsianWidth
	}
	tm := &textMeasure{width: wf}
	var g *tgrid
	var err error
	switch d := d.(type) {
	case *mr.Flowchart:
		g, err = flowText(d, tm)
	case *mr.ER:
		g, err = erText(d, tm)
	case *mr.Sequence:
		g, err = seqText(d, tm)
	default:
		return "", &mr.Error{Kind: mr.UnsupportedType, Msg: fmt.Sprintf("text art: %T", d)}
	}
	if err != nil {
		if debugArt && g != nil {
			return g.String(), err
		}
		return "", err
	}
	return g.String(), nil
}

func eastAsianWidth(r rune) int {
	switch width.LookupRune(r).Kind() {
	case width.EastAsianWide, width.EastAsianFullwidth:
		return 2
	}
	return 1
}

// textMeasure measures labels in cells, refusing what the art cannot
// place cell by cell.
type textMeasure struct {
	width func(rune) int
}

// check refuses a label the grid cannot hold faithfully: a control
// character (a decoded #27; is a real ESC), and a character that joins
// the one before it into one grapheme cluster, so that a rune is always a
// whole cluster.
func (tm *textMeasure) check(s string, line int) error {
	for _, r := range s {
		switch {
		case r == '\n':
		case r < 0x20, r == 0x7f, r >= 0x80 && r < 0xa0, r == '\t', r == 0x2028, r == 0x2029:
			return &mr.Error{Kind: mr.UnsupportedConstruct, Line: line, Msg: fmt.Sprintf("text art: a control character %U in a label", r)}
		case unicode.Is(unicode.Bidi_Control, r): // before joins: these are Cf too
			return &mr.Error{Kind: mr.UnsupportedConstruct, Line: line, Msg: fmt.Sprintf("text art: a bidi control %U in a label", r)}
		case joins(r):
			return &mr.Error{Kind: mr.UnsupportedConstruct, Line: line, Msg: fmt.Sprintf("text art: %U joins a grapheme cluster", r)}
		}
		if tm.width(r) < 1 {
			return &mr.Error{Kind: mr.UnsupportedConstruct, Line: line, Msg: fmt.Sprintf("text art: %U takes no cell", r)}
		}
	}
	return nil
}

// joins reports a rune that joins a grapheme cluster with its neighbour:
// combining and spacing marks, ZWJ, variation selectors, emoji modifiers,
// regional indicators, Hangul jamo (leading ones join what follows), the
// Thai and Lao SARA AM, Prepend characters, and other zero-width format
// characters.
func joins(r rune) bool {
	return unicode.In(r, unicode.Mn, unicode.Me, unicode.Mc, unicode.Cf) ||
		r >= 0x1100 && r <= 0x11ff || r >= 0xa960 && r <= 0xa97f || r >= 0xd7b0 && r <= 0xd7ff ||
		r == 0x0d4e || r == 0x111c2 || r == 0x111c3 || r == 0x1193f || r == 0x11941 || r == 0x11a3a ||
		r >= 0x11a84 && r <= 0x11a89 || r == 0x11d46 || r == 0x11f02 ||
		r == 0x0e33 || r == 0x0eb3 ||
		r >= 0xfe00 && r <= 0xfe0f || r >= 0xe0100 && r <= 0xe01ef ||
		r >= 0x1f3fb && r <= 0x1f3ff || r >= 0x1f1e6 && r <= 0x1f1ff
}

// cells is a line's width in cells.
func (tm *textMeasure) cells(s string) int {
	n := 0
	for _, r := range s {
		n += tm.width(r)
	}
	return n
}

// size is a (multi-line) text's width in cells and its lines.
func (tm *textMeasure) size(s string) (w, h int) {
	for _, l := range strings.Split(s, "\n") {
		w = max(w, tm.cells(l))
		h++
	}
	return w, h
}

// tcell is one tgrid cell: a rune, or a wide rune's right half (cont).
type tcell struct {
	r    rune
	cont bool
	own  int // what drew it: textFree, a box, a line, a label (below)
	dirs uint8
}

// Directions a line leaves a cell in.
const (
	dUp uint8 = 1 << iota
	dDown
	dLeft
	dRight
)

const textFree = 0

type tgrid struct {
	w, h  int
	cells [][]tcell
}

func newGrid(w, h int) (*tgrid, error) {
	if w*h > MaxTextCells || w < 0 || h < 0 {
		return nil, &mr.Error{Kind: mr.UnsupportedConstruct, Msg: fmt.Sprintf("text art would be %d x %d cells (limit %d)", w, h, MaxTextCells)}
	}
	g := &tgrid{w: w, h: h, cells: make([][]tcell, h)}
	for y := range g.cells {
		g.cells[y] = make([]tcell, w)
	}
	return g, nil
}

func (g *tgrid) in(x, y int) bool { return x >= 0 && y >= 0 && x < g.w && y < g.h }

// text writes s from (x, y), a wide rune taking two cells.
func (g *tgrid) text(x, y int, s string, own int, tm *textMeasure) {
	for _, r := range s {
		w := tm.width(r)
		if g.in(x, y) {
			g.cells[y][x] = tcell{r: r, own: own}
		}
		for k := 1; k < w; k++ {
			if g.in(x+k, y) {
				g.cells[y][x+k] = tcell{cont: true, own: own}
			}
		}
		x += w
	}
}

func (g *tgrid) String() string {
	var lines []string
	for _, row := range g.cells {
		var line strings.Builder
		for _, c := range row {
			switch {
			case c.cont:
			case c.r == 0:
				line.WriteByte(' ')
			default:
				line.WriteRune(c.r)
			}
		}
		lines = append(lines, strings.TrimRight(line.String(), " "))
	}
	// Room the layout kept that nothing uses: no blank rows at either end.
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	for len(lines) > 0 && lines[0] == "" {
		lines = lines[1:]
	}
	return strings.Join(lines, "\n")
}

// gridSpacing is the layered layout's spacing in grid units (a unit is a
// row down, two columns across): every spacing whole, measured on the real
// and random flowcharts by the specification's verification.
var gridSpacing = spacing{sepItem: 3, sepDummy: 2, framePad: 2, frameSep: 2, titleGap: 1, labelPad: 0,
	loopReach: 3, portSpread: portSpread, trackSep: 2, trackIn: 1}

// The rest of the grid's spacing, in units: link ends two apart on a face
// (one apart collided in 60% of random flowcharts), a row between layers,
// and two before a head, so that a head ends a straight run.
var gridPortGap, gridRankGap, gridEndRoom = 2.0, 1.0, 2.0

// snapper maps layout coordinates to grid lines once per value: values
// equal up to rounding error are one coordinate and land on one line, so a
// right angle never breaks at a half-cell tie.
type snapper struct {
	scale float64
	vals  []float64
}

func (s *snapper) add(v float64) { s.vals = append(s.vals, v) }

func (s *snapper) done() {
	sort.Float64s(s.vals)
	out := s.vals[:0]
	for _, v := range s.vals {
		if len(out) == 0 || v-out[len(out)-1] > 1e-6 {
			out = append(out, v)
		}
	}
	s.vals = out
}

// at is the grid line of v: the canonical value within 1e-6, scaled and
// rounded half up.
func (s *snapper) at(v float64) int {
	i := sort.SearchFloat64s(s.vals, v-1e-6)
	c := v
	if i < len(s.vals) && math.Abs(s.vals[i]-v) <= 1e-6 {
		c = s.vals[i]
	}
	return int(math.Floor(c*s.scale + 0.5))
}

// iRect is a rectangle of cells, both ends included.
type iRect struct{ x0, y0, x1, y1 int }

func (r iRect) has(x, y int) bool { return x >= r.x0 && x <= r.x1 && y >= r.y0 && y <= r.y1 }
func (r iRect) overlaps(o iRect) bool {
	return r.x0 <= o.x1 && o.x0 <= r.x1 && r.y0 <= o.y1 && o.y0 <= r.y1
}
