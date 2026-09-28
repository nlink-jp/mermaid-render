package raster

import (
	"errors"
	"flag"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	mr "github.com/nlink-jp/mermaid-render"
)

// fakeMeasure sizes text without a font: 0.6 em per ASCII character, 1 em
// per other character, 1.2 em per line.
func fakeMeasure(s string, bold bool) (float64, float64, error) {
	w, lines := 0.0, strings.Split(s, "\n")
	for _, l := range lines {
		lw := 0.0
		for _, r := range l {
			if r == '☃' {
				return 0, 0, &MissingGlyphError{Rune: r}
			}
			if r < utf8.RuneSelf {
				lw += 0.6
			} else {
				lw += 1
			}
		}
		w = math.Max(w, lw)
	}
	return w, 1.2 * float64(len(lines)), nil
}

func layoutOf(t *testing.T, src string, m measurer) (*mr.Flowchart, *Layout) {
	t.Helper()
	d, err := mr.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v\n%s", err, src)
	}
	f := d.(*mr.Flowchart)
	lay, err := layoutFlowchart(f, m)
	if err != nil {
		t.Fatalf("layout: %v\n%s", err, src)
	}
	return f, lay
}

// checkLayout asserts the layout properties (flowFaults), strictly: the
// tests hold what a render refuses and what it only draws less well.
// frameCrossings non-nil counts links through frames not theirs instead
// of failing on them.
func checkLayout(t *testing.T, name string, f *mr.Flowchart, lay *Layout, m measurer) {
	checkLayoutCounting(t, name, f, lay, m, nil)
}

func checkLayoutCounting(t *testing.T, name string, f *mr.Flowchart, lay *Layout, m measurer, frameCrossings *int) {
	t.Helper()
	for _, msg := range flowFaults(f, lay, m, true, frameCrossings) {
		t.Errorf("%s: %s", name, msg)
	}
}

var synthetic = map[string]string{
	"shapes": `flowchart TD
    a[rect] --> b(round) --> c([stadium]) --> d[[subroutine]] --> e[(cylinder)]
    e --> f((circle)) --> g>asymmetric] --> h{rhombus} --> i{{hexagon}}
    i --> j[/para/] --> k[\para alt\] --> l[/trap\] --> m[\trap alt/] --> n(((double)))`,
	"branches and labels": `flowchart TD
    A[Start] --> B{Is it?}
    B -->|Yes| C[OK]
    C --> D[Rethink]
    D --> B
    B ---->|No| E[End]`,
	"fan in and out": `flowchart TD
    A & B & C --> D & E
    D --> F
    E --> F
    A --> F`,
	"multi links": `flowchart LR
    A -->|one| B
    A -->|two| B
    A --> B
    B --> A`,
	"self links": `flowchart TD
    A --> A
    A -->|again| A
    A --> B
    B -.->|retry| B`,
	"cycle": `flowchart TD
    A --> B --> C --> D --> A
    C --> A`,
	"heads and strokes": `flowchart TD
    A <--> B
    B o--o C
    C x--x D
    D ==> E
    E -.- F
    F ~~~ G`,
	"subgraphs with endpoints": `flowchart TB
    c1-->a2
    subgraph one
    a1-->a2
    end
    subgraph two
    b1-->b2
    end
    subgraph three
    c1-->c2
    end
    one --> two
    three --> two
    two --> c2`,
	"link past a frame": `flowchart TD
    subgraph Client [Client Side]
        A[User Request] --> B[Browser Cache]
    end
    subgraph Server [Backend System]
        B -->|Cache Miss| C[API Gateway]
        C --> D[Auth Service]
        D -->|Authorized| E[App Service]
        E --> F[Database]
    end
    B -->|Cache Hit| G[Render View]
    E -->|Response Data| G`,
	"empty subgraph": `flowchart TD
    subgraph empty [Nothing inside]
    end
    A --> empty
    empty --> B`,
	"long titles": `flowchart LR
    subgraph s1 [A very long subgraph title that is wider than its content]
        x
    end
    subgraph s2 [Another long title]
        y
    end
    x --> y`,
}

func TestLayoutProperties(t *testing.T) {
	for name, src := range synthetic {
		for _, dir := range []string{"TD", "BT", "LR", "RL"} {
			src := strings.Replace(src, strings.Fields(src)[1], dir, 1)
			f, lay := layoutOf(t, src, fakeMeasure)
			checkLayout(t, name+" "+dir, f, lay, fakeMeasure)
		}
	}
}

