---
name: ticket-workflow
description: Use whenever a ticket is involved — opening one for an idea ("open a ticket for X", rapid idea capture), fleshing one out ("let's work through 12", "flesh this out", "is this ready"), or executing one once `ticket-kickoff` has routed it to inline work. Picking a ticket up by number ("grab 8", "work on issue 4", "pick up 12") triggers `ticket-kickoff` first, which picks the mode and hands the inline case back here. Tickets are GitHub Issues on Bparsons0904/lazyforge, via `gh -R Bparsons0904/lazyforge`; code and MRs stay on Forgejo (`fj -H https://git.bobparsons.dev`).
---

# Ticket Workflow

Three modes. In a `work-queue` run, Discovery rolls straight into delivery once the ticket is ready.

| Mode | Trigger | Exit |
|---|---|---|
| A. Capture | "open a ticket for X" | ticket with a category label. No plan, no AC. |
| B. Discovery | "let's work through N", "flesh this out", or kickoff found it not ready | scoped ticket with AC, or parked on a product question |
| C. Execute | `ticket-kickoff` routed the ticket to **inline** | code shipped via `ship-it` |

Every `gh` call below needs `-R Bparsons0904/lazyforge`.

## Who decides what

- **AC come from the PM, directly or through the design.** Two legitimate sources only: (1) the PM stated them; (2) Opus derived them from `docs/design.md`, which the PM owns. Mark each derived criterion with its source, e.g. `- pressing \`h\` from the repo list returns to the host picker *(design.md → Navigation model)*`. A Sonnet session asks Opus via `engineering-decision` to derive or confirm them.
- **Behavior the design doesn't specify is a product question.** Never invent it as AC. Park the ticket (below).
- **Technical design belongs to Opus.** Plan, files, package boundaries, interface shape, test approach: Opus decides these and writes them into the ticket. Never ask the PM a technical question. A Sonnet session that hits an unsettled technical decision asks Opus through the `engineering-decision` skill.
- **Product questions go to the PM**: user-visible behavior, keymap, box layout, scope, naming, anything `docs/design.md` doesn't already answer. Offer options with their user-visible consequences and a recommendation.

## Mode A — Capture

Title and problem, in the PM's words. No investigation, plan or scoping.

```bash
gh issue create -R Bparsons0904/lazyforge --title "title" --body-file problem.md --label feature   # body: "## Problem\n\n<PM's words>"; category label only
```

> [!IMPORTANT]
> **Never write an AC the PM didn't state or `docs/design.md` doesn't support, and always cite the design section for derived ones.** The harm is lost provenance: nothing on the ticket separates PM intent from agent guess, so a later agent defends the guess as if the PM wrote it.

Reading rule, every mode: **AC present = binding. AC absent = not yet defined.** Never derive AC from a title.

## Mode B — Discovery

Slim ticket to scoped ticket. In a `work-queue` run it continues straight into `ticket-kickoff` once ready.

