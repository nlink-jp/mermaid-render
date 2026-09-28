# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project adheres
to [Semantic Versioning](https://semver.org/).

## [0.1.0] - Unreleased

### Added

- `Parse` for `flowchart` / `graph` (mermaid 12.0.0): directions, the 14
  classic shapes, quoted text, entity codes and `<br>`, solid / thick / dotted
  links with their heads and lengths (flowDb.destructEndLink's rules), link
  text in both forms, chains and `&`, one level of subgraphs with mermaid's
  membership rule and subgraph endpoints; presentation lines ignored. Errors
  are `*Error` with a kind (syntax error / unsupported type / unsupported
  construct) and a line.
- `raster`: flowcharts drawn on a white card. A layered layout ranked in two
  levels (inside each subgraph, then subgraphs as blocks), label and bend
  dummies, barycentre ordering scored on crossings and frame crossings,
  coordinates from difference constraints, one port per link on each face
  (nodes grow to keep ports and self-link ends 0.8 em apart), frames kept
  clear of items in the layers beside them, titles drawn over links, all four
  directions. Text is drawn glyph by glyph: a character no face has is an
  error, characters without glyphs are skipped. `DefaultFont` loads Hiragino
  Sans W3 / W6. Limits: 300 nodes, 600 links, 6 Mpx.
- `tools/mmdpng` renders a mermaid file to PNG and prints the timings.
- Layout property tests (placement, overlaps, link ends on outlines, no link
  through a node, distinct link ends, determinism) on synthetic cases in four
  directions, the 22 real flowcharts, 400 seeded random flowcharts (-random N
  widens the sweep) and the random cases that exposed defects.
- `testdata/real`: the 51 mermaid blocks of gem-agent sessions (39 in replies,
  12 written to files), with each flowchart's parse read against its source
  and frozen.
- Repository scaffold: the `mermaidrender` and `raster` packages (documentation
  only), the development CLI `tools/mmdpng` (answers `-version` only), Makefile.
- RFP (`docs/en/mermaid-render-rfp.md`, `docs/ja/mermaid-render-rfp.ja.md`),
  approved 2026-09-28, including the phase 1 step 0 display measurement.
