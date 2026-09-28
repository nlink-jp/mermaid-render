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

Phase 1 covers `flowchart` / `graph`, `sequenceDiagram` and `erDiagram`, as
specified in the RFP (the reference is the official mermaid documentation).
`stateDiagram` and nested subgraphs follow in phase 2. Other diagram types are
returned as "unsupported diagram type".

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
}

font, err := raster.DefaultFont() // load once, reuse
// or a face of your own; characters it lacks come from Hiragino one by one
font, err = raster.LoadFont(raster.FontSpec{Path: "/path/Font.ttc", Name: "Font-Regular",
	BoldPath: "/path/Font.ttc", BoldName: "Font-Bold"})
img, err := raster.Render(d, raster.Options{Font: font}) // *image.RGBA; Scale 0 = 2
img, err = raster.RenderSource(src, raster.Options{Font: font})
```

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
participants with at most 2000 events and blocks nested 50 deep, 1000
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

`golang.org/x/image` (font loading, text drawing, filling shapes), maintained
by the Go team. This is a declared exception to lib-series' standard-library
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
