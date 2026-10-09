# 0009. Startup update check and self-update by rename

- Status: accepted
- Date: 2026-10-02
- Decided by: Opus (second opinion: none)

## Context

Ticket #12 has lazyforge check for a newer release at startup and offer to install it. Releases are the tarballs and `checksums.txt` from ADR 0008.

## Decision

- New leaf package `internal/update`. It uses the stdlib plus `golang.org/x/mod/semver`, a new dependency chosen because it handles prerelease and build edge cases correctly.
- Only plain release versions take part. A current or latest version that isn't valid semver, or a latest with a prerelease suffix, never triggers an offer. That also skips `dev` and `git describe` builds.
- The check runs on every launch with a 2s timeout and fails silently. There is no cache, and answering "no" asks again next launch. It is skipped when stdin isn't a terminal (`os.Stdin.Stat()` and `ModeCharDevice`, no `x/term`), with `--no-update-check`, `LAZYFORGE_NO_UPDATE_CHECK=1`, or `update.check = false`.
- Binaries under a package manager (`/opt/homebrew/`, `/usr/local/Cellar/`, `/home/linuxbrew/`, `/nix/store/`, `/usr/bin/`) or in a directory the user can't write get a one-line hint instead of a prompt.
- `Apply` downloads the archive and `checksums.txt` next to the executable, verifies SHA-256 (a missing line or a mismatch is an error), extracts to a temp file, renames `exe` to `exe.old`, then the new file to `exe`. The binary is replaced by rename, never overwritten in place: on macOS, overwriting a signed binary's inode trips the kernel's code-signature cache and the process gets SIGKILLed, while a new inode avoids it. The Go linker ad-hoc signs darwin/arm64 binaries and Go's HTTP download sets no quarantine attribute, so Gatekeeper isn't involved.
- After a successful swap the process re-execs with `syscall.Exec`, adding `LAZYFORGE_NO_UPDATE_CHECK=1` so a new build that reports a stale version can't loop. If exec fails, `exe.old` is renamed back. `CleanupOld` removes `exe.old` at every startup.

- Amended 2026-10-09: the check tries Forgejo first, then the GitHub mirror (`Bparsons0904/lazyforge`). It moves to the next source only on a transport error, a timeout or a 5xx; any other answer ends the check. `Release` records the source it was found on, and `Apply` downloads the archive and `checksums.txt` from that source alone, so the two hosts are never mixed.

## Consequences

- Windows is out of scope: it can't rename over a running binary and has no `syscall.Exec`.
- One network request, capped at 2s, is added to every interactive launch.
- Amended 2026-10-09: with Forgejo down, the launch check takes up to 4s, two 2s timeouts in a row. Each source keeps its own 2s timeout.

## Alternatives

- Overwriting the binary in place: rejected, it gets the process killed on macOS.
- A daily cache of the last check: rejected by the PM; every launch checks.
