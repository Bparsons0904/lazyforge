# 0002. Forge-agnostic core with per-forge adapters, one host per session

- Status: accepted
- Date: 2026-10-01
- Decided by: Opus (second opinion: none)

## Context

The PM wants lazyforge to work with Forgejo, Gitea, GitHub and GitLab without being tied to any one API. Users connect to one host at a time and pick it at startup. They never combine several hosts in one view.

## Decision

- lazyforge owns its domain model (`internal/domain`). Each forge family has an adapter (`internal/forge/<name>`) that converts its API to and from that model. No SDK type crosses the adapter boundary.
- Gitea and Forgejo share one adapter (`gitea`), with version checks where the two APIs diverge.
- Features that not every forge supports are optional Go interfaces, and capabilities are detected with type assertions. The UI hides whatever the active adapter lacks. It never limits every forge to the lowest common denominator.
- Each session uses exactly one host and one adapter, chosen by the host picker or `--host`. Capabilities and terminology ("PR" or "MR") are fixed for the session.
- Build order: the gitea adapter first, built and tested against the homelab instance. Then GitHub and GitLab are mapped onto the interface on paper before it is frozen. Then the GitHub adapter, then GitLab.

## Consequences

- The UI and the Renovate logic (★ view, grouping, dashboard ticks) are written once.
- There's no cross-host merging, mixed terminology, or per-repo capability checks.
- Every adapter must pass the shared contract suite (`internal/forge/forgetest`).
- The interface could still turn out to be the wrong shape for GitLab, which is furthest from the model. The paper-mapping step before freezing exists to catch that early.

## Alternatives

- A Forgejo-only client: rejected by the PM.
- A lowest-common-denominator interface with no capabilities: rejected, because every forge would be held back to what the weakest one supports.
- Several hosts in one session: rejected by the PM because it adds complexity without a matching use case.
