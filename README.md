# mermaid-render

A Go library that turns mermaid source into an image. gem-agent and lagent use
it to show mermaid diagrams in their transcripts as pictures, on terminals that
support the iTerm2 or kitty image protocol.

- **In-house, no outside code.** Parsing, layout and drawing are written here.
  mermaid.js is not used.
- **A runtime rendering feature, not a model tool.** The runtime renders
  mermaid fences in place while drawing the transcript; the transcript keeps
  the source verbatim.
- **Wrong pictures are refused; ugly ones are not.** Anything the engine
  cannot read, or a label character no font has, is an error, and the caller
  shows the source instead. So is a drawing that breaks the engine's own
  layout properties: every render checks them. Crossings and size never cause
  a refusal.

## Diagrams

`flowchart` / `graph`, `sequenceDiagram`, `erDiagram`, `pie`,
`stateDiagram` / `stateDiagram-v2`, `gantt` and `mindmap`, as specified in the RFP (the
reference is the official mermaid documentation, 12.0.0). Other diagram types are returned
as "unsupported diagram type".

A pie chart is drawn as mermaid draws it — slices in the order written,
clockwise from twelve o'clock; an item under 1% of the whole gets no slice
but keeps its legend row; a slice shows its share of the whole, rounded as
`toFixed(0)`; `showData` adds each value to the legend. One difference: every
item's percentage also stands in a column at the legend's right (`<1%` for an
item with no slice), and a slice carries its percentage only when it fits
inside (mermaid writes every one on its slice, where thin slices' labels
overlap).

A state diagram is read as mermaid reads it — `[*]` as its scope's start or
end, composites nested up to 20 deep, `--` regions, choice, fork and join,
notes, a direction per scope. Each scope is laid out on its own, innermost
first: a composite is a frame holding its title and its regions side by side,
and a transition to it stops at the frame. So a transition that crosses a
composite's frame (into its inner state from outside, or between regions) is
unsupported, and the source is shown. A note stands on the side it names
(beside its state in top-down diagrams), joined by a dotted line; in a
top-down diagram a note on a start, end, choice, fork or join, or on a state
with a transition to itself, cannot stand there and is unsupported.

A gantt chart's tasks are placed as mermaid places them — dates read as
dayjs reads them in `dateFormat`, `after` / `until`, durations, excluded
days, tags, milestones and `vert` markers — computed as in a browser set to
UTC; the axis ticks and labels are d3's. What would depend on the day it is
drawn or on the browser is unsupported and the source is shown: an `after`
or `until` naming no task (mermaid uses today), a start mermaid hands to the
browser's own date parser (unless it is ISO), a `dateFormat` whose date comes
from today. The today marker is never drawn. Section titles stand in a column
on the left; a task's text sits in its bar or to its right; dense axis labels
are thinned.

A mind map's tree is read as mermaid reads its indented outline — the parent
of a node is the last one before it indented less, shapes come from the
opening bracket, `::icon(…)` and `:::` classes are dropped. mermaid places it
with a physics simulation whose result is not fixed; here it is laid out by
fixed rules, as mermaid's `tidy-tree` option does in outline: the root's
children alternate left and right (the first on the left), every subtree in
a band of its own, lines curving from a parent's side to its children, each
branch in its own colour. Labels wrap as mermaid wraps them (12.5 em; 7.5 em
in rectangles, rounded rectangles and hexagons). Emphasis (`**x**`, `_x_`),
icons written in the text and math are unsupported and the source is shown.

## API

```go
d, err := mermaidrender.Parse(src) // src: the fence's contents
var e *mermaidrender.Error
if errors.As(err, &e) {
	// e.Kind: SyntaxError, UnsupportedType or UnsupportedConstruct (and
	// LayoutFault, from Render only); e.Line is 1-based in src. Every kind
	// means: show the source instead.
}
switch d := d.(type) {
case *mermaidrender.Flowchart: // Nodes, Links, Subgraphs, Direction
case *mermaidrender.ER:        // Entities (attributes), Relationships, Direction
case *mermaidrender.Sequence:  // Participants, Boxes, Events (messages, notes, blocks)
case *mermaidrender.Pie:       // Slices (label, value), ShowData
case *mermaidrender.Gantt:        // Tasks (times in ms, UTC), AxisFormat, ticks, excluded days
case *mermaidrender.StateDiagram: // Root: a scope of States, Transitions, Notes; composites hold Regions
case *mermaidrender.Mindmap:      // Nodes in source order: Text, Shape, Level, Parent, Children, Section
}

font, err := raster.DefaultFont() // load once, reuse
// or a face of your own; characters it lacks come from Hiragino one by one
font, err = raster.LoadFont(raster.FontSpec{Path: "/path/Font.ttc", Name: "Font-Regular",
	BoldPath: "/path/Font.ttc", BoldName: "Font-Bold"})
img, err := raster.Render(d, raster.Options{Font: font}) // *image.RGBA; Scale 0 = 2
img, err = raster.RenderSource(src, raster.Options{Font: font})

// Text art, for terminals that draw no images: box-drawing characters.
art, err := raster.RenderText(d, raster.TextOptions{Width: cellWidth}) // Width nil: East Asian Width
```

