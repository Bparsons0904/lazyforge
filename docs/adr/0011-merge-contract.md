# 0011. Merge contract and mutations in core

- Status: accepted
- Date: 2026-10-02
- Decided by: Opus (second opinion: none)

## Context

#9 ships merge (single and bulk), approve, close and comment. #17 set the merge contract and the PM settled its product questions in #13: freshness is pinned by head SHA, every target gets its own result, a retry is reconciled first, `esc` stops starting new merges, bulk carries on past failures, unmerged PRs stay marked, CI warns or refuses per the green-CI setting, and the strategy is always the repo default. Before this, core couldn't tell a forge's refusal (conflicts, branch protection) from a failure, because the gitea adapter returned both as plain errors.

## Decision

- **`forge.ErrRefused`** ("refused by the forge") joins ADR 0006's sentinels. The gitea adapter wraps it for 405 and for every 409 except `head out of date` (which stays `ErrHeadChanged`), and keeps the server's message, so the reason the user sees is the forge's own text.
- **The green-CI gate lives in core.** `core.Options.RequireGreenCI func(domain.RepoRef) bool` is injected by cmd from the selected host's config, so core never imports config; nil means off. `Merge` enforces it: with the gate on, CI that isn't pass, none or skipped is refused before the forge is called. `Service.RequiresGreenCI` lets the dialog label targets, but the dialog can't bypass the gate.
- **`Service.Merge(ctx, []Target) []MergeResult`.** Targets are deduped by (repo, number) in input order. Each target classifies into one outcome: merged; refused (`ErrHeadChanged`, `ErrRefused`, CI gate); unknown (`context.DeadlineExceeded` or a `net.Error` timeout, since the forge may have applied it); failed (anything else); not started (ctx was cancelled before it got a semaphore slot). `Err.Error()` is always the shown reason.
- **Cancellation:** slots are acquired with the caller's ctx, which stops new starts. Started merges run on `context.WithoutCancel(ctx)` so they finish and report; the adapter's 30s HTTP timeout still bounds them. `Merge` blocks until every result is in.
- **Reconcile before every merge.** `Service.Recheck` re-fetches each target (`GetChangeRequest`, under the semaphore) and writes the fresh CR into the cached list, or drops it when it's no longer open. The UI calls it on every `m`, first attempt or retry, and the dialog records the fresh head SHA. An unknown outcome is resolved by the next recheck.
- **Merge style:** `forge.MergeOpts.Method` is always "" (the repo default). `domain.Repo.MergeStyle` carries the repo's default so the dialog can name it.
- **Action messages carry the repo ref, not a `core.Key`,** because mutations have no cache key.
- **Mutations update the cache in core:** a successful merge or close drops the item from the cached open list, and a comment on an issue bumps its cached `Comments`. Approve, close and comment hold a semaphore slot like reads.

## Consequences

- Every adapter must map its refusal responses to `ErrRefused`; the contract suite checks that merging an already-merged change request returns it.
- There's no live per-target progress during a bulk merge, only the full result list at the end.
- The UI reseeds its boxes from `Peek…` after a mutation instead of refetching.

## Alternatives

- Classifying refusals by status code in core: rejected, because status codes are adapter detail.
- The gate in the UI dialog: rejected, because a second caller (★ update groups) could skip it.
- A per-result channel for progress: deferred until a bulk merge feels opaque.
