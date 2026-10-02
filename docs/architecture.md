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
│ Core                                         │  domain model · cache · Renovate logic
│                                              │  (★ view, grouping, bulk merge, dashboard ticks)
├──────────────────────────────────────────────┤
│ Forge interface + capabilities               │
├──────────────┬──────────────┬────────────────┤
│ gitea        │ github       │ gitlab         │  adapters: API ⇄ domain model
│ (+ forgejo)  │              │                │
└──────────────┴──────────────┴────────────────┘
```

Forgejo is a fork of Gitea and their APIs are still largely the same. The plan is one adapter that covers both, with version checks wherever they diverge.

## Stack

**Go, Bubble Tea and Lip Gloss** ([ADR 0001](adr/0001-go-and-charm.md)):

- Lip Gloss makes the bordered, titled boxes easy to build.
- Go has SDKs for Gitea and Forgejo, and existing CLIs (`tea`, `gh`) can supply auth tokens.
- It builds to a single static binary.

## Domain model (sketch)

```go
type Repo struct {
    Owner, Name, Description string
    LastActivity             time.Time
    WebURL                   string
}

type ChangeRequest struct { // PR on Gitea/Forgejo/GitHub, MR on GitLab
    Number                     int
    Title, Body, Author        string
    State                      State // open, merged, closed
    SourceBranch, TargetBranch string
    CI                         CIState // pass, fail, running, none
    Labels                     []string
    UpdatedAt                  time.Time
    Renovate                   *RenovateUpdate // nil unless authored by the Renovate user
}

type RenovateUpdate struct {
    Package, From, To string
    Bump              string // major, minor, patch, digest
}

type Issue struct { /* Number, Title, Body, Author, State, Labels, Comments… */ }
type Run struct    { ID int64; Workflow, Branch, Commit, Trigger string; Status CIState; Jobs []Job }
type Job struct    { ID int64; Name, Stage string; Status CIState } // Stage is only set on GitLab
type Release struct { Tag, Name, Notes string; PublishedAt time.Time }
```

## Forge interface (sketch)

A core interface that every adapter implements, plus optional interfaces for features that not every forge supports. Capabilities come from type assertions, so a capability can't be claimed without being implemented.

```go
type Forge interface {
    Info() HostInfo // kind, display terms ("PR" vs "MR"), user

    ListRepos(ctx context.Context, opts ListOpts) ([]Repo, error)

    ListChangeRequests(ctx context.Context, r RepoRef, f Filter) ([]ChangeRequest, error)
    GetChangeRequest(ctx context.Context, r RepoRef, n int) (ChangeRequest, error)
    Merge(ctx context.Context, r RepoRef, n int, opts MergeOpts) error

    ListIssues(ctx context.Context, r RepoRef, f Filter) ([]Issue, error)
    EditIssueBody(ctx context.Context, r RepoRef, n int, body string) error // Renovate dashboard ticks
    Comment(ctx context.Context, r RepoRef, n int, body string) error
    Close(ctx context.Context, r RepoRef, n int) error

    ListReleases(ctx context.Context, r RepoRef) ([]Release, error)
}

