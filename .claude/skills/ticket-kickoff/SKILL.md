---
name: ticket-kickoff
description: Use when the PM picks up a ticket by number — "grab 8", "work on issue 4", "pick up #12", "let's do ticket 20". Routes to the right execution mode BEFORE any deep reading. Not for opening a ticket ("open a ticket for X") or fleshing one out ("let's work through 12", "flesh this out", "is this ready") — those stay with `ticket-workflow`.
---

# Ticket Kickoff

A router, not a workflow. It reads only the ticket, picks an execution mode, proposes it, and stops. It never implements and never reads beyond the commands below.

**Why so little reading:** context gathered before the mode decision makes inline work feel inevitable. Once docs and source files are open, dispatching to a pipeline reads as wasted effort even when it's right. Make the call before that context exists.

Every `fj` call needs `-H https://git.bobparsons.dev`.

## 1. Read the ticket, nothing else

```bash
fj -H https://git.bobparsons.dev issue view <N> body
fj -H https://git.bobparsons.dev issue view <N> comments
```

No docs, no source reads, no greps. Any further reach belongs to the mode you route into.

## 1.5 Is this already done or taken?

```bash
git fetch -q origin && git log --oneline --grep "#<N>" origin/develop   # already shipped?
fj -H https://git.bobparsons.dev issue view <N>                          # assignee, linked MR
git cat-file -t <sha>                                                    # once per SHA the ticket cites
```

- **A grep hit is a prompt to verify, not a stop.** It also matches commits that merely mention the number, such as an MR that filed this ticket. Read the commit, or `git show origin/develop:<file>`, before believing it shipped.
- **A cited SHA is a checkable claim.** One that doesn't resolve means the ticket's premise, and likely its scope, is wrong.
- **A docs-only hit is weak evidence.** Docs can restate a premise the code has since falsified. Check the code.

## 2. Pick a mode

First match wins. Readiness is `ticket-workflow` Mode C's criteria (problem stated, AC PM-authored or confirmed, one MR, no open product question, technical decisions settled or settleable by Opus, survives a cold start). Judge against those; don't fork them here.

| Signal from the ticket body | Mode |
|---|---|
| Fails readiness | **none** → `ticket-workflow` Mode B (Discovery) |
| `bug` with no demonstrated root cause | **none** → `labs:triangulate`, then re-run kickoff |
| Subtasks whose file domains **overlap**, 5+ subtasks, or a cross-cutting change (e.g. a `Forge` interface change rippling through `internal/forge`, `internal/forge/gitea`, `internal/core` and `internal/ui`) | **`labs:composer`** |
| 2+ subtasks with **disjoint** file domains (e.g. a new adapter method in `internal/forge/gitea` plus a new box in `internal/ui`) | **`labs:composer-lite`** |
| One file domain, no separable subtasks (e.g. a fix inside `internal/core`, a flag in `cmd/lazyforge`) | **inline** → `ticket-workflow` Mode C |

The two "none" rows matter as much as the mode rows: a fuzzy ticket and a ready one both defaulting to inline is the failure this skill prevents.

**The composer / composer-lite line is file-domain overlap, not subtask count.** Lite fans out implementers in parallel too, inside one worktree. What separates them is whether subtasks need branch-level isolation and an integration branch to reconcile overlap. Disjoint domains don't.

If the PM names a mode up front ("grab 8 with composer"), skip the table and go to the gate with that mode.

**Driver label** (`opus` / `sonnet`, see `ticket-workflow`): if it doesn't match the running model, say so in one line. That's information, not a stop. A Sonnet session on an `opus` ticket should expect to lean on `engineering-decision`. No label on a ready ticket: propose one and apply it on confirmation.

## 3. Propose and stop

One block, then **stop and wait for the PM**. This is gate 1 (approval before code), not narration:

```
Ticket 8 · feature · readiness: PASS (AC PM-authored, one MR, no open product question)
Label: sonnet
Mode: composer-lite
Why: two subtasks, disjoint domains (internal/forge/gitea and internal/ui) —
     parallel in one worktree; nothing overlaps, so no integration branch.
Confirm, or override → composer / inline / discovery
```

Keep the "Why" line product-legible. Technical reasoning is fine, but don't ask the PM to judge it.

**Missing AC but enough evidence to propose them?** This covers agent-filed tickets (which fail readiness by construction) and PM tickets with a clear problem but no AC section. Propose AC in the same block so the PM confirms AC and mode in one reply:

```
Ticket 15 · bug · readiness: FAIL (no AC — agent-filed from #12's ship-it)
Proposed AC: <one or two lines>
Mode: inline (if you confirm the AC)
Confirm, or override → composer / composer-lite / discovery
```

AC still originate with the PM's confirmation; nothing is written to the ticket before it. A ticket too thin to propose AC from goes to Discovery.

## 4. Hand off

- **composer / composer-lite:** they own planning. Pass the ticket number and body. Their Phase 0.5 reads CLAUDE.md's "Repo facts" table.
- **inline:** `ticket-workflow` Mode C **at step 2**. Don't re-read the ticket.
- **discovery:** `ticket-workflow` Mode B.
- **undiagnosed bug:** `labs:triangulate`. Once it returns a diagnosis, re-run kickoff rather than assuming inline.
