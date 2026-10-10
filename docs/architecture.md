# Architecture

## Principles

- **One core app, with a translation layer for each forge.** The UI and the Renovate logic are written once. Each forge has an adapter that maps its API onto lazyforge's own domain model.
- **One host per session.** You choose a host at startup, either from the host picker or with `--host`, and that single adapter serves the whole session. Capabilities and terminology are known once and stay fixed for the session.
- **Capabilities, not the lowest common denominator.** Each adapter reports what it supports, and the UI hides the boxes, tabs and keys that aren't supported. It doesn't limit every forge to what all of them can do.
- **The domain model belongs to lazyforge.** No SDK type crosses the adapter boundary.

## Layers

```
┌──────────────────────────────────────────────┐
│ UI (Bubble Tea + Lip Gloss)                  │  host picker · repos · boxes · details
├──────────────────────────────────────────────┤
│ Core (+ core/renovate)                       │  domain model · cache · Renovate logic
│                                              │  (★ view, grouping, bulk merge, dashboard ticks)
├──────────────────────────────────────────────┤
│ Forge interface + capabilities               │
├──────────────┬──────────────┬────────────────┤
│ gitea        │ github       │ gitlab         │  adapters: API ⇄ domain model
│ (+ forgejo)  │              │                │
└──────────────┴──────────────┴────────────────┘
```

Forgejo is a fork of Gitea and their APIs are still largely the same. One adapter covers both. It detects which one it is talking to once at connect (a `+gitea-` version suffix means Forgejo) rather than gating on Gitea semver (ADR 0005).

## Stack

**Go, Bubble Tea and Lip Gloss** ([ADR 0001](adr/0001-go-and-charm.md)):

- Lip Gloss makes the bordered, titled boxes easy to build.
- Existing CLIs (`tea`, `gh`) can supply auth tokens. Adapters use thin hand-written HTTP clients rather than SDKs ([ADR 0005](adr/0005-thin-forgejo-client.md), [ADR 0017](adr/0017-github-adapter.md)).
- It builds to a single static binary.

## Domain model

The types live in `internal/domain`; ADR 0006 records the decisions behind them.

```go
type RepoRef struct{ Owner, Name string } // Owner may contain slashes (GitLab subgroups)
type Repo struct {
    RepoRef
    Description, WebURL string
    LastActivity        time.Time
    Access              Access // none, read, write, admin
    MergeStyle          string // the repo's default merge style; "" when unknown
}

type ChangeRequest struct { // PR on Gitea/Forgejo/GitHub, MR on GitLab
    Number                     int
    Title, Body, Author        string
    State                      State // open, merged, closed
    SourceBranch, TargetBranch string
    HeadSHA                    string
    CI                         CIState // none, pending, running, pass, fail, cancelled, skipped
    Labels                     []string
    UpdatedAt, CreatedAt       time.Time
    WebURL                     string
    Renovate                   []RenovateUpdate // nil from adapters; core fills it
}

type RenovateUpdate struct { // one row of the PR's update table
    Ecosystem, Package, DepType, UpdateType, From, To, SourceURL string
}

type Issue struct { /* Number, Title, Body, Author, State, Labels, Comments, UpdatedAt, WebURL */ }
type Comment struct { /* ID, Author, Body, CreatedAt */ }
type Run struct { /* ID, Number, Workflow, Title, Branch, Commit, Event, Status, StartedAt, Duration, WebURL */ }
type Job struct { /* ID, RunID, Name, Stage (GitLab only), Status, Attempt */ }
type Release struct { /* Tag, Name, Notes, Draft, Prerelease, PublishedAt, WebURL */ }
type Readme struct { /* Name, Body; Name is "" when the repo has no README */ }
type Commit struct { /* SHA, Message, Author, Date */ }
type Branch struct { /* Name, Default, Commit (the tip), WebURL */ }
type TreeEntry struct { /* Name, Path, Type (file, dir, symlink, submodule), Size, WebURL */ }
type FilePreview struct { /* Text, Binary, TooLarge */ }
```

## Forge interface

