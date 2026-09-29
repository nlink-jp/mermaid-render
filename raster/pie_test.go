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
	// A matter of looks, so the tests' alone: the labels outside the
	// circle keep their slices' order down each side, so leaders do not
	// cross — clockwise on the right, counter-clockwise on the left.
	var prevR, prevL *pieWedge
	for i := range pl.wedges {
		w := &pl.wedges[i]
		if w.inside {
			continue
		}
		if math.Sin((w.a0+w.a1)/2) >= 0 {
			if prevR != nil && w.box.Y0 < prevR.box.Y0 {
				t.Errorf("%s: outside labels on the right are out of order", name)
			}
			prevR = w
		} else {
			if prevL != nil && w.box.Y0 > prevL.box.Y0 {
				t.Errorf("%s: outside labels on the left are out of order", name)
			}
			prevL = w
		}
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
// order, and one legend row per item, with showData's values.
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
	if want := "[犬 [386] 猫 [85] 鼠 [0.5] 兎 [15]]"; fmt.Sprint(legend) != want {
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
		"a label off its slice": func(pl *pieLayout) {
			pl.wedges[0].box = pl.wedges[1].box
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
