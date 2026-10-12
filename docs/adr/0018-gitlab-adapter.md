# 0018. Thin hand-written HTTP client for the gitlab adapter

- Status: accepted
- Date: 2026-10-12
- Decided by: Opus (second opinion: none)

## Context

lazyforge needs a GitLab adapter (#137, #138) that serves gitlab.com and self-managed GitLab. GitLab addresses projects by numeric ID or by URL-encoded full path, puts subgroups in that path (`group/sub/repo`), reports CI as pipelines rather than per-commit checks, and fixes the merge method per project. gitlab.com allows 2,000 requests a minute per user today, and its Free tier drops to a 100-a-minute burst and 5,000 an hour from 2026-10-19. No gitlab.com token is wired up, so everything below comes from docs.gitlab.com (read 2026-10-12), not from a live server.

## Decision

- `internal/forge/gitlab` uses its own small `net/http` client, as the gitea and github adapters do (ADR 0005, ADR 0017), with no new module. It sends `PRIVATE-TOKEN: <token>`. The API root is `<web root>/api/v4`, and `Info().URL` stays the web root. `New` reads `GET /user` and `GET /metadata` (`/version` is deprecated), so a bad token fails at connect. `Probe` reads `GET /metadata` without a token and takes a 401 `{"message":"401 Unauthorized"}`, or a 200 with a `version`, as GitLab.
- **Addressing:** every call names the project by its URL-encoded full path (`/projects/group%2Fsub%2Frepo`), built from `RepoRef`, so the adapter holds no path-to-ID state. `RepoRef.Owner` is everything before the last slash of `path_with_namespace`. Go keeps the encoding in `URL.RawPath` and sends it on the request line.
- **Pagination** sends `per_page=100` and follows `Link: rel="next"` until it is absent, never stopping on a short page. A next link off the API origin is an error, as in ADR 0017. It uses offset paging only, because keyset paging doesn't support the orderings the lists need.
- **No conditional-request cache.** GitLab doesn't document ETag or 304 on its REST API, and nothing says a 304 would cost no rate-limit budget. Instead, the adapter keeps the calls per refresh low (CI below).
- **Rate limits:** a 429 waits for `Retry-After` (seconds or an HTTP date, capped at 60 seconds, cancelled with the context) and retries once. A second 429 is `ErrRateLimited`, never `ErrUnauthorized`.
- **CI:** the MR list carries no `head_pipeline`, so `ListChangeRequests` reads one page of the project's newest 100 pipelines and gives each MR the newest one whose `sha` is the MR's `sha`. An MR with no match gets one `GET …/merge_requests/:iid` for its `head_pipeline`, which covers merged-results pipelines, whose SHA differs; merged and closed MRs skip that GET and get none, so paging old history stays cheap. A 403 or 404 on the pipelines listing, as on a project with CI/CD disabled, counts as no pipelines. `GetChangeRequest` always uses `head_pipeline`. The fold is `failed` → fail; `canceled`, `canceling` → cancelled; `running` → running; `success` → pass; `skipped` → skipped; no pipeline → none; every other status, known or not, → pending, so it never reads as green. External commit statuses already land in a pipeline, so there is no separate status fold.
- **Merge:** GitLab has no per-call merge method, so the method is always the project's. `Merge` reads `GET /projects/:id` and sends `PUT …/merge` with the confirmed `sha` and `squash` true when `squash_option` is `always` or `default_on`. A non-empty `MergeOpts.Method` other than the project's is `ErrUnsupported`. `Repo.MergeStyle` uses the gitea strings the merge dialog already shows: `squash` under that squash rule, else `merge`, `rebase-merge` (`rebase_merge`) or `fast-forward-only` (`ff`).
- **Access:** the higher of `permissions.project_access` and `group_access`: 40 and 50 → admin, 30 → write, lower → read; archived → read. When both are null, as they often are for inherited membership, one extra paged `GET /projects?membership=true&min_access_level=30&simple=true` marks those projects write.
- **Errors:** 401 → `ErrUnauthorized`, except on merge and approve, where GitLab uses it for "you may not" and it is `ErrRefused` with GitLab's message. 403 → `ErrUnauthorized`, 404 → `ErrNotFound`, merge 409 → `ErrHeadChanged`, and 405, 406, 422 and other 409s → `ErrRefused`. `message` is a string or a `{field: [errors]}` object, and both are read.
- **Issues and comments** are user content (ADR 0006): issues are listed with `issue_type=issue`, so incidents, test cases and tasks never appear, and notes with `system: true` are dropped.

## Consequences

- No new modules, and the adapter shares its shape with gitea and github, so one review pattern covers all three.
- An open-MR refresh costs the MR list, one pipelines page, and one GET per MR whose head isn't in the newest 100 pipelines. A busy project with many stale MRs pays that N+1.
- Without a cache, an unchanged refresh costs the same as a changed one. On the 2026-10-19 Free-tier limits that bounds how often a large account can refresh.
- Every endpoint is unverified until it runs against gitlab.com, and the fixtures are synthesised from the docs.

## Alternatives

- `gitlab.com/gitlab-org/api/client-go`: rejected. It adds a module and a large type surface to convert from, as ADR 0017 found for `go-github`.
- Numeric project IDs with a path-to-ID map: rejected. It is state to keep in sync for no gain, since every endpoint takes the encoded path.
- A per-MR GET for `head_pipeline` on every list: rejected for the request count; the pipelines page answers most MRs in one call.
