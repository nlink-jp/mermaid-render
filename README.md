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
  shows the source instead. Crossings and size never cause a refusal.

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
	// e.Kind: SyntaxError, UnsupportedType or UnsupportedConstruct; e.Line
	// is 1-based in src. Every kind means: show the source instead.
}
switch d := d.(type) {
case *mermaidrender.Flowchart: // Nodes, Links, Subgraphs, Direction
case *mermaidrender.ER:        // Entities (attributes), Relationships, Direction
}

font, err := raster.DefaultFont() // load once, reuse
img, err := raster.Render(d, raster.Options{Font: font}) // *image.RGBA; Scale 0 = 2
img, err = raster.RenderSource(src, raster.Options{Font: font})
```

Parsing follows the mermaid 12.0.0 documentation; details it leaves open
(which characters an id may hold, how link symbols are read, subgraph
membership) follow that version's own parser; `erDiagram` is read with a port
of that version's lexer, rule by rule, so it agrees with mermaid on edge cases
(keywords such as `one` or `to` are never names, and `direction TD` is two
entities). `raster` draws flowcharts and ER diagrams (entities as tables,
cardinalities in crow's foot notation) on a white card with a layered layout; the caller encodes the PNG and chooses the
terminal box. Limits keep a render bounded: 300 nodes and subgraphs (at most
100 subgraphs), 500 links (mermaid's own limit, checked while parsing), 1000
characters per label, link length 10 (as mermaid), `Scale` up to 8, and 12 Mpx
per image (for time and memory). The PNG's size is the caller's to check — it
ran from 0.10 to 0.67 bytes per pixel — against its own limit (termimg's
2 MiB), showing the source when over. Beyond the limits the result is an
`UnsupportedConstruct` error, like a label character no font can draw.

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
