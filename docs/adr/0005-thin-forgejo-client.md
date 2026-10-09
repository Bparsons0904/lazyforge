# 0005. Thin hand-written HTTP client for the gitea adapter

- Status: accepted
- Date: 2026-10-02
- Decided by: Opus (second opinion: none)

## Context

The gitea adapter (Gitea and Forgejo) needs an HTTP client. Spike #3 compared `code.gitea.io/sdk/gitea` v0.25.1, `codeberg.org/mvdkleijn/forgejo-sdk` v3.0.0 and a thin client, prototyped against git.bobparsons.dev (Forgejo `16.0.5+gitea-1.22.0`). Both SDKs cover the v1 core operations. Neither takes a `context.Context` per call: each has one client-wide `SetContext`. Against Forgejo, the gitea SDK parses the version as 16.0.5, so every Gitea version gate passes, and its Actions calls (see #1) then fail or mis-decode (jobs: "cannot unmarshal array"; runs: `head_sha` and `conclusion` empty). The forgejo SDK adds 48 modules and has no jobs or logs calls.

## Decision

- `internal/forge/gitea` talks to the API through its own small client built on `net/http`, with no SDK dependency.
- Every call takes a `context.Context` and goes through one `*http.Client` per session, with explicit timeouts.
- The client maps HTTP status to the `forge` sentinel errors (401/403 → `ErrUnauthorized`, 404 → `ErrNotFound`, 429 → `ErrRateLimited`) and wraps them with `%w`.
- The client handles pagination with `page` and `limit`, reading `X-Total-Count` or the body's `total_count`. It always sends `page`, because Forgejo ignores `limit` without it.
- JSON structs mirror the shapes Forgejo actually returns (recorded in `testdata/`) and never leave the adapter.
- Gitea/Forgejo divergence is handled by detecting the server once at connect (`/api/v1/version`, `+gitea-` suffix ⇒ Forgejo), not by semver gates written for Gitea.

## Consequences

- Per-call cancellation works as `go-development` requires, so navigating away cancels in-flight requests.
- There are no new modules (the gitea SDK would add 15), and the binary is about 1 MB smaller.
- We write and maintain about ten endpoints ourselves. Each new operation costs a few lines plus a recorded fixture.
- If Gitea and Forgejo diverge further, the adapter absorbs it. The SDK would not have helped there.

## Alternatives

- `code.gitea.io/sdk/gitea`: rejected. It has no per-call context, its version gating is wrong for Forgejo, and its Actions structs don't decode Forgejo's responses.
- `codeberg.org/mvdkleijn/forgejo-sdk`: rejected. It has no per-call context, adds 48 modules, and lacks Actions jobs and logs.
- One SDK client per request, to work around `SetContext`: rejected. It's wasteful, and cancellation would still be awkward.
