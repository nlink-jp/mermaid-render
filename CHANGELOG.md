# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project adheres
to [Semantic Versioning](https://semver.org/).

## [Unreleased]

## [0.5.0] - 2026-09-30

### Added

- `mindmap` (mermaid 12.0.0, `mindmap.jison`, `mindmapDb.ts`): the tree read
  from its indented outline as mermaid reads it — shapes from the opening
  bracket, labels spanning lines, `::icon(…)` and `:::` classes dropped —
  after mermaid's own preparation of the source (a blank line right after
  `mindmap` is mermaid's syntax error, and so here). The reading was
  checked against a parser generated from the grammar. mermaid places a
  mind map with a physics simulation; here it is laid out by fixed rules,
  as mermaid's `tidy-tree` option does in outline: two sides, a band per
  subtree, curved lines, a colour per branch. Labels wrap as mermaid's
  (200 px or 120 px of 16 px text, by shape). Emphasis, icons written in the
  text and math are unsupported. Every render checks the tree's placing,
  texts, wrapping and lines.

### Fixed

- Emphasis in ER and state labels is found by delimiter runs, taking every
  reading of CommonMark's and marked's rules that could differ, instead of
  a regular expression that missed `_` inside the span (`_snake_case_`,
  `__init_db__` were drawn with their underscores where mermaid draws
  italics or bold): such a label is now refused.

## [0.4.0] - 2026-09-29

### Added

- `gantt` (mermaid 12.0.0, `gantt.jison`, `ganttDb.js`): tasks placed as
  ganttDb places them — dates read as dayjs 1.11.21 reads them in
  `dateFormat`, `after` / `until`, durations, excluded days and `weekend`,
  `inclusiveEndDates`, tags, milestones, `vert` markers, sections — in a
  browser set to UTC; the axis as d3 7.9.0 ticks and formats it
  (`axisFormat`, `tickInterval`, `weekday`). Each part was checked against
  the real library under node. What depends on the day it is drawn or on
  the browser is unsupported (so is a task ending before it starts); the
  today marker is never drawn. Every render checks bars on their rows,
  texts, section titles, markers, excluded days and axis ticks.

## [0.3.0] - 2026-09-29

### Added

- `stateDiagram` / `stateDiagram-v2` (mermaid 12.0.0, `stateDiagram.jison`):
  states and their descriptions, transitions with labels, `[*]` start and
  end per scope, composite states nested up to 20 deep, concurrent regions
  (`--`), choice, fork and join, notes, a direction per scope; styling and
  accessibility statements are read and dropped. The lexer is ported rule
  by rule and was checked against a parser generated from the jison
  grammar. Each scope is laid out on its own, innermost first, and a
  composite is drawn as a frame around its regions; a transition crossing a
  composite's frame is unsupported. A note stands on the side it names,
  beside its state in top-down diagrams; there, a note on a start, end,
  choice, fork or join, or on a state with a transition to itself, is
  unsupported. So is a state named `root`. Every render checks every scope's
  layout, each composite's frame, title and regions.

### Changed

- A port snapped onto its link's column stays on its node's face, off the
  rim (a state's end circle is narrow enough for the old snap to miss it).
  Flowchart layouts and the real ER diagrams are unchanged; 3 of 3,000
  random ER diagrams lay out differently.

### Fixed

- **`__bold__` in a markdown label was drawn as written**: it is formatting
  in mermaid, refused like `**bold**` (ER names and labels, state labels).

## [0.2.1] - 2026-09-29

### Fixed

- **The pie render check missed three wrong pictures**: a slice showing
  another slice's percentage, a legend percentage beside another item's
  label (or legend rows out of order), and a single slice's percentage off
  the circle. Every render now refuses them. What v0.2.0 draws is unchanged.

## [0.2.0] - 2026-09-29

### Added

- `pie` (mermaid 12.0.0, `pie.langium`): `showData`, `title` (also on the
  header's line), quoted labels with Langium's escapes, integer and decimal
  values; a repeated label keeps its first value, a negative value is an
  error. Drawn as pieRenderer draws it: slices in order, clockwise from
  twelve o'clock, items under 1% without a slice but in the legend,
  percentages of the whole rounded as `toFixed(0)`, `showData` values in the
  legend. Every item's percentage also stands in a column at the legend's
  right (`<1%` for an item with no slice), and a slice carries its
  percentage only when it fits inside, so no text overlaps. Every render
  checks the slices, the legend and the labels. At most 100 items.

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
