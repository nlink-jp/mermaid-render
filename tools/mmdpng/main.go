// mmdpng converts a mermaid file to PNG with mermaid-render. It is a
// development tool for hands-on checks and E2E runs, built by `make build`
// into dist/, and never released.
package main

import (
	"flag"
	"fmt"
	"os"
)

var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println("mmdpng", version)
		return
	}
	fmt.Fprintln(os.Stderr, "mmdpng: rendering is not implemented yet (Phase 1)")
	os.Exit(1)
}
