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
