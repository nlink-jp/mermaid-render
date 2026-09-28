// mmdpng converts a mermaid file to PNG with mermaid-render. It is a
// development tool for hands-on checks and E2E runs, built by `make build`
// into dist/, and never released.
//
//	mmdpng [-scale 2] [-o out.png] [-font path [-font-name name]] [-bold path [-bold-name name]] diagram.mmd
package main

import (
	"bytes"
	"flag"
	"fmt"
	"image/png"
	"os"
	"strings"
	"time"

	"github.com/nlink-jp/mermaid-render/raster"
)

var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "print the version and exit")
	scale := flag.Float64("scale", 2, "scale factor")
	out := flag.String("o", "", "output PNG (default: the input with .png)")
	var spec raster.FontSpec
	flag.StringVar(&spec.Path, "font", "", "font file for body text (default: Hiragino Sans W3)")
	flag.StringVar(&spec.Name, "font-name", "", "face in -font, by full or PostScript name")
	flag.StringVar(&spec.BoldPath, "bold", "", "font file for bold text (default: the body face)")
	flag.StringVar(&spec.BoldName, "bold-name", "", "face in -bold")
	flag.Parse()
	if *showVersion {
		fmt.Println("mmdpng", version)
		return
	}
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: mmdpng [-scale 2] [-o out.png] [-font path [-font-name name]] [-bold path [-bold-name name]] diagram.mmd")
		os.Exit(2)
	}
	in := flag.Arg(0)
	if err := run(in, *out, *scale, spec); err != nil {
		fmt.Fprintf(os.Stderr, "mmdpng: %s: %v\n", in, err)
		os.Exit(1)
	}
}

func run(in, out string, scale float64, spec raster.FontSpec) error {
	src, err := os.ReadFile(in)
	if err != nil {
		return err
	}
	var font *raster.Font
	if spec.Path != "" {
		font, err = raster.LoadFont(spec)
	} else {
		font, err = raster.DefaultFont()
	}
	if err != nil {
		return err
	}
	t0 := time.Now()
	img, err := raster.RenderSource(string(src), raster.Options{Font: font, Scale: scale})
	if err != nil {
		return err
	}
	rendered := time.Since(t0)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return err
	}
	if out == "" {
		out = strings.TrimSuffix(in, ".mmd") + ".png"
	}
	if err := os.WriteFile(out, buf.Bytes(), 0o644); err != nil {
		return err
	}
	fmt.Printf("%s: %dx%d px, %d KB, rendered in %v, PNG in %v\n", out, img.Bounds().Dx(), img.Bounds().Dy(),
		buf.Len()/1024, rendered.Round(time.Millisecond), (time.Since(t0) - rendered).Round(time.Millisecond))
	return nil
}