A core interface that every adapter implements, plus optional interfaces for features that not every forge supports. Capabilities come from type assertions, so a capability can't be claimed without being implemented. See ADR 0006.

```go
type Forge interface {
    Info() HostInfo // kind, URL, version, user, "PR" vs "MR"

    ListRepos(ctx context.Context) ([]domain.Repo, error)

    ListChangeRequests(ctx context.Context, r domain.RepoRef, f Filter) ([]domain.ChangeRequest, error)
    GetChangeRequest(ctx context.Context, r domain.RepoRef, n int) (domain.ChangeRequest, error)
    Merge(ctx context.Context, r domain.RepoRef, n int, opts MergeOpts) error // opts.HeadSHA required; ErrHeadChanged on mismatch, ErrRefused when the forge declines

    ListIssues(ctx context.Context, r domain.RepoRef, f Filter) ([]domain.Issue, error)
    EditIssueBody(ctx context.Context, r domain.RepoRef, n int, body string) error // Renovate dashboard ticks
    ListComments(ctx context.Context, item ItemRef) ([]domain.Comment, error)
    Comment(ctx context.Context, item ItemRef, body string) error
    Close(ctx context.Context, item ItemRef) error // ItemRef says issue or change request

    ListReleases(ctx context.Context, r domain.RepoRef) ([]domain.Release, error)

    Gate(a Action) error // server-version gates learned at connect; no I/O
}

// Optional: the UI checks for these with type assertions.
type Approver  interface { Approve(ctx context.Context, r domain.RepoRef, n int) error }
type RunLister interface {
    ListRuns(ctx context.Context, r domain.RepoRef, f RunFilter) ([]domain.Run, error)
    ListJobs(ctx context.Context, r domain.RepoRef, runID int64) ([]domain.Job, error)
}
type LogReader interface { JobLog(ctx context.Context, r domain.RepoRef, jobID int64) (io.ReadCloser, error) }
type ReadmeReader interface { GetReadme(ctx context.Context, r domain.RepoRef) (domain.Readme, error) } // ErrNotFound when there is no README
type BranchReader interface {
    ListBranches(ctx context.Context, r domain.RepoRef) ([]domain.Branch, error) // GitHub: at most 100
    ListCommits(ctx context.Context, r domain.RepoRef, branch string) ([]domain.Commit, error) // ErrNotFound when the branch is gone
}
type TreeReader interface {
    ListTree(ctx context.Context, r domain.RepoRef, ref, dir string) ([]domain.TreeEntry, error) // ref "" is the default branch, dir "" is the root; ErrNotFound when ref or dir is missing or the repo is empty
    ReadFile(ctx context.Context, r domain.RepoRef, ref, path string) ([]byte, error) // ref "" is the default branch; ErrNotFound when ref or the file is missing
}
type Labeler interface {
    ListLabels(ctx context.Context, r domain.RepoRef) ([]domain.Label, error)
    ItemLabels(ctx context.Context, item ItemRef) ([]domain.Label, error)
    SetLabels(ctx context.Context, item ItemRef, ids []int64) ([]domain.Label, error)
}
type AssetReader interface { OpenAsset(ctx context.Context, u *url.URL) (io.ReadCloser, error) }
type BranchUpdater interface { UpdateStyles() []UpdateStyle; UpdateBranch(ctx context.Context, r domain.RepoRef, n int, style UpdateStyle) error } // UpdateStyles: merge first; ErrUnsupported for a style it omits
```

`forge.Can(f, action, repo)` answers whether the UI should enable an action. It checks the capability interface, then `f.Gate`, then `repo.Access`, and the first failure supplies the user-facing `Reason`. `forgetest.Fake` and `forgetest.RunContract` give core and every adapter a shared fake and behavior suite.

## API mapping (first pass)

