# shellcheck shell=bash
# Shared by every hook in this directory. Sourced, never executed.
#
# Three things live here because each has to agree across the script that writes it and
# the scripts that read it, and a drifted copy would not error — it would silently satisfy
# every gate, which is the one failure mode the gates exist to end: the ledger path, the
# path-to-skill classification, and the refusal format.

# Refusals never go through jq. jq builds the payload everywhere else, but emitting a
# denial through it means a missing or broken jq produces empty stdout and a zero exit —
# indistinguishable from a pass, so every gate would fail open while still looking
# enforced.
hook_json_escape() {
  local s=$1
  s=${s//\\/\\\\}
  s=${s//\"/\\\"}
  s=${s//$'\n'/\\n}
  s=${s//$'\r'/\\r}
  s=${s//$'\t'/\\t}
  printf '%s' "$s"
}

hook_deny() {
  printf '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"%s"}}\n' \
    "$(hook_json_escape "$1")"
  exit 0
}

# Named separately because it is the one denial an agent must not try to satisfy by
# invoking skills: nothing it can do from inside the session clears a broken environment,
# and retrying forever is what a gate with no satisfying condition produces.
hook_deny_broken() {
  hook_deny "$1 This cannot be cleared by invoking skills — the hook environment itself is broken. Tell the user rather than retrying."
}

# Returns non-zero when jq cannot answer, so callers fail closed instead of reading a
# failed parse as "the payload had nothing in it". Testing `command -v jq` is not enough:
# it proves the file exists, not that it runs, so a jq that errors would leave every gate
# seeing an empty command and concluding there was no commit to check.
hook_jq() {
  local out
  out=$(printf '%s' "$2" | jq -r "$1" 2>/dev/null) || return 1
  printf '%s' "$out"
}

# Empty rather than a literal fallback: a shared bucket would let concurrent sessions
# satisfy each other's gate, so callers decide whether an unidentifiable session is
# harmless (skip) or grounds to refuse (fail closed).
hook_session() {
  hook_jq '.session_id // empty' "$1"
}

hook_ledger() {
  printf '%s' "${TMPDIR:-/tmp}/claude-skills-$1.txt"
}

# Prints the skills from "$@" that the session has not invoked, space-prefixed.
hook_missing_skills() {
  local session="$1"
  shift
  local ledger missing=""
  ledger=$(hook_ledger "$session")
  for skill in "$@"; do
    grep -qxF "$skill" "$ledger" 2>/dev/null || missing="$missing $skill"
  done
  printf '%s' "$missing"
}

# Succeeds when the session has invoked at least one of the skills in "$@".
hook_any_skill_recorded() {
  local session="$1"
  shift
  local ledger
  ledger=$(hook_ledger "$session")
  for skill in "$@"; do
    grep -qxF "$skill" "$ledger" 2>/dev/null && return 0
  done
  return 1
}

# Resolved from the payload's own cwd, not from CLAUDE_PROJECT_DIR alone. Ticket work runs
# in a worktree beside the main checkout, and a root that names the main checkout inspects
# a clean tree and reports nothing to enforce — inert for exactly the sessions it polices.
# Returns non-zero when no git tree can be resolved; callers fail closed on that.
hook_repo_root() {
  local cwd root
  cwd=$(hook_jq '.cwd // empty' "$1") || return 1
  [ -n "$cwd" ] || cwd=${CLAUDE_PROJECT_DIR:-$PWD}
  root=$(git -C "$cwd" rev-parse --show-toplevel 2>/dev/null) || return 1
  [ -n "$root" ] || return 1
  printf '%s' "$root"
}

# Matches the git subcommand, not the string "git commit". The substring form misses
# `git -C <path> commit` — the natural form when committing in a worktree without changing
# directory — so a string match would leave the chokepoint blind on an ordinary path.
# Shell separators become token boundaries so a chained command is still seen.
hook_git_subcommand_is() {
  local want=$1 cmd=$2
  cmd=${cmd//[;&|()]/ }
  local -a tokens=()
  read -r -a tokens <<<"$cmd"
  local n=${#tokens[@]} i=0
  while [ "$i" -lt "$n" ]; do
    case "${tokens[$i]}" in
      git | */git)
        i=$((i + 1))
        while [ "$i" -lt "$n" ]; do
          case "${tokens[$i]}" in
            -C | -c | --git-dir | --work-tree | --namespace | --exec-path | --super-prefix)
              i=$((i + 2))
              ;;
            -*)
              i=$((i + 1))
              ;;
            *) break ;;
          esac
        done
        if [ "$i" -lt "$n" ] && [ "${tokens[$i]}" = "$want" ]; then
          return 0
        fi
        ;;
    esac
    i=$((i + 1))
  done
  return 1
}

# Every changed path in the working tree, NUL-separated and repo-relative. No pathspec:
# hook_required_skills is the single definition of what is gated, and a pathspec here
# would be a second copy that could drift from it and let a file class through unseen.
#
# NUL-separated because a path containing a space comes back quoted from git and any
# whitespace-based parse then misclassifies it into passing the gate. --untracked-files=all
# because git otherwise collapses a wholly-new directory to one entry that matches no file
# pattern — and a new directory is the ordinary shape of a new package.
hook_changed_paths() {
  local root=$1
  git -C "$root" -c core.quotePath=false status --porcelain=v1 -z \
    --untracked-files=all 2>/dev/null | hook_strip_status
}

hook_strip_status() {
  local record status
  while IFS= read -r -d '' record; do
    status=${record:0:2}
    printf '%s\0' "${record:3}"
    # A rename record is followed by its source path as a second entry; consuming it here
    # keeps the caller reading one path per record.
    case "$status" in
      R* | C*) IFS= read -r -d '' _ || true ;;
    esac
  done
}

# The single definition of which standard governs which file. Reads NUL-separated
# repo-relative paths, prints the required skills space-separated (empty when none apply).
hook_required_skills() {
  local path go="" comments=""
  while IFS= read -r -d '' path; do
    case "$path" in
      # `*` in a case pattern crosses `/`, so *.go gates Go source at any depth, not only
      # under cmd/ and internal/. go.sum is gated with go.mod because a dependency change
      # touches both, and adding a dependency is a decision go-development governs.
      *.go | go.mod | go.sum | */go.mod | */go.sum | cmd/* | internal/*)
        go="go-development"
        ;;
      *) continue ;;
    esac
    # comment-standard governs production Go only; the standard exempts tests and fixtures.
    case "$path" in
      *_test.go | */testdata/*) ;;
      *.go) comments="comment-standard" ;;
    esac
  done
  printf '%s' "$go${comments:+ }$comments"
}
