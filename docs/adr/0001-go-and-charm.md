# 0001. Go and the Charm stack

- Status: accepted
- Date: 2026-10-01
- Decided by: PM (a product constraint; every other technical decision is Opus's)

## Context

lazyforge is a terminal UI in the lineage of lazygit and yazi. The PM fixed the language and UI framework up front, so engineering decisions are made inside that constraint rather than reopening it.

## Decision

lazyforge is written in Go and uses the Charm stack: Bubble Tea for the program loop, Lip Gloss for layout and styling, and Bubbles for standard components such as lists, viewports, text inputs and key bindings. The project ships as a single static binary.

## Consequences

- Bordered, titled boxes and the two-column layout are built from Lip Gloss primitives.
- Go SDKs exist for Gitea, Forgejo and GitHub, and `tea`/`gh` can supply tokens.
- The UI follows the Elm architecture: state changes only in `Update`, and I/O runs in commands. `go-development` sets the rules for this.

## Alternatives

- Rust + ratatui: just as capable, and yazi's source is a good reference. Not chosen; this was the PM's call.