The Gitea / Forgejo column is verified against Forgejo `16.0.5+gitea-1.22.0` (#1, #3, #6). The GitHub rows for repos, change requests, merge, approve, PR CI state, issues, edit issue, comment and releases are verified against github.com (#84, [ADR 0017](adr/0017-github-adapter.md)), and its runs, jobs, job log and labels rows too (#85); its re-run row is unverified (no `Rerunner`, ADR 0006) and its changed-files row is unused. The README row is unverified on every forge: it was written from the API docs for #104 and has not yet run against a live server. The branch rows (Branches, Branch commits) are unverified on every forge too: they were written from the API docs for #105 and have not yet run against a live server. The file rows (Files (list), File contents) are unverified on every forge as well: they were written from the API docs for #106 and have not yet run against a live server. The Update branch row is unverified on every forge: it was written from the API docs and the Forgejo swagger for #118 and has not yet run against a live server. The GitLab column is unverified, and each row needs checking against current API docs before that adapter is built.

| Operation | Gitea / Forgejo | GitHub | GitLab |
|---|---|---|---|
| List repos by activity | `GET /user/repos` (owned, collaborator and team repos; sorted by `updated_at` client-side) | `GET /user/repos?affiliation=owner,collaborator,organization_member&sort=pushed` (sorted by `pushed_at` client-side; `allow_*` merge flags are null here) | `GET /projects?membership=true&order_by=last_activity_at` |
| List change requests | `GET /repos/{o}/{r}/pulls?state=open` | `GET /repos/{o}/{r}/pulls?state=open` (closed with `merged_at` set is merged) | `GET /projects/:id/merge_requests?state=opened` |
| Merge | `POST /repos/{o}/{r}/pulls/{n}/merge` with `Do` (sent explicitly from the repo's `default_merge_style`, because an empty `Do` means `merge`) and `head_commit_id`; a stale head is 409 `head out of date` | `PUT /repos/{o}/{r}/pulls/{n}/merge` with `sha` and `merge_method` (first allowed of merge, squash, rebase from `GET /repos/{o}/{r}`); a stale head is 409 | `PUT /projects/:id/merge_requests/:iid/merge` |
| Update branch | `POST /repos/{o}/{r}/pulls/{n}/update?style=merge\|rebase`; a conflict is 409, no permission 403 | `PUT /repos/{o}/{r}/pulls/{n}/update-branch` (merge only; 202, finishes asynchronously); a conflict or nothing to update is 422 | `PUT /projects/:id/merge_requests/:iid/rebase` (rebase only) |
| Approve | `POST …/pulls/{n}/reviews` (`event: APPROVED`) | `POST …/pulls/{n}/reviews` (`event: APPROVE`) | `POST /projects/:id/merge_requests/:iid/approve` |
| Changed files | `GET …/pulls/{n}/files` | `GET …/pulls/{n}/files` | `GET /projects/:id/merge_requests/:iid/diffs` |
| PR CI state | `GET …/commits/{ref}/status` | `GET …/commits/{sha}/check-runs` (paginated) + `GET …/commits/{sha}/status`, folded; a status `total_count: 0` counts as none | MR head pipeline |
| Issues | `GET …/issues?type=issues` | `GET …/issues` (drop items with a `pull_request` key) | `GET /projects/:id/issues` |
| Edit issue body | `PATCH …/issues/{n}` | `PATCH …/issues/{n}` | `PUT /projects/:id/issues/:iid` |
| Comment | `POST …/issues/{n}/comments` | `POST …/issues/{n}/comments` | `POST …/notes` |
| Labels | `GET …/labels`; `GET` and `PUT …/issues/{n}/labels` with label IDs | `GET …/labels`; `GET` and `PUT …/issues/{n}/labels` with `{labels: [names]}`, IDs mapped to names through `GET …/labels` | `GET /projects/:id/labels`; `PUT /projects/:id/issues/:iid` with `labels` |
| List runs | `GET …/actions/runs` (send `page`, else `limit` is ignored; `ref` needs the full `refs/heads/<branch>` form; total in the body's `total_count`) | `GET …/actions/runs` (`branch` takes the bare name; `{total_count, workflow_runs}` body; newest 100 only, `Link` not followed (ADR 0017); status + conclusion folded as check runs) | `GET /projects/:id/pipelines` |
| Run jobs | `GET …/actions/runs/{id}/jobs` (bare array) | `GET …/actions/runs/{id}/jobs` (`{total_count, jobs}` body, `Link` paging) | `GET /projects/:id/pipelines/:id/jobs` |
| Job log | `GET …/actions/jobs/{id}/logs` (job `id`, not `task_id`) | `GET …/actions/jobs/{id}/logs` (302 to a signed blob URL on another host; streamed, outside the ETag cache) | `GET /projects/:id/jobs/:id/trace` |
| Re-run | none on Forgejo 16: no `Rerunner` | `POST …/actions/runs/{id}/rerun` | `POST /projects/:id/pipelines/:id/retry` |
| Releases | `GET …/releases` (core sorts newest first; `created_at` stands in for a missing `published_at`, unverified) | `GET …/releases` (drafts only for a token that can push; `created_at` stands in for a null `published_at`, unverified) | `GET /projects/:id/releases` |
| README | `GET …/contents` (root listing; the best name wins: `README.md`, then `README.markdown`, then a bare `README`, then other `README.*` files), then `GET …/contents/{name}` (base64 `content`) | `GET …/readme` (base64 `content`) | `GET /projects/:id/repository/tree` to find the name, then `GET /projects/:id/repository/files/:path?ref=` |
| Branches | `GET …/branches` (paginated; the default is flagged from `GET /repos/{o}/{r}`) | `GET …/branches?per_page=100` (first page only; the default is fetched by `GET …/branches/{name}`, and each other branch's tip commit by `GET …/commits/{sha}`, 8 at a time) | `GET /projects/:id/repository/branches` |
| Branch commits | `GET …/commits?sha={branch}&limit=30` | `GET …/commits?sha={branch}&per_page=30` | `GET /projects/:id/repository/commits?ref_name={branch}` |
| Files (list) | `GET …/contents/{dir}` (the root without `{dir}`; default branch; 409 means no commits and maps to an empty listing) | `GET …/contents/{dir}` (default branch; at most 1,000 entries) | `GET /projects/:id/repository/tree?path={dir}` |
| File contents | `GET …/contents/{path}` (base64 `content`; any other `encoding` is an error) | `GET …/contents/{path}` (base64 `content`; `download_url` null means a submodule, not a file) | `GET /projects/:id/repository/files/:path?ref=` (base64 `content`) |

How the forges differ in practice:

- **Pagination:** GitHub uses `Link` headers, Gitea uses `X-Total-Count`, and GitLab offers keyset pagination. Each adapter hides this.
- **Rate limits:** GitHub allows about 5,000 requests per hour, so the core cache is required there, not optional. The github adapter also revalidates GETs with ETags, so an unchanged refresh costs no budget, and maps a rate-limit 403 to `ErrRateLimited` ([ADR 0017](adr/0017-github-adapter.md)).
- **CI shape:** GitHub and Forgejo use runs → jobs → steps. GitLab uses pipelines → stages → jobs, which maps to `Job.Stage`.
- **README lookup:** Forgejo has no README endpoint, so its adapter reads the repo root only. GitHub's `/readme` also finds a README in `docs/` and `.github/`, so on Forgejo a README kept only there shows as "No README" (#104).
- **Branch lists:** GitHub's branch list carries only SHAs, so its adapter makes one commit call per branch: at most 100 branches, the default always kept, and the GETs ETag-revalidated like the rest. Repos with more branches show the first 100. Gitea's list already carries each tip's message and time. The open-PR marker on the Branches tab is derived from the loaded change requests' `SourceBranch`, with no extra call, so a fork PR whose head branch has the same name as one of this repo's branches can false-match it.
- **Branch dates:** Gitea's commit list dates come from `commit.author.date`, its branch tips from the branch's `commit.timestamp`, and GitHub's from the committer date. Which time the Gitea tip timestamp carries is unverified, so the Branches tab can mix the two kinds.
- **File browsing:** the Files tab reads a directory's listing, then a file's contents only when it is previewed. A file whose listing `Size` is over 256 KiB (`core.MaxPreviewSize`) is never downloaded and shows as too large. A file is binary when it has a NUL byte or isn't valid UTF-8, and previews are plain text only. GitHub's contents API returns at most 1,000 entries per directory, and the adapter doesn't page past that. A GitHub submodule is a file entry whose `download_url` is null, so it maps to `EntrySubmodule` and is never read. Each cursor landing issues its own request, and only the directory or file the user has moved to is cancelled when the cursor moves on. Holding `j` therefore queues one load per row behind the core semaphore, and the stale ones finish and cache without being shown (#106).
- **Reviews:** the domain model stays deliberately small: approve, comment, merge, close.

## Configuration and stored state

See ADR 0004. There is no database.

```toml
# ~/.config/lazyforge/config.toml (written by onboarding and the settings screen)
default_host = "homelab"

[update]
check = true

[splash]
show = true

[hosts.homelab]
type = "forgejo"
url = "https://git.bobparsons.dev"
token_cmd = "infisical secrets get FORGEJO_TOKEN --plain"
renovate_user = "renovate-bot"
renovate = true # pin the ★ Renovate row; default is on when renovate_user is set
require_green_ci = true

[hosts.homelab.repos."deadstyle/lazyforge"]
require_green_ci = false # per-repo override of the host setting

[hosts.github]
type = "github"
token_cmd = "gh auth token"
renovate_user = "renovate[bot]"
```

- **The app manages the config.** Onboarding creates it and the settings screen edits it. Hand edits are allowed and picked up on the next start, but a save from the UI rewrites the file, so comments don't survive.
- **Tokens:** a host has either `token_cmd` (preferred; it keeps secrets out of the file) or `token`, which is a pasted token stored in the file. When `token` is present, the file is written with mode `0600` and lazyforge refuses to read it if it's group- or world-readable. OS keyring support may come later.
- **Writes are atomic:** write a temp file in the same directory, then rename it. A crash never leaves a half-written config.
- **State** (`~/.local/state/lazyforge/state.toml`): things the app remembers rather than settings, such as the last update check, a skipped version, and the last-used host. Losing this file is harmless.
- **Logs:** `~/.local/state/lazyforge/lazyforge.log`.
- Paths follow the XDG variables (`$XDG_CONFIG_HOME`, `$XDG_STATE_HOME`) on Linux, and the platform equivalents on macOS.
- The host picker is skipped when only one host is configured or `--host` is passed.

## Caching and refresh

See [ADR 0007](adr/0007-core-cache-and-concurrency.md).

- `core.Service` wraps the session's one `Forge` and holds an in-memory cache keyed by `core.Key{Kind, Repo, Number}`. Entries never expire on their own, except for the image cache below.
- Images have a separate 16-entry LRU of decoded images and failures, cleared by `r` and not by the five-minute timer. This is the one exception to "entries never expire" ([ADR 0016](adr/0016-inline-images.md)).
- Each read has a `Peek…` form (cached value and fetch time, no I/O) and a fetching form that calls the forge and stores the result. A failed fetch keeps the old entry.
- A semaphore (4 by default) bounds forge calls across all methods. Waiting for a slot honors `ctx`.
- The UI renders from `Peek…` immediately, then issues the fetching call in a `tea.Cmd`. `r` and a five-minute background timer in the UI trigger refetches ([design.md](design.md#decided)); the UI also owns cancellation and drops stale results.
- `core.Coverage` tracks per-repo scan outcomes for host-wide scans such as ★ Renovate. `Service.RenovateScan` fetches one repo's open PRs and issues through the cache and returns its Renovate PRs and parsed dashboards, and the UI builds the view with the pure `internal/core/renovate` package ([ADR 0013](adr/0013-renovate-view.md)).

Mutations go through core too ([ADR 0011](adr/0011-merge-contract.md)). `Service.Merge` takes confirmed targets, pinned by head SHA, and returns one result per target: merged, refused (`ErrHeadChanged`, `forge.ErrRefused`, or the green-CI gate that cmd injects through `core.Options.RequireGreenCI`), failed, unknown (timed out), or not started (cancelled before its turn). Cancelling stops new starts, and started merges finish. `Service.Recheck` re-fetches targets before every merge so a retry is reconciled. Successful merges and closes drop the item from the cached open list. `Service.UpdateBranch` updates one change request with its target branch, then re-fetches it into the cached open list so its head and CI are current.

## UI

Details and rationale in [ADR 0010](adr/0010-ui-shell.md).

- Charm v2, pinned: `charm.land/bubbletea/v2` v2.0.10, `charm.land/bubbles/v2` v2.2.1, `charm.land/lipgloss/v2` v2.0.6. Only the root model returns a `tea.View`; sub-models return strings.
- A root model in `internal/ui` routes to sub-models for the repo list, the boxes and the details. One keymap generates both the help overlay and the status-bar hints. Every color and style lives in `internal/ui/style`. `internal/ui/markdown` turns PR and issue bodies into styled text for the details pane (ADR 0014). `internal/ui/termimg` emits kitty graphics placeholders and detects once per run whether the terminal shows them (ADR 0016).
- Loading: selecting a repo cancels the previous selection's fetches, seeds the boxes from `Peek…`, and fetches only the kinds that missed. `r` and the five-minute tick refetch everything visible. Messages carry their `core.Key`, and a message for another repo is dropped.
- App root ([ADR 0012](adr/0012-ui-app-root-and-settings.md)): `ui.App` sits above the session model and owns the screen (onboarding, host picker, Settings, session), the config and the one live session. It catches `S` and, at the repo list, `h` before the session sees them, unless a dialog or the help overlay is open.
- Switching host cancels the old session's context and bumps a generation number. Every session command's result is stamped with its generation, and the app drops results from a replaced session, because two hosts can serve the same `owner/name`.
- `ui` never imports an adapter: cmd injects `Connect` and `Probe` functions. The session's green-CI gate reads the live config through an atomic pointer, so a Settings toggle applies on the next merge without reconnecting.
- Config saves run as commands, ordered by sequence number so an older snapshot never overwrites a newer one. Settings changes apply at once and report a failed save in the status bar; onboarding's save applies only after the write succeeds.
- Demo mode: `lazyforge --demo` runs on `forgetest.NewDemo` data and never contacts a host or checks for updates. Verify UI changes by driving it in tmux at 80x24.
- Tests send messages to `Update` against a real `core.Service` over the demo `Fake` and assert state, commands and plain-text `View` output. There are no golden files and no `teatest`.

## Testing

- Each adapter has a **contract test suite**: the same tests run against every adapter, using API responses recorded as fixtures.
- The core and UI are tested against an in-memory fake `Forge`; the UI approach is in [UI](#ui).

## Releases

Tagged commits on `main` publish four platform tarballs to Forgejo releases, installed by `install.sh` ([ADR 0008](adr/0008-release-and-install.md)). A `v*` tag also mirrors the same files to GitHub releases, the fallback when Forgejo is unreachable ([ADR 0009](adr/0009-self-update.md)). CI and release share one setup action and its caches, and a release verifies the same archives it publishes ([ADR 0015](adr/0015-ci-caching-and-release-pipeline.md)). Interactive launches offer a newer release and self-update by rename ([ADR 0009](adr/0009-self-update.md)).

## Build order

Revised after the [plan review](plan-review.md):

1. Run the spikes below.
2. Domain model and `Forge` interface, revised with the spike findings and checked on paper against GitHub and GitLab before freezing.
3. One complete path: connect → list repos → inspect a PR → confirm merge → show the result.
4. Cross-repo grouping and partial-failure handling (★ Renovate).
5. The remaining v1 features (see [design.md](design.md#v1-scope)), then the GitHub adapter, then GitLab.

## Spikes to run first

- [x] Forgejo Actions API on the deployed version (#1): runs, jobs and logs exist; re-run doesn't. Samples in `internal/forge/gitea/testdata/actions/`.
- [x] Renovate PR body parsing (#2): fixtures and format notes in `internal/core/testdata/renovate/`. One update per table row; unparseable bodies fall back to the title.
- [x] Gitea/Forgejo SDKs vs a thin client (#3): thin client, see ADR 0005.
