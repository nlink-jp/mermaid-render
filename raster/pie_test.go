package raster

import (
	"errors"
	"flag"
	"fmt"
	"math"
	"math/rand"
	"strings"
	"testing"

	mr "github.com/nlink-jp/mermaid-render"
)

func pieOf(t *testing.T, src string, m measurer) (*mr.Pie, *pieLayout) {
	t.Helper()
	d, err := mr.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v\n%s", err, src)
	}
	p := d.(*mr.Pie)
	pl, err := layoutPie(p, m)
	if err != nil {
		t.Fatalf("layout: %v\n%s", err, src)
	}
	return p, pl
}

func checkPie(t *testing.T, name string, p *mr.Pie, pl *pieLayout) {
	t.Helper()
	for _, f := range pieFaults(p, pl) {
		t.Errorf("%s: %s", name, f)
	}
}

// pieRenderer's arithmetic: items under 1% get no slice, the drawn slices
// share the circle, the percentage is of the whole, toFixed(0) rounding.
func TestPieShares(t *testing.T) {
	// Of the whole 95.9: 52% and 47%; of the drawn 95 it would be 53% and 47%.
	p, _ := pieOf(t, "pie\n\"a\" : 50\n\"b\" : 45\n\"c\" : 0.9", fakeMeasure)
	kept, angle, pct := pieShares(p)
	if fmt.Sprint(kept) != "[0 1]" || fmt.Sprint(pct) != "[52% 47%]" {
		t.Errorf("kept %v, pct %v", kept, pct)
	}
	// The two drawn items fill the circle between them: 50 / 95 of it.
	if math.Abs(angle[1]-50.0/95*2*math.Pi) > 1e-12 || math.Abs(angle[2]-2*math.Pi) > 1e-12 {
		t.Errorf("angles %v", angle)
	}
	for x, want := range map[float64]string{0.5: "1", 1.5: "2", 2.5: "3", 2.4999999: "2", 99.5: "100", 0: "0"} {
		if got := toFixed0(x); got != want {
			t.Errorf("toFixed0(%v) = %s, want %s", x, got, want)
		}
	}
}

func TestPieLayoutCases(t *testing.T) {
	for name, src := range map[string]string{
		"one item":          "pie\n\"all\" : 1",
		"two halves":        "pie\n\"a\" : 1\n\"b\" : 1",
		"thin slices":       "pie showData\n\"a\" : 412\n\"b\" : 233\n\"c\" : 118\n\"d\" : 41\n\"e\" : 22\n\"f\" : 12\n\"g\" : 9\n\"h\" : 5\n\"i\" : 3",
		"all zero":          "pie\n\"a\" : 0\n\"b\" : 0",
		"no items":          "pie title nothing",
		"a thin slice last": "pie\n\"big\" : 97\n\"x\" : 1\n\"y\" : 1\n\"z\" : 1",
		"a long legend":     "pie showData\n\"a label that goes on for quite a while\" : 3\n\"b\" : 1",
	} {
		p, pl := pieOf(t, src, fakeMeasure)
		checkPie(t, name, p, pl)
	}
}

// randomPie is a pie of 1–30 items with values across several scales, so
// thin slices crowd both sides of the circle.
func randomPie(seed int64) string {
	r := rand.New(rand.NewSource(seed))
	var b strings.Builder
	b.WriteString("pie")
	if r.Intn(2) == 0 {
		b.WriteString(" showData")
	}
	b.WriteString("\n")
	for i := range 1 + r.Intn(30) {
		v := math.Pow(10, r.Float64()*4) * float64(r.Intn(3))
		fmt.Fprintf(&b, "\"item %d\" : %.2f\n", i, v)
	}
	return b.String()
}

func TestPieLayoutRandom(t *testing.T) {
	flag.Parse()
	for seed := int64(1); seed <= int64(*randomN); seed++ {
		src := randomPie(seed)
		p, pl := pieOf(t, src, fakeMeasure)
		checkPie(t, fmt.Sprintf("seed %d", seed), p, pl)
		if t.Failed() {
			t.Logf("source:\n%s", src)
			return
		}
	}
}

