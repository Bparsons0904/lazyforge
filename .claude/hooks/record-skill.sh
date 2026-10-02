#!/usr/bin/env bash
# PreToolUse:Skill — records every skill a session invokes, so the standards gates can
# tell "the standard was read" from "the standard was skipped". Without this ledger the
# gates have no satisfying condition and would block the session forever.
set -uo pipefail

# shellcheck source=.claude/hooks/lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh" || exit 0

payload=$(cat)
session=$(hook_session "$payload")
skill=$(hook_jq '.tool_input.skill // empty' "$payload")

# An unidentifiable session gets no ledger at all: writing to a shared bucket would hand
# its skill invocations to whichever other session read that bucket next.
if [ -n "$session" ] && [ -n "$skill" ]; then
  printf '%s\n' "$skill" >>"$(hook_ledger "$session")"
fi

exit 0
