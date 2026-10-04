---
name: work-queue
description: The default for a lazyforge session. Use when a session starts without a specific instruction, or when the PM says "work the queue", "keep going", or "work through the tickets". It finishes any agent MR left open, then picks the next open ticket, delivers it through ticket-kickoff, merges it, and repeats until nothing actionable is left. It parks tickets that need a product decision and ends with a summary for the PM.
---

# Work Queue

The PM starts a session; the session works the backlog. Nothing in this loop waits for approval. The only reason to stop on a ticket is a product question, and a product question parks that ticket while the loop carries on with the next one.

Every `fj` call needs `-H https://git.bobparsons.dev`; every `gh` call needs `-R Bparsons0904/lazyforge`. Every comment an agent posts starts with `🤖 `. All `fj` and `gh` traffic uses the PM's account, so this prefix is the only way to tell a PM reply from an agent note.

## 0. Sync and finish what's in flight

```bash
git -C ~/Development/lazyforge fetch -q --prune && git -C ~/Development/lazyforge switch -q develop && git -C ~/Development/lazyforge pull -q --ff-only
fj -H https://git.bobparsons.dev pr search --repo deadstyle/lazyforge --state open
git -C ~/Development/lazyforge worktree list
```

An open MR from an earlier session comes first. Check its CI (CLAUDE.md → merge rule) and fix it if it's red. If its review never ran, run `ship-it` from step 2. Then merge it. Clean up worktrees whose branch has merged with `git worktree remove`.

## 1. Build the queue

```bash
gh issue list -R Bparsons0904/lazyforge --state open -L 200 --json number,title,labels
```

For each `needs-pm` ticket, read the comments. If a comment **without** the `🤖 ` prefix appears after the parking comment, the PM has answered:

- fold the answer into `docs/design.md`, and into the ticket body
- remove the label (`gh issue edit <N> -R Bparsons0904/lazyforge --remove-label needs-pm`)
- treat the ticket as open

Otherwise skip it.

Order the remaining tickets:

1. **Dependencies first.** A ticket whose `## Depends on` section (or "Depends on #N" in its text) lists an open ticket waits until that ticket is done.
2. **Spikes before the work they inform.**
3. **Driver label versus the running model.** An Opus session takes any ticket. A Sonnet session takes `sonnet` tickets first. It takes an `opus` ticket only when no `sonnet` ticket is actionable, and then routes every technical decision through `engineering-decision`.
4. **Lowest number** otherwise.

## 2. Deliver one ticket

Invoke `ticket-kickoff` with the ticket number. It routes to composer, composer-lite, inline work, discovery, or triangulate, then hands off in the same turn. Delivery ends with the MR **merged** and the ticket **closed** (CLAUDE.md → merge rule).

Between steps, post short progress notes on the ticket (`🤖 started: composer-lite`, `🤖 merged: <MR url>`), so the PM can follow along from Forgejo.

**When composer asks for plan approval,** the orchestrating session approves it as engineering lead. Check the plan against the ticket's AC and the docs first. This is not a PM gate.

**When a product question comes up mid-ticket,** park the ticket (`ticket-workflow` → Parking a ticket). Leave any branch pushed with a 🤖 comment saying where it stands, then go back to step 1.

**When a ticket turns out to be wrong** (its premise is false, or it has outgrown 2x its scope), comment with the evidence. If the fix is to split it, split it and carry on. If the fix needs a product decision, park it.

## 3. Repeat

Go back to step 1, because a merge can unblock tickets. Keep going while the context allows; compaction is fine, and the ticket comments are the durable record.

## 4. Stop and report

Stop when nothing is actionable: every remaining ticket is parked, or blocked by a parked one. The final message follows `product-voice`:

1. **Questions waiting for the PM** come first: one line each, with the ticket number and the recommended answer.
2. **What shipped:** each merged ticket, described as what a user can now do.
3. **What's next:** what will unblock once the questions are answered.
