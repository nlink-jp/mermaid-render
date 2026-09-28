# AGENTS.md — mermaid-render

## Summary

Go library that turns mermaid source into an image (a white card, `*image.RGBA`),
for gem-agent and lagent to show diagrams in the terminal through their
`internal/termimg`. Module `github.com/nlink-jp/mermaid-render`. Two packages:
`mermaidrender` (parse into drawing-independent diagram data) and `raster`
(layout and drawing). Phase 1 covers flowchart / graph, sequenceDiagram and
erDiagram. Dependency: `golang.org/x/image` only (declared exception to the
lib-series standard-library rule). The specification is
`docs/en/mermaid-render-rfp.md` (ja: `docs/ja/mermaid-render-rfp.ja.md`).

## Build & Test

```bash
make test     # go test ./...
make vet      # go vet ./...
make build    # dist/mmdpng, the development CLI; never released
```

## Structure

```
mermaid-render/
├── doc.go            # package mermaidrender
├── diagram.go        # Diagram, Error, ErrorKind
├── parse.go          # Parse: front matter, comments, type dispatch
├── flowchart.go      # Flowchart, Node, Link, Subgraph, Shape, Stroke, Head
├── flowparse.go      # the flowchart parser (flow.jison / flowDb.ts rules noted inline)
├── label.go          # label text: quotes, entity codes, <br>, HTML refused
├── er.go             # ER, Entity, Attribute, Relationship, Cardinality
├── erlex.go          # the erDiagram.jison lexer, ported rule by rule
├── erparse.go        # the erDiagram grammar; generics, markdown refusal
├── sequence.go       # Sequence, Participant, Box, Event
├── seqlex.go         # the sequenceDiagram.jison lexer (on erlex.go's runLexer)
├── seqparse.go       # the sequenceDiagram grammar and sequenceDb's rules
├── testdata/real/    # real session blocks: reply/, file/; *.parse = reviewed goldens
├── raster/
│   ├── er.go         # ER: table sizes, layout with marker spacing, markers, tables
│   ├── seq.go        # sequence: columns from pairwise needs, events stacked, drawing
│   ├── text.go       # Font, DefaultFont, glyph-by-glyph fallback, MissingGlyphError
│   ├── fontload.go   # FontSpec, LoadFont, name-table reader (every language)
│   ├── layout.go     # layered layout: chains, two-level ranks, items, ordering
│   ├── coords.go     # cross-axis coordinates: difference constraints + descent
│   ├── place.go      # rank positions, frames, ports, routing, direction transform
│   ├── geom.go       # shape outlines, clipping, segment tests
│   ├── draw.go       # rasterizing shapes, links, heads, frames
│   └── render.go     # Options, Render, RenderSource, MaxPixels
├── tools/mmdpng/     # development CLI: mermaid file -> PNG
├── Makefile
└── docs/{en,ja}/     # RFP
```

## Gotchas

- **`*.parse` goldens were read against their sources** before they were
  frozen. `go test -run TestRealBlocks -update` rewrites them; read every
  changed file against its `.mmd` before committing, or the golden only
  proves the code agrees with itself.
- **Layout properties are the correctness gate** (they replace the text-art
  faithfulness checks of gem-agent ADR-0042). `checkLayout` in
  `raster/layout_test.go` is the list; run `go test ./raster/ -random 20000`
  after any layout change — 20000 seeds found defects 400 did not. Pin a seed
  that exposed a defect in `TestLayoutRegressions`.
- **A link crossing a foreign subgraph frame is counted, not failed**, in the
  random sweep (see the RFP's Discussion Log); fixed cases still assert none.
- **Links are orthogonal between layers** (`assignTracks` in place.go):
  every across run has its own track; the rule "a link leaving near another's
  arrival column turns above it" orders tracks, and a cycle of that rule (two
  links swapping near-equal columns) is broken by a detour through a free
  column. A detour's two halves are ordered directly — the column rule
  assumes vertical runs reaching the gap's ends, which a detour's middle
  column does not. Below those hard rules, a soft preference orders runs so
  none passes another's vertical (a run over another's drop column turns
  below it, over its rise column above it); a preference that would close a
  cycle is dropped, and gaps over 60 parts skip it (quadratic).
- **Staircases come from local moves** (coords.go): single-variable descent
  cannot move a member past the frame edge hugging the widest member
  (`intervalWith` lets members carry the edges), and a two-neighbour tie
  broken by "nearer the current position" is stable as a staircase (the tie
  takes the neighbour above). `alignRuns` then moves whole runs of one-to-one
  links; per-item "reachable" rules were tried and made more staircases.
- **`pushRuns` moves runs whole and keeps only improvements**: a single
  item moved aside would step its own run; the try is scored by `steps`
  (links between neighbours that change column) and kept only when lower.
  It is ER only; turning it on for flowcharts changes reviewed images.
- **A link ending on a subgraph is tied to every member of the end layer**
  (`endItems`), so its neighbours are not a column to aim at: `upFrame` /
  `dnFrame` mark the dummy next to such an end, and targets, block shifts and
  `centerNodes` use the frame's middle instead.
- **A gap has two arrowhead sides.** Heads at a gap's end got `trackOut`;
  a link drawn against the layer order has its head at the gap's start,
  which got only `trackIn` (0.45 em) and put the head on the bend. `headTop`
  gives such a gap `trackOut` there too.
