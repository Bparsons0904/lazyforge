# 0014. Markdown rendering

- Status: accepted
- Date: 2026-10-08
- Decided by: Opus (second opinion: none)

## Context
The details pane showed PR and issue bodies as raw markdown. Renovate bodies are the worst case: pipe tables with long link cells, rules, task boxes, and hidden HTML comments such as `<!--renovate-debug:...-->`. The PM also wants links to be clickable.

## Decision
- Parse with `github.com/yuin/goldmark` (CommonMark plus GFM; no dependencies of its own) and render the AST ourselves with Lip Gloss, in the new package `internal/ui/markdown`. It is the only package that imports goldmark and takes every colour from `internal/ui/style`.
- One export, `Render(body string, width int) string`: pure, no I/O. No output line is wider than `width`: layout fits lines where it can, and a final clamp (`lipgloss.MaxWidth`) enforces it for the cases it can't, such as a wide character at width 1.
- Rendering rules: links show their text only, as OSC 8 terminal hyperlinks (each wrapped piece carries the link), and only `http`, `https` and `mailto` destinations become hyperlinks; images render as their alt text as a link (`🖼 alt`); raw HTML is dropped except `<br>` (a line break) and the text of `<summary>` (a heading line); task items show ☐ / ☑; a table that fits is drawn as aligned columns, and one that doesn't is stacked as `Header: value` lines.
- Input is untrusted: control characters (C0 except newline and tab, DEL, C1) are stripped before parsing and after entity decoding, so a forge user can't send escape sequences to the terminal. Backslash escapes and entities are decoded.
- Rendering is synchronous in `syncDetails`, memoized by the details model with one entry (body and width), since only one item is on screen.

## Consequences
Renovate bodies read cleanly at 80x24 and links open from the pane. We own a few hundred lines of rendering. Table cells keep inline styling, so links inside a table stay clickable. Images draw inline in capable terminals; elsewhere, and while loading or on failure, they stay the `🖼 alt` link ([ADR 0016](0016-inline-images.md)).

## Alternatives
- `charm.land/glamour/v2`: about 15 extra modules (chroma, bluemonday and others) for features we don't use; it prints every link's URL after its text with no switch to turn that off, emits its own padding, and cannot drop HTML comments without a pre-pass.
- A hand-rolled line-based renderer: gets nesting and inline markup wrong.
