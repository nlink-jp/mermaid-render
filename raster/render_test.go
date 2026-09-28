package raster

import (
	"bytes"
	"errors"
	"fmt"
	"image/png"
	"strings"
	"testing"

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
		"sequenceDiagram\n A->>B: hi":             {mr.UnsupportedType, 1},
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