- **Late fixes beat global rules for alignment** (`centerLoneEnds`): moving
  a fan's lone end onto its box's middle by changing the descent's tie rule
  raised bends 106 -> 134; a final pass over already-stepped links does it
  without touching straight ones.
- **Attachment spacing is measured, not ruled by shape** (`attachmentsClear`):
  shape-by-shape rules kept missing cases (a rhombus's shared slopes, a
  stadium's rounded ends). Measure after every other growth, and sample the
  port range's ends exactly.
- **Straightness comes from four steps** (coords.go, place.go): median
  targets, block shifts for subgraphs (single-variable descent cannot move a
  frame and its members together), straightening dummies onto port columns
  (repeat until stable: a packed fan frees from one end), and snapping steps
  under 0.2 em. `TestRealBends` holds the baseline.
- **Centring a node must not move its frame by itself** (`centerNodes`): the
  frame is loosened for the move and tightened after, and tightening can land
  it elsewhere when it was not tight. If the node did not move, restore the
  frame: ports are only recomputed when something moved, and a frame that
  moved alone leaves its ports outside it (seed 33636).
- **Ranks are two-level**: a subgraph is ranked inside, then placed as one
  block. Ranking members directly let a cycle through subgraphs make one
  frame straddle another (the mermaid docs' own example did).
- **A regex tried at every token must not scan to the line's end.** The
  ER `direction` and `~` rules did (106 s on a 40 KB line); they are now
  per-line matchers (`fresh` factories, made anew per run) and a
  differential test compares them with the original regexes. Lowering the
  rest of the source per token made the sequence lexer quadratic too.
  `MaxTextSize` (mermaid's 50,000) is the backstop, not the fix.
- **Both jison lexers share `runLexer`** (erlex.go): rules may pop several
  states, match with a hand-written function (Go's regexp has no lookahead)
  and trim their text. The sequence lexer ends with a NEWLINE as jison's
  `<<EOF>>` rule does, and its source gets a final newline (NUM needs one).
- **The ER lexer is a port, not a reading of the docs** (`erlex.go`): rules
  in `erDiagram.jison` order, first match wins, case-insensitive, `\b`
  appended to a rule ending in a word character (jison-lex does that), and
  Go-side lookaheads for the four `(?=...)` rules. JavaScript's `\s` and `.`
  are spelled out (`jsSpace`, `jsDot`). Do not "fix" a surprising result
  (`direction TD` as two entities) without checking mermaid does otherwise.
- **A mutant that does not compile proves nothing**: when checking a
  property by breaking the code, make sure the broken copy builds (an
  unused variable once passed for "not caught").
- **Drawing is checked through a trace** (`render(d, opts, trace)`,
  `canvas.tracef`): markers, heads and every polyline's dash flag are
  reported, and tests compare them with the source. Trace inside the
  primitive (polyline), not beside the call: a trace line computed apart
  from the drawn value let a swapped dash flag pass.
- **Sequence columns are a longest path over pairwise needs**: every
  constraint is between a column and one to its left, so one pass from the
  left places them. A frame's width comes from what lies in its rows.
- **Spacing is per layout** (`layouter.portGap`, `rankGap`, `endRoom`,
  `faceSpread`, `labelRoom`, `pushSteps`, and `sizes` for caller-measured
  nodes): flowcharts keep the constants, ER
  widens them for its markers. After touching the layout, render the 22 real
  flowcharts and compare bytes with the last reviewed set.
- **Link tokens follow flowDb.destructEndLink**: a start mark counts only when
  the end mark is the same kind; `A---oB` has length 2.
- **A glyph index is not a glyph**: `face.can` also checks the probe glyph
  leaves ink (spaces aside). Apple Color Emoji maps ✅ to a bitmap glyph
  x/image cannot draw; index and `Glyph`'s ok both said yes. Format and
  default-ignorable characters no face draws are skipped, not missing.
- **Font is shared under a lock** (`Font.mu`, taken for a whole render):
  faces cache sizes and drawability in maps; concurrent renders panicked.
- **Font names**: `sfnt.Name` returns the first name record whatever its
  language. Matching by name means walking the name table for every language's
  full name (ID 4) and PostScript name (ID 6). Font Book shows Japanese names
  on a Japanese system.
- **Font failures happen at drawing time too.** Some faces load and then fail
  `LoadGlyph` on specific characters (ITFDevanagari, measured 2026-09-28).
  A .ttc can have readable and unreadable faces; report errors per face.
- **Characters without glyphs** (variation selectors, ZWJ, ZWSP) are skipped,
  not reported as missing.
- **Line height** is the largest ascent plus the largest descent of the faces
  used on the line; Hiragino's own line gap (1.5em) is not used.
- **Cell aspect differs by terminal** (iTerm2 2.25, kitty 1.86 on the operator's
  machines). The box is the caller's business, and the caller reads the cell
  size from `TIOCGWINSZ` — never from a terminal query, which leaks into the
  Bubble Tea input box.
- **The caller hands over the source before gem-agent's rewrite table**
  (which flattens shapes and turns `&` in labels into `＆` for the text art).
- **Real-data samples are test-request output**, not evidence of use: every
  mermaid block in the operator's sessions came from display tests. Use them
  as syntax samples only.
