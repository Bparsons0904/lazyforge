# Renovate PR body fixtures

Raw PR and issue bodies from `git.bobparsons.dev`, collected 2026-10-02 for #2. Every
file is the API `body` field byte for byte. `prs.json` indexes them (repo, number,
title, head branch, labels, state, category, file).

Renovate runs as user `renovate-bot`, version 44.115.13 (from the debug blob). The
instance had 17 `renovate-bot` PRs, all closed (16 merged, `adventure#2` closed
unmerged), plus 13 Dependency Dashboard issues. The ten `actions/checkout` v4 → v7
PRs (home, infisical, jellyfin, karakeep, notes, ntfy, postgres, traefik, warden,
watchlist, all `#2`) have byte-identical bodies, so they share
`github-actions-major_home_2.md`.

## Coverage

| Category | Fixture(s) |
|---|---|
| docker-compose, single-package major | `docker-compose-major_adventure_2.md`, `docker-compose-major_forgejo_8.md`, `docker-compose-major_forgejo_10.md` |
| github-actions (Forgejo workflows), major | `github-actions-major_home_2.md` (×10), `github-actions-major_forgejo_6.md`, `github-actions-major_forgejo_7.md` |
| non-major group branch, one member | `docker-group-nonmajor_forgejo_13.md` |
| two rows, two managers, one PR | `multi-manager-major_forgejo_9.md` (release notes trimmed from 793 KB; table and debug comment intact) |
| Dependency Dashboard | `dashboard_traefik_3.md`, `dashboard_adventure_3.md`, `dashboard_jellyfin_3.md` |

No PR exists on the instance for: gomod, terraform/opentofu, npm, custom.regex, a
batched multi-package non-major, a digest or pin update, or a single minor. Partial
evidence only from dashboards:

- Multi-package non-major group: `dashboard_traefik_3.md` lists the pending title
  ``Update Docker images non-major (`cloudflare/cloudflared`, `favonia/cloudflare-ddns`, `traefik`)``.
- custom.regex: `dashboard_jellyfin_3.md` shows the manager as `regex`, detecting
  `jellyfin/jellyfin 12.0` in `docker-compose.yml`, the same dependency the
  `dockerfile` manager finds in `Dockerfile`.
- gomod: only the lazyforge dashboard (not saved) lists `go 1.27.1` under `gomod`.

The digest/pin rendering is therefore unverified; a parser must not assume a shape for it.

## The update table

Each PR body opens with `This PR contains the following updates:`, then a blank
line, then one Markdown table, then `---`. The table is the only structured data.
Release notes (when present) follow in `<details>` and can contain arbitrary
Markdown, so a parser takes the first table after that intro line and stops at the
first non-table line.

Two header shapes occur:

| Header | Seen in |
|---|---|
| `Package \| Update \| Change` | every docker-compose PR (`adventure_2`, `forgejo_8`, `forgejo_10`, `forgejo_13`) |
| `Package \| Type \| Update \| Change` | github-actions PRs (`Type` = `action`), and `forgejo_9` (`uses-with`, `final`) |

The `Type` column appears when any row has a dependency type. It is the
manager-specific depType, not the manager name: `final` in `forgejo_9` is a
Dockerfile `FROM` stage.

Cells:

- **Package**: `[name](url)` or `[name](url) ([source](url))`. The first URL is the
  homepage and does not identify the package: `forgejo_8` and `forgejo_13` both
  link `https://forgejo.org` for two different images. The name is the depName.
- **Update**: `major` or `patch` here (Renovate also emits `minor`, `digest`, `pin`, …).
- **Change**: `` `from` → `to` `` with U+2192. Versions are raw tags: `v4`, `16-3.5`,
  `15.19-alpine`, `22-bookworm`.

Multi-row: `forgejo_9` has two rows both named `node` — one from a workflow
`uses-with` (homepage `actions/node-versions`), one from a Dockerfile (homepage
`hub.docker.com/_/node`). Same name, different ecosystems. Its footer also switches to
"won't be reminded about these updates", the only wording change for multiple rows.

