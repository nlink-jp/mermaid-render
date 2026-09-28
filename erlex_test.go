package mermaidrender

import (
	"math/rand"
	"strings"
	"testing"
	"time"
)

// The per-line matchers answer exactly what the rules' regular expressions
// (kept here as the oracle) would, position by position, as the lexer asks.
func TestLineMatchersAgree(t *testing.T) {
	oracle := map[string]func(string) int{}
	re := func(s string) func(string) int {
		r := lexRE(s)
		return func(rest string) int {
			if loc := r.FindStringIndex(rest); loc != nil {
				return loc[1]
			}
			return -1
		}
	}
	oracle["tb"] = re(`(DOT)*direction\s+TB[^\n]*`)
	oracle["~"] = re(`((NOTSPACE)*)[~](DOT)*[~]((NOTSPACE)*)`)
	parts := []string{"a", "~", " ", "\t", "　", " ", "#", "direction", "Direction", " TB", "tb", "x~y", "\n", "LR"}
	rng := rand.New(rand.NewSource(1))
	for n := 0; n < 3000; n++ {
		var b strings.Builder
		for range 1 + rng.Intn(12) {
			b.WriteString(parts[rng.Intn(len(parts))])
		}
		src := b.String()
		for name, fresh := range map[string]func() func(string) int{"tb": directionMatcher("TB"), "~": tildeMatcher} {
			m := fresh()
			for i := range src {
				if got, want := m(src[i:]), oracle[name](src[i:]); got != want {
					t.Fatalf("%s at %d of %q: %d, want %d", name, i, src, got, want)
				}
			}
		}
	}
}

// A long line no longer costs a scan per token (it took 106 s at 40 KB).
func TestLongLinesLexFast(t *testing.T) {
	for _, src := range []string{
		"erDiagram\n" + strings.Repeat("A ", 20000) + "direction TB",
		"erDiagram\nE {\n" + strings.Repeat("#", 20000) + "~a~\n}",
		"sequenceDiagram\n" + strings.Repeat("A-B-", 4000) + "C->>D: x",
	} {
		start := time.Now()
		Parse(src)
		if el := time.Since(start); el > 2*time.Second {
			t.Errorf("%.30q…: %v", src, el)
		}
	}
}
