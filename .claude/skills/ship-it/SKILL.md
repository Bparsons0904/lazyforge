---
name: ship-it
description: Use when inline ticket work is done and needs to become an MR — triggers on "ship it", "wrap up", "open the MR", or finishing an inline ticket. Runs CI-parity verification, fresh-context standards/comments/spec review by subagents, fixes, doc updates, commit, push, MR creation, merge once CI is green, and a retro, in that fixed order. Not for labs:composer or labs:composer-lite runs — those have their own close-out.
---

# Ship It

Turns finished **inline** work into a merged MR. Composer and composer-lite close out themselves; don't run this on top of them.

Fixed order. A later step is worthless if an earlier one didn't actually run, so don't skip ahead because the diff "looks fine". Every `fj` call needs `-H https://git.bobparsons.dev`.

## 0. Branch, integrate develop, escalation check

```bash
git rev-parse --abbrev-ref HEAD   # must not be develop or main
```

On `develop` or `main`: branch off `origin/develop` now (pattern `(feature|bugfix|chore|spike)/<N>-<slug>`) and confirm your changes carried over.

Then integrate the base before anything else, because every later step grades the branch against it:

```bash
git fetch origin && git merge origin/develop --no-edit
```

Resolve conflicts now. A clean textual merge doesn't prove it builds, so step 1 runs after it. If `develop` moves again during steps 1–6, merge and re-verify. If this branch was stacked on one that has since squash-merged, rebase your delta instead of merging, or you'll conflict against your own merged work.

Then list what changed. Use both forms: the wrong one silently prints nothing, and a brand-new file shows only in `git status`.

```bash
git diff origin/develop --name-only && git status --short          # uncommitted
git diff origin/develop...HEAD --name-only && git status --short   # committed
```

Check the list against CLAUDE.md:

- **Product escalation → stop and ask the PM:** user-visible behavior, keymap or box layout not described in `docs/design.md`; scope past 2x; a `docs/design.md` edit (propose it instead, see step 4).
- **Technical decision → settle it, don't ask the PM:** `go.mod`/`go.sum` (new dependency), a new package or changed boundary, `Forge` interface or domain-model shape, CI/tooling. If the ticket didn't already authorize it, Opus decides (Sonnet asks via `engineering-decision`), and step 4 records it.

A hit in a file the ticket never mentioned counts as unauthorized, even if it's tidy-up.

## 1. Verify (CI parity)

```bash
make check   # build + test + lint + format check: the CI gate
```

Paste the output as evidence. If `.forgejo/workflows/` runs anything `make check` doesn't, run that too; the set must match CI. Check that your local `golangci-lint --version` matches the version CI pins, because a different version both invents and drops findings. `go install` overwrites the binary in place, so re-check the version right before the run you report.

- **Findings in files you didn't touch:** suspect stale cache or a sibling worktree before blaming the code (`golangci-lint cache clean`, re-run).
- **New behavior needs at least one test that fails if the behavior breaks.** A green suite with no test covering the change is not evidence.
- **TUI rendering is not verified by logic tests.** If the payoff is what appears on screen (layout, borders, truncation, focus, keymap feedback), show it with a golden/`teatest` snapshot or a captured screen from the `run` skill. If you couldn't, say so in the MR body in those words ("layout reasoned from the code, not observed"). Silence reads as verified.
- **Docs- or skill-only diff:** `make check` still runs and is the whole of this step. Say so in the MR body so missing test output doesn't read as an omission.

All green before step 2. Report a failure as a failure, with output. Never call it "unrelated" without showing it fail on `develop` too.

## 1.5 Self-audit

Re-read the checklists the reviewers will use and grade your own diff: `comment-standard`'s `## Review checklist` for every comment you added or changed, and `go-development` for every seam and package boundary you touched. Fix what you find. "Clean" is a valid outcome; say it.

## 2. Review: three fresh subagents, in parallel

The author is anchored on the diff; a fresh agent isn't. Nobody else reviews before the PM, so this is the review.

