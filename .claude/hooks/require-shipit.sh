#!/usr/bin/env bash
# Stop — one reminder per session when a turn ends with uncommitted Go work (the same file
# classes the standards gates cover) that ship-it never closed out. ship-it owns close-out
# for inline work (CI-parity verification, fresh-context standards and comments review,
# doc updates, commit, MR, retro); running its commands by hand covers only the last step
# and skips every review. Composer runs close out themselves, so a session that invoked
# one is left alone.
#
# Scoped to the working tree only, not commits already on the branch. Checking committed
# work against origin/develop would fire in every later session that touches a long-lived
# branch already shipped but not yet merged — the per-session marker below can't remember
# that across sessions, so it would read as constant nagging.
#
# Fires at most once per session: a Stop hook that blocks unconditionally loops forever.
set -uo pipefail

# shellcheck source=.claude/hooks/lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh" || exit 0

payload=$(cat)
session=$(hook_session "$payload")

# Nothing to key the once-per-session marker or the ledger lookup on, and this is a
# reminder rather than a gate — staying quiet beats warning on every single turn.
[ -n "$session" ] || exit 0

fired="${TMPDIR:-/tmp}/claude-shipit-warned-${session}"
[ -f "$fired" ] && exit 0

root=$(hook_repo_root "$payload") || exit 0

[ -n "$(hook_changed_paths "$root" | hook_required_skills)" ] || exit 0

hook_any_skill_recorded "$session" ship-it labs:composer labs:composer-lite && exit 0

: >"$fired"
printf '{"decision":"block","reason":"This branch has uncommitted Go changes (*.go, go.mod, go.sum, cmd/ or internal/) and ship-it has not run this session. ship-it owns close-out for inline work: CI-parity verification, parallel standards and comments review by fresh-context subagents, fixes, doc updates, commit, MR, retro. Running the verify commands by hand is only its final step. Invoke ship-it now, or tell the user explicitly that you are ending without it and why."}\n'
exit 0
