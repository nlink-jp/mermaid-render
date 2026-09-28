// mmdpng converts a mermaid file to PNG with mermaid-render. It is a
// development tool for hands-on checks and E2E runs, built by `make build`
// into dist/, and never released.
//
//	mmdpng [-scale 2] [-o out.png] diagram.mmd
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
	flag.Parse()
	if *showVersion {
		fmt.Println("mmdpng", version)
		return
	}
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: mmdpng [-scale 2] [-o out.png] diagram.mmd")
		os.Exit(2)
	}
	in := flag.Arg(0)
	if err := run(in, *out, *scale); err != nil {
		fmt.Fprintf(os.Stderr, "mmdpng: %s: %v\n", in, err)
		os.Exit(1)
	}
}

func run(in, out string, scale float64) error {
	src, err := os.ReadFile(in)
	if err != nil {
		return err
	}
	font, err := raster.DefaultFont()
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