// The real session flowcharts, with the real font.
func TestLayoutRealBlocks(t *testing.T) {
	fn, err := DefaultFont()
	if err != nil {
		t.Skipf("no system font: %v", err)
	}
	files, _ := filepath.Glob("../testdata/real/*/*.mmd")
	n := 0
	for _, file := range files {
		b, _ := os.ReadFile(file)
		d, err := mr.Parse(string(b))
		if err != nil {
			continue
		}
		f, ok := d.(*mr.Flowchart)
		if !ok {
			continue // ER blocks: er_test.go
		}
		lay, err := layoutFlowchart(f, fn.measureEm)
		if err != nil {
			t.Errorf("%s: %v", file, err)
			continue
		}
		checkLayout(t, filepath.Base(file), f, lay, fn.measureEm)
		n++
	}
	if n != 22 {
		t.Errorf("laid out %d real flowcharts, want 22", n)
	}
}

func TestLayoutIsDeterministic(t *testing.T) {
	for name, src := range synthetic {
		_, a := layoutOf(t, src, fakeMeasure)
		_, b := layoutOf(t, src, fakeMeasure)
		if !reflect.DeepEqual(a, b) {
			t.Errorf("%s: two layouts differ", name)
		}
	}
}

func TestLayoutRefusals(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("flowchart TD\n")
	for i := 0; i <= MaxNodes; i++ {
		fmt.Fprintf(&sb, " n%d\n", i)
	}
	for src, line := range map[string]int{
		sb.String():                                     0,
		"flowchart TD\n A[ok] --> B[☃]":                 2,
		"flowchart TD\n A -->|☃| B":                     2,
		"flowchart TD\n subgraph s [☃]\n A\n end":       2,
		"flowchart TD\n subgraph s\n A\n end\n s --> s": 5,
		"flowchart TD\n subgraph s\n A\n end\n s --> A": 5,
	} {
		d, err := mr.Parse(src)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		_, err = layoutFlowchart(d.(*mr.Flowchart), fakeMeasure)
		var e *mr.Error
		if !errors.As(err, &e) || e.Kind != mr.UnsupportedConstruct || (line > 0 && e.Line != line) {
			t.Errorf("%.40q: err %v, want unsupported construct at line %d", src, err, line)
		}
	}
}

// randomFlowchart builds a flowchart with subgraphs and links drawn from a
// seeded generator: cycles, self-links, multi-links, labels, lengths and
// subgraph endpoints included.
func randomFlowchart(seed int64, dir string) string {
	rng := rand.New(rand.NewSource(seed))
	var b strings.Builder
	fmt.Fprintf(&b, "flowchart %s\n", dir)
	nNodes := 3 + rng.Intn(10)
	nSub := rng.Intn(4)
	shapes := []string{"[%s]", "(%s)", "([%s])", "{%s}", "((%s))", "[/%s/]", "{{%s}}"}
	label := func() string {
		words := []string{"alpha", "調査", "b", "リスク判定", "long label here", "x"}
		return words[rng.Intn(len(words))]
	}
	node := func(i int) string { return fmt.Sprintf("n%d", i) }
	sub := make([]int, nNodes)
	for i := range sub {
		sub[i] = rng.Intn(nSub + 1) // 0: top level
	}
	for i := range nNodes {
		if sub[i] == 0 {
			fmt.Fprintf(&b, "  %s"+shapes[rng.Intn(len(shapes))]+"\n", node(i), label())
		}
	}
	for s := 1; s <= nSub; s++ {
		fmt.Fprintf(&b, "  subgraph s%d [%s]\n", s, label())
		for i := range nNodes {
			if sub[i] == s {
				fmt.Fprintf(&b, "    %s"+shapes[rng.Intn(len(shapes))]+"\n", node(i), label())
			}
		}
		b.WriteString("  end\n")
	}
	arrows := []string{"-->", "---", "-.->", "==>", "<-->", "--o", "--->"}
	// An end is a node (subgraph 0 = top level) or a whole subgraph. Links
	// between a subgraph and itself or its own member are refused by
	// design (TestLayoutRefusals); they are not generated here.
	type endp struct {
		name string
		sg   int // the subgraph it is, or 0
		in   int // the subgraph a node is in
	}
	end := func() endp {
		if nSub > 0 && rng.Intn(6) == 0 {
			k := 1 + rng.Intn(nSub)
			return endp{fmt.Sprintf("s%d", k), k, 0}
		}
		i := rng.Intn(nNodes)
		return endp{node(i), 0, sub[i]}
	}
	for range rng.Intn(nNodes * 2) {
		ea, ec := end(), end()
		if ea.sg > 0 && (ea.sg == ec.sg || ea.sg == ec.in) || ec.sg > 0 && ec.sg == ea.in {
			continue
		}
		a, c := ea.name, ec.name
		arrow := arrows[rng.Intn(len(arrows))]
		if rng.Intn(3) == 0 {
			fmt.Fprintf(&b, "  %s %s|%s| %s\n", a, arrow, label(), c)
		} else {
			fmt.Fprintf(&b, "  %s %s %s\n", a, arrow, c)
		}
	}
	return b.String()
}

