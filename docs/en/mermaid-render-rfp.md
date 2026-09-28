# RFP: mermaid-render

> Generated: 2026-09-28
> Status: Approved (2026-09-28) — second draft, incorporating two independent verification passes and operator decisions

## 1. Problem Statement

When a gem-agent reply contains a mermaid diagram, the agent today converts it into box-drawing
text art for the terminal (gem-agent ADR-0042 / ADR-0063). That approach cannot express much of
the syntax, breaks easily once Japanese is mixed in, and often ends up showing the source
instead. The operator wants diagrams but cannot use them at this quality, so work instructions
do not ask for them. lagent has no diagram path at all. On top of that, the converter,
`AlexanderGrooff/mermaid-ascii`, is community code, adopted before the organization settled that
it does not ship code written outside it.

Both runtimes can already put images in the terminal (`internal/termimg`, the iTerm2 and kitty
protocols, gem-agent ADR-0089). This project gives them an in-house engine, using no third-party
code, that turns mermaid source into an image. When a runtime draws the transcript, it renders
mermaid fences in place as images, and the operator reads diagrams as pictures in the terminal.
Once diagrams can be drawn, the operator will include them in work instructions.

The intended users are operators of gem-agent and lagent. They prepare nothing: the default
font is Hiragino, which ships with macOS, and nothing touches the network or any credentials.

## 2. Functional Specification

### How it is used (a runtime rendering feature, not a model tool)

- When a runtime draws a reply into the transcript, it finds mermaid fences, hands them to this
  engine, and shows the returned image in place. The model is given no diagram tool. The system
  prompt says nothing about diagrams (carried over unchanged from gem-agent ADR-0063). Diagrams
  are asked for by the operator's work instructions.
- The transcript stores the source verbatim. Images are for display only.
- The engine receives the source **before** gem-agent's existing rewrite table (which, for the
  text-art renderer, flattens shapes to rectangles and turns `&` inside labels into a full-width
  `＆`). Handing it the rewritten source would lose shapes and label characters.

### Commands / API Surface

A Go library. Module: `github.com/nlink-jp/mermaid-render`.

| API | Purpose |
|---|---|
| `Parse(src string) (Diagram, error)` | Turns source into diagram data that is independent of how it is drawn. The phase 2 text renderer uses the same data |
| `raster.Render(d Diagram, opts Options) (*image.RGBA, error)` | Draws the diagram data as an image on a white card |
| `raster.RenderSource(src string, opts Options) (*image.RGBA, error)` | Does both of the above in one call |
| `raster.LoadFont(spec FontSpec) (*Font, error)` | Loads the given font. The result can be reused |
| `raster.DefaultFont() (*Font, error)` | Loads Hiragino Sans W3 (body) and W6 (bold) |

`Options` holds only two things:

- `Font` — the font to use. Hiragino when omitted.
- `Scale` — the scale factor. Defaults to **2** (set by the step 0 measurement). Body text is 14px
  at `Scale` 1, so 28px by default.

Encoding to PNG is the caller's job (`termimg.Payload` takes already-encoded bytes; termimg has
no code that turns an `image.Image` into PNG).

**Development CLI**: a small CLI under `tools/`, driven from a make target, that converts a
mermaid file to PNG, for hands-on checks and E2E runs. It is neither released nor distributed.

### Display size

