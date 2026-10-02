#!/usr/bin/env bash
# Run: bash .claude/hooks/hooks_test.sh
#
# These gates block the agent's own tool calls, and their characteristic defect is a
# silent fail-open — a gate that waves work through while still reading as enforced. By
# eye a broken gate and a satisfied one are the same thing: no output, exit 0. That is
# what this file exists to tell apart.
#
# Cases run against a throwaway repo, never the working tree.
set -uo pipefail

HOOKS=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT
export TMPDIR="$WORK/tmp"
mkdir -p "$TMPDIR"

pass=0
fail=0

repo() {
  local dir="$WORK/repo"
  rm -rf "$dir"
  mkdir -p "$dir/cmd/lazyforge" "$dir/internal/domain"
  git -C "$dir" init -q
  git -C "$dir" config user.email t@t.t
  git -C "$dir" config user.name t
  printf 'package main\n' >"$dir/cmd/lazyforge/main.go"
  printf 'package domain\n' >"$dir/internal/domain/repo.go"
  printf 'module example.com/lazyforge\n' >"$dir/go.mod"
  printf '' >"$dir/go.sum"
  printf '# x\n' >"$dir/README.md"
  git -C "$dir" add -A
  git -C "$dir" commit -qm base
  printf '%s' "$dir"
}

record() {
  printf '{"session_id":"%s","tool_input":{"skill":"%s"}}' "$1" "$2" |
    bash "$HOOKS/record-skill.sh" >/dev/null
}

# run <hook> <payload> -> prints hook stdout
run() {
  printf '%s' "$2" | bash "$HOOKS/$1" 2>/dev/null
}

check() {
  local label=$1 expected=$2 actual=$3
  if [ "$expected" = "$actual" ]; then
    pass=$((pass + 1))
    printf '  ok    %s\n' "$label"
  else
    fail=$((fail + 1))
    printf '  FAIL  %s (expected %s, got %s)\n' "$label" "$expected" "$actual"
  fi
}

verdict() {
  case "$1" in
    "") printf 'allow' ;;
    *permissionDecision*deny*) printf 'deny' ;;
    *'"decision":"block"'*) printf 'block' ;;
    *) printf 'other' ;;
  esac
}

commit_payload() {
  jq -nc --arg c "$1" --arg d "$2" --arg s "${3:-S}" '{session_id:$s,cwd:$d,tool_input:{command:$c}}'
}

# commit_verdict <repo> [session] -> verdict for a plain `git commit` in that repo
commit_verdict() {
  verdict "$(run require-standards-commit.sh "$(commit_payload 'git commit -m x' "$1" "${2:-S}")")"
}

echo "== commit gate: which command forms are seen =="
# A substring match on "git commit" misses every form below that does not contain those
# two words adjacently.
R=$(repo)
printf 'package main\n// edit\n' >"$R/cmd/lazyforge/main.go"
for form in \
  "git commit -m x" \
  "git -C $R commit -m x" \
  "git -c user.name=x commit -m x" \
  "git --git-dir=$R/.git commit -m x" \
  "env -C $R git commit -m x" \
  "git commit --amend --no-edit" \
  "cd $R && git commit -m x"; do
  check "sees: $form" deny "$(verdict "$(run require-standards-commit.sh "$(commit_payload "$form" "$R")")")"
done
check "ignores: ls -la" allow "$(verdict "$(run require-standards-commit.sh "$(commit_payload 'ls -la' "$R")")")"
check "ignores: git status" allow "$(verdict "$(run require-standards-commit.sh "$(commit_payload 'git status' "$R")")")"
check "ignores: git -C x log" allow "$(verdict "$(run require-standards-commit.sh "$(commit_payload "git -C $R log" "$R")")")"

echo "== commit gate: which changes are seen =="
# git collapses a wholly-untracked directory to one entry, so a brand-new package
# directory — the ordinary shape of new work — would match no file pattern and pass.
R=$(repo)
mkdir -p "$R/internal/forge/gitea"
printf 'package gitea\n' >"$R/internal/forge/gitea/client.go"
check "brand-new untracked package directory" deny "$(commit_verdict "$R")"

