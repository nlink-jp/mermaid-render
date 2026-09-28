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
├── doc.go            # package mermaidrender: Parse, Diagram (phase 1)
├── raster/           # Render, RenderSource, LoadFont, DefaultFont (phase 1)
├── tools/mmdpng/     # development CLI: mermaid file -> PNG
├── Makefile
└── docs/{en,ja}/     # RFP
```

## Gotchas

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
