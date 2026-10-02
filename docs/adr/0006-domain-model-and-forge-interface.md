# 0006. Domain model and Forge interface for v1

- Status: accepted
- Date: 2026-10-02
- Decided by: Opus (second opinion: none; Fable was unavailable)

## Context

#4 freezes the types and interface every adapter, core and UI build on. The spikes and plan review changed the sketch in `architecture.md`. Renovate PRs carry one update per table row, several rows per PR (#2). Forgejo 16 has runs, jobs and logs but no re-run, and jobs come from a separate call per run (#1). Comment and Close must know whether the target is an issue or a change request (GitLab routes them differently). Capability depends on the adapter, the server version and the user's repo permission. Bulk merge must refuse a PR that changed after confirmation.

## Decision

- `domain.RepoRef{Owner, Name}` addresses a repo. `Owner` may contain slashes (GitLab subgroups). `domain.Repo` embeds it and carries `Access` (read, write, admin).
- `domain.ChangeRequest` carries `HeadSHA` and `Renovate []RenovateUpdate`. Adapters leave `Renovate` nil, and core's parser fills it. `RenovateUpdate` is one table row: ecosystem, package, dep type, update type, from, to, source URL.
- `domain.CIState` is one of: none, pending, running, pass, fail, cancelled, skipped. Adapters fold their forge's statuses into it. On GitHub that means check-runs plus commit statuses.
- `forge.ItemRef{Repo, Kind, Number}` (kind is issue or change request) is the target of `Comment`, `ListComments` and `Close`.
- `forge.MergeOpts.HeadSHA` is required. A head mismatch returns `forge.ErrHeadChanged`. Gitea and Forgejo send `head_commit_id`, GitHub `sha`, GitLab `sha`.
- Pagination never crosses the interface. List methods return everything, and the adapter follows pages to the end.
- Optional capabilities are interfaces: `Approver`, `RunLister` (runs plus `ListJobs(runID)`), `LogReader`. `Rerunner` and `DiffReader` are not defined until an adapter implements them, because none can on Forgejo 16 and the Files tab is post-v1.
- Availability is `forge.Can(f, action, repo) Availability{OK, Reason}`, a pure function. It checks, in order: the capability interface (type assertion), then `f.Gate(action)` (server-version gates the adapter learned at connect, with no I/O), then `repo.Access` (write actions need write access). `Reason` is user-facing text.
- Sentinels: `ErrNotFound`, `ErrUnauthorized`, `ErrRateLimited`, `ErrUnsupported`, `ErrHeadChanged`.
- `internal/forge/forgetest` has an in-memory `Fake` (core methods plus all three capabilities, seeded, recording mutations) and `RunContract`, a behavioral suite every adapter runs.

## Paper check: GitHub and GitLab

| Method | GitHub | GitLab | Gap |
|---|---|---|---|
| `ListRepos` (+ `Access`) | `GET /user/repos?sort=updated`, `permissions` | `GET /projects?membership=true&order_by=last_activity_at`, `permissions` | none |
| `ListChangeRequests` / `Get` | `GET …/pulls`, CI from check-runs + statuses of `head.sha` | `GET /projects/:id/merge_requests`, `head_pipeline` | GitHub CI costs 2 calls per PR. Fine behind the cache. |
| `Merge` + `HeadSHA` | `PUT …/merge` `sha`, 409 on mismatch | `PUT …/merge` `sha`, 409 | none |
| `Approver` | `POST …/reviews` `APPROVE` | `POST …/approve` | none |
| `ListIssues` | `GET …/issues`, drop PRs | `GET /projects/:id/issues` | none |
| `EditIssueBody` | `PATCH …/issues/{n}` | `PUT …/issues/:iid` | none |
| `ListComments` / `Comment` (`ItemRef`) | `…/issues/{n}/comments` for both kinds | `…/issues/:iid/notes` vs `…/merge_requests/:iid/notes` | why `ItemRef` exists |
| `Close` (`ItemRef`) | `PATCH` issue or pull `state=closed` | `PUT` `state_event=close` on issue or MR | none |
| `ListReleases` | `GET …/releases` | `GET /projects/:id/releases` | none |
| `RunLister` | `…/actions/runs`, `…/runs/{id}/jobs` | `…/pipelines`, `…/pipelines/:id/jobs` (`Job.Stage`) | none |
| `LogReader` | `…/actions/jobs/{id}/logs` (redirect) | `…/jobs/:id/trace` | none |

GitLab addresses a project by URL-encoded `owner/name`, so `RepoRef` needs no numeric ID. `Number` is the MR `iid`.

## Consequences

- Core and UI can be built against `forgetest.Fake` before the gitea adapter exists.
- Every new adapter implements `Gate` even when it has no version gates (it returns nil).
- Core owns Renovate data on a domain type it didn't fetch. That's simpler for the UI than a wrapper type, but adapters must leave the field nil.

## Alternatives

- Separate `CommentOnIssue` and `CommentOnChangeRequest` methods: rejected, because they double the surface for one routing bit.
- Capability as a method returning a bool on each adapter: rejected, because it can't explain why something is disabled and it duplicates the type assertion.
- Defining `Rerunner` now for GitHub: rejected under YAGNI. It gets added with the first adapter that implements it.
- A core wrapper type for Renovate PRs: rejected, because every list view would have to deal with two types.
