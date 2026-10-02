# 0004. Config and state as TOML files, no database

- Status: accepted
- Date: 2026-10-01
- Decided by: Opus (second opinion: none). The PM set the format (TOML) and required onboarding and a settings screen.

## Context

lazyforge needs to store host settings and a few remembered values, such as a skipped update version and when it last checked. The PM wants setup done through onboarding and a settings screen in the UI rather than hand-written config, with TOML as the format. Forge data (repos, PRs, runs) always comes fresh from the forge.

## Decision

- Settings live in a single `config.toml` under the XDG config directory. The app manages it: onboarding creates it and the settings screen edits it.
- Remembered values live in a separate `state.toml` under the XDG state directory, so settings and app memory never mix, and deleting state is harmless.
- Writes are atomic: a temp file in the same directory, then a rename.
- Tokens: `token_cmd` is preferred. A pasted `token` is allowed, and when present the file must be mode `0600`; lazyforge refuses to read it if it's group- or world-readable.
- No database. Forge data is cached in memory for the session only.
- TOML library: `github.com/BurntSushi/toml`. It's mature and widely used, and it handles both reading and writing.

## Consequences

- No cgo and no schema migrations, so the single static binary and self-update stay simple.
- Saving from the UI rewrites the file, so comments added by hand are lost. That's acceptable for a file the app manages.
- A pasted token sits on disk, with permissions as the only protection. OS keyring support can be added later without changing the file layout (`token` simply becomes optional).

## Alternatives

- SQLite: too much machinery for a handful of values. It would add either cgo or a heavy pure-Go driver, and nothing needs querying.
- YAML (the earlier sketch): replaced by the PM's choice of TOML.
- `pelletier/go-toml/v2`: faster, but speed doesn't matter for a small file read once at startup. BurntSushi is the more conservative choice.
- Keyring-only tokens: unreliable on headless Linux, so it isn't required in v1.
