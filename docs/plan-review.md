# Project plan review

Reviewed: 2026-10-01

## Assessment

The product direction is strong: the cross-repo Renovate view gives lazyforge a clear purpose, and the separation between UI, core, and forge adapters is sensible. Resolve the behavior and interface gaps below before implementation.

This review covers the design, architecture, ADRs, mockup source, and current entry-point stub. These are plan findings rather than implementation defects.

## Findings

### 1. High: dependency groups do not represent everything a merge changes

The domain model holds one `RenovateUpdate` per PR and groups by `pkg + from + to`. Renovate supports multiple dependency updates in one PR ([Renovate documentation](https://docs.renovatebot.com/configuration-options/#groupname)). Selecting one dependency could therefore merge additional updates that the group does not show.

Model a collection of updates per PR, include ecosystem/package identity in grouping, deduplicate merge targets, and show the complete PR contents in confirmation. Keep ambiguous parsing out of automatic grouping.

References: [domain model](architecture.md#domain-model-sketch), [Renovate features](design.md#renovate-features).

### 2. High: bulk merge needs a failure and freshness contract

Confirmation and CI warnings are specified, but behavior after confirmation is not. The plan needs to cover a PR changing, three merges succeeding while two fail, and a request timing out after the server has merged it.

Define head-commit validation, per-target results, reconciliation before retry, and cancellation behavior. The confirmation should describe the exact targets being executed.

Reference: [merge behavior](design.md#keymap).

### 3. High: Comment and Close lose the target's resource type

Both methods accept only a repository and number. That leaves the adapter unable to distinguish an issue from a change request reliably. GitLab explicitly uses different issue and merge-request routes for comments ([GitLab Notes API](https://docs.gitlab.com/api/notes/)).

Use a typed resource reference or separate methods before freezing the interface.

Reference: [Forge interface](architecture.md#forge-interface-sketch).

### 4. Medium: implemented capability is treated as available capability

A Go type assertion establishes that the adapter has a method; it does not establish that the connected server version supports it or that the repository permits the action. This matters especially because one adapter covers both Gitea and Forgejo with version differences.

Separate adapter support from server availability and repository permissions, with understandable disabled-action reasons.

Reference: [capability detection](architecture.md#forge-interface-sketch).

### 5. Medium: the host-wide view needs explicit completeness semantics

“Every repo on the host” needs a defined boundary: accessible repositories, memberships, or a configured selection. Pagination, partial failures, and loading progress are unspecified. Without these, an incomplete scan can look like a complete dependency group.

Define progressive loading, bounded concurrency, and a visible “scanned X of Y repositories” state. Bulk confirmation should disclose incomplete coverage.

References: [host-wide view](design.md#boxes), [caching and refresh](architecture.md#caching-and-refresh).

### 6. Medium: v1 scope has conflicting interpretations

V1 includes the ★ Renovate view, whose fourth box is failing/running CI, while “boxes [4] and [5]” are deferred. Workflow rerun appears in the keymap but not the v1 action list. Actions is included despite API support remaining an unresolved spike.

Write an explicit v1 matrix for regular and virtual repositories, including what ships if the deployed forge lacks Actions support.

Reference: [v1 scope](design.md#v1-scope).

## Recommended build order

1. Run the API and Renovate parsing spikes.
2. Revise the domain model and interface using the findings, including the GitHub/GitLab paper mapping.
3. Build one complete path: connect → list repos → inspect PR → confirm merge → display result.
4. Add cross-repo grouping and partial-failure handling.
5. Complete the remaining v1 features.

## Smaller cleanup

- The architecture calls Go/Charm “not yet confirmed,” despite accepted [ADR 0001](adr/0001-go-and-charm.md).
- The update plan needs failure recovery and a concrete way to identify package-managed installations before implementation.

These recommendations are review notes, not approved changes to product scope or architecture.