// Optional: the UI checks for these with type assertions.
type Approver   interface { Approve(ctx context.Context, r RepoRef, n int) error }
type DiffReader interface { ChangedFiles(ctx context.Context, r RepoRef, n int) ([]FileDiff, error) }
type RunLister  interface { ListRuns(ctx context.Context, r RepoRef, f Filter) ([]Run, error) }
type LogReader  interface { JobLog(ctx context.Context, r RepoRef, jobID int64) (io.ReadCloser, error) }
type Rerunner   interface { Rerun(ctx context.Context, r RepoRef, runID int64) error }
```

## API mapping (first pass)

This mapping is unverified. Each row needs checking against current API docs, and against the Forgejo version actually deployed, before the interface is frozen.

| Operation | Gitea / Forgejo | GitHub | GitLab |
|---|---|---|---|
| List repos by activity | `GET /repos/search?sort=updated` | `GET /user/repos?sort=updated` | `GET /projects?membership=true&order_by=last_activity_at` |
| List change requests | `GET /repos/{o}/{r}/pulls?state=open` | `GET /repos/{o}/{r}/pulls?state=open` | `GET /projects/:id/merge_requests?state=opened` |
| Merge | `POST /repos/{o}/{r}/pulls/{n}/merge` | `PUT /repos/{o}/{r}/pulls/{n}/merge` | `PUT /projects/:id/merge_requests/:iid/merge` |
| Approve | `POST …/pulls/{n}/reviews` (`event: APPROVED`) | `POST …/pulls/{n}/reviews` (`event: APPROVE`) | `POST /projects/:id/merge_requests/:iid/approve` |
| Changed files | `GET …/pulls/{n}/files` | `GET …/pulls/{n}/files` | `GET /projects/:id/merge_requests/:iid/diffs` |
| PR CI state | `GET …/commits/{ref}/status` | check-runs + combined status for the head SHA | MR head pipeline |
| Issues | `GET …/issues?type=issues` | `GET …/issues` (filter out PRs) | `GET /projects/:id/issues` |
| Edit issue body | `PATCH …/issues/{n}` | `PATCH …/issues/{n}` | `PUT /projects/:id/issues/:iid` |
| Comment | `POST …/issues/{n}/comments` | `POST …/issues/{n}/comments` | `POST …/notes` |
| List runs | ⚠ check what the deployed Forgejo version exposes | `GET …/actions/runs` | `GET /projects/:id/pipelines` |
| Run jobs | ⚠ check | `GET …/actions/runs/{id}/jobs` | `GET /projects/:id/pipelines/:id/jobs` |
| Job log | ⚠ check | `GET …/actions/jobs/{id}/logs` | `GET /projects/:id/jobs/:id/trace` |
| Re-run | ⚠ check | `POST …/actions/runs/{id}/rerun` | `POST /projects/:id/pipelines/:id/retry` |
| Releases | `GET …/releases` | `GET …/releases` | `GET /projects/:id/releases` |

How the forges differ in practice:

- **Pagination:** GitHub uses `Link` headers, Gitea uses `X-Total-Count`, and GitLab offers keyset pagination. Each adapter hides this.
- **Rate limits:** GitHub allows about 5,000 requests per hour, so the core cache is required there, not optional.
- **CI shape:** GitHub and Forgejo use runs → jobs → steps. GitLab uses pipelines → stages → jobs, which maps to `Job.Stage`.
- **Reviews:** the domain model stays deliberately small: approve, comment, merge, close.

## Configuration and stored state

See ADR 0004. There is no database.

```toml
# ~/.config/lazyforge/config.toml (written by onboarding and the settings screen)
default_host = "homelab"

[update]
check = true

[hosts.homelab]
type = "forgejo"
url = "https://git.bobparsons.dev"
token_cmd = "infisical secrets get FORGEJO_TOKEN --plain"
renovate_user = "renovate"

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

- The core keeps an in-memory cache, scoped to the session's host and keyed by request.
- Navigating renders from the cache immediately, and the data refreshes in the background.
- `r` forces a refresh, and the UI also refreshes in the background every five minutes ([design.md](design.md#decided)).

## Testing

- Each adapter has a **contract test suite**: the same tests run against every adapter, using API responses recorded as fixtures.
- The core and UI are tested against an in-memory fake `Forge`.

## Build order

Revised after the [plan review](plan-review.md):

1. Run the spikes below.
2. Domain model and `Forge` interface, revised with the spike findings and checked on paper against GitHub and GitLab before freezing.
3. One complete path: connect → list repos → inspect a PR → confirm merge → show the result.
4. Cross-repo grouping and partial-failure handling (★ Renovate).
5. The remaining v1 features (see [design.md](design.md#v1-scope)), then the GitHub adapter, then GitLab.

## Spikes to run first

- [ ] Forgejo Actions API on the deployed version: list runs, jobs, job logs, re-run.
- [ ] Renovate PR body parsing: collect real PR bodies across the different managers (docker, gomod, github-actions, terraform).
- [ ] The Gitea and Forgejo Go SDKs against the deployed instance: is the SDK useful, or is a thin hand-written client simpler?
