package raster

import (
	"bytes"
	"errors"
	"fmt"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	mr "github.com/nlink-jp/mermaid-render"
)

func systemFont(t *testing.T) *Font {
	t.Helper()
	fn, err := DefaultFont()
	if err != nil {
		t.Skipf("no system font: %v", err)
	}
	return fn
}

func TestRenderSource(t *testing.T) {
	fn := systemFont(t)
	src := "---\ntitle: 調査の流れ\n---\nflowchart TD\n A([開始]) --> B{判定}\n B -->|はい| C[(保存)]\n B -- いいえ --> D[再入力]\n D --> B"
	img, err := RenderSource(src, Options{Font: fn})
	if err != nil {
		t.Fatal(err)
	}
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	half, err := RenderSource(src, Options{Font: fn, Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	// Scale 0 means 2: twice the Scale-1 size, give or take rounding.
	if hw := half.Bounds().Dx(); w < 2*hw-2 || w > 2*hw+2 {
		t.Errorf("default scale: %dx%d vs Scale 1 %dx%d", w, h, hw, half.Bounds().Dy())
	}
	// The same input gives the same image.
	again, _ := RenderSource(src, Options{Font: fn})
	var a, b bytes.Buffer
	_ = png.Encode(&a, img)
	_ = png.Encode(&b, again)
	if !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Error("two renders of the same source differ")
	}
	// A white card: the corners are background.
	if c := img.RGBAAt(0, 0); c != colBG {
		t.Errorf("corner colour %v", c)
	}
}

func TestRenderErrors(t *testing.T) {
	fn := systemFont(t)
	for src, want := range map[string]struct {
		kind mr.ErrorKind
		line int
	}{
		"stateDiagram-v2\n [*] --> A":             {mr.UnsupportedType, 1},
		"flowchart TD\n A --> B C":                {mr.SyntaxError, 2},
		"flowchart TD\n A --> B[完了 ✅]":            {mr.UnsupportedConstruct, 2}, // no face has U+2705
		"flowchart TD\n A -->|✅| B":               {mr.UnsupportedConstruct, 2},
		"flowchart TD\n subgraph s [✅]\n A\n end": {mr.UnsupportedConstruct, 2},
		"---\ntitle: ✅\n---\nflowchart TD\n A":    {mr.UnsupportedConstruct, 2}, // the title's line
	} {
		_, err := RenderSource(src, Options{Font: fn})
		var e *mr.Error
		if !errors.As(err, &e) || e.Kind != want.kind || e.Line != want.line {
			t.Errorf("%.40q: %v, want %s at line %d", src, err, want.kind, want.line)
		}
	}
	// Characters without glyphs are skipped, not refused: ⚠️ is U+26A0 U+FE0F.
	if _, err := RenderSource("flowchart TD\n A[注意⚠️] --> B", Options{Font: fn}); err != nil {
		var me *mr.Error
		if errors.As(err, &me) && strings.Contains(me.Msg, "FE0F") {
			t.Errorf("a variation selector was refused: %v", err)
		}
	}
}

func TestRenderPixelLimit(t *testing.T) {
	fn := systemFont(t)
	var sb strings.Builder
	sb.WriteString("flowchart LR\n")
	for i := range 120 {
		fmt.Fprintf(&sb, " n%d[%s] --> n%d\n", i, strings.Repeat("長いラベル", 6), i+1)
	}
	_, err := RenderSource(sb.String(), Options{Font: fn})
	var e *mr.Error
	if !errors.As(err, &e) || e.Kind != mr.UnsupportedConstruct || !strings.Contains(e.Msg, "pixels") {
		t.Errorf("a %d-node chain: %v, want the pixel limit", 121, err)
	}
}

// The step-2 review's adversarial inputs are refused, and quickly: before
// the fixes they took seconds to a minute and gigabytes first.
func TestResourceLimits(t *testing.T) {
	fn := systemFont(t)
	var empties, amp strings.Builder
	empties.WriteString("flowchart TD\n")
	for i := range 2000 {
		fmt.Fprintf(&empties, " subgraph e%d\n end\n", i)
	}
	amp.WriteString("flowchart LR\n ")
	for i := range 2000 {
		if i > 0 {
			amp.WriteString(" & ")
		}
		fmt.Fprintf(&amp, "a%d", i)
	}
	amp.WriteString(" --> b")
	var chain strings.Builder
	chain.WriteString("flowchart TD\n")
	for i := range 499 {
		fmt.Fprintf(&chain, " n%d ----------> n%d\n", i%250, (i+1)%250)
	}
	for name, src := range map[string]string{
		"2000 empty subgraphs":             empties.String(),
		"a 350k-character label":           "flowchart TD\n A[" + strings.Repeat("長", 350000) + "]",
		"2000 & 1 links (mermaid's limit)": amp.String(),
		"a million-dash link":              "flowchart TD\n A " + strings.Repeat("-", 1000000) + "> B",
		"long links in a big graph":        chain.String(),
	} {
		start := time.Now()
		_, err := RenderSource(src, Options{Font: fn})
		var e *mr.Error
		if !errors.As(err, &e) || e.Kind != mr.UnsupportedConstruct {
			// A million-dash link is length 10 (mermaid's cap) and renders.
			if name != "a million-dash link" || err != nil {
				t.Errorf("%s: %v, want an unsupported-construct refusal", name, err)
			}
		}
		if d := time.Since(start); d > 3*time.Second {
			t.Errorf("%s: took %v", name, d)
		}
	}
	for _, sc := range []float64{math.Inf(1), -1, math.NaN(), MaxScale + 1} {
		if _, err := RenderSource("flowchart TD\n A", Options{Font: fn, Scale: sc}); err == nil {
			t.Errorf("Scale %v accepted", sc)
		}
	}
}

// Every real diagram that parses (22 flowcharts, 11 ER, 10 sequence) renders at the default scale: a limit that refuses
// one of them has lost what the engine is for (a pixel cap once did).
func TestRealBlocksRender(t *testing.T) {
	fn := systemFont(t)
	files, _ := filepath.Glob("../testdata/real/*/*.mmd")
	n := 0
	for _, f := range files {
		b, _ := os.ReadFile(f)
		d, err := mr.Parse(string(b))
		if err != nil {
			continue
		}
		if _, err := Render(d, Options{Font: fn}); err != nil {
			t.Errorf("%s: %v", filepath.Base(f), err)
		}
		n++
	}
	if n != 44 {
		t.Errorf("rendered %d real diagrams, want 44", n)
	}
}
