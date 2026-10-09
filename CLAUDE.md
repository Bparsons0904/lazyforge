# lazyforge Agent Guide

This file routes. Standards that a skill owns live in that skill, not here.

## What we're building

lazyforge is a keyboard-driven terminal UI for git forges (Forgejo/Gitea first, then GitHub and GitLab), with first-class Renovate support. It connects to one host per session.

| Topic | Source of truth |
|---|---|
| Product: UI model, boxes, keymap, Renovate features, v1 scope | `docs/design.md` |
| Engineering: layers, domain model, `Forge` interface, API mapping | `docs/architecture.md` |
| Why a technical decision was made | `docs/adr/` |
| What's being worked on | Forgejo issues on `deadstyle/lazyforge` (the old GitHub tickets GH-1 to GH-6 were carried over; earlier commits cite the GH numbers) |
| Clickable UI reference | `docs/mockup.html` |

## Roles

**The user is the product manager.** They own what gets built and why: scope, priority, user-visible behavior, the keymap, naming, and `docs/design.md`. They set direction through tickets and answer product questions. They don't approve plans or merges, and they don't make engineering decisions, so don't ask them to.

**Claude Opus is the engineering lead.** Opus owns every technical decision inside one fixed constraint: **Go and the Charm stack** (Bubble Tea, Lip Gloss, Bubbles), which is the PM's call. Opus owns `docs/architecture.md` and `docs/adr/`.

**Claude Sonnet consults Opus before making a technical decision.** If a Sonnet session hits a technical decision that CLAUDE.md, a skill, or an ADR doesn't already settle, it stops and asks Opus through the `engineering-decision` skill, then follows the answer. Sonnet doesn't make the call itself and doesn't take it to the PM.

**Opus may ask Fable for a second opinion** on high-stakes or uncertain calls. The `engineering-decision` skill covers how. Fable advises and Opus decides.

What counts as a technical decision:

- adding a dependency
- package boundaries
- interface or domain-model shape
- concurrency approach
- error-handling strategy
- test approach
- CI and tooling changes
- anything that would change `docs/architecture.md`

Following a pattern that's already established is not a technical decision.

**Talking to the PM:** `.claude/skills/product-voice` sets the voice. It loads automatically at session start, so nothing here restates it. Take only product questions to the PM, and settle engineering questions internally.

## How we work

Tickets, MRs and CI all live on Forgejo, and the plan for a ticket lives in the ticket. `fj` always needs `-H https://git.bobparsons.dev` because it can't use the `:2222` SSH remote as a host. Don't use `gh` for tickets.

- **Opening or fleshing out a ticket:** `ticket-workflow`.
- **Picking up a ticket by number:** `ticket-kickoff`. It routes to **`labs:composer`** or **`labs:composer-lite`**, which are the default delivery path, or to inline work for single-domain tickets. Inline work closes out with `ship-it`. Composer stops once its MR is open, so the session that ran it then follows the merge rule below.
- **A session works the queue.** When the PM starts a session without a specific instruction, run the `work-queue` skill: pick the next open ticket, deliver it, merge it, and repeat. There are no approval gates. Don't ask permission to start a ticket, commit, open the MR, or merge. When composer presents a plan for approval, the session approves it as engineering lead.
- **Product questions park a ticket, not the session.** Post the question on the ticket, label it `needs-pm`, and move on to the next ticket (`ticket-workflow` → Parking a ticket). Every comment an agent posts starts with `🤖 `, so the PM's replies stand out.
- **Agents merge their own MRs.** A ticket is done when its MR is merged into `develop` and the ticket is closed, not when the MR is opened. Merge when all of these hold:
  1. The review passes have run: composer's review phases, or `ship-it`'s reviewers for inline work.
  2. CI is green on the MR's head commit. Check with `curl -s https://git.bobparsons.dev/api/v1/repos/deadstyle/lazyforge/commits/<sha>/status`; the `state` must be `success`.
  3. No open product question remains on the ticket.

  Then run `fj -H https://git.bobparsons.dev pr merge <PR> -M squash -d`, using the MR title as the squash title, and close the ticket by hand with `fj -H https://git.bobparsons.dev issue close <N> -w "🤖 merged: <MR url>"`. Merging doesn't close the ticket by itself, because commits carry only `Refs #<N>`. If CI fails, fix it on the branch, push, and wait again. Never merge red.
