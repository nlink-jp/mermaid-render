package mermaidrender

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The real blocks are every mermaid block gem-agent sessions produced up to
// 2026-09-28: reply/ holds those in reply text (what the transcript
// renders), file/ those the model wrote into files. All came from requests
// to test the display, so they are syntax samples, not a measure of use.
//
// Each block's .parse file is what Parse gave, read line by line against
// its source before it was frozen (2026-09-28, during development).
// -update rewrites them; a rewritten file must be read against its source
// again before it is committed.
var update = flag.Bool("update", false, "rewrite testdata/real/*/*.parse")

func outcome(src string) string {
	d, err := Parse(src)
	if err != nil {
		return "error: " + err.Error() + "\n"
	}
	switch d := d.(type) {
	case *ER:
		return dumpER(d)
	case *Sequence:
		return dumpSeq(d)
	case *Pie:
		return dumpPie(d)
	case *StateDiagram:
		return dumpState(d)
	default:
		return dump(d.(*Flowchart))
	}
}

func TestRealBlocks(t *testing.T) {
	files, err := filepath.Glob("testdata/real/*/*.mmd")
	if err != nil || len(files) != 51 {
		t.Fatalf("real blocks: %d files, %v (want 51)", len(files), err)
	}
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		got := outcome(string(src))
		golden := strings.TrimSuffix(f, ".mmd") + ".parse"
		if *update {
			if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(golden)
		if err != nil {
			t.Fatalf("%s: %v (run with -update, then read the result against the source)", f, err)
		}
		if got != string(want) {
			t.Errorf("%s changed:\n got\n%s\n want\n%s", f, got, want)
		}
	}
}