1. **Read the ticket:** `gh issue view <N> -R Bparsons0904/lazyforge --json body -q .body` and `gh issue view <N> -R Bparsons0904/lazyforge --comments`.
2. **Read the relevant docs, not all of them:** the `docs/design.md` sections for product behavior, the `docs/architecture.md` sections and any `docs/adr/` for the layers involved. If the ticket touches an item under design.md's "Open questions", that's a product decision to put to the PM, not to resolve silently.
3. **Read the code** the ticket touches. Docs say what it should do; code says what it does.
4. **Settle the technical design** (Opus directly; Sonnet via `engineering-decision`). A decision big enough to change `docs/architecture.md` gets an ADR in the delivering MR; note that in the plan.
5. **Derive AC** from `docs/design.md`, citing sections, and write them to the ticket.
6. **Product questions left over** (behavior the design doesn't cover): park the ticket.

**Decomposition is a first-class outcome.** A ticket too big for one MR exits as 3-5 well-scoped child tickets, with the parent kept as a tracking issue. Don't write a 200-line plan for what should be five tickets.

**Visuals go in the body.** When a plan is hard to hold in one read, `/labs:visualize` draws what it touches (state diagram for a Bubble Tea model, a flow across `ui → core → forge`) and marks `← GAP` where the plan is silent. Forgejo renders mermaid. Skip it when the plan is linear and a few files wide.

**Exit:** AC present and scope fits one MR, then write the scoped body, apply a driver label, and continue. Readiness is a judgement from the body, not a label.

### Parking a ticket

When a ticket can't proceed without a product decision:

```bash
gh issue comment <N> -R Bparsons0904/lazyforge --body-file question.md   # the question, options with user-visible consequences, recommendation
gh issue edit <N> -R Bparsons0904/lazyforge --add-label needs-pm
```

Then move on to the next ticket. The PM answers in the ticket. A later `work-queue` run sees the answer, folds it into `docs/design.md` and the ticket body, removes `needs-pm`, and picks the ticket up.

### Driver labels

One per scoped ticket, naming which model should drive it. Advisory: the PM may drive any ticket with anything.

| Label | Fits |
|---|---|
| `opus` | Design-heavy or risky: an unsettled technical decision, a new package, `Forge` interface or domain-model shape, concurrency/refresh/cancellation, anything that would change `docs/architecture.md`. A one-line change here is still `opus`. Opus may consult Fable through `engineering-decision`. |
| `sonnet` | Well-bounded work that follows established patterns. Any technical decision that surfaces goes to Opus via `engineering-decision`. |

Risk only raises the label. Apply it at Discovery's exit and on any ready ticket an agent files. Relabel when the shape changes (`gh issue edit <N> -R Bparsons0904/lazyforge --remove-label sonnet --add-label opus`). Capture gets none.

### Category labels

Every ticket, Capture included, gets one: `feature`, `bug`, `spike`, `tech-debt`, `maintenance`, `docs`. Add a second only when it clearly is both.

## Mode C — Execute

### 1. Load the ticket

Skip when `ticket-kickoff` routed you here; it already loaded the ticket and judged readiness. Start at step 2.

Entering directly: read body and comments, then **assess readiness yourself** against these criteria, stated explicitly:

- Problem is stated.
- AC present: PM-stated, or derived from `docs/design.md` with a citation.
- Scope fits one MR.
- No open product question (CLAUDE.md → "Product questions").
- Technical decisions the plan depends on are settled in the ticket, an ADR or `docs/architecture.md`, or Opus can settle them now. An open technical decision never sends a ticket back to the PM.
- The plan survives a cold start: an agent with only this body could execute it.

Failing these is Mode B: run discovery. Never refuse and dead-end. A polished-looking ticket gets *more* scrutiny, not less.

### 2. Adversarial pre-implementation check

Before writing code:

- Restate the ticket in your own words. Name what's ambiguous or readable two ways.
- **Every claim in the body is a claim to check against current code**: file paths, "this currently does X", named functions, cited commits. Plans go stale between writing and pickup. Evidence-citing bodies read as more trustworthy than they are, and this step has the highest catch rate in the workflow.
- Verify your base first: `git fetch origin && git merge-base --is-ancestor origin/develop HEAD`. A stale base makes a correct ticket look wrong.
- If the ticket leans on a rule, test the rule, not just the call sites it names.
- **Library newer than your training?** Before coding, `go get` it in a scratch module, read its CHANGELOG and the source of every API the plan touches, and write the deltas into the ticket body. Code written from memory against an old API can still compile and still be wrong.
- If the fix is guarded by a *modified* existing test, add "revert the change, confirm the test goes red" to the work. A test nobody proved can fail is worth nothing.

If the ticket is wrong, say so **before** implementing.

### 3. Entry gates by type

Already done if `ticket-kickoff` routed you here. Entering directly:

- **bug:** run `labs:triangulate` first. Root cause must be demonstrated (a reproduction, or the code path plus why the alternatives aren't it) before any fix planning.
- **feature:** not through Discovery yet? Run it.
- **maintenance / docs / tech-debt:** usually straight to ready.

### 4. State the dispatch

> Dispatch: <what, to whom> · Inline: <what> — <why>

State it even when the answer is "all inline". Hand cold lookups ("how does this repo do X", precedent hunts) to a subagent. Doing them inline piles up the context that makes delegating feel unnecessary.

**If the shape changes** (one file becomes a refactor across packages, or scope passes 2x), this may no longer be an inline ticket. Stop and re-route rather than continue because you started.

### 5. Do the work

- Work in a worktree, never the main checkout: `git worktree add ../lazyforge-<N> -b <branch> origin/develop`, with `<branch>` matching `(feature|bugfix|chore|spike)/<N>-<slug>`.
- Follow `go-development`. Ticket work is normal work with a number attached.
- `labs:codex-implement` is available for multi-file edits (you brief and review, Codex types). Check readiness rather than assuming it, and say which backend is typing before you start.

### 6. Ship it

Hand off to `ship-it` **immediately, in the same turn, without asking**. There are no approval gates; from pickup through merge is the agent's job. "Should I open the MR?" and "Should I merge?" are gates CLAUDE.md says not to add. Stop mid-flight only to park a product question.

## Decomposition vs fixing in place

If work doesn't fit one well-scoped ticket, it isn't ready: break it down, unless the PM says proceed anyway.

The reverse also holds: a small fix found mid-task goes in the same diff, not into a new ticket. File a new ticket only when the fix touches a genuinely separate area, would itself exceed 2x the current scope, or needs a PM decision.

## Ticket body format

Propose a size; the PM can override.

- **Trivial** (one file, obvious): `## Problem` + `## Acceptance criteria`.
- **Standard:** `## Problem` / `## Acceptance criteria` (testable: "pressing `m` on a non-mergeable PR shows the reason", not "handles merge") / `## Plan` (approach + files) / `## Context` / `## Out of scope`.

**`## Context` carries the most weight.** Docs consulted and what they said, code refs as `file:line`, technical decisions and why. The body is the only brief a cold subagent gets; a thin Context shows up later as re-derived decisions.

**The plan lives in the body, never a comment.** Comments accumulate and a cold agent may read a superseded one. Comments are a progress and decision log.

Edit the body file you wrote with `gh issue edit <N> -R Bparsons0904/lazyforge --body-file`, never text scraped from `gh issue view` (it's rendered and loses blank lines). `test -s` the file first: an empty body would blank the ticket. `fj pr create` also takes `--body-file`; prefer it.

## Closing

Commits and the MR description carry `GH-<N>`. Never a bare `#N`: it links a Forgejo issue, and a closing keyword on the mirror would close the wrong GitHub issue.

Nothing closes a ticket automatically, because GitHub can't see Forgejo MRs. Close it by hand once the MR is merged and the AC are met: `gh issue close <N> -R Bparsons0904/lazyforge --comment "<summary>"`. If blocked or partial, comment and leave it open.

## Common commands

```bash
gh issue view <N> -R Bparsons0904/lazyforge --json body -q .body
gh issue view <N> -R Bparsons0904/lazyforge --comments
gh issue create -R Bparsons0904/lazyforge --title "title" --body-file plan.md
gh issue edit <N> -R Bparsons0904/lazyforge --body-file plan.md
gh issue edit <N> -R Bparsons0904/lazyforge --add-label opus
gh issue comment <N> -R Bparsons0904/lazyforge --body-file note.md
gh issue close <N> -R Bparsons0904/lazyforge --comment "text"
```
