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
| 2. Boxes | The repo's numbered boxes; the focused box grows | Details of the selected item | `l` or `1`–`6` from the repo list |
| 3. Details | Boxes (unfocused) | Details pane, focused, with tabs | `l` from a box |

- `h` always steps back one level. From the repo list, it goes back to the host picker.
- A breadcrumb in the header shows where you are: `host › repo › [1] Pull requests › #42`.
- Action keys (`m`, `a`, `x`, `c`, `R`, `o`) work at both the box and details levels.

**Images.** Image files attached to a PR or issue (not written into its body) show below the body the same way; one the body already shows appears once. An image that stands alone in a top-level paragraph of a PR or issue body draws inline in the Details pane, scrolling and clipping with the text. It must be hosted on the connected forge. It fits the pane width, shrinking a larger image but never enlarging a smaller one, and is capped at the visible height; a resize re-fits it. Kitty and Ghostty draw images, detected automatically; inside tmux the terminal needs `allow-passthrough`. Everywhere else, and while the setting is off, an image stays the `🖼 alt` link, as it does while it loads, after a failed load, when it sits inside text, a list, a quote, a table or an HTML `<img>`, or when it is on another host. Images load only while the Details pane is showing, never from the repo list. They stay held while you're on the host picker or Settings, and are deleted on quit or when you switch host. A force-killed lazyforge leaves them until the terminal clears them.

## Splash

Every launch opens on a splash screen: a hammer striking an anvil with sparks flying, the lazyforge name, and a tagline picked at random. It stays up for five seconds, and any key skips it. The key does nothing else. `ctrl+c` quits.

Behind the splash, lazyforge is already on the screen it would have opened (onboarding on first run, otherwise the repo list or the host picker), and the repo list loads while the splash is up. On first run, onboarding starts at **Welcome** once the splash closes. A terminal too small for the art shows just the name and tagline. Settings can turn the splash off.

## Onboarding

The first launch, with no config yet, starts onboarding instead of the host picker. Nobody has to write a config file by hand.

1. **Welcome:** one screen explaining what lazyforge is.
2. **Forge type:** Forgejo, Gitea, GitHub or GitLab. Types without an adapter yet are listed as "coming soon" and can't be selected.
3. **Address:** the server URL (pre-filled with `github.com` for GitHub). lazyforge checks that the server is reachable and is the chosen forge type.
4. **Sign in:** either paste a token, or give a command that prints one (such as `gh auth token` or a secrets-manager command). The screen links to the page where a token is created (`<url>/user/settings/applications` on Forgejo and Gitea) and lists the permissions it needs:
   - `read:user`: the sign-in check
   - `write:repository`: listing repos, PRs, CI status and runs; merging, approving and closing PRs
   - `write:issue`: issues, comments, closing issues, editing labels and ticking Renovate dashboard entries

   On GitHub the page is `<url>/settings/tokens`, and the token is either:
   - a classic token with `repo`: repos, PRs, CI status, merging, approving and closing PRs, issues, comments, labels and Renovate dashboard ticks
   - a fine-grained token with Contents, Pull requests and Issues read/write, plus Actions, Checks and Commit statuses read
5. **Connection test:** shows "Signed in as *name* · *N* repositories". On failure it says why (bad token, missing permission, server unreachable) and stays on the step until it's fixed.
6. **Renovate:** lazyforge looks for existing Renovate PRs and suggests the bot's username. You can confirm it, edit it, or skip.
7. **Name and save:** a short name for the host (suggested from the URL). Saving goes straight into the repo list.

## Settings

- `S` opens Settings from anywhere except inside a dialog.
- **Hosts:** add (the onboarding flow from step 2), edit, remove, and set the default.
- **Updates:** turn the startup update check on or off.
- **Splash screen:** turn the startup splash on or off.
- **Images:** show images inline in the details pane, on by default.
- **Merging:** "only merge when CI is green" per host, with a per-repo override.
- The host picker has a **+ Add host** entry, which opens the same flow.
- Settings are saved to the config file right away. There's no separate save step.

## Boxes

### Regular repo

