# 0008. Plain-Go release builds and a curl install script

- Status: accepted
- Date: 2026-10-02
- Decided by: Opus (second opinion: none)

## Context

Ticket #11 needs release binaries for linux and darwin on amd64 and arm64, and a one-line install. We publish to Forgejo releases on git.bobparsons.dev. There is no Homebrew tap, signing or changelog generation in v1.

## Decision

- No GoReleaser. `make release` cross-compiles with `go build` (`CGO_ENABLED=0`, `-trimpath`, `-ldflags "-s -w -X main.version=$(VERSION)"`) into `dist/`, and `scripts/publish-release.sh` creates a draft release, uploads the assets with `curl` against the Forgejo API, and publishes only after every upload succeeds, so "latest" never lacks assets.
- Artifacts are `lazyforge_<version>_<os>_<arch>.tar.gz` (one `lazyforge` binary at the archive root) plus `checksums.txt` in `sha256sum` format. `<version>` is the tag verbatim.
- `.forgejo/workflows/release.yml` runs on `v*` tags. It refuses a commit that isn't reachable from `origin/main`, runs `make check`, builds, and publishes with the workflow's automatic token, which is never echoed.
- `install.sh` detects OS and arch, downloads the archive and `checksums.txt`, verifies the checksum, then replaces the binary atomically (copy to a temp name in the install dir, `mv` over). It needs only bash, curl, tar and sha256sum or shasum, with no jq. A mismatch installs nothing.
- `scripts/test-install.sh` runs `install.sh` against a local `python3 -m http.server`. `make install-test` runs shellcheck and that test, and `make check` includes it.

## Consequences

- Four targets and one API call are about 40 lines of shell, with no extra tool or config to keep current.
- The install test runs in Linux CI only. macOS isn't available as a runner, so darwin is covered by cross-compilation and the shared script paths, not executed.
- If we add a tap, signing or changelogs, revisit GoReleaser then.

## Alternatives

- GoReleaser: rejected. It adds a tool, a config file and a version to maintain for features we don't use.