# git quotes a path containing a space, and any whitespace-based parse then reads the
# wrong token and classifies the file as nothing at all.
R=$(repo)
printf 'package domain\n' >"$R/internal/domain/two words.go"
check "path containing a space" deny "$(commit_verdict "$R")"

R=$(repo)
printf 'module example.com/lazyforge\n\nrequire x v1\n' >"$R/go.mod"
check "go.mod change" deny "$(commit_verdict "$R")"

R=$(repo)
printf 'x v1 h1:abc\n' >"$R/go.sum"
check "go.sum change" deny "$(commit_verdict "$R")"

# Go source is gated wherever it lives, not only under cmd/ and internal/.
R=$(repo)
mkdir -p "$R/tools"
printf 'package tools\n' >"$R/tools/gen.go"
check "Go file outside cmd/ and internal/" deny "$(commit_verdict "$R")"

# Everything under cmd/ and internal/ is gated, including fixtures and embedded assets.
R=$(repo)
mkdir -p "$R/internal/forge/testdata"
printf '{}\n' >"$R/internal/forge/testdata/pulls.json"
check "non-Go file under internal/" deny "$(commit_verdict "$R")"

R=$(repo)
mv "$R/internal/domain/repo.go" "$R/internal/domain/repository.go"
git -C "$R" add -A
check "staged rename" deny "$(commit_verdict "$R")"

R=$(repo)
printf 'x\n' >>"$R/README.md"
mkdir -p "$R/docs"
printf 'x\n' >"$R/docs/design.md"
check "docs-only change is not gated" allow "$(commit_verdict "$R")"

echo "== commit gate: the ledger is what clears it =="
R=$(repo)
printf 'package main\n// edit\n' >"$R/cmd/lazyforge/main.go"
record L ship-it
check "an unrelated skill does not clear it" deny "$(commit_verdict "$R" L)"
record L comment-standard
check "comment-standard alone does not clear it" deny "$(commit_verdict "$R" L)"
record L go-development
check "allows once both standards are recorded" allow "$(commit_verdict "$R" L)"
check "another session's ledger does not clear it" deny "$(commit_verdict "$R" M)"

echo "== commit gate: fails closed, never open =="
R=$(repo)
printf 'package main\n// edit\n' >"$R/cmd/lazyforge/main.go"
check "no session_id" deny \
  "$(verdict "$(run require-standards-commit.sh "$(jq -nc --arg d "$R" '{cwd:$d,tool_input:{command:"git commit -m x"}}')")")"
check "cwd is not a git repo" deny \
  "$(verdict "$(run require-standards-commit.sh "$(commit_payload 'git commit -m x' "$WORK/tmp")")")"

# A refusal emitted through jq would vanish when jq is what broke, leaving empty stdout
# and exit 0 — a pass, indistinguishable from a real one.
SHIM="$WORK/shim"
mkdir -p "$SHIM"
printf '#!/bin/sh\nexit 1\n' >"$SHIM/jq"
chmod +x "$SHIM/jq"
out=$(printf '%s' "$(commit_payload 'git commit -m x' "$R")" |
  PATH="$SHIM:$PATH" bash "$HOOKS/require-standards-commit.sh" 2>/dev/null)
check "jq broken" deny "$(verdict "$out")"

# A PATH of /nonexistent would prove nothing — bash itself would not resolve, and the
# hook never running looks exactly like the hook allowing. This PATH has everything the
# gate needs except jq.
NOJQ="$WORK/nojq"
mkdir -p "$NOJQ"
for bin in bash dirname cat git grep tr awk sed; do
  src=$(command -v "$bin") && ln -sf "$src" "$NOJQ/$bin"
done
out=$(printf '%s' "$(commit_payload 'git commit -m x' "$R")" |
  PATH="$NOJQ" "$NOJQ/bash" "$HOOKS/require-standards-commit.sh" 2>/dev/null)
