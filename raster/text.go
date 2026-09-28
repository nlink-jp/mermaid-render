package raster

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"os"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

// Font is the set of faces text is drawn with: the chosen faces first, then
// Hiragino as the fallback, character by character. Load it once and share
// it; it is safe for one goroutine at a time.
type Font struct {
	body, bold []*face
}

type face struct {
	sf    *sfnt.Font
	name  string
	buf   sfnt.Buffer
	sized map[float64]font.Face
	// ok caches whether a rune can be drawn with this face.
	ok map[rune]bool
}

const (
	hiraginoW3 = "/System/Library/Fonts/ヒラギノ角ゴシック W3.ttc"
	hiraginoW6 = "/System/Library/Fonts/ヒラギノ角ゴシック W6.ttc"
)

// DefaultFont loads Hiragino Sans W3 (body) and W6 (bold) from the macOS
// system fonts.
func DefaultFont() (*Font, error) {
	w3, err := loadFace(hiraginoW3, 0)
	if err != nil {
		return nil, err
	}
	w6, err := loadFace(hiraginoW6, 0)
	if err != nil {
		return nil, err
	}
	return &Font{body: []*face{w3}, bold: []*face{w6}}, nil
}

func loadFace(path string, index int) (*face, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("font %s: %w", path, err)
	}
	c, err := opentype.ParseCollection(b)
	if err != nil {
		return nil, fmt.Errorf("font %s: %w", path, err)
	}
	sf, err := c.Font(index)
	if err != nil {
		return nil, fmt.Errorf("font %s, face %d: %w", path, index, err)
	}
	return &face{sf: sf, name: path, sized: map[float64]font.Face{}, ok: map[rune]bool{}}, nil
}

func (f *face) at(size float64) font.Face {
	if fc := f.sized[size]; fc != nil {
		return fc
	}
	fc, err := opentype.NewFace(f.sf, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingNone})
	if err != nil {
		// NewFace fails only for an invalid size.
		panic(err)
	}
	f.sized[size] = fc
	return fc
}

// probeSize is the size runes are tried at; drawability does not depend on
// size, and hinting is off.
const probeSize = 32

// can reports whether r can be drawn with this face: its glyph index is not
// 0 (Face.Glyph would draw .notdef and say ok) and its glyph loads.
func (f *face) can(r rune) bool {
	if v, seen := f.ok[r]; seen {
		return v
	}
	v := false
	if gi, err := f.sf.GlyphIndex(&f.buf, r); err == nil && gi != 0 {
		_, _, _, _, v = f.at(probeSize).Glyph(fixed.Point26_6{}, r)
	}
	f.ok[r] = v
	return v
}

// ignorable are characters that carry no glyph: variation selectors, ZWJ,
// ZWNJ, ZWSP. They are skipped, never reported as missing.
func ignorable(r rune) bool {
	return r >= 0xFE00 && r <= 0xFE0F || r >= 0xE0100 && r <= 0xE01EF ||
		r == 0x200B || r == 0x200C || r == 0x200D
}

// MissingGlyphError says no face can draw a character of a label.
type MissingGlyphError struct{ Rune rune }

func (e *MissingGlyphError) Error() string {
	return fmt.Sprintf("no font can draw %q (U+%04X)", e.Rune, e.Rune)
}

type run struct {
	f *face
	s string
}

// runs splits one line into runs of a single face, skipping ignorable
// characters.
func (fn *Font) runs(line string, bold bool) ([]run, error) {
	faces := fn.body
	if bold {
		faces = fn.bold
	}
	var out []run
	for _, r := range line {
		if ignorable(r) {
			continue
		}
		var pick *face
		for _, f := range faces {
			if f.can(r) {
				pick = f
				break
			}
		}
		if pick == nil && bold {
			for _, f := range fn.body {
				if f.can(r) {
					pick = f
					break
				}
			}
		}
		if pick == nil {
			return nil, &MissingGlyphError{Rune: r}
		}
		if n := len(out); n > 0 && out[n-1].f == pick {
			out[n-1].s += string(r)
		} else {
			out = append(out, run{pick, string(r)})
		}
	}
	return out, nil
}

// lineMetrics is one line's width and extent at size px.
type lineMetrics struct {
	runs        []run
	width       float64
	ascent, dsc float64
}

func (fn *Font) line(line string, bold bool, size float64) (lineMetrics, error) {
	rs, err := fn.runs(line, bold)
	if err != nil {
		return lineMetrics{}, err
	}
	lm := lineMetrics{runs: rs}
	used := rs
	if len(used) == 0 {
		// An empty line still has the height of the first face.
		f := fn.body[0]
		if bold {
			f = fn.bold[0]
		}
		used = []run{{f: f}}
	}
	for _, r := range used {
		fc := r.f.at(size)
		m := fc.Metrics()
		lm.ascent = math.Max(lm.ascent, fix(m.Ascent))
		lm.dsc = math.Max(lm.dsc, fix(m.Descent))
		prev := rune(-1)
		for _, c := range r.s {
			if prev >= 0 {
				lm.width += fix(fc.Kern(prev, c))
			}
			adv, _ := fc.GlyphAdvance(c)
			lm.width += fix(adv)
			prev = c
		}
	}
	return lm, nil
}

func fix(v fixed.Int26_6) float64 { return float64(v) / 64 }

// measureEm is the width and height of a (multi-line) text in em.
func (fn *Font) measureEm(text string, bold bool) (w, h float64, err error) {
	const size = 100
	for _, l := range strings.Split(text, "\n") {
		lm, err := fn.line(l, bold, size)
		if err != nil {
			return 0, 0, err
		}
		w = math.Max(w, lm.width/size)
		h += (lm.ascent + lm.dsc) / size
	}
	return w, h, nil
}

// drawText draws text centred on (cx, cy) in pixels; each line is centred.
func (fn *Font) drawText(dst draw.Image, cx, cy float64, text string, bold bool, size float64, col color.Color) error {
	lines := strings.Split(text, "\n")
	var ms []lineMetrics
	total := 0.0
	for _, l := range lines {
		lm, err := fn.line(l, bold, size)
		if err != nil {
			return err
		}
		ms = append(ms, lm)
		total += lm.ascent + lm.dsc
	}
	y := cy - total/2
	src := image.NewUniform(col)
	for _, lm := range ms {
		x := cx - lm.width/2
		base := y + lm.ascent
		for _, r := range lm.runs {
			fc := r.f.at(size)
			prev := rune(-1)
			for _, c := range r.s {
				if prev >= 0 {
					x += fix(fc.Kern(prev, c))
				}
				dot := fixed.Point26_6{X: fixed.Int26_6(math.Round(x * 64)), Y: fixed.Int26_6(math.Round(base * 64))}
				dr, mask, mp, adv, ok := fc.Glyph(dot, c)
				if !ok {
					// can() said this face draws c; a failure here is a bug.
					return &MissingGlyphError{Rune: c}
				}
				draw.DrawMask(dst, dr, src, image.Point{}, mask, mp, draw.Over)
				x += fix(adv)
				prev = c
			}
		}
		y += lm.ascent + lm.dsc
	}
	return nil
}
