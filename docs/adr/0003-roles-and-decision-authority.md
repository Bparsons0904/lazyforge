# 0003. Roles and decision authority

- Status: accepted
- Date: 2026-10-01
- Decided by: PM

## Context

Most of the work on lazyforge is done by Claude sessions running different models. Without a clear owner for technical decisions, they drift: each session makes its own call, or pushes the call to the PM, who doesn't make engineering decisions.

## Decision

- **PM (the user):** owns what gets built and why: scope, priority, user-visible behavior, keymap, naming, and `docs/design.md`. Approves each ticket's plan before work starts, and decides when to release.
- **Agents merge their own MRs** into `develop` once review has passed and CI is green. Plan approval is the PM's only gate on a ticket. (This differs from kirria, where the PM merges.)
- **Opus:** the engineering lead. Owns every technical decision within ADR 0001, plus `docs/architecture.md` and `docs/adr/`.
- **Sonnet:** implements, and consults Opus on any technical decision that isn't already settled. It never makes that decision itself and never takes it to the PM.
- **Fable:** Opus can ask it for a second opinion on hard-to-reverse or uncertain calls. Fable advises and Opus decides.
- The process is in the `engineering-decision` skill.

## Consequences

- Every technical decision has a single owner and leaves a record, either an ADR or a "Decisions" line in the MR.
- Questions the PM receives are product questions, framed by their user-visible consequence.
- A Sonnet session spends an extra agent dispatch whenever it hits an unsettled technical decision.
