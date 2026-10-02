# Design

This document covers the UI and interaction model. [architecture.md](architecture.md) covers how the app is built, and [mockup.html](mockup.html) is a clickable version of everything below.

## Goals

- Triage and act on forge work (PRs, issues, CI, releases) without leaving the terminal.
- Make Renovate upkeep fast, especially the same update landing in many repos.
- Feel familiar to lazygit and yazi users: vim keys, numbered panels, column navigation.
- Work with any forge, but only one at a time.

## Non-goals (for now)

- Line-level code review (inline comments on diffs).
- Connecting to multiple hosts in one session.
- Replacing the web UI for admin or settings tasks. `o` opens the browser for anything lazyforge doesn't cover.

## Navigation model

Two columns are visible at any time. The left column is where you are, and the right column shows what you'd step into.

| Level | Left column | Right column | Enter with |
|---|---|---|---|
| 0. Host picker | Configured hosts | — | Skipped when a single host is configured or `--host` is passed |
| 1. Repos | Repo list: ★ Renovate pinned first, then by most recent activity | Preview of the selected repo's boxes | `l` from the host picker |
| 2. Boxes | The repo's numbered boxes; the focused box grows | Details of the selected item | `l` or `1`–`5` from the repo list |
| 3. Details | Boxes (unfocused) | Details pane, focused, with tabs | `l` from a box |

- `h` always steps back one level. From the repo list, it goes back to the host picker.
- A breadcrumb in the header shows where you are: `host › repo › [1] Pull requests › #42`.
- Action keys (`m`, `a`, `x`, `c`, `R`, `o`) work at both the box and details levels.

## Boxes

### Regular repo

| Box | Contents | Details tabs |
|---|---|---|
| `[1]` Pull requests | Open PRs/MRs with CI status. Renovate PRs show `pkg from → to` and a bump label. | Overview · Files · Comments · Checks |
| `[2]` Issues | Open issues; the Dependency Dashboard is marked ⚙ | Overview · Comments |
| `[3]` Actions | Recent workflow runs | Overview · Logs |
| `[4]` Renovate | Dependency Dashboard checkbox entries (open, awaiting schedule, rate-limited) | Overview |
| `[5]` Releases | Releases and tags | Overview |

### ★ Renovate (virtual, spans every repo on the host)

| Box | Contents |
|---|---|
| `[1]` Renovate PRs | Every open Renovate PR, prefixed with its repo |
| `[2]` Updates by dependency | Each `pkg from → to` once, with a count of repos and their CI status. `m` merges that update everywhere. |
| `[3]` Dashboards | Every Dependency Dashboard issue |
| `[4]` Failing / running CI | Runs that need attention |

## Keymap

| Key | Action |
|---|---|
| `j` / `k` | Move. In the details pane: scroll. |
| `h` / `l` | Back / in |
| `1`–`5` | Jump to a box (also works from the repo list) |
| `tab` / `shift-tab` | Next / previous box |
| `[` / `]` | Previous / next details tab |
| `gg` / `G` | Top / bottom |
| `ctrl-d` / `ctrl-u` | Half-page scroll in the details pane |
| `space` | Mark a PR for bulk actions, or tick a Renovate dashboard entry |
| `m` | Merge: the marked PRs, the current PR, or every PR in an update group |
| `a` | Approve |
| `x` | Close a PR or issue |
| `c` | Comment (opens `$EDITOR`) |
| `R` | Re-run a workflow |
| `o` | Open in browser |
| `r` | Refresh |
| `?` | Help |
| `esc` | Close a dialog / clear marks |

Every merge and close goes through a confirm dialog. The dialog lists the targets, the merge strategy, and a warning for any target with failing or pending CI.

## Renovate features

- **Detecting Renovate PRs:** by author, using the `renovate_user` setting for each host. The `renovate/` branch prefix is a fallback.
- **Package, versions and bump type:** parsed from the table in the Renovate PR body. If parsing fails, the PR still shows with its plain title.
- **Grouping by dependency:** the key is `pkg + from + to`, collected across every repo on the host.
- **Ticking dashboard entries:** done by editing the Dependency Dashboard issue body, changing `- [ ]` to `- [x]`. Renovate acts on the change during its next run.

## Open questions

- [ ] When `j` reaches the bottom of a box, should it stop (like lazygit) or carry on into the next box?
- [ ] Should the focused box grow, as in the mockup, or should box sizes stay fixed?
- [ ] Is this the right set of boxes? Candidates to add: Branches, Milestones, Packages.
- [ ] Should per-repo `[4]` Renovate list the dashboard entries, as mocked, or Renovate PRs split out of `[1]`?
- [ ] Refresh: manual `r` only, interval polling, or both?
- [ ] Merge strategy: the repo's default from the forge, or a config override?

## v1 scope

- Host picker, repo list, boxes `[1]`–`[3]`, details with the Overview tab
- ★ Renovate view, including `[2]` Updates by dependency
- Merge, approve, close, comment (`$EDITOR`), open in browser, refresh
- Bulk merge, with a CI warning
- Forgejo/Gitea adapter only

After v1: the Files tab with diffs (optionally rendered through `delta`), the Logs tab, boxes `[4]` and `[5]`, then the GitHub adapter, then GitLab.