**Commit and push first** (step 5's rules, `git push -u origin <branch>`). The review wait is the longest idle stretch, and the pushed branch is the durable copy, not the worktree.

**Materialize the diff to a file. Reviewers run no git commands.** Subagents don't inherit your cwd and will happily diff the wrong checkout. Never dispatch with `isolation: "worktree"`, since a fresh worktree can't see uncommitted work.

```bash
mkdir -p <worktree>/.claude/review
rm -f <worktree>/.claude/review/{standards,comments,spec}.md
git -C <worktree> add -N <your-paths>     # new files show in the diff; scope it, `add -N .` also stages deletions
git -C <worktree> diff origin/develop > <worktree>/.claude/review/diff.patch
git -C <worktree> diff origin/develop --stat
```

Diff against `origin/develop`, never `...HEAD`: that sees only commits, so a re-review after an uncommitted fix grades pre-fix code.

**Reports go to files.** Each brief names its own `<REPORT_FILE>` and asks for a one-line reply (PASS/FAIL + path). Then `ls`/read the file yourself, whatever the reply says. Each report must open with the file list it graded. Compare that list to `--stat`: an empty or unfamiliar list, or a missing report file, is a **dispatch failure, not a pass**. Re-dispatch.

**Wait for all three before fixing anything.** The patch file doesn't change when the tree does, so an early fix leaves the others grading stale code. A slow reviewer is not a dead one; check its status before replacing it.

**Docs-only diff:** skip standards-review, and add to comments-review: *"This diff is documentation. Fenced code blocks are illustrative, including deliberately-bad examples. Grade only comments in real source files, of which there may be none."*

**No subagents available this session?** Run the three briefs yourself, sequentially, against the same patch file, and put this line in the MR body: *"Reviewed inline; fresh-context subagents were unavailable, so the anchoring bias this step removes was not removed."*

Substitute real paths for `<DIFF_FILE>`, `<REPORT_FILE>` and `<N>`. Never paste a git command into a brief.

### (a) standards-review (model: sonnet)

```
The diff under review is in <DIFF_FILE>. Read it. Run no git commands;
your working directory is not where this diff came from. Begin your
report with the list of files the patch touches.

Invoke the go-development skill and review the diff against it, plus
the dependency direction in CLAUDE.md (ui → core → forge → domain; only
cmd/ knows concrete adapters). You have no other context.

Report three sections:
1. STANDARDS — each violation as `file:line — what the standard says —
   what the diff does`.
2. CORRECTNESS — real defects whether or not a rule covers them: logic
   errors, nil paths, unhandled or swallowed errors, goroutine leaks,
   races, blocking calls in a Bubble Tea Update/View, context not
   propagated. `file:line — what breaks — the input or state that
   triggers it`. For any bug the diff fixes, name where else the same
   mechanism is reachable and which paths you checked.
3. SHAPE — is this the right design, separately from whether it is
   safe? Guards that go permanently dead after first run, lazy init where
   explicit construction would remove a race, state threaded through
   callers that one lower layer could own, a type that leaks across the
   adapter boundary. If the shape is right, say so in one line.

Only `+` lines are under review; lines starting with a space are
context. Over-report: say when unsure, but report it. A false positive
costs one line to dismiss. Do not fix anything.

Write the full report to <REPORT_FILE>. Reply with one line only: PASS
or FAIL and the path. If the write fails, put the report in your reply.
```

### (b) comments-review (model: haiku)

```
Invoke the comment-standard skill and use its `## Review checklist` as your
checklist, verbatim.

The diff under review is in <DIFF_FILE>. Read it. Run no git commands.
Begin your report with the list of files the patch touches.

Review ONLY comments added or changed in the patch (`+` lines). A
comment moved verbatim from elsewhere is not new; if unsure, say so.
First check every comment against the checklist's length caps; length is
a hard flag that content doesn't rescue. Then grade each multi-sentence
comment sentence by sentence: a good later clause doesn't excuse an
opening clause that restates the name or signature. Report
`file:line — rule broken — offending sentence`. Do not fix anything and
do not grade code.

Write the full report to <REPORT_FILE>. Reply with one line only: PASS
or FAIL and the path. If the write fails, put the report in your reply.
```

### (c) spec-review (model: sonnet)

```
The diff under review is in <DIFF_FILE>. Read it. Run no git commands
except `fj -H https://git.bobparsons.dev issue view <N> body`. Begin
your report with the list of files the patch touches.

Read the ticket, then review the patch against it. Derive every
expectation from the acceptance criteria, never from reading the
implementation. For each AC: met / partially met / missing, with
file:line evidence. Also: is there at least one test that would fail if
the new behavior broke, and does it target an AC rather than incidental
coverage? No style or comment review. Do not fix anything.

Write the full report to <REPORT_FILE>. Reply with one line only: PASS
or FAIL and the path. If the write fails, put the report in your reply.
```

**No ticket?** Replace the `fj` allowance with "run no git commands at all" and inline the AC: *"There is no ticket. The acceptance criteria are: 1. … Treat this exactly as a ticket's AC; do not infer more intent from the implementation."* Write those AC from what the PM asked for **before** looking at the diff, and include any approvals the PM gave in conversation so approved work isn't flagged as scope creep.

## 3. Fix

Triage every finding. A dismissal gets a one-line reason, not silence. Standards and comment violations are not judgement calls: fix them.

- **A finding is a claim to re-derive, in both directions.** Before removing code or reverting a change on a reviewer's word, check it yourself. A reviewer that traced two of three paths will call a needed check dead. Equally, a finding framed softly as "fragile" can turn out to be a real bug once you test it. Grade the findings list, not the verdict line; reviewers have written PASS above a blocking finding.
- **Counts are where summaries go wrong.** "The only caller is X", "removed 7 tests whose invariants live elsewhere": one grep settles it.
- **When the fix is "add X to the list", ask whether the list should exist.** A structural fix that removes the enumeration beats patching it once per finding.
- **A fix must not break the standard around it.** Re-read the checklist for whatever you touch.

Beyond a rename or comment edit, dispatch a fresh fixer, not the agent that wrote the code. Commit and push each round. After non-trivial fixes, re-run step 1, re-materialize the diff and re-dispatch the affected reviewer.

## 4. Docs

Always check, even when the answer is "nothing to update". Say which in the MR body.

1. **`docs/architecture.md`:** did layers, the domain model, the `Forge` interface, API mapping, caching or testing approach change? Update it.
2. **`docs/adr/`:** did this MR make a technical decision (see CLAUDE.md's list)? Write an ADR recording Opus's decision and why. If a Sonnet session got the answer via `engineering-decision`, the ADR records that answer.
3. **`docs/design.md` and `docs/mockup.html`:** these belong to the PM. **Propose, don't decide.** If behavior, keymap or layout diverged from them, put the proposed change in a "Proposed design changes" section of the MR body for the PM to accept.

Doc updates ship in **this** MR, never as a follow-up.

## 5. Commit

Format: `type(scope): subject`, with `#<N>` in the body. Types: `feat`, `fix`, `chore`, `docs`, `refactor`, `test`. Scope is the package or area (`ui`, `core`, `gitea`, `domain`, `config`, `cmd`).

```
fix(gitea): page through repos beyond the first 50

#14
```

Lowercase subject, no trailing period. Explain the *why* in the body when the diff doesn't make it obvious.

**One commit per logical change.** Split unrelated changes with `git add <specific files>`; never `git add -A`. Run `git status --short` before **every** commit, since another process's staged change can ride along. If you used `add -N`, `git reset` first. Commit without asking; that is the agreed flow.

## 6. Push and MR

Write the body to `<worktree>/.claude/review/mr-body.md`, then:

```bash
git push -u origin <branch>
fj -H https://git.bobparsons.dev pr create --base develop --head <branch> "type(scope): subject" --body-file <worktree>/.claude/review/mr-body.md
fj -H https://git.bobparsons.dev pr view <PR>   # confirm the body isn't empty
```

The body carries:

- a summary in product terms
- `Fixes #<N>` (the closing keyword is required; a bare `#N` won't close the ticket)
- verification evidence from step 1 (result lines, not scrollback)
- the review outcome and any dismissals with their reasons
- the docs outcome and any proposed design changes

Keep the body file; it's the source for later `fj pr edit`. Push every follow-up commit as soon as it's made.

### Merge

Opening the MR isn't the end state; merging it is (CLAUDE.md → How we work).

```bash
sha=$(git rev-parse HEAD)
curl -s https://git.bobparsons.dev/api/v1/repos/deadstyle/lazyforge/commits/$sha/status | jq -r .state
# pending → wait (Monitor with an until-loop, not sleep); failure → fix, push, re-check; success → merge
fj -H https://git.bobparsons.dev pr merge <PR> -M squash -d -t "type(scope): subject (#<PR>)"
fj -H https://git.bobparsons.dev issue view <N>   # Fixes #N should have closed it; close by hand if not
```

Don't merge while a `docs/design.md` change in the diff lacks PM approval, or while a product question is still open. Ask, and merge once it's answered.

**End the response with the MR's URL** on its own line, since `fj` doesn't print one:

```
https://git.bobparsons.dev/deadstyle/lazyforge/pulls/<PR>
```

## 7. Retro

Invoke `labs:retro` to capture this session's friction and wins. It runs after the MR so the reflection covers the whole session. If it commits to this branch, push.