// What is drawn is what the source says: one slice per item over 1%, in
// order, and one legend row per item, with showData's values and its
// percentage of the whole — "<1%" for the item with no slice.
func TestPieDrawn(t *testing.T) {
	fn := systemFont(t)
	d, err := mr.Parse("pie showData\n\"犬\" : 386\n\"猫\" : 85\n\"鼠\" : 0.5\n\"兎\" : 15")
	if err != nil {
		t.Fatal(err)
	}
	var trace []string
	if _, err := render(d, Options{Font: fn}, probe{trace: func(s string) { trace = append(trace, s) }}); err != nil {
		t.Fatal(err)
	}
	var slices, legend []string
	for _, s := range trace {
		switch {
		case strings.HasPrefix(s, "slice "):
			slices = append(slices, strings.Fields(s)[1])
		case strings.HasPrefix(s, "legend "):
			legend = append(legend, strings.SplitN(s, " ", 3)[2])
		}
	}
	if fmt.Sprint(slices) != "[0 1 3]" {
		t.Errorf("slices drawn for items %v, want [0 1 3] (item 2 is under 1%%)", slices)
	}
	if want := "[犬 [386] | 79% 猫 [85] | 17% 鼠 [0.5] | <1% 兎 [15] | 3%]"; fmt.Sprint(legend) != want {
		t.Errorf("legend %v, want %s", legend, want)
	}
}

// Every render checks a pie's layout: a corrupted one is a layout fault.
func TestRenderRefusesPieFaults(t *testing.T) {
	fn := systemFont(t)
	for name, corrupt := range map[string]func(*pieLayout){
		"a slice missing":      func(pl *pieLayout) { pl.wedges = pl.wedges[1:] },
		"a legend row missing": func(pl *pieLayout) { pl.legend = pl.legend[1:] },
		"a label on another":   func(pl *pieLayout) { pl.legend[1].box = pl.legend[0].box },
		"a wrong percentage":   func(pl *pieLayout) { pl.legend[2].pct = "9%" },
		"slice percentages swapped": func(pl *pieLayout) {
			pl.wedges[0].pct, pl.wedges[1].pct = pl.wedges[1].pct, pl.wedges[0].pct
		},
		"legend percentages swapped": func(pl *pieLayout) {
			pl.legend[0].pctBox, pl.legend[2].pctBox = pl.legend[2].pctBox, pl.legend[0].pctBox
		},
		"legend rows swapped": func(pl *pieLayout) {
			a, b := &pl.legend[0], &pl.legend[1]
			a.swatch, b.swatch, a.box, b.box, a.pctBox, b.pctBox = b.swatch, a.swatch, b.box, a.box, b.pctBox, a.pctBox
		},
		"a percentage on its label": func(pl *pieLayout) {
			// On its own line, slid left over the label.
			r := &pl.legend[0]
			w := r.pctBox.X1 - r.pctBox.X0
			r.pctBox.X0, r.pctBox.X1 = r.box.X0, r.box.X0+w
		},
		"a legend over the circle": func(pl *pieLayout) {
			// The middle row, on its own line, slid left onto the circle.
			r := &pl.legend[1].pctBox
			w := r.X1 - r.X0
			r.X0, r.X1 = pl.c.X, pl.c.X+w
		},
		"a label off its slice": func(pl *pieLayout) {
			// At the centre, on every slice's edge and over no other text.
			b := pl.wedges[0].box
			w, h := (b.X1-b.X0)/2, (b.Y1-b.Y0)/2
			pl.wedges[0].box = rect{pl.c.X - w, pl.c.Y - h, pl.c.X + w, pl.c.Y + h}
		},
	} {
		d, _ := mr.Parse("pie\n\"a\" : 60\n\"b\" : 30\n\"c\" : 10")
		_, err := render(d, Options{Font: fn}, probe{corruptPie: corrupt})
		var e *mr.Error
		if !errors.As(err, &e) || e.Kind != mr.LayoutFault {
			t.Errorf("%s: %v, want a layout fault", name, err)
		}
	}
}

// A single slice's percentage sits at the centre; moved off the circle,
// clear of every other text, it is still a fault.
func TestRenderRefusesSinglePieLabelOffTheCircle(t *testing.T) {
	fn := systemFont(t)
	d, _ := mr.Parse("pie\n\"all\" : 1")
	_, err := render(d, Options{Font: fn}, probe{corruptPie: func(pl *pieLayout) {
		b := &pl.wedges[0].box
		dy := pl.c.Y + pieR - b.Y0 // just below the circle
		b.Y0, b.Y1 = b.Y0+dy, b.Y1+dy
		pl.H += b.Y1 - b.Y0 // and still inside the picture
	}})
	var e *mr.Error
	if !errors.As(err, &e) || e.Kind != mr.LayoutFault {
		t.Errorf("%v, want a layout fault", err)
	}
}
