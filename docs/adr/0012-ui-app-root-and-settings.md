# 0012. UI app root: screens, stale sessions and saves

- Status: accepted
- Date: 2026-10-02
- Decided by: Opus (second opinion: none)

## Context

Ticket #24 adds onboarding, the host picker and Settings, and lets the user switch host without restarting. The session model from ADR 0010 knows one host and no config, and `cmd` used to exit when there was no config or no single host.

## Decision

1. **A new root model, `ui.App`**, owns the screen (`onboarding | picker | settings | session`), the config and the one live session. The session model `ui.Model` is unchanged, and `ui.New(ctx, svc)` still works for `--demo` and the existing tests.
2. **App intercepts `S` (Settings) and `h`/`left` at the repo list (host picker)** before the session, and only when no dialog or help overlay is open. Every other key goes to the session.
3. **Generation stamping drops stale session messages.** Switching host cancels the old session's context and bumps `App.gen`. Every command the session returns is wrapped by `stamp(gen, cmd)`: a `tea.BatchMsg` is re-stamped element-wise, a message carrying the unexported `fromSession()` marker is wrapped in `stampedMsg{gen, msg}`, and anything else (the runtime's exec message, `tea.QuitMsg`) passes through. App delivers a `stampedMsg` only when its generation is current. The session's own repo-key check isn't enough, because two hosts can serve the same `owner/name`.
4. **`ui` never imports an adapter.** cmd injects `Connect func(ctx, config.Host) (forge.Forge, error)` and `Probe func(ctx, url) (forge.Kind, error)`. `ui` may import `config`, a leaf package.
5. **"Only merge when CI is green" reads the live config.** App holds an `atomic.Pointer[config.Config]`, and each session's `core.Options.RequireGreenCI` closure loads it. Every change stores a new config built from `Config.Clone()`, so a config is never mutated once published and the merge goroutine reads it without a lock.
6. **Saves are commands ordered by sequence number.** One shared saver, under a mutex, skips a write whose sequence is older than the last one attempted. Settings changes apply optimistically and show a status error when the save fails. Onboarding's final save is not optimistic: App applies the new host only when that save succeeds, and otherwise onboarding stays on its last step with the error.
7. **The last host is remembered fire-and-forget.** Starting a session from the picker or onboarding writes `last_host` to the state file in a command, keeping the other state fields, and ignores errors because losing state is harmless.

The startup connect for a host chosen by `--host`, the only host or `default_host` stays in cmd, so its errors still exit with the existing messages.

## Consequences

- Session message types must carry the `fromSession()` marker, or a replaced session's results can reach the new one. The markers sit next to the interface in `app.go`.
- Tests point `ConfigPath` and `StatePath` into `t.TempDir()` and exercise the real 0600 write.

## Alternatives

- Teach `ui.Model` about screens and hosts: rejected, it would mix config and connection state into the session model and touch every existing test.
- Key each session message by host as well as repo: rejected, it changes every load function and message for what one wrapper does.
- Debounce saves: rejected, ordering by sequence is enough and every change still lands at once.
