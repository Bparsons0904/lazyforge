# 0007. Core cache, refresh and concurrency

- Status: accepted
- Date: 2026-10-02
- Decided by: Opus (second opinion: none)

## Context

#7: the UI renders from cache immediately, refreshes in the background, must drop late results, cancels work when the user navigates away, and must not flood the forge when ★ Renovate loads every repo. The PM chose manual `r` plus a background refresh every five minutes (#13, Q3).

## Decision

- `internal/core` does not import Bubble Tea. It exposes blocking, `context`-taking calls, and `internal/ui` wraps each call in a `tea.Cmd`. Results carry their request key on the UI side, which is where stale results are dropped.
- `core.Service` wraps one `forge.Forge` for the session. Each read has two forms: `Peek…` returns the cached value and when it was fetched, without I/O; the fetching form calls the forge and stores the result. A failed fetch keeps the old entry.
- The cache is in memory, keyed by `core.Key{Kind, Repo, Number}`. The host is implicit, since there's one per session. Entries never expire on their own. `Invalidate(key)` and the UI's refresh decide when to refetch. The five-minute timer lives in the UI, which just re-issues fetches.
- Every forge call made through the service holds a slot in one semaphore (size 4 by default). Acquiring a slot respects `ctx`, so a cancelled request that's still queued never reaches the forge.
- Cancellation belongs to the UI. It derives a context per selection and cancels it on navigation. Core only passes `ctx` through.
- A host-wide scan is many per-repo fetches. `core.Coverage` tracks total, done, and failed per repo, which gives the UI "scanned X of Y" and lets bulk merge see that coverage is incomplete.
- The repo order is the adapter's: the contract suite requires descending activity order, so core doesn't re-sort.

## Consequences

- Core is unit-tested against `forgetest.Fake` with no Bubble Tea machinery.
- The UI owns more wiring: commands, keys, contexts. That's where `go-development`'s Bubble Tea rules already put it.
- The cache can grow over a long session. It holds only small domain values for one host, which is acceptable for v1.

## Alternatives

- Core returning `tea.Cmd`: rejected, because it couples core to the UI framework and makes core tests drive message plumbing.
- Per-host or per-repo semaphores: unnecessary with one host per session.
- `singleflight` deduplication of identical in-flight requests: deferred until a duplicate fetch is actually observed.
- TTL expiry inside the cache: rejected. Refresh is the UI's policy (manual `r` plus the timer), so expiry in the cache would be a second policy.