The existing `termimg.BoxFor` is a policy for photos: it fits an image into at most 40 columns
and a third of the screen height. A 1600×1200 diagram in that box has its text shrunk to about
5.6pt and cannot be read (the verification pass's estimate). Diagrams therefore get their own
box policy.

The box policy set by the step 0 measurement (iTerm2 and kitty; see the Discussion Log):

- **Diagram text appears the same size as the terminal's text.** The diagram's em is matched to
  "cell height ÷ 1.2". The operator compared 0.85, 1.00 and 1.15 times and chose 1.00 on both
  terminals.
  - Height (rows) = image height ÷ diagram text size (px) ÷ 1.2
  - Width (columns) = image width ÷ diagram text size (px) ÷ 1.2 × cell aspect (height ÷ width)
- **Width is capped at the terminal width − 1.** A diagram beyond it is shrunk, keeping its
  shape. Small, hard-to-read text is an aesthetic problem, so display is not refused (if it
  cannot be read, the operator tells the model to redo it).
- **The cell aspect comes from `TIOCGWINSZ`.** That is a read from the OS, not a query to the
  terminal, so no reply can leak into the input box while BubbleTea runs. When it is unavailable,
  2.25 is used (termimg's assumed value, which matched iTerm2's measurement). Terminal queries
  (CSI 16t and the like) are not used.
- **Height has no aesthetic cap.** How an image taller than the screen interacts with the
  bottom-pinned layout is checked, and its handling decided, in the integration ADR.
- The box function lives with the integration (next to termimg). The engine does not know the
  display size.

### Choosing a font

`FontSpec{Path, Name, BoldPath, BoldName}`

- `Path` — path to a font file (.ttf / .otf / .ttc).
- `Name` — selects a face inside a .ttc. It is matched, case-insensitively, against **every
  language's** full name (name ID 4) and PostScript name (name ID 6). The recommended value is
  the PostScript name (for example `HiraginoSans-W6`). In a Japanese environment Font Book shows
  Japanese names such as 「ヒラギノ角ゴシック W6」; those match too. When nothing matches, the
  error lists the face names in the file. `sfnt.Name` cannot choose a language (it returns the
  first record), so the name table is walked in-house. The first face is used when omitted.
- `BoldPath` / `BoldName` — the bold face, used for ER entity names, subgraph titles and the
  like. Same as the body face when omitted.

Behaviour:

1. **Missing characters** — a character counts as missing from a face when its glyph index is 0
   (`GlyphIndex == 0`) or loading its glyph fails (an error from `LoadGlyph`). Faces exist that
   load fine yet fail on specific characters (9 faces in the verification pass's measurement).
   `font.Drawer`'s `DrawString` / `MeasureString` draw missing characters as tofu and silently
   drop failures, so they are not used; the per-character face selection loop is written
   in-house.
2. **Fill in from Hiragino** — characters the chosen face lacks are drawn with Hiragino, one
   character at a time.
3. **Still missing is an error** — a character missing from a label is a wrong picture, not an
   ugly one, so no image with missing characters is returned. The caller shows the source.
   Emoji (✅ and the like) are not in Hiragino and fall under this rule.
4. **Characters without glyphs are skipped** — variation selectors (U+FE00–FE0F), ZWJ (U+200D)
   and ZWSP (U+200B) are not drawn and are not errors.
5. **Line height** — the largest ascent plus the largest descent among the faces used on that
   line. Hiragino's line gap (1.5em) is not used.
6. **An unreadable face is an error** — errors are reported per face, not per file (a .ttc can
   have readable and unreadable faces), with the reason (which part of the font could not be
   read).

Fonts are loaded at startup, by the runtime's cmd layer. The drawing (view) layer never opens
files (gem-agent ADR-0089 §5 / ADR-0090 §5).

### Input / Output

- Input: mermaid source as a string, without the code fence.
- Output: `*image.RGBA`, drawn on a white card. The terminal's background colour is never queried.
- Errors come in three kinds, each carrying a line number. The caller shows the source in every
  case. The kinds are separated because the existing display already treats them differently
  (gem-agent ADR-0063 §4: an unsupported diagram type passes through silently as source; a
  diagram that was attempted and failed gets the source plus a one-line note).
  - **Unsupported diagram type** — state, class, gantt and so on.
  - **Unsupported construct** — a construct this engine does not draw, inside a supported type.
  - **Syntax error** — not valid mermaid.
- Resource limits: node count and image pixel count are capped; exceeding them is treated like an
  unsupported construct. The caps are chosen so the encoded PNG stays under `termimg.MaxBytes`
  (2MiB). Today's `drawImage` silently draws nothing for an image over that limit; making sure a
  diagram never vanishes from the screen together with its source is an integration requirement
  (§4, phase 3).
- The same input with the same font always produces the same image. A test fixes that nothing
  depends on map iteration order.

### Syntax supported in phase 1

**The reference is the official mermaid documentation** (the Flowchart, Sequence diagram and
Entity Relationship Diagram pages on mermaid.js.org). **The version consulted is mermaid 12.0.0
(published 2026-09-10).** Lexical details the documentation does not state (which characters a
node ID may hold, how link symbols are read, subgraph membership) are checked against the same
version's `flow.jison` and `flowDb.ts`, and the tests say so. What follows is chosen from that specification by combining the real
data (Discussion Log) with the specification's core syntax. [R] marks constructs seen in the
real data.

**Common**

- Front-matter (between `---` lines): `title` is drawn as the card heading; `config` is ignored.
- `<br>` / `<br/>` inside labels is a line break. Entity codes such as `#quot;` and numeric codes
  such as `#35;` are turned back into characters.
- `%%` comments are ignored.

**flowchart / graph**

- Directions: TD / TB / BT / LR / RL
- Node shapes: all 14 classic shapes (rectangle, rounded, stadium [R], subroutine, cylinder [R],
  circle, asymmetric, rhombus [R], hexagon, parallelogram [R], parallelogram alt, trapezoid,
  trapezoid alt, double circle)
- Quoted labels `A["…"]` [R]
- Node IDs: IDs containing Japanese, capitalised words such as `End` [R] (only lowercase `end`
  is reserved)
- Links: solid, dotted and thick; no arrowhead; longer links (`--->` and so on); `o` and `x`
  ends; both ends (`<-->` `o--o` `x--x`)
- Link labels: `-->|text|`, `-- text -->` [R], `-. text .->`, `== text ==>`, `-- text ---`
- Chaining `A --> B --> C`, and `&` for several nodes at once [R]
- Cycles [R]
- Subgraphs, one level [R]: `subgraph id [title]`, `subgraph id["title"]`, `subgraph title`,
  Japanese IDs [R]. Links whose endpoint is a subgraph [R]
- **Subgraph membership rule** — a node belongs to **the first subgraph that mentions it**.
  Declaring it at the top level does not settle membership. This is mermaid.js's own rule
  (`makeUniq` in flowDb). It changes the result for 6 of the 11 replies in the real data that use
  subgraphs; getting it wrong puts nodes in the wrong box, a wrong picture.

**sequenceDiagram**

- participant / actor, aliases with `as` [R]
- Messages: `->` `-->` `->>` `-->>` `-x` `--x` `-)` `--)` and the bidirectional `<<->>` `<<-->>`
- Messages to self [R]
- autonumber [R] (including start and step)
- activate / deactivate, and the `+` / `-` shorthand [R]
- note (left of / right of / over) [R]
- alt / else / loop / opt / par / and / critical / option / break frames
- box (a group of participants)
- `rect` is a colour setting: its frame is ignored and its contents are drawn

**erDiagram**

- Entities, aliases, names containing hyphens, quoted names
- Entities without an attribute block [R]
- Attributes (type, name, PK / FK / UK and their combinations, comment) [R], optional types
  `string?`, generics with `~` (`List~int~` shows as `List<int>`)
- Every cardinality marker, and the word forms (`one or more` and so on); a marker may stand on
  either side (`o{--||`)
- Identifying (`--`) and non-identifying (`..`) relationships, relationship labels (quoted ones
  included) [R]
- `direction` (supported, since ER shares the flowchart layout)

**Ignored (presentation only)**: classDef, style, class, `:::`, linkStyle, `%%{init}%%`, click,
`direction` inside a subgraph, `~~~` (invisible links, which exist only for layout),
accTitle / accDescr.

**Unsupported (an unsupported-construct error)**: nested subgraphs (phase 2), the extended
`A@{ shape: … }` shapes, edge IDs (`e1@-->`), Markdown strings (`` "`…`" ``), sequence
create / destroy and half or central arrows, ER subgraphs, and every other line not listed
above.

### Layout specification

- **flowchart and ER**: a layered layout (the Sugiyama method), written in-house.
  1. Cycle removal: back edges found by depth-first search are reversed for layout and drawn in
     their original direction.
  2. Layer assignment: longest path.
  3. A link spanning two or more layers gets a dummy node on each layer. A link label also takes
     up a layer as a dummy node.
  4. Crossing reduction: the barycentre method, swept down and up repeatedly. Ties are broken by
     order of appearance in the source, so the result is deterministic.
  5. Coordinates: packed from the left within a layer, with dummy-node chains kept straight. A
     node that one link fans out from (or into) lines up with that link's column. A node with one
     link on a face sits centred on that link's column (a rhombus takes the link at its vertex). A
     run of items linked one to one moves onto one column that every item of it can reach. A link
     ending on a subgraph aims at the frame's middle, so subgraphs linked frame to frame line up
     on their middles.
  6. Links run straight down through a layer and change column between layers with a
     down-across-down right-angle path. Each across run gets its own height (a track) in the gap,
     so links only ever meet at right angles; tracks are 0.8 em apart. When two links swap into
     each other's columns, one detours through a free column. An across run that passes over
     another link's leaving column turns below it, and one that passes over its arriving column turns
     above it, so links fanning out of one node do not cross. Every arrowhead ends a straight run
     longer than the head (links drawn against the layer order too). A link that changes column does
     so beside a node where it can. A node taking one link of a fan alone is met at its middle
     (a link already straight stays as it is). Ports sit, where they can, right above or below the column the
     link goes on in.
  7. Links sharing a node get their own ports, at least 0.8 em apart per face; a node or frame grows
     when a face is short (self-link ends included, checked by measuring on the real outline). A link
     from a node to itself is drawn as a loop beside the node; an outer loop leaves beyond the inner
     loops' labels.
  8. Subgraphs: member nodes are kept adjacent within each layer, and an enclosing frame and
     title are drawn. A link whose endpoint is a subgraph stops at the frame's edge.
  9. Direction: layout is done in TD, and LR / RL / BT are obtained by transforming coordinates.
- **sequence**: participants are lined up across and messages stacked down. A message to self
  folds back to the right.

### Configuration

The library has no configuration file and no environment variables. Settings such as the font
live in the gem-agent and lagent configuration and reach the library as a `FontSpec`. The key
names, and whether a bad setting stops startup or falls back to the default font, are decided in
each runtime's integration ADR.

### External Dependencies

- `golang.org/x/image` (font loading, text drawing, shape filling). go.mod also gains
  `golang.org/x/text` and `golang.org/x/sys`, but only `x/text/encoding/charmap` is linked (the
  verification pass's measurement; about 0.4MB of binary). All are maintained by the Go team.
  x/image v0.46.0 requires go 1.26 or later.
- This dependency is an exception to lib-series' standard-library-only principle, so it is
  declared not only in this RFP but also in the library's README / README.ja / AGENTS.md and in
  its row of the lib-series catalog. Updates and vulnerabilities are checked with govulncheck
  before each release.
- At run time the only outside resource is the macOS system fonts (`/System/Library/Fonts`).
  Nothing is bundled or downloaded.

## 3. Design Decisions

- **Implement it in Go, in-house.** gem-agent and lagent, the consumers, are both Go, and the
  organization does not ship code written outside it (ADR-024). The common approach, running
  mermaid.js in headless Chrome (mermaid-cli), would ship outside code as is, needs Chrome, and
  starts slowly, so it is not taken.
- **Accept the dependency on `golang.org/x/image`** (operator decision, 2026-09-28). Writing font
  loading and rasterization from scratch does not pay. It is maintained by the Go team and treated
  as first-party (gem-agent already uses `x/oauth2` and `x/sys` directly).
- **Build it as a runtime rendering feature, not a model tool** (operator decision, 2026-09-28).
  Models barely use a diagram tool, so it would go unused (gem-agent's `render_diagram` fired once
  in 76 sessions and was removed by ADR-0063).
- **Revise A3 of gem-agent ADR-0089 (the rejection of rendering mermaid to PNG).** A3's reason was
  that ADR-0042's faithfulness checks (every label present, link count equal to arrowhead count)
  exist for the text-art renderer and would vanish along with it under PNG. Those checks were
  needed because they reverse-engineered, from a string, the output of someone else's renderer
  whose internals could not be inspected. An in-house engine can inspect its own layout result
  directly, so the checks move inside the engine.
  - Layout checks: every node, link and label is placed; labels do not overlap other labels,
    nodes or subgraph titles; links do not pass through the frame of any node other than their
    endpoints; multiple links do not overlap.
  - Misreading is not caught by checks at drawing time (past failures, such as reading
    `-- text -->` as a separate node, had every label present and still a wrong graph). That is
    guarded by a table of syntax tests drawn up from the official specification, and by a person
    looking at each output of the real data (§4). This RFP does not claim that wrong pictures
    "cannot happen by construction".
  - The formal revision is made in the gem-agent integration ADR (with an Amended note on
    ADR-0089).
- **Keep parsing separate from drawing**, so the phase 2 text renderer can use the same diagram
  data.
- **Draw on a white card.** Querying the terminal's background colour has leaked the reply into
  the input box under BubbleTea before (gem-agent, 2026-08-18). A white card reads on light and
  dark terminals alike.
- **Guard against wrong pictures; do not judge ugly ones** (carried over from gem-agent
  ADR-0042). No diagram is refused for crossings, size, or text shrunk small.
- **Resource limits do exist.** They are not an aesthetic judgment: they keep rendering from
  running away or exhausting memory. The values are measured and set during implementation.
- **The font can be chosen** (operator request, 2026-09-28). A missing character is still treated
  as a wrong picture.
- **It takes on no role the OS manages.**

**Existing tools this complements**: `internal/termimg` in gem-agent and lagent (image display),
and gem-agent's `internal/diagram` (finding mermaid fences, text-art display).

**Out of scope**:

- Diagram types other than flowchart, sequence and ER (state is phase 2; class, gantt, pie,
  mindmap, journey, gitGraph and others are not planned).
- Themes, colour settings, applying style.
- SVG output.
- Images on sixel terminals or terminals without image support; these keep today's display.
- Correcting badly written mermaid (errors lead to the source being shown).
- Any tool for the model, and any diagram instruction in the system prompt.

## 4. Development Plan

### Phase 1: Core

0. **Display measurement** — **done (2026-09-28)**. Hand-drawn PNGs of representative diagrams,
   at several text sizes and boxes, were shown with termimg on real iTerm2 and kitty for the
   operator to compare. Conclusions are in §2 "Display size" and `Scale`; numbers in the
   Discussion Log.
1. Diagram data types, and parsing flowchart
2. Layout (subgraphs one level deep) and drawing flowchart
3. ER (reusing the same layout)
4. sequence (its own layout)
5. Fonts: per-face loading, selection by name, per-character fallback to Hiragino, detection of
   missing characters
6. Development CLI (`tools/`)

Tests:

- Parsing: a table per supported construct, drawn up from the official specification; the three
  error kinds are told apart correctly. The subgraph membership rule gets regression tests from
  the affected real blocks. The traps gem-agent pinned in its fourth review (quoted text is
  literal, `;` separates statements, IDs starting with a keyword, a bidirectional link counts as
  two) carry over.
- Layout properties: nodes never overlap; link endpoints sit on node frames; a subgraph encloses
  its contents; everything fits inside the image; labels do not overlap other labels, nodes or
  titles; links do not pass through nodes other than their endpoints; multiple links do not
  overlap. Pixel-by-pixel comparison is not the centre of the suite: it breaks across `x/image`
  versions.
- Real data: the real blocks are frozen in `testdata`, replies and file writes kept apart. Every
  block of a supported type becomes an image, and every block that does not fails for the stated
  reason.
- **A person looks**: each output of the real data is looked at, and whether nodes, links,
  labels and membership match the source is recorded item by item (green tests can still pass a
  misread diagram).
- **The tests are shown to work**: for each property test, the implementation is broken on
  purpose and the test is seen to fail.
- The same input produces the same image.
- Independent reviews after step 2 and after step 5.

### Phase 2: Features

- stateDiagram (reusing the flowchart layout)
- Nested subgraphs
- Replace the text-art renderer with an in-house one and remove `mermaid-ascii` from gem-agent
  (the binary shrinks by about 4.3MB net)
- An independent review after phase 2 as well.

### Phase 3: Release

- First release of mermaid-render and registration in lib-series. Before release, an independent
  implementation review and govulncheck.
- **Integrate into gem-agent** (with an ADR):
  - Revise A3 of ADR-0089 (for the reasons in §3).
  - Change the type of the reply rendering function. Today `diagram.Split` runs inside glamour's
    renderer (`func(string) string`) and the result is counted as a string. An image must be
    handed over as a segment that declares the rows it occupies, or row accounting (ADR-0089)
    breaks. The rendering function becomes one that returns a list of segments carrying row
    counts.
  - The diagram box policy (§2 "Display size", with the values measured in step 0).
  - When an image exceeds `termimg.MaxBytes` or cannot be drawn, the source is always shown
    (nothing vanishes silently).
  - Terminals without image support use today's text art (the in-house text art after phase 2).
  - The font configuration keys, and what happens on a bad setting.
  - Verify on real iTerm2 and kitty.
- **Integrate into lagent**: as a runtime rendering feature, the same as gem-agent. lagent has no
  mermaid path (lagent ADR-0020), so fence detection and the display path are built new.
  Terminals without image support show the source.
- README / README.ja / CHANGELOG / AGENTS.md, the lib-series catalog, the org profile, feedback to
  knowledge.

Phase 1 steps 0, 1–2, 3, 4 and 5 can each be reviewed on their own.

## 5. Required API Scopes / Permissions

None. No network access and no credentials.

## 6. Series Placement

Series: lib-series
Reason: a shared library used by both gem-agent and lagent, belonging to neither. Placed the
same way as pathguard.

## 7. External Platform Constraints

- Images appear only on terminals speaking the iTerm2 or kitty protocol. Detecting support and
  declaring the rows and columns an image occupies is done by the existing termimg (gem-agent
  ADR-0089). sixel cannot declare the rows it occupies and is out of scope.
- One image is capped at `termimg.MaxBytes` (2MiB).
- From sending one image to the terminal answering the next query took 17–105ms on iTerm2 and
  15–106ms on kitty (PNGs of 59–267KB, measured in step 0). This bounds when the terminal finished
  receiving the image, not when it finished drawing it.
- The cell aspect differs by terminal and settings (step 0: 2.25 on iTerm2, 1.86 on kitty). kitty
  places the image across the whole declared rows and columns, so a wrong ratio may distort the
  diagram. The ratio is read from `TIOCGWINSZ` (§2).
- Fonts come from the macOS system fonts. The target is macOS only (gem-agent and lagent are
  darwin/arm64 only as well).

---

## Discussion Log

- **2026-09-28, starting point**: the operator proposed an engine that renders mermaid to an
  image, so that gem-agent / lagent transcripts could show diagrams as pictures in the terminal.
  Investigation found the display half (termimg, iTerm2 / kitty) already present in both
  runtimes; only mermaid → image was missing.
- **How to build it**: running mermaid.js in headless Chrome would ship outside code, so it is
  written in Go in-house instead.
- **Operator decisions (2026-09-28, first draft)**: the `golang.org/x/image` dependency is
  acceptable. Replacing the text art is phase 2 (parsing is shareable from the start). Draw on a
  white card. The name is `mermaid-render`. The CLI is for development only. Presentation-only
  settings are ignored; constructs with meaning that cannot be read are errors, and the source is
  shown. The font can be chosen.
- **Premise measured — fonts**: `x/image` read Hiragino Sans W3 (a .ttc of 4 faces, about 7.8MB)
  and drew Japanese, arrows and circled digits with nothing missing. The verification pass
  re-measured: neither W3 nor W6 lacks any of the 333 distinct characters in the real data;
  loading through NewFace for both W3 and W6 takes about 6ms.
- **Premise measured — which fonts load**: of the 535 font files on the machine, 12 files have
  unreadable faces, 32 faces in all (HelveticaNeue reads 5 of 14, Songti 5 of 8, and so on).
  Beyond that, 9 faces load fine yet fail to draw specific characters (the digit '1' in
  ITFDevanagari, for example). Apple Color Emoji has glyph indices but no outlines (sbix). → The
  specification now reports errors per face, treats drawing-time failures as missing characters,
  and does not use `font.Drawer`.
- **Real data (recounted for the second draft)**: across gem-agent's 250 sessions (252 was the
  count of .jsonl files including working files) there are 51 distinct mermaid blocks: 39 in reply
  text (flowchart / graph 19, ER 10, sequence 9, state 1) and 12 in file writes (all six other
  types are here). lagent's 140 sessions contain none. 15 sessions contain mermaid: 14 were
  requests to test the display (08-22 and 09-02, the days the text art was built and reworked;
  3 of them E2E runs that dictated the diagram's content) and 1 was a code-review session.
  Mermaid arising naturally during real work is close to zero.
- **The real data is not evidence of demand (operator's explanation)**: it is not that there is no
  demand; the operator wants diagrams but has no means, and the text art's quality is too poor to
  use. Once usable, diagrams will go into work instructions. lagent's zero is explained by having
  no path to draw them. The real data is therefore used as samples for choosing the syntax to
  support, not as grounds for demand or coverage (the first draft's "the first three types cover
  84%" is withdrawn).
- **Constructs recounted**: flowchart subgraphs 12, rhombi 12, other shapes 8 (stadium 7,
  parallelogram 3, cylinder 2), `-- text -->` 6, links to a subgraph 3, `direction` inside a
  subgraph 2, nested subgraphs 0. `&` as an operator appears in only 1 block (the first draft's 8
  counted `&` inside labels). Sequence: participant-as 10, actor 9, autonumber 9, activation 2,
  note 1, alt and the like 0, messages to self 3. ER: PK in all 11, FK in 3 (the first draft's "all
  have PK / FK" was wrong), entities without attributes 3. Cycles 2. For 6 of the 11 replies using
  subgraphs, the membership rule changes the result.
- **Two independent verification passes (2026-09-28)**: a check against conventions and recorded
  lessons (16 findings) and a re-verification of facts and measurements found the first draft's
  main gaps: no mention of ADR-0089 A3; text unreadable in the display box (at most 40 columns);
  the subgraph membership rule; drawing-time font failures; a wrong premise about selecting faces
  by name; the change to the TUI rendering function's type; handing over the source before the
  rewrite table; the error kinds; an under-specified layout; tests that did not check
  correctness; a lasting declaration of the x/image exception; factual errors. All are reflected
  in this second draft.
- **Operator decisions (2026-09-28, second draft)**: A3 is revised by making diagram drawing a
  runtime rendering feature rather than a model tool (models barely use tools of this kind).
  lagent is integrated the same way, as a runtime rendering feature.
- **Operator decision (2026-09-28, at approval)**: the second draft is approved. Labels containing emoji are errors and the source is
  shown (none in labels in the real data; about 10% of replies overall contain emoji, so this is
  re-evaluated after integration).
- **Step 0 measurement (2026-09-28)**: four real diagrams, drawn by hand (the investigation flow as
  flowchart TD, the MCP layout as flowchart LR, the attribution sequence, the domain ER), shown
  through a copy of gem-agent's termimg. The measuring tool lives in the scratchpad and is in no
  repository.
  - **Today's box (at most 40 columns, a third of the height)**: judged unreadable on both terminals.
  - **Diagram text size**: 0.85, 1.00 and 1.15 times the terminal's text (relative to em = cell
    height ÷ 1.2) compared; **1.00** chosen on both. The boxes were, on iTerm2 (180×80): flowchart
    TD 72×33, flowchart LR 116×15, sequence 137×34, ER 86×39; on kitty (256×114): 60×33, 96×15,
    113×34, 71×39. Everything fitted the width on both terminals; nothing was shrunk.
  - **Scale**: 1, 2 and 3 compared in the same box; **2** is enough (3 only makes the PNG 1.6–1.9
    times larger).
  - **Cell size**: iTerm2 reports 1440×1440 through `TIOCGWINSZ` (8×18, aspect 2.25, matching
    termimg's assumption) and does not answer CSI 16t. kitty reports 1792×1482 (7×13, aspect
    1.86), and its CSI 16t answer, 13×7, agrees. **The aspect differs by terminal**, so it is read
    from `TIOCGWINSZ` rather than assumed.
  - **Time**: from sending an image to the terminal answering the next query, 17–105ms on iTerm2
    and 15–106ms on kitty (PNGs of 59–267KB, 79–358KB of base64), well under ADR-0089's estimate
    (0.8–1.5 s for 2.4KB).
  - **Noticed in passing (outside this project)**: the existing `termimg.BoxFor` fixes the aspect
    at 2.25. If kitty stretches the image across the declared box, photos on this operator's kitty
    (ratio 1.86) would come out about 17% squashed vertically. Step 0 did not check this (the
    samples were diagrams, not photos, and set A was only judged unreadable), so it is recorded as
    something for gem-agent / lagent to confirm.
- **What step 2 (layout and drawing) found (2026-09-28)**:
  - Layout property tests (everything placed, no overlaps, link ends on outlines, no link through a
    node other than its ends, link ends meeting a node at least 0.8 em apart) ran on 11 synthetic
    cases in four directions, the 22 real flowcharts and seeded random flowcharts. 20,000 random
    seeds found defects 400 did not. Each class was traced to its cause and fixed, and the seeds that
    exposed them were pinned as regression cases.
    - Ranking subgraph members directly let a cycle through subgraphs make one frame straddle
      another (the documentation's own subgraph example did). → Ranks are two-level: inside each
      subgraph, then subgraphs as blocks.
    - When no reordering sweep ran, subgraphs had no left-to-right order and frames overlapped.
    - Links gathering on one face ended as close as 0.15 em, arrowheads indistinguishable; the same
      for self-link ends. → Nodes grow when a face is short; slanted shapes keep ends in the middle
      0.4 of a face.
    - A self-link's label was not counted in its layer's size and overlapped a label next door.
    - A link crossed a frame's title area in the layer just above or below it (one real diagram).
  - **A link crossing an unrelated subgraph's frame is classed as ugly, not wrong (a developer's
    judgement, approved by the operator on 2026-09-28).** The heads still say which nodes a link joins,
    and mermaid itself draws such crossings. Ordering weighs them heavily so avoidable ones are
    avoided; the synthetic and real cases must have none; in the 20,000-seed sweep 16 segments
    remain and are counted.
  - **Limits measured**: rendering takes 1-9 ms on the real diagrams, PNG encoding 4-49 ms. A dense
    diagram of 150 nodes, 300 links and six subgraphs is 3725×1634 at scale 1 (233 ms, PNG 1,856 KB,
    0.30 bytes per pixel). → The cap was first 6 Mpx; the step-2 review found text-heavy diagrams at
    0.67 bytes per pixel, so it went to 3 Mpx. Then straightening links made one real diagram 3.22 Mpx
    and it was refused: guarding the PNG's size with a pixel count refuses ordinary large diagrams that
    are mostly white card. The roles are now split: the engine caps 12 Mpx for time and memory; the
    caller checks the encoded size. A test keeps all 22 real diagrams rendering at the default scale.
  - **Step-2 independent review (2026-09-28)**: 18 of 19 findings taken. Three misreadings (an id
    starting with `direction`, `&` inside an id, an entity code's `;`); links running too close to
    tell apart (shallow diagonals between layers, frame ports beside member columns, loop ends); a
    label spilling out of its shape; arrowheads hidden by titles; unbounded resources (subgraph count,
    label length, `&` products, link length, Scale); test gaps (0.8 em checked at 0.2 em; link spacing
    and labels inside shapes unchecked). Links between layers became orthogonal with a track per link.
    Limits: 300 nodes and subgraphs (at most 100 subgraphs), 500 links (mermaid's own), 1000
    characters per label, link length 10 (as mermaid), Scale 8, 3 Mpx.
  - **Human check (2026-09-28)**: the operator marked the 22 real diagrams on a review page. Round
    1 marked 16 for needless bends: coordinates moved toward the median of an item's neighbours
    instead of the mean, subgraphs shift as blocks, bend points move onto their ports' columns, and
    steps under 0.2 em snap straight: bends 254 to 114, small steps 61 to 4. Round 2 marked four
    kinds: a box off the centre of its link, a rhombus not taking its link at the vertex, fanning
    links that look merged, and a route bending three times where two would do. A node a single link
    fans out from lines up with that link, a node with one link on a face sits centred on it, and
    tracks are 0.8 em apart (was 0.45, now the port spacing). Lining nodes up costs bends elsewhere,
    so the baseline rose to 116 / 5, on purpose and with the reason in the test. The link spacing
    check rose from 0.3 em to 0.6 em. The three marked "could bend without overlapping" came from a
    track order that ignored crossings; tracks now prefer the order in which no across run passes
    another link's vertical (crossings between links sharing an end node 8 to 0, kept by a test).
    The notes asking to centre groups (8 diagrams in round 2) had three causes: a link ending on a
    subgraph aimed at its members instead of the frame's middle; the frame edge hugging the widest
    member pinned it; and a tie between two neighbours went to the one nearer the current position,
    freezing staircases. Links now aim at the frame's middle, members carry the frame's edges, the
    tie goes to the neighbour above, and a last pass moves runs of one-to-one links onto one column.
    The bend baseline fell to 106 / 2.
    Round 3 left 18 of 22 unmarked. Two of the other four (Resubmit, and the link back from the
    error box) had the arrowhead of a link drawn against the layer order on its bend: the track sat
    0.45 em from the gap's start. A head at a gap's start now gets 0.9 em there too, and a straight
    run before every head is a layout property. Cache Hit bent halfway down because a port's column
    was carried into the link even when that saved no step (now only when it does). A box taking
    one link of a fan alone was not met at its middle because nothing moved the link when the box
    could not move (a last pass moves its bend points there; changing the tie rule instead raised
    bends from 106 to 134 and was dropped).
    Round 4 (2026-09-28): all 22 marked ○ on all four items, no notes. This closes the flowchart
    check.
  - **Step 3a: reading ER (2026-09-28)**: the lexer rules of `erDiagram.jison` are ported in their
    order, with jison's behaviour (first matching rule wins, case-insensitive, `\b` appended to a
    rule ending in a word character). Rules derived from the documentation's prose would disagree
    with mermaid on details. Consequences shared with mermaid: `one`, `to`, `many`, `end`, `class`,
    `style` and the like are never names; `direction TD` is not a direction but two entities
    ("direction", "TD": the lexer has no TD); an unquoted two-word label (`: places order`) makes
    its second word an entity; any line containing `direction TB` is a direction statement.
    Entity names and relationship labels are markdown in mermaid, so formatting (emphasis, code,
    headings) is refused as unsupported rather than drawn as marks. The `u` cardinality (in the
    lexer only, not the documentation) and ER subgraphs are unsupported too. Presentation
    (style, classDef, class, `:::`) is checked for form and dropped. Entity codes go through
    mermaid's own placeholders (`ﬂ°…¶ß`), so they fail inside attribute words exactly as in
    mermaid. The 11 real blocks were checked against an independently written reader before
    their goldens were frozen.
  - **Step 3b: laying out and drawing ER (2026-09-28)**: the flowchart layout is reused, with each
    entity passed in as a table's size (a bold name header, then a row per attribute: type, name,
    keys, comment; the last two columns only when some attribute has them, as mermaid's erBox).
    Relationships are lines with a cardinality marker at both ends (next to the entity the
    maximum, a bar or a crow's foot; further out the minimum, a bar or a circle). A marker reaches
    1.32 em along its line, so a link runs straight 1.6 em into each end, layers are 1.6 em apart,
    and ends on one face keep 1.4 em apart (1.2 looked cramped with three side by side). These
    spacings were constants for flowcharts and became per-layout values; flowchart images were
    checked byte for byte to be unchanged. Layout properties add: straight past each marker,
    crow's feet side by side do not touch (from the marker's width), no label over a marker, and
    every table fits its box; 20,000 random ER diagrams keep them.
  - **ER check, round 1 (2026-09-28)**: all 11 marked ○ for entities, relationships and
    cardinality (reading and markers are right). Notes of three kinds: labels too near a bend (6
    diagrams), too many bends, and a link that would run straight if its neighbour left further
    over. A link bending right below its label now keeps 1.2 em between them (was 0.45), and a
    table, being wide, lets its links use 80% of a face (a shape's 60% kept an end from standing
    over the box it goes to). Bends over the 11: 48 to 46 (the worst link 6 to 4; its remaining
    four follow from where the boxes are). Both values are ER only; flowchart images are unchanged.
  - **ER check, round 2 (2026-09-28)**: readings and markers all correct. Notes: labels too low, too
    near a line's end (6 diagrams), and a bend at a label that one fewer would do. A label sat
    midway between two entities, and the lower marker (crow's foot and circle, 1.32 em) is longer
    than the upper (two bars, 0.75 em), so it looked pressed against the lower one: layers are now
    2.2 em apart (was 1.6), and 0.7 em between a label and its own link's markers is a layout
    property. The step at the label: the label was held by the next label and the bend point by the
    next entity, and moving one item at a time could not line them up. For a run that stays stepped,
    the runs in the way are now pushed aside whole, and a try is kept only when the steps along
    links go down (pushing apart only widens gaps, so no constraint breaks). Bends over the 11:
    46 to 40. ER only for now.
    Round 3 (2026-09-28): all 11 marked ○ on all four items, no notes. This closes the ER check.
  - **Known difference from mermaid (recorded only)**: for `A -- go--> B` mermaid takes the `o`
    before the closing symbol as a start mark and reads label "g", length 2; this engine reads label
    "go", length 1. That is closer to what the author meant, so it is not matched.
- **Considered and not taken**: colours matched to the terminal background, and asking the
  terminal for its cell size (both queries leak into the input box). Refusing display for size,
  crossings or small text (aesthetic judgment belongs to people). Text drawing through CoreText
  via cgo (heavier builds, and `x/image` draws well enough). A distributed CLI (signing,
  notarization and versioning cost more than it is worth). A diagram tool for the model (it would
  go unused).
