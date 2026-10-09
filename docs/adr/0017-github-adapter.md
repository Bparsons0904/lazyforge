# 0017. Thin hand-written HTTP client for the github adapter

- Status: accepted
- Date: 2026-10-09
- Decided by: Opus (second opinion: none)

## Context

lazyforge needs a GitHub adapter (#84) that serves github.com and GitHub Enterprise Server. GitHub allows about 5,000 REST requests an hour, and ★ Renovate scans every repo, with two CI calls per PR, so refreshes must be cheap. `google/go-github` takes a context per call, but it adds a module and its own types, and it hides the HTTP layer we need for conditional requests.

## Decision

- `internal/forge/github` uses its own small `net/http` client, as the gitea adapter does (ADR 0005). It sends `Authorization: Bearer`, `Accept: application/vnd.github+json` and `X-GitHub-Api-Version: 2022-11-28`.
- A web root of `github.com` (any case) talks to `https://api.github.com`, and any other root to `<root>/api/v3`. `Info().URL` stays the web root. On GHES, `Version` is `installed_version` from `GET /meta`.
- Pagination sends `per_page=100` and follows `Link: rel="next"` until it is absent, never stopping on a short page. A next link off the API origin is an error, so the token never leaves the host.
- **Conditional requests:** the client keeps a mutex-guarded cache of URL → (ETag, Link, body) for 2xx GETs, capped at 1,000 entries and 32 MiB in total and evicted oldest first. A body over 1 MiB is never cached. It sends `If-None-Match` and replays the cached body on a 304, which GitHub doesn't count against the limit. It lives in the adapter because core's cache never sees HTTP.
- **Runs** are an exception to ADR 0006's "list methods return everything" (#85): `ListRuns` reads one page of 100, newest first, and never follows `Link`. The `[3]` box shows recent runs, and a busy repo's full history is tens of thousands of runs, which would spend the rate limit on rows nobody scrolls to.
- **Job logs** bypass the cache (#85). The log endpoint redirects to a blob host that sends an `ETag`, so a cached GET would read the whole log into memory before returning; `JobLog` streams the body instead. Go's redirect rule forwards `Authorization` to subdomains and other ports of the same host, so `JobLog` drops it itself on any hop off the API origin, as the gitea adapter does for assets. Transport errors drop the request URL, because the redirect target's query carries the blob host's signature.
- **Images** (#87): `OpenAsset` (ADR 0016) accepts only URLs on the web root's origin (`github.com`, not `api.github.com`) and streams them like job logs. It sends the token there and drops it on any hop off that origin, such as the signed S3 URL that `/user-attachments/assets/<id>` redirects to. Public-repo attachments open this way (verified 2026-10-09). Private-repo attachments are reported to need a browser session, so for an API token they fail and stay a link, as design.md → Images says for a failed load.
- **CI** folds check runs (all pages) and the combined status for the head SHA. A combined status with `total_count: 0` contributes nothing, because GitHub reports it as `pending`. A source that answers 404 or 422 (a head SHA GitHub no longer has) counts as none, so one such PR can't fail the list. Precedence is fail > cancelled > running > pending > pass > skipped > none. An unknown status or conclusion counts as pending, so it never reads as green.
- **Merge** sends the confirmed `sha`; a 409 from the merge endpoint is `ErrHeadChanged`. An empty `Method` reads `GET /repos/{o}/{r}` and picks the first allowed of merge, squash, rebase, the merge button's order. If no flag is readable, it sends no `merge_method` and GitHub uses merge. `ListRepos` leaves `Repo.MergeStyle` empty, because `/user/repos` returns null `allow_*` flags (verified 2026-10-09) and one GET per repo per load would cost more than the dialog's "repo default" text saves.
- **Errors:** 401 → `ErrUnauthorized`. 403 with `X-RateLimit-Remaining: 0` or a message containing "rate limit" → `ErrRateLimited`, and any other 403 → `ErrUnauthorized`. 404 → `ErrNotFound`, 429 → `ErrRateLimited`, and 405, 422 and non-merge 409s → `ErrRefused` with GitHub's message and per-field errors.

## Consequences

- No new modules, and the adapter shares its shape with gitea, so one review pattern covers both.
- A steady-state refresh of an unchanged account costs almost no rate-limit budget. The cache is bounded at 32 MiB, so a very large account trades at most that much memory for it, and a page over 1 MiB is always fetched in full.
- The PR list is N+1 (two CI calls per PR). If a large account's first load is too slow, GraphQL is the follow-up.
- We maintain the endpoint list and fixtures ourselves.

## Alternatives

- `google/go-github`: rejected. It adds a module and a large type surface to convert from, and conditional requests would have to go through an `http.RoundTripper` cache layered under it.
- A cache in core: rejected. Core sees domain values, not ETags.
- Fetching `allow_*` per repo in `ListRepos`: rejected for the cost above; Merge resolves the method with one GET at merge time.
