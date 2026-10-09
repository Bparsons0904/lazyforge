# 0010. UI shell: Charm v2, concrete core dependency, no teatest

- Status: accepted
- Date: 2026-10-02
- Decided by: Opus (second opinion: none)

## Context

Ticket #8 builds the first real UI: repo list, boxes and details over `core.Service`, runnable on built-in demo data.

## Decision

- Charm v2, pinned to the latest stable: `charm.land/bubbletea/v2` v2.0.10, `charm.land/bubbles/v2` v2.2.1, `charm.land/lipgloss/v2` v2.0.6. The v2 paths are `charm.land/...`. `View()` returns `tea.View`, and keys arrive as `tea.KeyPressMsg`.
- No modules beyond those three. Label truncation is a few lines over `lipgloss.Width`, and tests strip ANSI with a regexp.
- No teatest. `x/exp/teatest/v2` exists only as a pseudo-version, adds a module, and its golden files churn on every styling change. Tests drive `Update` with messages against `core.New(forgetest.NewDemo(now), core.Options{})`, assert state and returned commands, and make plain-string assertions on ANSI-stripped `View` content. A helper runs returned commands with a short timeout, expands `tea.BatchMsg`, and drops commands that never return (the tick).
- `internal/ui` depends on `*core.Service` concretely. There is one implementation, and tests use a real service over the Fake. `ui` may import `forge` and `domain` for types and constants only (`forge.HostInfo`, `forge.ActRuns`), never to call a `Forge` method.
- Loading policy, applying ADR 0007: selecting a repo cancels the previous selection's context, seeds from `Peek…` and fetches only the kinds that missed. Runs are fetched only when `svc.Can(forge.ActRuns, repo).OK`. `r` and a five-minute tick refetch the repo list and every visible kind. Every per-repo message carries its `core.Key`, and a message for a different repo is dropped. `context.Canceled` is dropped silently, and any other error goes to the status bar with the previous data kept. The repo list uses the session context and is never cancelled by navigation.
- Box [1]'s title follows `HostInfo.ChangeRequestTerm` ("Pull requests" / "Merge requests"); no string in `ui` hardcodes either.
- `lazyforge --demo` runs on `forgetest.NewDemo`, ignores `--config` and `--host`, and skips the update check. `cmd/lazyforge` is the only non-test package that imports an adapter or `forgetest`.

## Consequences

- The UI can't be swapped onto a different service implementation without introducing an interface later.
- View regressions are caught by targeted string assertions, not snapshots, so visual drift needs the tmux check.

## Alternatives

- teatest with golden files: rejected, see above.
- A `ui`-side interface over `core.Service`: rejected, one implementation.
- `charmbracelet/x/ansi` for truncation and ANSI stripping: rejected, not worth a module.
