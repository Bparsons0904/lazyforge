# 0013. Renovate view

- Status: accepted
- Date: 2026-10-02
- Decided by: Opus (second opinion: none)

## Context

#10 builds the ★ Renovate view: a host-wide, five-box view over every repo the user can see, with bulk merge of an update group. It needs Renovate PR detection, body parsing, grouping, scoring and Dependency Dashboard parsing, plus a scan model that fills in as repos load. Spike #2 captured real PR bodies (`internal/core/testdata/renovate/`).

## Decision

- **D1. `internal/core/renovate` is a pure package.** It imports `domain` and the stdlib only, with no I/O, no `forge` and no `core`. `core` imports it, and `ui` may import it for types and pure functions. Fixtures stay at `internal/core/testdata/renovate/`.
- **D2. Detection is inclusive.** A change request is a Renovate PR when its author matches `renovate_user` (case-insensitive, when set) or its source branch starts with `renovate/`. A renamed bot or a missing `renovate_user` never hides PRs. The scan lists open PRs only, so the `refs/pull/N/head` branch gotcha of closed PRs doesn't apply.
- **D3. Core fills `ChangeRequest.Renovate`.** `Service.ChangeRequests` fills it before caching and `Service.Recheck` re-parses the fresh CR, so the merge dialog lists the updates a target carries at the pinned head SHA. The field is nil when the body is rejected. Other PRs are left as the forge gave them. `core.Options.RenovateUser` carries the host's setting.
- **D4. Body parsing** takes the first table after `This PR contains the following updates:` and maps cells by header name. Required headers are `Package`, `Update` and `Change`; `Type` is optional; Renovate's documented badge columns (`Pending`, `References`, `File`, `Age`, `Adoption`, `Passing`, `Confidence`) are accepted and ignored. The whole body is rejected on a missing intro, no table, a width mismatch, any other header, a missing required header, a Change cell that isn't `` `from` → `to` ``, or zero rows.
- **D5. Ecosystem is inferred, never read from the body alone.** The `Type` cell maps per row (`action` and `uses-with` to github-actions, `final` and `stage` to docker, `require`, `indirect` and `toolchain` to go, the npm dependency types to npm). With no usable `Type` and exactly one row, the title noun decides (`docker tag`, `docker digest`, `docker image`, ` action `, ` module `, `helm release`). Anything else is unknown.
- **D6. Updates are grouped by `{Ecosystem, Package, To}`.** `From` stays out of the key and a group carries its distinct sorted `Froms`. A PR with several updates is a batched row that always merges whole. A single update with an unknown ecosystem gets its own row and joins no group. A PR with a rejected body isn't in the groups. Rows sort by member count, then label, then first repo.
- **D7. Counting and impact.** Each Renovate PR counts once, in the bucket of its highest-ranked update type (major, minor, patch, other), so the buckets sum to the PR count. Impact per PR is `1 + log2(1 + days open)`, a repo's impact is the sum, and repos with no Renovate PRs are omitted. The unit is PRs (PQ1, recommended default pending the PM).
- **D8. Box [5] holds Renovate PRs whose CI isn't green.** Forgejo reports a running Action as pending, so running can't be told from pending there.
- **D9. Dashboards.** An issue is a dashboard when titled `Dependency Dashboard` or when `renovate_user` opened it and its body says `This issue lists Renovate updates`. `ParseDashboard` returns one entry per checkbox line above `## Detected Dependencies`, with section, checked state, branch and title. Ticking is out of scope.
- **D10. One scan command per repo.** `Service.RenovateScan` fetches open CRs then open issues through the cached, semaphore-bounded methods, so ADR 0007's bound limits the whole scan. `PeekRenovateScan` reports ok only when both lists are cached. The UI owns a `core.Coverage`, a per-repo result map and a scan sequence, rebuilds the view with the pure `renovate.Build` after each result, and cancels the scan context when the cursor leaves ★.
- **D11. Bulk merge reuses `Service.Recheck` and `Service.Merge` unchanged.** Targets are deduped by (repo, number), repos without write access are skipped with the reason, and unmerged targets stay marked (ADR 0011). The merge dialog is generalized for multiple repos, not forked.
- **D12. No new dependencies, and no change to `forge.Forge`** beyond the domain field `ChangeRequest.CreatedAt`, which the gitea adapter maps from `created_at`.

## Consequences

- Detection is inclusive, so a user's own PR on a `renovate/` branch counts as Renovate's.
- A body Renovate changes the shape of is rejected instead of half-read. The PR still shows in the view, with the title only.
- Ecosystem inference can be wrong for titles outside the known nouns. Those rows stay unknown and never merge as part of a group.
- Switching [1] to count updates instead of PRs only touches `renovate.Build`.

## Alternatives

- Putting the parser in `core`: rejected, because `ui` needs the types and pure functions and shouldn't import a package full of I/O plumbing.
- Reading the ecosystem from the `renovate-debug` comment: rejected, because it carries no manager or datasource.
- Keying groups on `From` too: rejected, because the same bump from different versions in different repos is one update to the user.
- A forge-level "list all PRs for the host" capability: rejected, because `ListRepos` plus per-repo lists already covers the scope and fits the existing cache.
