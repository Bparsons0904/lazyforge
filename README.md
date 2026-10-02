# lazyforge

A keyboard-driven terminal UI for your git forge: pull requests, issues, Actions, releases and Renovate, with vim keys throughout.

lazygit handles the git side of your work. lazyforge handles the forge side: everything that lives on the server.

> **Status:** design phase. No code yet.

## The idea

- **Navigation in yazi-style columns.** Pick a repo, and its sections move into the left column while the details of the selected item fill the right.
- **lazygit-style numbered boxes.** `[1] Pull requests [2] Issues [3] Actions [4] Renovate [5] Releases`. Jump between them with `1`–`5`.
- **Renovate as a first-class feature.** A cross-repo ★ Renovate view groups pending updates by dependency, so you can merge one update in every repo at once.
- **Forge-agnostic core.** Forgejo/Gitea first, with GitHub and GitLab to follow. You connect to one host per session.

## Docs

- [Design](docs/design.md): UI model, navigation, keymap, Renovate features
- [Architecture](docs/architecture.md): core/adapter split, domain model, host config, per-forge API mapping
- [Interactive mockup](docs/mockup.html): open it in a browser and use vim keys

## Name

A *forge* is a platform that hosts git repositories and adds the collaboration layer that git itself doesn't have: PRs, issues, CI and releases. GitHub, GitLab, Gitea and Forgejo are all forges.
