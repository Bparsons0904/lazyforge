# lazyforge Agent Guide

This file routes. Standards that a skill owns live in that skill, not here.

## What we're building

lazyforge is a keyboard-driven terminal UI for git forges (Forgejo/Gitea first, then GitHub and GitLab), with first-class Renovate support. It connects to one host per session.

| Topic | Source of truth |
|---|---|
| Product: UI model, boxes, keymap, Renovate features, v1 scope | `docs/design.md` |
| Engineering: layers, domain model, `Forge` interface, API mapping | `docs/architecture.md` |
| Why a technical decision was made | `docs/adr/` |
| What's being worked on | Forgejo issues on `deadstyle/lazyforge` |
| Clickable UI reference | `docs/mockup.html` |

## Roles

**The user is the product manager.** They own what gets built and why: scope, priority, user-visible behavior, the keymap, naming, and `docs/design.md`. They approve each ticket's plan before work starts. They do not make engineering decisions and should not be asked to.

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

Tickets are Forgejo issues on `deadstyle/lazyforge`, and the plan for a ticket lives in the ticket. Always pass `-H https://git.bobparsons.dev` to `fj`, because it can't use the `:2222` SSH remote as a host.

- **Opening or fleshing out a ticket:** `ticket-workflow`.
- **Picking up a ticket by number:** `ticket-kickoff`. It routes to **`labs:composer`** or **`labs:composer-lite`**, which are the default delivery path, or to inline work for single-domain tickets. Inline work closes out with `ship-it`. Composer stops once its MR is open, so the session that ran it then follows the merge rule below.
- **One gate:** the PM approves the plan or mode before code is written. Everything after that is the agent's job, through to merge. Don't ask permission to commit, open the MR, or merge.
- **Agents merge their own MRs.** A ticket is done when its MR is merged into `develop` and the ticket is closed, not when the MR is opened. Merge when all of these hold:
  1. The review passes have run: composer's review phases, or `ship-it`'s reviewers for inline work.
  2. CI is green on the MR's head commit. Check with `curl -s https://git.bobparsons.dev/api/v1/repos/deadstyle/lazyforge/commits/<sha>/status`; the `state` must be `success`.
  3. No open product question remains. A change to `docs/design.md` that the PM hasn't approved blocks the merge, so ask first.

  Then run `fj -H https://git.bobparsons.dev pr merge <PR> -M squash -d`, using the MR title as the squash title. If CI fails, fix it on the branch, push, and wait again. Never merge red.
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
| Commit format | `type(scope): subject` (`feat`, `fix`, `chore`, `docs`, `refactor`, `test`) and a `#<ticket>` reference in the body |

## Escalate to the PM

Stop and ask the PM when work would:

- change user-visible behavior, the keymap, or the box layout in a way `docs/design.md` doesn't already describe
- change scope: the ticket turns out to be wrong, or the fix has grown past 2x its scoped size
- change `docs/design.md`. Propose the change in the MR rather than deciding it.

Technical uncertainty is not a reason to ask the PM. It goes to Opus (see Roles).

When you ask, give the PM product-level options, each with its user-visible consequence, plus a recommendation. Ask once, then keep working on anything that doesn't depend on the answer.

## Evidence over assertion

Claiming "done", "fixed" or "passing" requires command output pasted from this session. Report a failing test as failing, never as "unrelated". Treat every agent's factual claim, including your own brief, as a claim until a command or the source backs it up.

## Where things live

```
cmd/lazyforge/          entry point: flags, config load, host pick, start the UI
internal/domain/        lazyforge's own types (Repo, ChangeRequest, Issue, Run…). No imports from other internal packages.
internal/forge/         Forge interface, optional capability interfaces, HostInfo
internal/forge/gitea/   Gitea + Forgejo adapter (github/, gitlab/ later)
internal/core/          cache, refresh, Renovate logic (★ view, grouping, dashboard ticks)
internal/config/        config file + token_cmd
internal/ui/            Bubble Tea models and Lip Gloss styles
```

The dependency direction is `ui → core → forge → domain`. Only `cmd/` knows about concrete adapters. `go-development` owns the detail.