## Hidden metadata

- `<!--renovate-debug:BASE64-->` on the last line of every PR body. Decoded, every
  one is
  `{"createdInVer":"44.115.13","updatedInVer":"44.115.13","targetBranch":"develop"|"main","labels":["maintenance"]}`.
  No manager, datasource, package or version, so it cannot replace table parsing.
  `labels` is the configured set, not the applied set: the forgejo PRs carry no labels.
- `<!-- rebase-check -->` on the rebase checkbox, in every PR.
- Release notes carry upstream comments that are not Renovate's:
  `docker-compose-major_forgejo_8.md` embeds Forgejo's release-notes-assistant markup
  (`<!--number 1722 --><!--description BASE64-->`, which decodes to upstream PR
  titles). Never grep comments from the whole body.

## Title and branch

- Title style depends on the repo's commit convention. forgejo uses semantic:
  `chore(deps): update postgres docker tag to v18`, `chore(deps): update dependency node to v24`.
  The rest use `Update postgis/postgis Docker tag to v17`,
  `Update actions/checkout action to v7`.
- Majors branch as `renovate/<depName slug>-<major>.x`: `renovate/postgis-postgis-17.x`,
  `renovate/data.forgejo.org-forgejo-runner-13.x`, `renovate/node-24.x`. The slug
  replaces `/` with `-`, so it cannot be reversed to a depName reliably.
- Groups branch as `renovate/<groupName slug>`: `renovate/docker-images-non-major`,
  `renovate/ci-tools-non-major`. A group with one member gets the single-update
  title (`forgejo_13`), so the branch, not the title, says it is a group.
- Closed PRs report `head.ref` as `refs/pull/<n>/head` once the branch is deleted.
  The branch name survives only in `head.label` (see `prs.json` `head_branch` vs `head_ref`).

## Dependency Dashboard

Sections seen: `Awaiting Schedule` (traefik, jellyfin), `PR Closed (Blocked)`
(adventure), and the sentence `This repository currently has no open or pending branches.`
Entries are `- [ ] <!-- unschedule-branch=renovate/... -->title` or
`- [ ] <!-- recreate-branch=renovate/... -->[title](pulls/2)` (relative link).
`## Detected Dependencies` is nested `<details>`: manager `(count)` → file `(count)` →
``- `dep version` `` with an optional `` → [Updates: `x`] ``.

## Parsing approach

Parse the first table after the intro line; map cells by header name, not position;
reject the body (fall back to the PR title, row still shown) if the intro is missing,
a row's width differs from the header, a column name is unknown, or a Change cell
lacks the `→` form. A scratch prototype parsed every PR fixture here and rejected
every dashboard.

Cross-repo grouping key per row: ecosystem + package + to-version + update type. From
differs per repo (`actions/checkout` is v3 → v7 in `forgejo_6`, v4 → v7 in `home_2`)
and stays out of the key. Ecosystem is not in the body; infer it from `Type`
(`action`, `uses-with` → github-actions; `final` → docker) or the title noun
(`Docker tag`, `action`). Rows whose ecosystem stays unknown (title `dependency X`
with no `Type`, as `forgejo_9` would be without its Type column) stay out of
automatic grouping.

## Hand-written

Not captured from a live instance. The instance had no PR with these shapes (see Coverage), so these follow Renovate's documented body format and exist to exercise the parser.

| File | Shape |
|---|---|
| `batched-gomod-nonmajor.md` | three rows, `Package \| Type \| Update \| Change`, Type `require`, minor and patch |
| `digest-docker.md` | one row, Update `digest`, backticked short SHAs |
| `confidence-columns.md` | `Package \| Update \| Change` plus the `Age`, `Adoption`, `Passing` and `Confidence` badge columns |
| `rejected-unknown-column.md` | an extra `Foo` column; the parser must reject the body |
| `rejected-no-arrow.md` | a Change cell without `→`; the parser must reject the body |
