---
name: go-development
description: Use when writing or reviewing any Go code in lazyforge (cmd/, internal/, go.mod) — packages, the Forge interface and adapters, core logic, Bubble Tea models, Lip Gloss styles, tests, or lint config. Mandatory before editing Go files; a hook enforces it. Covers layering, the Bubble Tea rules, error handling, testing, dependencies and golangci-lint.
---

# Go Development

These are the project's engineering standards. Opus owns them. To change one, use the `engineering-decision` skill. Don't edit this file in passing.

## Toolchain

- Go version: whatever `go.mod` declares. Module path: `git.bobparsons.dev/deadstyle/lazyforge`.
- `make check` before anything is called done. It runs, in order: format check, `golangci-lint run`, `go test -race ./...`, and the build.
- Formatting is `gofumpt` + `goimports` with the local prefix, run through `golangci-lint fmt`. Never hand-format.

## Layering

```
cmd/lazyforge  →  internal/ui  →  internal/core  →  internal/forge  →  internal/domain
                         (cmd/ also wires internal/forge/<adapter> and internal/config)
```

- **`internal/domain`** holds plain types and pure helpers. It imports nothing from `internal/`, and contains no I/O.
- **`internal/forge`** holds the `Forge` interface, the optional capability interfaces, `HostInfo` and shared errors such as `ErrUnsupported`. It contains interfaces and small types, never implementations.
- **`internal/forge/<name>`** is one adapter per forge family (`gitea` serves both Gitea and Forgejo). An adapter imports `domain` and `forge` only. It converts every API type at its boundary, so SDK or JSON types never escape it. Pagination, rate limits and API quirks stay inside the adapter.
- **`internal/core`** is everything forge-agnostic: the cache, refresh, and Renovate logic (detection, PR-body parsing, grouping by dependency, ticking dashboard entries). It talks only to the `forge.Forge` interface.
- **`internal/ui`** contains Bubble Tea models. It calls `core`, never `forge` directly and never an adapter.
- **`cmd/lazyforge`** is the only place that knows the concrete adapters exist.

An import that breaks this direction is a bug, even if it compiles.

## Domain language

- In code, call it `ChangeRequest`. The UI shows "PR" or "MR" using `HostInfo` terminology, so no string in `ui` hardcodes either word.
- Optional features are optional interfaces (`forge.Approver`, `forge.LogReader`, …), checked with a type assertion. The UI hides a box, tab or key when the capability is missing. Never stub a capability with a method that returns `ErrUnsupported` just to satisfy the interface.

## Bubble Tea rules

- `Update` must be fast and never block. All I/O (forge calls, `token_cmd`, opening `$EDITOR` or the browser) happens in a `tea.Cmd`, and its result comes back as a typed message.
- Only `Update` mutates model state. Goroutines and commands never touch the model. They return messages.
- Message types are named for what happened (`changeRequestsLoadedMsg`), carry the request key, and carry an `err` field rather than having a separate error type for each call.
- Stale results: every load carries a key (host, repo, box, item). A message whose key no longer matches the current selection is dropped. This is what makes fast `j`/`k` navigation safe.
- Each view is a sub-model with its own `Update` and `View`. The parent routes messages, and children don't reach into siblings.
- Key bindings are `bubbles/key` bindings, kept in a single keymap so the help overlay and the status-bar hints are generated from them. Never compare raw key strings inside view code.
- Styles live in one styles package (`internal/ui/style`). Views never construct colors inline.
- Layout is computed from `tea.WindowSizeMsg`. Nothing hardcodes a width or height. Every view must render correctly at 80×24.

## Errors

- Wrap errors with context: `fmt.Errorf("list change requests for %s: %w", repo, err)`. Check them with `errors.Is` or `errors.As`, never by comparing strings.
- Adapters translate HTTP failures into a small set of `forge` sentinel errors (`ErrNotFound`, `ErrUnauthorized`, `ErrRateLimited`, `ErrUnsupported`) so the UI can respond to them meaningfully.
- No `panic` outside `main` startup. The UI reports errors in the status bar or a dialog. They never crash the program and never print to stdout.

## I/O, context and secrets

- Every forge call takes a `context.Context`. Navigating away cancels requests that are still in flight.
- Use one `*http.Client` per session, with explicit timeouts. Never use `http.DefaultClient`.
- Logging uses `log/slog` to a file under the user's state directory, because the TUI owns stdout and stderr. Never `fmt.Print` from UI code.
- Tokens come from `token_cmd` or the config file. They are never logged, never included in error strings, and never written to the cache.

## Testing

- `go test -race ./...`. Prefer table-driven tests. Test files live next to the code (`foo_test.go`), and fixtures go in `testdata/`.
- **domain and core:** plain unit tests against a fake `forge.Forge`. A fake lives in a `forgetest` package, never in production code.
- **Adapters:** an `httptest.Server` serving recorded API responses from `testdata/`. A shared **contract suite** (`internal/forge/forgetest`) runs the same behavioral tests against every adapter.
- **UI:** test `Update` by sending messages and asserting the resulting state and commands. Golden-file tests of `View` output are welcome, but adopting a library for them is an `engineering-decision`.
- Never add production code that exists only so a test can reach in. Restructure the code or test through the public seam instead.

## Dependencies

- Adding a module is an `engineering-decision`, and so is replacing one. Bumping one is not, but read the changelog for every major or minor bump before treating it as routine.
- The Charm stack is pre-approved: `bubbletea`, `bubbles`, `lipgloss`. So are the stdlib and `golang.org/x/*`. Anything else goes through `engineering-decision`.

## Lint

- The config is `.golangci.yml`. Fix findings rather than silencing them.
- Adding a `//nolint` requires an `engineering-decision`, and the directive must name the linter and give the reason (`//nolint:gosec // reason`). `nolintlint` enforces that format.

## Comments

The `comment-standard` skill covers comments. Exported identifiers get Go doc comments. Other comments say why, not what.
