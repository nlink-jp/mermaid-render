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
	// MaxPixels keeps the encoded PNG under termimg's 2 MiB: a dense
	// diagram measured 0.30 bytes per pixel (150 nodes, 300 links, six
	// subgraphs: 6.1 Mpx, 1856 KB), the real session diagrams 0.10-0.12.
	MaxPixels = 6 << 20
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
	fn := opts.Font
	if fn == nil {
		var err error
		if fn, err = DefaultFont(); err != nil {
			return nil, err
		}
	}
	scale := opts.Scale
	if scale == 0 {
		scale = 2
	}
	f, ok := d.(*mr.Flowchart)
	if !ok {
		return nil, &mr.Error{Kind: mr.UnsupportedType, Msg: fmt.Sprintf("%T", d)}
	}
	lay, err := layoutFlowchart(f, fn.measureEm)
	if err != nil {
		return nil, err
	}
	title := f.Title()
	var tw, th float64
	if title != "" {
		if tw, th, err = fn.measureEm(title, true); err != nil {
			return nil, glyphErr(err, f.TitleLine())
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
	c := &canvas{img: img, em: em}
	c.outlineShape(roundRect(Rect{0.06, 0.06, wEm - 0.06, hEm - 0.06}, 0.5), nil, colCard, 0.06, false)
	if title != "" {
		if err := fn.drawText(img, wEm/2*em, (cardPad+th/2-0.2)*em, title, true, em, colText); err != nil {
			return nil, glyphErr(err, f.TitleLine())
		}
	}
	c.offX, c.offY = cardPad+(wEm-2*cardPad-lay.W)/2, cardPad+th
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
	for i, n := range lay.Nodes {
		c.node(n)
		if err := text(labelCenter(n), n.Label, false); err != nil {
			return nil, glyphErr(err, f.Nodes[i].Line)
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