| Box | Contents | Details tabs |
|---|---|---|
| `[1]` Pull requests | Open PRs/MRs with CI status. Renovate PRs show `pkg from → to` and a bump label. | Overview · Files · Comments · Checks |
| `[2]` Issues | Open issues; the Dependency Dashboard is marked ⚙ | Overview · Comments |
| `[3]` Actions | Recent workflow runs | Overview · Logs |
| `[4]` Renovate | Dependency Dashboard checkbox entries (open, awaiting schedule, rate-limited) | Overview |
| `[5]` Releases | Releases and tags | Overview |
| `[6]` Repo | The repo itself | README · Files · Branches |

The Branches tab lists the repo's branches, the default branch first and then by most recent commit. Each row shows the name, the tip's subject and age, a `default` marker, and `#N` when an open PR comes from that branch. The selected branch's recent commits show below the list. `j`/`k` move the cursor, `l` or `enter` shows the branch on the Files tab, and `o` opens the branch in the browser. The tab is read-only, with no ahead/behind counts.

The Files tab browses the repo's tree at the default branch, or at the branch picked on the Branches tab until the repo changes or another branch is picked. A pick starts at the repo root. The breadcrumb ends in `<branch>:<path>`, for example `develop:src/ui`, on every branch, the default branch included. The tree is in two columns. The left column is the current directory, directories first and then by name. The right column previews the cursor entry: a directory's names, or a text file's contents. `j`/`k` move the cursor, `l` opens a directory or, on a file, moves into its preview, and `h` goes back out, to the list and then up a directory. `o` opens the cursor entry on the shown branch in the browser. Files over 256 KiB and binary files show a note instead of a preview, and submodules and symlinks show as entries with no preview. The tab is read-only.

The Releases box lists the repo's releases, newest first by publish date; a release with no date sorts last and shows no age. A draft is marked `draft` and a prerelease `pre-release`. Selecting a release shows its name and notes in the details pane, with the notes rendered as markdown. `o` opens the release in the browser. The box is read-only.

### ★ Renovate (virtual, spans the repos you own or belong to)

Its job is to show at a glance which repos have Renovate updates waiting, how many and how much they matter, and then to let you act on them, including in bulk.

| Box | Contents |
|---|---|
| `[1]` By repo | Each repo with open Renovate PRs: counts of major, minor and patch, plus an impact score, sorted worst first. Host totals at the top. `l` shows that repo's Renovate PRs. |
| `[2]` Updates by dependency | Each `package → target version` once, with how many repos have it and their CI status. `m` merges it in every repo. |
| `[3]` Renovate PRs | Every open Renovate PR, prefixed with its repo |
| `[4]` Dashboards | Every Dependency Dashboard issue |
| `[5]` Failing / running CI | Renovate PRs whose CI is failing or still running |

- **Impact score:** each open Renovate PR counts 1 plus log₂(1 + days open). A PR open a day scores 2, a week 4, a month about 6, a year about 9.5. A repo's score is the sum over its PRs. Majors aren't weighted extra, because the major count sits next to the score.
- **Loading:** the view fills in as repos load and shows "scanned X of Y repositories". If any repo failed to load, the view says so, and a bulk merge's confirm dialog warns that coverage is incomplete.

## Keymap

| Key | Action |
|---|---|
| `j` / `k` | Move. In the details pane: scroll, or move the cursor on the Branches or Files tab. |
| `h` / `l` | Back / in. On the Files tab: `l` enters a directory or a file's preview, and `h` leaves the preview, then goes up a directory. On the Branches tab, `l` shows the cursor branch on the Files tab. |
| `1`–`6` | Jump to a box (also works from the repo list) |
| `tab` / `shift-tab` | Next / previous box |
| `[` / `]` | Previous / next details tab |
| `gg` / `G` | Top / bottom |
| `ctrl-d` / `ctrl-u` | Half-page scroll in the details pane, or half-page move on the Branches or Files tab |
| `space` | Mark a PR for bulk actions, or tick a Renovate dashboard entry |
| `m` | Merge: the marked PRs, the current PR, or every PR in an update group (`[2]` of ★ Renovate) |
| `a` | Approve |
| `x` | Close a PR or issue |
| `c` | Comment (opens `$EDITOR`) |
| `L` | Edit labels on the selected PR or issue (requires repository write access) |
| `R` | Re-run a workflow: opens the run's page in the browser until the forge offers a re-run API |
| `o` | Open in browser: the selected item, or the repo's page on the repo list, the Repo box and ★ By repo rows |
| `r` | Refresh |
| `S` | Settings |
| `?` | Help |
| `esc` | Close a dialog / clear marks |

