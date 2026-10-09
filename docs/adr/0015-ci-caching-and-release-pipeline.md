# 0015. CI caching, run cancellation and a single release build

- Status: accepted
- Date: 2026-10-08
- Decided by: Opus (second opinion: none)

## Context

Ticket #64. A warm CI run took about 109 s, of which the checks themselves were 27 s. Log timestamps from run 703 showed where the rest went: 45 s re-uploading setup-go's 470 MB module/build cache, 12 s restoring it, 10 s downloading Go, and 7 s installing golangci-lint and ShellCheck.

The re-upload happened on every run. The runner's cache server matches keys case-insensitively and returned the stored `setup-go-linux-…` entry for the primary key `setup-go-Linux-…`. setup-go compares the two case-sensitively, saw a miss, and saved again. Every run, PRs included, also wrote the cache that later trusted runs restored.

The release job built all four platforms twice: once at `v0.0.0-test` inside `make check`'s installer test, and again with the real tag.

## Decision

- `.forgejo/actions/setup` is a composite action used by both workflows. It restores two caches, both keyed in lowercase:
  - **tools**: the Go toolchain plus pinned golangci-lint and ShellCheck. The key is the Go version from `go.mod`, both tool versions and a hash of the setup action, so changing the install recipe also invalidates it. It has no fallback. On a miss the tools are installed, and the ShellCheck archive is checked against a pinned SHA-256.
  - **go**: the module cache, the build cache and golangci-lint's cache. The key is the Go version, the `go.sum` hash and the ISO week. It falls back to the newest entry for the same Go version, because Go validates build-cache entries itself.
- Only `push` runs on `develop`/`main` save caches, and only when they missed. PR and dispatched runs restore but never write, so an ordinary branch run can't plant a binary that a trusted run would then execute. This gate lives in the workflow, so a PR that edits the workflow could still write a cache unless the cache server scopes entries by branch. It narrows, but doesn't close, a hole that was wide open before, when every run saved.
- Parallel jobs were rejected. The checks are 15–30 s, and each extra job would pay about 20 s of container and cache setup.
- A new push to a PR cancels that PR's older run. Every other run has its own concurrency group, so pushes to `develop`/`main` and dispatched runs (whose result `github-pull` copies onto imported branches) are never cancelled.
- The release job runs `make release VERSION=<tag>` first, then `make check` with `TEST_INSTALL_REUSE_DIST=1 TEST_INSTALL_VERSION=<tag>`. The installer tests run against the exact archives that get published, and they check that each binary reports the tag. A `workflow_dispatch` dry run does the same with a version input, but skips the main-ancestry gate and the publish step.

## Consequences

- Measured on the same runner. Warm: 109 s → 37 s per run. Setup fell from 31 s to 21 s, and the end-of-run cache save from 45 s to under 1 s. Cold (no cache, both caches saved): about 151 s → 161 s, which happens once per Go, tool or `go.sum` change and once a week. The tasks API reports 0 s queue time for every run measured, so all figures are execution time.
- Bumping golangci-lint or ShellCheck means editing the setup action. A ShellCheck bump also needs `SHELLCHECK_SHA256`, so a Renovate bump of it fails CI until the hash is updated.
- Go is still downloaded on a tools-cache miss. Baking Go and the linters into `deadstyle/runner-images` would remove that, but it's runner-image work outside this repo.
- The GitHub sync workflows were measured and left alone: `github-pull` runs in about 6 s every 10 minutes.
