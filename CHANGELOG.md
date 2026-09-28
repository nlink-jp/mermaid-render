# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project adheres
to [Semantic Versioning](https://semver.org/).

## [0.1.0] - 2026-09-29

First release. How each part was reviewed and changed on the way is in the
RFP's Discussion Log (`docs/{en,ja}`).

### Added

- `Parse` reads `flowchart` / `graph`, `erDiagram` and `sequenceDiagram` as
  mermaid 12.0.0 does, and returns a `*Flowchart`, `*ER` or `*Sequence`
  that says what the diagram means. Every error is an `*Error` with a kind —
  syntax error, unsupported diagram type, unsupported construct — and a
  line; each means "show the source instead". A leading BOM is dropped.
  - flowchart: directions, the 14 classic shapes, quoted text, entity codes
    and `<br>`, solid / thick / dotted links with their heads and lengths
    (flowDb.destructEndLink's rules), link text in both forms, chains and
    `&`, one level of subgraphs with mermaid's membership rule and subgraph
    endpoints; presentation lines ignored.
  - erDiagram: entities, aliases, quoted names, attributes (types with `?`
    and `~` generics, PK / FK / UK, comments), every cardinality in symbols
    and words, identifying and non-identifying relationships, labels,
    `direction`. The lexer is a rule-by-rule port of `erDiagram.jison`.
    Markdown in names and labels, the undocumented `u` cardinality and ER
    subgraphs are unsupported constructs; style, classDef, class and `:::`
    are ignored.
  - sequenceDiagram: participants and actors with aliases, boxes, the ten
    message arrows, activations (statements and +/-), notes, loop / opt /
    alt / par / critical / break blocks, autonumber with start and step,
    title. The lexer is a rule-by-rule port of `sequenceDiagram.jison`.
    create / destroy, half arrows, central connections, typed participants,
    par_over and a box around participants that are not side by side are
    unsupported constructs; rect draws its contents without the colour.
- `raster.Render` / `RenderSource` draw a diagram on a white card as an
  `*image.RGBA`; the caller encodes it and chooses the terminal box.
  - Flowcharts and ER diagrams: a layered layout (ranked inside each
    subgraph, then subgraphs as blocks), orthogonal links on tracks of their
    own, ports kept apart on each face, fewer bends, frames and chains
    centred, all four directions. ER entities are tables (as mermaid's
    erBox) with crow's foot markers at both ends of each relationship.
  - Sequence diagrams: participant boxes and actor figures top and bottom,
    lifelines, the ten arrows, messages to self as loops, activation bars
    (arrows end outside all of a participant's bars, as mermaid does),
    notes, block frames with sections, boxes, autonumber circles.
  - Every render checks its layout before drawing — no box on another,
    every label inside its box and clear of other text, frames holding their
    members, links ending on their ends' outlines and through no other node,
    heads and crow's feet with a run of line behind them, sequence arrows
    pointing at their receivers — and a drawing that breaks one is a
    `LayoutFault` error rather than a wrong picture. The tests hold the same
    properties more strictly; what is only a matter of looks never refuses
    a render. At the limits the check takes tens of milliseconds.
- Text is drawn glyph by glyph and a character no face can draw is an error,
  never a gap: a glyph that leaves no ink (spaces aside) counts as missing;
  variation selectors, joiners and format characters no face draws are
  skipped. `DefaultFont` loads Hiragino Sans W3 / W6; `LoadFont(FontSpec)`
  picks a face by file and name (full or PostScript, any language, any case)
  with a bold face of its own, and Hiragino fills in the characters it lacks.
  A font file is read only if it is a regular file of at most `MaxFontBytes`
  (256 MiB). One `Font` may serve renders on several goroutines.
- Limits, each an unsupported-construct error: 50,000 characters of source
  (mermaid's maxTextSize, in UTF-16 units); 300 nodes and subgraphs (100
  subgraphs); 500 links or relationships; 300 ER entities with 200
  attributes each; 300 participants, 2000 sequence events, blocks nested 50
  deep; 1000 characters per label; link length 10; 20,000 layout items;
  `Scale` in (0, 8]; 12 Mpx per image. The PNG's size is the caller's to
  check.
- `tools/mmdpng`, a development CLI: a mermaid file to PNG with timings;
  `-font`, `-font-name`, `-bold`, `-bold-name` pick faces.
- Tests: layout properties over synthetic cases, the real diagrams and
  seeded random diagrams of each type (`-random N` widens the sweep), with
  a mutation check for each property; the drawing is tied to the source
  through a trace; `testdata/real` holds the 51 mermaid blocks of gem-agent
  sessions, each parse read against its source and frozen.