// -random widens the sweep for a one-off search (go test -random 20000).
var randomN = flag.Int("random", 400, "number of random flowcharts TestLayoutRandom lays out")

func TestLayoutRandom(t *testing.T) {
	dirs := []string{"TD", "BT", "LR", "RL"}
	laid, refused, crossed := 0, 0, 0
	reasons := map[string]int{}
	for seed := int64(1); seed <= int64(*randomN); seed++ {
		src := randomFlowchart(seed, dirs[seed%4])
		d, err := mr.Parse(src)
		if err != nil {
			t.Fatalf("seed %d: parse: %v\n%s", seed, err, src)
		}
		f := d.(*mr.Flowchart)
		lay, err := layoutFlowchart(f, fakeMeasure)
		if err != nil {
			var e *mr.Error
			if errors.As(err, &e) {
				refused++
				reasons[e.Msg]++
				continue
			}
			t.Fatalf("seed %d: %v\n%s", seed, err, src)
		}
		before := t.Failed()
		checkLayoutCounting(t, fmt.Sprintf("seed %d", seed), f, lay, fakeMeasure, &crossed)
		// What a render checks is a subset of what the tests hold: a frame
		// crossing is counted here, never refused there.
		for _, msg := range flowFaults(f, lay, fakeMeasure, false, nil) {
			t.Errorf("seed %d: a render would refuse: %s", seed, msg)
		}
		if t.Failed() && !before {
			t.Logf("source of seed %d:\n%s", seed, src)
		}
		laid++
	}
	t.Logf("laid out %d random flowcharts, refused %d: %v; link segments crossing a foreign frame: %d", laid, refused, reasons, crossed)
	if laid != *randomN {
		t.Errorf("only %d of %d random flowcharts were laid out", laid, *randomN)
	}
}

// The random flowcharts that exposed each class of defect, kept as fixed
// cases and checked strictly (frame crossings included): 10454 subgraph
// order never set when no sweep ran; 11589 self-link ends capped by the face;
// 10330 and 11191 a self-link label outside its layer; 1226 self-link ends
// on a rhombus's shared slope; 14882 self-link ends past a parallelogram's
// flat face; 12 an item beside a frame on the wrong side; 1138 an order
// whose link had to cross a frame.
func TestLayoutRegressions(t *testing.T) {
	dirs := []string{"TD", "BT", "LR", "RL"}
	// Step-2 review round: 342 a frame port beside a member's column; 11,
	// 195, 299 two links swapping near-equal columns (the detour); 10439 a
	// track inside a frame stretched for its title; 404, 456, 686 a frame
	// face too narrow for its ports and through-columns; 691 loop ends on a
	// stadium's rounded end; 785 a port range end missed by the sampling.
	// Review round 2: 33636 a frame moved by centring with no node moved,
	// its ports left behind.
	for _, seed := range []int64{10454, 11589, 10330, 11191, 1226, 14882, 12, 1138,
		342, 11, 195, 299, 10439, 404, 456, 686, 691, 785, 33636} {
		src := randomFlowchart(seed, dirs[seed%4])
		f, lay := layoutOf(t, src, fakeMeasure)
		checkLayout(t, fmt.Sprintf("seed %d", seed), f, lay, fakeMeasure)
	}
}

// clipAt lands on the outline even when the inside point it aims at lies
// outside the shape: here beyond a parallelogram's slant.
func TestClipAtFallsBackToTheCentre(t *testing.T) {
	r := Rect{0, 0, 6, 2}
	outside, inside := Pt{5.9, 5}, Pt{5.9, 1} // below the slanted right end
	p := clipAt(mr.Parallelogram, r, 1, outside, inside)
	if !onOutline(outline(mr.Parallelogram, r, 1), p) {
		t.Errorf("clipAt = %v, not on the outline", p)
	}
	if _, ok := firstHit(outline(mr.Parallelogram, r, 1), outside, inside); ok {
		t.Fatal("the case no longer needs the fallback; pick another")
	}
}