Text art draws flowcharts and ER diagrams in box-drawing characters on a grid
of terminal cells, running the picture's layered layout snapped to the grid
(sequence diagrams follow). ER entities are tables, laid out left to right,
with the cardinality in mermaid's notation (`||`, `o{`, …) next to each table. `Width` gives the columns a character takes on the
caller's terminal. A label holding a control character or a multi-rune
grapheme cluster is unsupported in the art. Every render is checked on the
grid; a fault is a `LayoutFault` and the caller shows the source.

Parsing follows the mermaid 12.0.0 documentation; details it leaves open
(which characters an id may hold, how link symbols are read, subgraph
membership) follow that version's own parser (one recorded exception: `A -- go--> B`
is read as label "go", where mermaid reads "g" and a start mark); `erDiagram` and `sequenceDiagram`
are read with a port of that version's lexers, rule by rule, so it agrees with mermaid on edge cases
(keywords such as `one` or `to` are never names, and `direction TD` is two
entities). `raster` draws flowcharts, ER diagrams (entities as tables,
cardinalities in crow's foot notation) and sequence diagrams on a white card with a layered layout; the caller encodes the PNG and chooses the
terminal box. Limits keep a render bounded: 50,000 characters of source (mermaid's own
maxTextSize), 300 nodes and subgraphs (at most
100 subgraphs), 500 links or relationships (mermaid's own limit, checked while
parsing), 300 ER entities with at most 200 attributes each, 300 sequence
participants with at most 2000 events and blocks nested 50 deep, 100 pie
items whose total is a finite number, composite states nested 20 deep, 500
gantt tasks (at the default scale about 200 rows reach the pixel limit first),
200,000 days checked against `excludes`, 300 mind map nodes, 1000
characters per label, link length 10 (as mermaid), `Scale` up to 8, and 12 Mpx
per image (for time and memory). A flowchart whose links would need more than
20,000 layout items (very long links through many layers) is refused too. The PNG's size is the caller's to check — it
ran from 0.10 to 0.67 bytes per pixel — against its own limit (termimg's
2 MiB), showing the source when over. Beyond the limits the result is an
`UnsupportedConstruct` error, like a label character no font can draw.

Every render checks its own layout before drawing: no box on another, every
label inside its box and clear of other text, frames holding their members,
links starting and ending on their ends' outlines and passing through no other
node, heads and crow's feet with a run of line behind them, sequence arrows
pointing at their receivers and frames holding their rows. A drawing that
breaks one is a `LayoutFault` error — the engine's defect, not the source's —
so a layout bug the tests never met shows the source rather than a wrong
picture. The check holds only what makes a picture wrong; the tests hold the
same properties more strictly (spacing, centring, frame crossings), which are
matters of looks and never refuse a render. At the limits it takes tens of
milliseconds.

## Fonts

`FontSpec.Name` picks a face in a file (a .ttc holds several) by its full
name or PostScript name, in any language the file records, ignoring case:
`HiraginoSans-W6` and `ヒラギノ角ゴシック W6` both work. An unknown name is an
error that lists the file's faces; a face that fails to load is reported by
name, since a collection's faces can fail one by one. Characters the chosen
faces lack are drawn with Hiragino Sans, one by one; a character no face has
(an emoji, say) is an error, never a gap; a glyph that leaves no ink (Apple
Color Emoji's bitmaps, which x/image cannot draw) counts as missing, except
for spaces. A font file is read only if it is a regular file of at most
`MaxFontBytes` (256 MiB; the largest system font is 183 MiB). Variation selectors, ZWJ, ZWNJ and ZWSP are skipped, and so is any
format or default-ignorable character (LRM, word joiner) no face draws. One
Font may serve renders on several goroutines; they take turns. `mmdpng -font path -font-name name` tries a face.

## Dependencies

`golang.org/x/image` (font loading, text drawing, filling shapes) and
`golang.org/x/text` (character widths for text art), maintained by the Go team. This is a declared exception to lib-series' standard-library
rule, approved by the operator on 2026-09-28. At run time the only outside
resource is the macOS system fonts; nothing is bundled or downloaded. The
default font is Hiragino Sans W3 / W6.

## Development

```bash
make test     # go test ./...
make vet      # go vet ./...
make build    # dist/mmdpng — the development CLI (mermaid file -> PNG), never released
```

## Documentation

- [RFP](docs/en/mermaid-render-rfp.md) — the specification, the measurements it
  rests on, and the decisions taken ([日本語](docs/ja/mermaid-render-rfp.ja.md))

## License

MIT
