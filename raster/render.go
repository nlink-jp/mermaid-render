package raster

import (
	"fmt"
	"image"
	"image/draw"
	"math"

	mr "github.com/nlink-jp/mermaid-render"
)

// Options configure Render.
type Options struct {
	// Font draws the text. Nil loads DefaultFont on every call; load it
	// once and pass it instead.
	Font *Font
	// Scale multiplies the base text size of 14px. 0 means 2, the value the
	// display measurement chose for Retina screens.
	Scale float64
}

const (
	baseEm  = 14.0
	cardPad = 1.2 // em, card edge to content
	// MaxPixels bounds the time and memory one render may take (12 Mpx:
	// 48 MB of RGBA; a 6 Mpx dense diagram encoded in 148 ms). It does not
	// bound the PNG's size: bytes per pixel ran from 0.10 (the real
	// diagrams, mostly white card) to 0.67 (text-heavy), so any pixel cap
	// low enough for termimg's 2 MiB at the worst refused ordinary large
	// diagrams — one real diagram at 3.2 Mpx. The caller checks the
	// encoded size and shows the source when it is over.
	MaxPixels = 12 << 20
	// MaxScale bounds Options.Scale.
	MaxScale = 8
)

// RenderSource parses src and renders it.
func RenderSource(src string, opts Options) (*image.RGBA, error) {
	d, err := mr.Parse(src)
	if err != nil {
		return nil, err
	}
	return Render(d, opts)
}

// Render draws a diagram on a white card. Every error means the caller
// should show the source instead; a *mr.Error tells why.
func Render(d mr.Diagram, opts Options) (*image.RGBA, error) {
	return render(d, opts, probe{})
}

// probe lets tests see into a render.
type probe struct {
	// trace is told what is drawn: markers, heads, lines.
	trace func(string)
	// corrupt changes the layout before it is checked, to prove the check
	// runs: a fault no real layout has cannot be reached otherwise.
	corrupt func(lay *Layout, seq *seqLayout)
}

// render is Render with a probe.
func render(d mr.Diagram, opts Options, pr probe) (*image.RGBA, error) {
	fn := opts.Font
	if fn == nil {
		var err error
		if fn, err = DefaultFont(); err != nil {
			return nil, err
		}
	}
	fn.mu.Lock()
	defer fn.mu.Unlock()
	scale := opts.Scale
	if scale == 0 {
		scale = 2
	}
	if math.IsNaN(scale) || scale <= 0 || scale > MaxScale {
		return nil, fmt.Errorf("raster: Scale %v is outside (0, %d]", opts.Scale, MaxScale)
	}
	var (
		lay       *Layout
		er        *erLayout
		seq       *seqLayout
		nodeLines []int
		title     string
		titleLine int
	)
	switch d := d.(type) {
	case *mr.Flowchart:
		var err error
		if lay, err = layoutFlowchart(d, fn.measureEm); err != nil {
			return nil, err
		}
		for _, n := range d.Nodes {
			nodeLines = append(nodeLines, n.Line)
		}
		title, titleLine = d.Title(), d.TitleLine()
	case *mr.ER:
		var err error
		if er, err = layoutER(d, fn.measureEm); err != nil {
			return nil, err
		}
		lay = er.Layout
		for _, e := range d.Entities {
			nodeLines = append(nodeLines, e.Line)
		}
		title, titleLine = d.Title(), d.TitleLine()
	case *mr.Sequence:
		var err error
		if seq, err = layoutSequence(d, fn.measureEm); err != nil {
			return nil, err
		}
		lay = &Layout{W: seq.W, H: seq.H}
		title, titleLine = d.Title(), d.TitleLine()
	default:
		return nil, &mr.Error{Kind: mr.UnsupportedType, Msg: fmt.Sprintf("%T", d)}
	}
	if pr.corrupt != nil {
		pr.corrupt(lay, seq)
	}
	if fs := verify(d, lay, er, seq, fn.measureEm); len(fs) > 0 {
		return nil, &mr.Error{Kind: mr.LayoutFault, Msg: fs[0]}
	}
	var tw, th float64
	var err error
	if title != "" {
		if tw, th, err = fn.measureEm(title, true); err != nil {
			return nil, glyphErr(err, titleLine)
		}
		th += 0.8
	}
	em := baseEm * scale
	wEm := math.Max(lay.W, tw) + 2*cardPad
	hEm := lay.H + th + 2*cardPad
	wPx, hPx := int(math.Ceil(wEm*em)), int(math.Ceil(hEm*em))
	if wPx*hPx > MaxPixels {
		return nil, &mr.Error{Kind: mr.UnsupportedConstruct, Msg: fmt.Sprintf("image would be %dx%d pixels (limit %d)", wPx, hPx, MaxPixels)}
	}
	img := image.NewRGBA(image.Rect(0, 0, wPx, hPx))
	draw.Draw(img, img.Bounds(), image.NewUniform(colBG), image.Point{}, draw.Src)
	c := &canvas{img: img, em: em, trace: pr.trace}
	c.outlineShape(roundRect(Rect{0.06, 0.06, wEm - 0.06, hEm - 0.06}, 0.5), nil, colCard, 0.06, false)
	if title != "" {
		if err := fn.drawText(img, wEm/2*em, (cardPad+th/2-0.2)*em, title, true, em, colText); err != nil {
			return nil, glyphErr(err, titleLine)
		}
	}
	c.offX, c.offY = cardPad+(wEm-2*cardPad-lay.W)/2, cardPad+th
	if er != nil {
		c.rels = er.rels
	}
	if seq != nil {
		if err := c.drawSequence(seq, fn); err != nil {
			return nil, glyphErr(err, 0)
		}
		return img, nil
	}
	text := func(p Pt, s string, bold bool) error {
		return fn.drawText(img, (p.X+c.offX)*em, (p.Y+c.offY)*em, s, bold, em, colText)
	}
	for _, fr := range lay.Frames {
		c.frame(fr)
	}
	for _, e := range lay.Edges {
		c.edge(e)
	}
	// Titles go over the links, on the frame's own colour, so a link
	// entering the frame never runs through the title's text.
	for _, fr := range lay.Frames {
		if fr.Title != "" {
			tb := fr.TitleBox
			c.fill(roundRect(Rect{tb.X0 - labelPad, tb.Y0 - labelPad/2, tb.X1 + labelPad, tb.Y1 + labelPad/2}, 0.2), colFrame)
			if err := fn.drawText(img, (tb.X0+tb.W()/2+c.offX)*em, (tb.Center().Y+c.offY)*em, fr.Title, true, em, colText); err != nil {
				return nil, glyphErr(err, 0)
			}
		}
	}
	for _, e := range lay.Edges {
		c.heads(e)
	}
	for i, n := range lay.Nodes {
		if er != nil {
			if err := c.entity(n, er.tables[i], fn); err != nil {
				return nil, glyphErr(err, nodeLines[i])
			}
			continue
		}
		c.node(n)
		if err := text(labelCenter(n), n.Label, false); err != nil {
			return nil, glyphErr(err, nodeLines[i])
		}
	}
	for _, e := range lay.Edges {
		if e.Label == "" {
			continue
		}
		c.labelBG(e.LabelBox)
		if err := text(e.LabelBox.Center(), e.Label, false); err != nil {
			return nil, glyphErr(err, e.Link.Line)
		}
	}
	return img, nil
}
