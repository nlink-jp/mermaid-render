# CLAUDE.md — mermaid-render

Organization rules: https://github.com/nlink-jp/.github/blob/main/CONVENTIONS.md
Read AGENTS.md first. The specification is the RFP in `docs/`.

## Non-negotiable rules

- **No code from outside the organization.** The only dependency is
  `golang.org/x/image` (and what it pulls in from the Go team). Adding any other
  `require` line needs the operator's decision, recorded in the RFP.
- **Wrong is refused; ugly is not.** A construct the engine cannot read, a label
  character no font can draw, or a resource limit exceeded is an error, so the
  caller shows the source. Never add a rule that refuses a diagram for looking
  bad (crossings, size, small text).
- **Never draw a missing character.** Do not use `font.Drawer` (`DrawString`,
  `MeasureString`): it draws tofu and drops `LoadGlyph` failures silently.
- **Syntax comes from the official mermaid documentation**, at the version
  recorded in the RFP. Do not infer a rule from what models happen to write.
- **The subgraph membership rule is mermaid.js's**: a node belongs to the first
  subgraph that mentions it.
- **Deterministic output.** Break every tie by order of appearance in the
  source; never let map iteration order reach the result.
- **Tests with every change**, and a mutation check for every property test: a
  property no test fails without is not guarded.
- **Build with `make build`** (outputs to `dist/`), never `go build` at the root.
- **Docs in sync**: README.md and README.ja.md, and the RFP pair, in the same
  commit as the change they describe.