- **Claude cloud works from GitHub.** A cloud session can't read Forgejo tickets, so the PM pastes it the brief. It pushes to a `claude/<ticket>-<slug>` branch and never merges. The `github-pull` workflow imports the branch as a Forgejo MR within 10 minutes and copies the dispatched CI result onto it as the `ci / check (dispatch)` status. Whoever merges it follows the merge rule above.
- **Never push directly to `develop` or `main`.** Every change lands through an MR. Releases (`develop` → `main`, plus a tag) are the PM's call.
- **After an MR is open:** push every follow-up commit to its branch as soon as it's made.
- **Worktrees:** ticket work happens in `git worktree add ../lazyforge-<ticket> -b <branch> origin/develop`, never in the main checkout.

### Repo facts for composer and other skills

| Key | Value |
|---|---|
| `forge_cli` | `fj -H https://git.bobparsons.dev` (Forgejo). Create an MR with `fj pr create --base develop`. |
| MR target | `develop`. `main` is release-only. |
| `branch_pattern` | `(feature\|bugfix\|chore\|spike)/<ticket>-<slug>` |
| `prebuild_command` | none |
| Build / test / lint | `make build` · `make test` · `make lint` · `make check` runs all three plus a format check |
| `project_conventions` | invoke skill `go-development`, and read this file |
| `comments_convention` | invoke skill `comment-standard` |
| Commit format | `type(scope): subject` (`feat`, `fix`, `chore`, `docs`, `refactor`, `test`) and a `Refs #<ticket>` line in the body. Never a closing keyword (`Closes`, `Fixes`): agents close tickets by hand after the merge. |

## Product questions

A ticket needs the PM when the work would:

- add or change user-visible behavior, the keymap, or the box layout in a way `docs/design.md` doesn't describe
- change scope in a way only the PM can choose (for example, two viable directions that lead to different products)

Park that ticket: comment with the options, each with its user-visible consequence, plus a recommendation, and label it `needs-pm`. Then move on. `docs/design.md` stays the PM's: agents update it to record the PM's answers and to keep it accurate, but never to invent new product behavior.

Technical uncertainty is never a product question. It goes to Opus (see Roles). A ticket that's merely wrong, or has grown past 2x its scope, gets split or corrected by the agent with evidence in a 🤖 comment.

## Evidence over assertion

Claiming "done", "fixed" or "passing" requires command output pasted from this session. Report a failing test as failing, never as "unrelated". Treat every agent's factual claim, including your own brief, as a claim until a command or the source backs it up.

## Where things live

```
cmd/lazyforge/          entry point: flags, config load, host pick, start the UI
internal/domain/        lazyforge's own types (Repo, ChangeRequest, Issue, Run…). No imports from other internal packages.
internal/forge/         Forge interface, optional capability interfaces, HostInfo
internal/forge/gitea/   Gitea + Forgejo adapter
internal/forge/github/  GitHub adapter (github.com and GHES); gitlab/ later
internal/core/          cache, refresh, Renovate logic (★ view, grouping, dashboard ticks)
internal/config/        config file + token_cmd
internal/ui/            Bubble Tea models and Lip Gloss styles
internal/ui/markdown/   markdown bodies to styled, width-fitted text (goldmark)
internal/ui/termimg/    kitty graphics escape sequences, image IDs, terminal detection
```

The dependency direction is `ui → core → forge → domain`. Only `cmd/` knows about concrete adapters. `go-development` owns the detail.
