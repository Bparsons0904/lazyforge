#!/usr/bin/env bash
# PreToolUse:Bash — refuses to commit Go changes (*.go, go.mod, go.sum, or anything under
# cmd/ or internal/) until go-development has been invoked this session.
#
# The Edit/Write gate alone is not enforcement. It hooks three tools, and writing a file
# through Bash (sed -i, a heredoc, tee, gofmt -w, go mod tidy) walks straight past it.
# Enumerating every way to write a file is unwinnable; committing is the one chokepoint
# every path converges on, whatever wrote the bytes.
#
# That only holds if the match covers how commits are actually issued. A substring match
# on "git commit", or an `if` predicate in settings.json, misses `git -C <path> commit` —
# so the gate sees every Bash call and decides for itself, matching the git subcommand
# rather than a string.
set -uo pipefail

# Inlined rather than delegated: this is the one refusal that has to work when the shared
# library is exactly what failed to load.
# shellcheck source=.claude/hooks/lib.sh
if ! . "$(dirname "${BASH_SOURCE[0]}")/lib.sh" 2>/dev/null; then
  printf '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"The standards gate could not load .claude/hooks/lib.sh, so it cannot verify the mandatory skills were read. Refusing the commit rather than passing unchecked. This cannot be cleared by invoking skills — the hook environment itself is broken. Tell the user rather than retrying."}}\n'
  exit 0
fi

payload=$(cat)

command=$(hook_jq '.tool_input.command // empty' "$payload") ||
  hook_deny_broken "The standards gate could not read the hook payload (jq failed or is not installed), so it cannot tell whether this command is a commit."

hook_git_subcommand_is commit "$command" || exit 0

session=$(hook_session "$payload") ||
  hook_deny_broken "The standards gate could not read the hook payload (jq failed or is not installed), so it cannot verify the mandatory skills were read."

# Fails closed, unlike the other hooks. A blank session id would make every session share
# one ledger bucket, so a gate that kept going here would silently accept another
# session's skill invocations as this one's — worse than refusing, because it still reads
# as enforced.
[ -n "$session" ] ||
  hook_deny_broken "The standards gate cannot identify this session (no session_id in the hook payload), so it cannot verify the mandatory skills were read."

root=$(hook_repo_root "$payload") ||
  hook_deny_broken "The standards gate could not resolve a git repository from this session's working directory, so it cannot see what is being committed."

required=$(hook_changed_paths "$root" | hook_required_skills)
[ -n "$required" ] || exit 0

# shellcheck disable=SC2086 # required is a deliberately word-split skill list
missing=$(hook_missing_skills "$session" $required)
[ -n "$missing" ] || exit 0

hook_deny "CLAUDE.md requires these skills before changing Go code (*.go, go.mod, go.sum, cmd/, internal/), and this commit contains such changes:${missing}. Invoke each with the Skill tool, then retry the commit."
