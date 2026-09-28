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
- Links between layers are orthogonal: down, across on a track of their own
  in the gap, down (step-2 review: shallow diagonals ran as close as 0.07 em
  and could not be told apart). Two links swapping near-equal columns detour
  through a free column. Ports align with the column their link goes on in.
- Nodes and frames grow until link ends, self-link ends and the columns
  links cross a frame's face in keep 0.8 em apart, measured on the real
  outline; a slanted shape's slant is fixed from its size before growing, so
  its label never spills out; an outer self-link clears the inner labels;
  frame titles no longer hide arrowheads (heads are drawn again after them).
- Fewer bends (the operator's check marked 16 of 22 real diagrams for
  needless bends): coordinates move toward the median of an item's
  neighbours, not their mean, so links line up with one of them; subgraphs
  shift as whole blocks toward their outside links; each link's bend points
  move onto its ports' columns where the constraints allow (repeated until
  nothing moves, else carried forward as far as they go); steps under 0.2 em
  snap straight. Over the 22 real diagrams: 254 bends to 114, 61 small steps
  to 4 (TestRealBends keeps the baseline).
- The operator's second check: a node one link fans out from (or into) lines
  up with that link; a node with one link on a face sits centred on it (a
  rhombus takes it at the vertex); tracks in a gap are 0.8 em apart (was
  0.45), so fanning links no longer look merged. The bend baseline rose to
  116 / 5 on purpose. Link spacing is now checked at 0.6 em.
- Limits: 300 nodes and subgraphs (at most 100 subgraphs), 1000 characters per
  label, Scale in (0, 8], 12 Mpx per image for time and memory (the PNG's
  size is the caller's to check: 0.10-0.67 bytes per pixel, so a pixel cap
  low enough for 2 MiB refused a real diagram); dummy items are counted
  before any is built; text
  runs no longer grow quadratically. Each adversarial input of the review is
  refused at once.
- Layout properties now also check: link ends 0.8 em apart (was 0.2), links
  crossing only at a clear angle or keeping 0.3 em apart, no link through
  another link's label, every node label inside its shape. A 60,000-seed
  sweep passes.
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