// A chain of single links through subgraphs of different widths lies on one
// column (the operator's second check: boxes off their line). Before, the
// frame edge hugging the widest member pinned it, and ties between two
// neighbours settled into a staircase.
func TestChainThroughFramesIsStraight(t *testing.T) {
	src := `flowchart TD
    subgraph Ingestion
        A[Input Query]
    end
    subgraph Processing
        B[Passive Lookup] --> C[Threat Correlation Engine Step]
    end
    subgraph Result
        D[Generate Report]
    end
    A --> B
    C --> D`
	for _, dir := range []string{"TD", "LR"} {
		f, lay := layoutOf(t, strings.Replace(src, "TD", dir, 1), fakeMeasure)
		checkLayout(t, dir, f, lay, fakeMeasure)
		horiz := dir == "LR"
		col := func(r Rect) float64 {
			if horiz {
				return r.Center().Y
			}
			return r.Center().X
		}
		x0 := col(lay.Nodes[0].Box)
		for _, n := range lay.Nodes[1:] {
			if math.Abs(col(n.Box)-x0) > 1e-6 {
				t.Errorf("%s: %s at %.3f, not on the chain's column %.3f", dir, n.ID, col(n.Box), x0)
			}
		}
	}
}

// Subgraphs linked frame to frame line up on their middles, and the links
// run through them (the operator's second check: centre the groups).
func TestFrameToFrameLinksAreCentred(t *testing.T) {
	src := `flowchart TD
    subgraph Input
        IN[Target Artifact]
    end
    subgraph Recon
        DNS[DoH and rDNS]
        WHOIS[RDAP and Registry]
    end
    subgraph Output
        REPORT[Attribution Report]
    end
    IN --> Recon
    Recon --> Output`
	f, lay := layoutOf(t, src, fakeMeasure)
	checkLayout(t, "frames", f, lay, fakeMeasure)
	mid := map[string]float64{}
	for _, fr := range lay.Frames {
		mid[fr.ID] = fr.Box.Center().X
	}
	if d := math.Abs(mid["Recon"] - mid["Output"]); d > 0.5 {
		t.Errorf("Recon's middle %.3f and Output's %.3f are %.3f apart", mid["Recon"], mid["Output"], d)
	}
	for _, e := range lay.Edges {
		if e.Link.To.ID != "Recon" && e.Link.From.ID != "Recon" {
			continue
		}
		end := e.Points[len(e.Points)-1]
		if e.Link.From.ID == "Recon" {
			end = e.Points[0]
		}
		if d := math.Abs(end.X - mid["Recon"]); d > 0.5 {
			t.Errorf("%s->%s meets Recon %.3f from its middle", e.Link.From.ID, e.Link.To.ID, d)
		}
	}
}

// A link from a fan into a box that takes it alone meets the box's middle
// when the box cannot move under the link (the operator's third check:
// 2d6ce5dc96); its step goes beside the fan.
func TestLoneEndMeetsTheMiddle(t *testing.T) {
	src := `flowchart TD
    T[Target Artifact] --> A[Passive Recon: DNS and WHOIS]
    T --> B[Passive Recon: ASN and BGP]
    A --> C[Correlation and Threat Intel]
    B --> C
    C --> R[Attribution Report]`
	f, lay := layoutOf(t, src, fakeMeasure)
	checkLayout(t, "lone end", f, lay, fakeMeasure)
	var b NodeBox
	for _, n := range lay.Nodes {
		if n.ID == "B" {
			b = n
		}
	}
	for _, e := range lay.Edges {
		if e.Link.From.ID == "T" && e.Link.To.ID == "B" {
			end := e.Points[len(e.Points)-1]
			if d := math.Abs(end.X - b.Box.Center().X); d > 1e-6 {
				t.Errorf("T->B meets B %.3f em from its middle", d)
			}
		}
	}
}

// A link that must step aside steps beside its node, not halfway down: the
// port's column is carried into the link only when that saves a step (the
// operator's third check: 3bd61ab0d7's Cache Hit bent at its label).
func TestStepBesideTheNode(t *testing.T) {
	src := `flowchart TD
    subgraph Client [Client Side]
        A[User Request] --> B[Browser Cache]
    end
    subgraph Server [Backend System]
        B -->|Cache Miss| C[API Gateway]
        C --> D[Auth Service]
        D -->|Authorized| E[App Service]
        E --> F[Database]
    end
    B -->|Cache Hit| G[Render View]
    E -->|Response Data| G`
	f, lay := layoutOf(t, src, fakeMeasure)
	checkLayout(t, "step", f, lay, fakeMeasure)
	var b NodeBox
	for _, n := range lay.Nodes {
		if n.ID == "B" {
			b = n
		}
	}
	for _, e := range lay.Edges {
		if e.Link.From.ID != "B" || e.Link.To.ID != "G" {
			continue
		}
		for i := 0; i+1 < len(e.Points); i++ {
			p, q := e.Points[i], e.Points[i+1]
			if math.Abs(p.Y-q.Y) < 1e-9 && math.Abs(p.X-q.X) > 1e-9 {
				if d := p.Y - b.Box.Y1; d > 3 {
					t.Errorf("B->G first steps aside %.3f em below B", d)
				}
				break
			}
		}
	}
}
