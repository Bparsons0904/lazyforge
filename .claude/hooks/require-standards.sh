#!/usr/bin/env bash
# PreToolUse:Edit|Write|MultiEdit — refuses an edit to Go code (*.go, go.mod, go.sum, or
# anything under cmd/ or internal/) until go-development has actually been invoked this
# session.
#
# A standard that CLAUDE.md only calls mandatory in prose loses: a session that never
# invokes it reads identically to one that did, right up until review. product-voice is
# the precedent — it is loaded by a SessionStart hook for exactly this reason. This gate
# defers to the moment of the decision rather than session start, because the standard is
# large and only matters once Go code is about to change.
#
# Fail-fast convenience, not the enforcement itself: anything written through Bash never
# reaches this hook. require-standards-commit.sh is the chokepoint.
set -uo pipefail

# shellcheck source=.claude/hooks/lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh" || exit 0

payload=$(cat)
session=$(hook_session "$payload")
file=$(hook_jq '.tool_input.file_path // empty' "$payload")

# The commit gate is authoritative, so an unidentifiable session is left to it rather than
# refused twice.
[ -n "$file" ] && [ -n "$session" ] || exit 0

# Classification is repo-relative, so an absolute path has to lose its root first.
if root=$(hook_repo_root "$payload"); then
  file=${file#"$root"/}
fi

# Still absolute means the file is outside this session's repo (a scratch file, the module
# cache, another worktree). *.go matches at any depth, so without this every Go file on
# disk would be gated. Work in another worktree is still caught when it is committed.
case "$file" in
  /*) exit 0 ;;
esac

required=$(printf '%s\0' "$file" | hook_required_skills)
[ -n "$required" ] || exit 0

# shellcheck disable=SC2086 # required is a deliberately word-split skill list
missing=$(hook_missing_skills "$session" $required)
[ -n "$missing" ] || exit 0

hook_deny "CLAUDE.md requires these skills before editing ${file}:${missing}. Invoke each with the Skill tool, then retry this edit."
