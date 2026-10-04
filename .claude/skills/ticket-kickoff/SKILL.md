---
name: ticket-kickoff
description: Use when a ticket is picked up — by `work-queue`, or by number ("grab 8", "work on issue 4", "pick up #12"). Routes to the right execution mode BEFORE any deep reading. Not for opening a ticket ("open a ticket for X") or fleshing one out ("let's work through 12", "flesh this out", "is this ready") — those stay with `ticket-workflow`.
---

# Ticket Kickoff

A router, not a workflow. It reads only the ticket, picks an execution mode, states it, and hands off in the same turn. It never implements and never reads beyond the commands below.

**Why so little reading:** context gathered before the mode decision makes inline work feel inevitable. Once docs and source files are open, dispatching to a pipeline reads as wasted effort even when it's right. Make the call before that context exists.

Every `gh` call needs `-R Bparsons0904/lazyforge`. Every `fj` call needs `-H https://git.bobparsons.dev`.

## 1. Read the ticket, nothing else

```bash
gh issue view <N> -R Bparsons0904/lazyforge --json body -q .body
gh issue view <N> -R Bparsons0904/lazyforge --comments
```

No docs, no source reads, no greps. Any further reach belongs to the mode you route into.

## 1.5 Is this already done or taken?

```bash
git fetch -q origin && git log --oneline --grep "GH-<N>" origin/develop   # already shipped?
gh issue view <N> -R Bparsons0904/lazyforge                                                   # assignee, state
git cat-file -t <sha>                                                    # once per SHA the ticket cites
```

- **A grep hit is a prompt to verify, not a stop.** It also matches commits that merely mention the number, such as an MR that filed this ticket. Read the commit, or `git show origin/develop:<file>`, before believing it shipped.
- **A cited SHA is a checkable claim.** One that doesn't resolve means the ticket's premise, and likely its scope, is wrong.
- **A docs-only hit is weak evidence.** Docs can restate a premise the code has since falsified. Check the code.

## 2. Pick a mode

First match wins. Readiness is `ticket-workflow` Mode C's criteria (problem stated, AC present (PM-stated or derived from `docs/design.md`), one MR, no open product question, technical decisions settled or settleable by Opus, survives a cold start). Judge against those; don't fork them here.

| Signal from the ticket body | Mode |
|---|---|
| Fails readiness | **none** → `ticket-workflow` Mode B (Discovery) |
| `bug` with no demonstrated root cause | **none** → `labs:triangulate`, then re-run kickoff |
| Subtasks whose file domains **overlap**, 5+ subtasks, or a cross-cutting change (e.g. a `Forge` interface change rippling through `internal/forge`, `internal/forge/gitea`, `internal/core` and `internal/ui`) | **`labs:composer`** |
| 2+ subtasks with **disjoint** file domains (e.g. a new adapter method in `internal/forge/gitea` plus a new box in `internal/ui`) | **`labs:composer-lite`** |
| One file domain, no separable subtasks (e.g. a fix inside `internal/core`, a flag in `cmd/lazyforge`) | **inline** → `ticket-workflow` Mode C |

The two "none" rows matter as much as the mode rows: a fuzzy ticket and a ready one both defaulting to inline is the failure this skill prevents.

**The composer / composer-lite line is file-domain overlap, not subtask count.** Lite fans out implementers in parallel too, inside one worktree. What separates them is whether subtasks need branch-level isolation and an integration branch to reconcile overlap. Disjoint domains don't.

If the PM names a mode up front ("grab 8 with composer"), skip the table and use that mode.

**Driver label** (`opus` / `sonnet`, see `ticket-workflow`): if it doesn't match the running model, say so in one line. That's information, not a stop. A Sonnet session on an `opus` ticket should expect to lean on `engineering-decision`. No label on a ready ticket: apply one.

## 3. State and continue

There is no approval gate. Print one block, then hand off **in the same turn**:

```
Ticket 8 · feature · readiness: PASS (AC from design.md → Navigation model, one MR)
Label: sonnet
Mode: composer-lite
Why: two subtasks, disjoint domains (internal/forge/gitea and internal/ui) —
     parallel in one worktree; nothing overlaps, so no integration branch.
```

**Missing AC?** If the behavior is specified in `docs/design.md`, derive the AC from it (see `ticket-workflow` → Who decides what), write them to the ticket, and continue. If the ticket needs behavior the design doesn't specify, it's a product question: post it on the ticket (`ticket-workflow` → Parking a ticket), and return to `work-queue` for the next ticket.

## 4. Hand off

- **composer / composer-lite:** they own planning. Pass the ticket number and body. Their Phase 0.5 reads CLAUDE.md's "Repo facts" table.
- **inline:** `ticket-workflow` Mode C **at step 2**. Don't re-read the ticket.
- **discovery:** `ticket-workflow` Mode B.
- **undiagnosed bug:** `labs:triangulate`. Once it returns a diagnosis, re-run kickoff rather than assuming inline.