The label picker lists colored labels with the current selection checked. Type to filter, use arrows to move and Space to toggle, then Enter to save the complete selection. Esc cancels without writing. Failed saves keep the selection available to retry.

Every merge and close goes through a confirm dialog. The dialog lists the targets (for a Renovate PR, every package it carries), the merge strategy (always the repo's default), and a warning for any target with failing or running CI.

- **Branch protection is the forge's job.** A repo that requires passing checks refuses the merge whatever lazyforge does, and lazyforge shows the forge's reason.
- **"Only merge when CI is green"** is a setting per host, with a per-repo override. When it's on, failing or running CI turns the warning into a refusal. It's off by default.
- **Bulk merges carry on past a failure.** The dialog then shows a result per PR: merged, refused (for example "changed since you confirmed", or conflicts) or failed. PRs that didn't merge stay marked, so `m` retries just those after lazyforge re-checks them. A PR that changed after you confirmed is never merged unseen.

## Renovate features

- **Detecting Renovate PRs:** by author, using the `renovate_user` setting for each host. The `renovate/` branch prefix is a fallback.
- **Package, versions and bump type:** parsed from the table in the Renovate PR body. If parsing fails, the PR still shows with its plain title.
- **Grouping by dependency:** the key is ecosystem + package + target version, so `actions/checkout` v3 → v7 and v4 → v7 share a row (the row shows the starting versions). PRs whose body can't be parsed stay out of groups but still appear in `[3]`.
- **Batched PRs:** a PR that carries several packages (such as "Go non-major") gets its own row in Updates by dependency and always merges whole. Merging one package never merges others as a side effect.
- **Ticking dashboard entries:** done by editing the Dependency Dashboard issue body, changing `- [ ]` to `- [x]`. Renovate acts on the change during its next run.

## Install and updates

- **Install:** one command, `curl -fsSL https://git.bobparsons.dev/deadstyle/lazyforge/raw/branch/main/install.sh | bash`. It picks the right binary for the OS and architecture, verifies its checksum, and installs to `~/.local/bin`. Running it again upgrades in place. Windows has the PowerShell equivalent, `install.ps1`, which installs to `%LOCALAPPDATA%\Programs\lazyforge` from zip archives.
- **Updates:** at every startup, before the UI opens, lazyforge checks for a newer release. If there is one, it asks whether to update first. Yes downloads, verifies and replaces the binary, then starts the new version. No starts the current version, and the next launch asks again.
- The check never delays startup by more than about two seconds and never blocks when offline. It can be turned off with a flag, an environment variable or config. When lazyforge was installed by a package manager, it says an update is available instead of replacing itself.

## Decided

Answered by the PM on 2026-10-02 (#13):

- `j` at the bottom of a box stops there, like lazygit.
- The focused box grows, as in the mockup.
- The boxes stay `[1]`–`[6]` as listed. Others may come later.
- Refresh: `r`, plus a background refresh every five minutes.
- Merge strategy: always the repo's default from the forge.
- ★ Renovate scans the repos you own or belong to, including your orgs.
- Updates: checked on every launch; declining asks again next launch.

## Open questions

- [ ] Should per-repo `[4]` Renovate list the dashboard entries, as mocked, or Renovate PRs split out of `[1]`? (After v1.)

## v1 scope

- Onboarding, Settings, host picker, repo list, boxes `[1]`–`[3]`, the Releases box `[5]` (Overview tab) and the Repo box `[6]` with its README, Files and Branches tabs, details with the Overview tab
- ★ Renovate view, all five boxes
- Merge, approve, close, comment (`$EDITOR`), open in browser, refresh, `R` (opens the run page)
- Bulk merge (marked PRs and update groups), with the CI warning and the "only merge when green" setting
- Forgejo/Gitea and GitHub adapters (the GitHub adapter ships with the public release, the PM's call in #83)
- Install script, release binaries, and the startup update check

After v1: the Files tab with diffs (optionally rendered through `delta`), the Logs tab, box `[4]`, the Repo box's Files tab, then the GitLab adapter.