check "jq absent from PATH" deny "$(verdict "$out")"

CLONE="$WORK/clone"
cp -r "$HOOKS" "$CLONE"
rm -f "$CLONE/lib.sh"
out=$(printf '%s' "$(commit_payload 'git commit -m x' "$R")" |
  bash "$CLONE/require-standards-commit.sh" 2>/dev/null)
check "lib.sh missing" deny "$(verdict "$out")"

echo "== edit gate =="
R=$(repo)
ep() { jq -nc --arg f "$1" --arg d "$2" '{session_id:"E",cwd:$d,tool_input:{file_path:$f}}'; }
edit_verdict() { verdict "$(run require-standards.sh "$(ep "$1" "$R")")"; }
check "absolute Go path" deny "$(edit_verdict "$R/internal/ui/model.go")"
check "repo-relative Go path" deny "$(edit_verdict "internal/ui/model.go")"
check "Go test file" deny "$(edit_verdict "$R/internal/core/cache_test.go")"
check "go.mod" deny "$(edit_verdict "$R/go.mod")"
check "go.sum" deny "$(edit_verdict "$R/go.sum")"
check "non-Go file under cmd/" deny "$(edit_verdict "$R/cmd/lazyforge/README.md")"
check "unrelated path" allow "$(edit_verdict "$R/docs/design.md")"
check "repo .claude config" allow "$(edit_verdict "$R/.claude/settings.json")"
# *.go matches at any depth, so an absolute path the repo root could not be stripped from
# must not be classified — it would gate every Go file on disk.
check "Go file outside the repo" allow "$(edit_verdict "$WORK/elsewhere/x.go")"
check "no session_id is left to the commit gate" allow \
  "$(verdict "$(run require-standards.sh "$(jq -nc --arg f "$R/cmd/lazyforge/main.go" --arg d "$R" '{cwd:$d,tool_input:{file_path:$f}}')")")"
record E go-development
check "test file needs only go-development" allow "$(edit_verdict "$R/internal/core/cache_test.go")"
check "allows go.mod with only go-development" allow "$(edit_verdict "$R/go.mod")"
check "production Go also needs comment-standard" deny "$(edit_verdict "$R/internal/ui/model.go")"
record E comment-standard
check "allows production Go once both are recorded" allow "$(edit_verdict "$R/internal/ui/model.go")"

echo "== stop gate =="
R=$(repo)
printf 'package main\n// edit\n' >"$R/cmd/lazyforge/main.go"
sp() { jq -nc --arg s "$1" --arg d "$2" '{session_id:$s,cwd:$d}'; }
check "blocks when ship-it never ran" block "$(verdict "$(run require-shipit.sh "$(sp T1 "$R")")")"
check "blocks only once per session" allow "$(verdict "$(run require-shipit.sh "$(sp T1 "$R")")")"
record T2 ship-it
check "silent when ship-it ran" allow "$(verdict "$(run require-shipit.sh "$(sp T2 "$R")")")"
record T3 labs:composer
check "silent in a composer run" allow "$(verdict "$(run require-shipit.sh "$(sp T3 "$R")")")"
record T4 labs:composer-lite
check "silent in a composer-lite run" allow "$(verdict "$(run require-shipit.sh "$(sp T4 "$R")")")"
check "silent with no session_id" allow "$(verdict "$(run require-shipit.sh "$(jq -nc --arg d "$R" '{cwd:$d}')")")"
R=$(repo)
printf 'module example.com/lazyforge\n\nrequire x v1\n' >"$R/go.mod"
check "blocks on a go.mod-only change" block "$(verdict "$(run require-shipit.sh "$(sp T5 "$R")")")"
R=$(repo)
printf 'x\n' >>"$R/README.md"
check "silent with docs-only work" allow "$(verdict "$(run require-shipit.sh "$(sp T6 "$R")")")"
R=$(repo)
check "silent with a clean tree" allow "$(verdict "$(run require-shipit.sh "$(sp T7 "$R")")")"

printf '\n%s passed, %s failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
