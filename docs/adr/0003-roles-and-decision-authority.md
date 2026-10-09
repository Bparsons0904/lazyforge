# 0003. Roles and decision authority

- Status: accepted
- Date: 2026-10-01
- Decided by: PM

## Context

Most of the work on lazyforge is done by Claude sessions running different models. Without a clear owner for technical decisions, they drift: each session makes its own call, or pushes the call to the PM, who doesn't make engineering decisions.

## Decision

- **PM (the user):** owns what gets built and why: scope, priority, user-visible behavior, keymap, naming, and `docs/design.md`. Sets direction through tickets, answers product questions (asked on the ticket with the `needs-pm` label), and decides when to release. Approves neither plans nor merges.
- **Agents work the queue autonomously.** A session picks up open tickets (the `work-queue` skill), delivers them and merges them into `develop` once review has passed and CI is green. There are no approval gates. A product question parks only that ticket. (This differs from kirria, where the PM approves plans and merges.)
- **Acceptance criteria** are either stated by the PM or derived by Opus from `docs/design.md`, citing the section.
- **Opus:** the engineering lead. Owns every technical decision within ADR 0001, plus `docs/architecture.md` and `docs/adr/`.
- **Sonnet:** implements, and consults Opus on any technical decision that isn't already settled. It never makes that decision itself and never takes it to the PM.
- **Fable:** Opus can ask it for a second opinion on hard-to-reverse or uncertain calls. Fable advises and Opus decides.
- The process is in the `engineering-decision` skill.

## Consequences

- Every technical decision has a single owner and leaves a record, either an ADR or a "Decisions" line in the MR.
- Questions the PM receives are product questions, framed by their user-visible consequence.
- A Sonnet session spends an extra agent dispatch whenever it hits an unsettled technical decision.
