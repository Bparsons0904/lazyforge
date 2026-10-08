This PR contains the following updates:

| Package | Type | Update | Change |
|---|---|---|---|
| [actions/checkout](https://github.com/actions/checkout) | action | major | `v3` → `v7` |

---

### Release Notes

<details>
<summary>actions/checkout (actions/checkout)</summary>

### [`v7.0.1`](https://github.com/actions/checkout/blob/HEAD/CHANGELOG.md#v701)

[Compare Source](https://github.com/actions/checkout/compare/v7.0.0...v7.0.1)

- Skip running unsafe pr check if input is default by [@&#8203;aiqiaoy](https://github.com/aiqiaoy) in [#&#8203;2518](https://github.com/actions/checkout/pull/2518)
- Trim only ascii whitespace for branch by [@&#8203;aiqiaoy](https://github.com/aiqiaoy) in [#&#8203;2521](https://github.com/actions/checkout/pull/2521)
- Escape values passed to --unset by [@&#8203;aiqiaoy](https://github.com/aiqiaoy) in [#&#8203;2530](https://github.com/actions/checkout/pull/2530)
- Various dependency updates

### [`v7.0.0`](https://github.com/actions/checkout/blob/HEAD/CHANGELOG.md#v700)

[Compare Source](https://github.com/actions/checkout/compare/v6.1.0...v7.0.0)

- Block checking out fork PR for pull\_request\_target and workflow\_run by [@&#8203;aiqiaoy](https://github.com/aiqiaoy) in [#&#8203;2454](https://github.com/actions/checkout/pull/2454)
- Various dependency updates

### [`v6.1.0`](https://github.com/actions/checkout/releases/tag/v6.1.0)

[Compare Source](https://github.com/actions/checkout/compare/v6.0.3...v6.1.0)

#### What's Changed

- **\[BREAKING]** backport `allow-unsafe-pr-checkout` to v6 by [@&#8203;aiqiaoy](https://github.com/aiqiaoy) in [#&#8203;2500](https://github.com/actions/checkout/pull/2500)
- backport fixes to releases-v6 by [@&#8203;aiqiaoy](https://github.com/aiqiaoy) in [#&#8203;2527](https://github.com/actions/checkout/pull/2527)

<https://github.blog/changelog/2026-06-18-safer-pull_request_target-defaults-for-github-actions-checkout/> for more details about this breaking change

**Full Changelog**: <https://github.com/actions/checkout/compare/v6.0.3...v6.1.0>

### [`v6.0.3`](https://github.com/actions/checkout/blob/HEAD/CHANGELOG.md#v603)

[Compare Source](https://github.com/actions/checkout/compare/v6.0.2...v6.0.3)

- Fix checkout init for SHA-256 repositories by [@&#8203;yaananth](https://github.com/yaananth) in [#&#8203;2439](https://github.com/actions/checkout/pull/2439)
- fix: expand merge commit SHA regex and add SHA-256 test cases by [@&#8203;yaananth](https://github.com/yaananth) in [#&#8203;2414](https://github.com/actions/checkout/pull/2414)

### [`v6.0.2`](https://github.com/actions/checkout/blob/HEAD/CHANGELOG.md#v602)

[Compare Source](https://github.com/actions/checkout/compare/v6.0.1...v6.0.2)

- Fix tag handling: preserve annotations and explicit fetch-tags by [@&#8203;ericsciple](https://github.com/ericsciple) in [#&#8203;2356](https://github.com/actions/checkout/pull/2356)

### [`v6.0.1`](https://github.com/actions/checkout/blob/HEAD/CHANGELOG.md#v601)

[Compare Source](https://github.com/actions/checkout/compare/v6.0.0...v6.0.1)

- Add worktree support for persist-credentials includeIf by [@&#8203;ericsciple](https://github.com/ericsciple) in [#&#8203;2327](https://github.com/actions/checkout/pull/2327)

### [`v6.0.0`](https://github.com/actions/checkout/blob/HEAD/CHANGELOG.md#v600)

[Compare Source](https://github.com/actions/checkout/compare/v5.1.0...v6.0.0)

- Persist creds to a separate file by [@&#8203;ericsciple](https://github.com/ericsciple) in [#&#8203;2286](https://github.com/actions/checkout/pull/2286)
- Update README to include Node.js 24 support details and requirements by [@&#8203;salmanmkc](https://github.com/salmanmkc) in [#&#8203;2248](https://github.com/actions/checkout/pull/2248)

### [`v5.1.0`](https://github.com/actions/checkout/releases/tag/v5.1.0)

[Compare Source](https://github.com/actions/checkout/compare/v5.0.1...v5.1.0)

#### What's Changed

- **\[BREAKING]** backport `allow-unsafe-pr-checkout` to v5 by [@&#8203;aiqiaoy](https://github.com/aiqiaoy) in [#&#8203;2501](https://github.com/actions/checkout/pull/2501)
- backport fixes to releases-v5 by [@&#8203;aiqiaoy](https://github.com/aiqiaoy) in [#&#8203;2523](https://github.com/actions/checkout/pull/2523)

<https://github.blog/changelog/2026-06-18-safer-pull_request_target-defaults-for-github-actions-checkout/> for more details about this breaking change

**Full Changelog**: <https://github.com/actions/checkout/compare/v5.0.1...v5.1.0>

### [`v5.0.1`](https://github.com/actions/checkout/blob/HEAD/CHANGELOG.md#v501)

[Compare Source](https://github.com/actions/checkout/compare/v5.0.0...v5.0.1)

- Port v6 cleanup to v5 by [@&#8203;ericsciple](https://github.com/ericsciple) in [#&#8203;2301](https://github.com/actions/checkout/pull/2301)

### [`v5.0.0`](https://github.com/actions/checkout/blob/HEAD/CHANGELOG.md#v500)

[Compare Source](https://github.com/actions/checkout/compare/v4.4.0...v5.0.0)

- Update actions checkout to use node 24 by [@&#8203;salmanmkc](https://github.com/salmanmkc) in [#&#8203;2226](https://github.com/actions/checkout/pull/2226)

### [`v4.4.0`](https://github.com/actions/checkout/releases/tag/v4.4.0)

[Compare Source](https://github.com/actions/checkout/compare/v4.3.1...v4.4.0)

#### What's Changed

- **\[BREAKING]** backport `allow-unsafe-pr-checkout` to v4 by [@&#8203;aiqiaoy](https://github.com/aiqiaoy) in [#&#8203;2502](https://github.com/actions/checkout/pull/2502)
- backport fixes to releases-v4 by [@&#8203;aiqiaoy](https://github.com/aiqiaoy) in [#&#8203;2524](https://github.com/actions/checkout/pull/2524)

<https://github.blog/changelog/2026-06-18-safer-pull_request_target-defaults-for-github-actions-checkout/> for more details about this breaking change

**Full Changelog**: <https://github.com/actions/checkout/compare/v4.3.1...v4.4.0>

### [`v4.3.1`](https://github.com/actions/checkout/blob/HEAD/CHANGELOG.md#v431)

[Compare Source](https://github.com/actions/checkout/compare/v4.3.0...v4.3.1)

- Port v6 cleanup to v4 by [@&#8203;ericsciple](https://github.com/ericsciple) in [#&#8203;2305](https://github.com/actions/checkout/pull/2305)

### [`v4.3.0`](https://github.com/actions/checkout/blob/HEAD/CHANGELOG.md#v430)

[Compare Source](https://github.com/actions/checkout/compare/v4.2.2...v4.3.0)

- docs: update README.md by [@&#8203;motss](https://github.com/motss) in [#&#8203;1971](https://github.com/actions/checkout/pull/1971)
- Add internal repos for checking out multiple repositories by [@&#8203;mouismail](https://github.com/mouismail) in [#&#8203;1977](https://github.com/actions/checkout/pull/1977)
- Documentation update - add recommended permissions to Readme by [@&#8203;benwells](https://github.com/benwells) in [#&#8203;2043](https://github.com/actions/checkout/pull/2043)
- Adjust positioning of user email note and permissions heading by [@&#8203;joshmgross](https://github.com/joshmgross) in [#&#8203;2044](https://github.com/actions/checkout/pull/2044)
- Update README.md by [@&#8203;nebuk89](https://github.com/nebuk89) in [#&#8203;2194](https://github.com/actions/checkout/pull/2194)
- Update CODEOWNERS for actions by [@&#8203;TingluoHuang](https://github.com/TingluoHuang) in [#&#8203;2224](https://github.com/actions/checkout/pull/2224)
- Update package dependencies by [@&#8203;salmanmkc](https://github.com/salmanmkc) in [#&#8203;2236](https://github.com/actions/checkout/pull/2236)

### [`v4.2.2`](https://github.com/actions/checkout/blob/HEAD/CHANGELOG.md#v422)

[Compare Source](https://github.com/actions/checkout/compare/v4.2.1...v4.2.2)

- `url-helper.ts` now leverages well-known environment variables by [@&#8203;jww3](https://github.com/jww3) in [#&#8203;1941](https://github.com/actions/checkout/pull/1941)
- Expand unit test coverage for `isGhes` by [@&#8203;jww3](https://github.com/jww3) in [#&#8203;1946](https://github.com/actions/checkout/pull/1946)

### [`v4.2.1`](https://github.com/actions/checkout/blob/HEAD/CHANGELOG.md#v421)

[Compare Source](https://github.com/actions/checkout/compare/v4.2.0...v4.2.1)

- Check out other refs/\* by commit if provided, fall back to ref by [@&#8203;orhantoy](https://github.com/orhantoy) in [#&#8203;1924](https://github.com/actions/checkout/pull/1924)

### [`v4.2.0`](https://github.com/actions/checkout/blob/HEAD/CHANGELOG.md#v420)

[Compare Source](https://github.com/actions/checkout/compare/v4.1.7...v4.2.0)

- Add Ref and Commit outputs by [@&#8203;lucacome](https://github.com/lucacome) in [#&#8203;1180](https://github.com/actions/checkout/pull/1180)
- Dependency updates by [@&#8203;dependabot-](https://github.com/dependabot-) [#&#8203;1777](https://github.com/actions/checkout/pull/1777), [#&#8203;1872](https://github.com/actions/checkout/pull/1872)

### [`v4.1.7`](https://github.com/actions/checkout/blob/HEAD/CHANGELOG.md#v417)

[Compare Source](https://github.com/actions/checkout/compare/v4.1.6...v4.1.7)

- Bump the minor-npm-dependencies group across 1 directory with 4 updates by [@&#8203;dependabot](https://github.com/dependabot) in [#&#8203;1739](https://github.com/actions/checkout/pull/1739)
- Bump actions/checkout from 3 to 4 by [@&#8203;dependabot](https://github.com/dependabot) in [#&#8203;1697](https://github.com/actions/checkout/pull/1697)
- Check out other refs/\* by commit by [@&#8203;orhantoy](https://github.com/orhantoy) in [#&#8203;1774](https://github.com/actions/checkout/pull/1774)
- Pin actions/checkout's own workflows to a known, good, stable version. by [@&#8203;jww3](https://github.com/jww3) in [#&#8203;1776](https://github.com/actions/checkout/pull/1776)

### [`v4.1.6`](https://github.com/actions/checkout/blob/HEAD/CHANGELOG.md#v416)

[Compare Source](https://github.com/actions/checkout/compare/v4.1.5...v4.1.6)

- Check platform to set archive extension appropriately by [@&#8203;cory-miller](https://github.com/cory-miller) in [#&#8203;1732](https://github.com/actions/checkout/pull/1732)

### [`v4.1.5`](https://github.com/actions/checkout/blob/HEAD/CHANGELOG.md#v415)

[Compare Source](https://github.com/actions/checkout/compare/v4.1.4...v4.1.5)

- Update NPM dependencies by [@&#8203;cory-miller](https://github.com/cory-miller) in [#&#8203;1703](https://github.com/actions/checkout/pull/1703)
- Bump github/codeql-action from 2 to 3 by [@&#8203;dependabot](https://github.com/dependabot) in [#&#8203;1694](https://github.com/actions/checkout/pull/1694)
- Bump actions/setup-node from 1 to 4 by [@&#8203;dependabot](https://github.com/dependabot) in [#&#8203;1696](https://github.com/actions/checkout/pull/1696)
- Bump actions/upload-artifact from 2 to 4 by [@&#8203;dependabot](https://github.com/dependabot) in [#&#8203;1695](https://github.com/actions/checkout/pull/1695)
- README: Suggest `user.email` to be `41898282+github-actions[bot]@users.noreply.github.com` by [@&#8203;cory-miller](https://github.com/cory-miller) in [#&#8203;1707](https://github.com/actions/checkout/pull/1707)

### [`v4.1.4`](https://github.com/actions/checkout/blob/HEAD/CHANGELOG.md#v414)

[Compare Source](https://github.com/actions/checkout/compare/v4.1.3...v4.1.4)

- Disable `extensions.worktreeConfig` when disabling `sparse-checkout` by [@&#8203;jww3](https://github.com/jww3) in [#&#8203;1692](https://github.com/actions/checkout/pull/1692)
- Add dependabot config by [@&#8203;cory-miller](https://github.com/cory-miller) in [#&#8203;1688](https://github.com/actions/checkout/pull/1688)
- Bump the minor-actions-dependencies group with 2 updates by [@&#8203;dependabot](https://github.com/dependabot) in [#&#8203;1693](https://github.com/actions/checkout/pull/1693)
- Bump word-wrap from 1.2.3 to 1.2.5 by [@&#8203;dependabot](https://github.com/dependabot) in [#&#8203;1643](https://github.com/actions/checkout/pull/1643)

### [`v4.1.3`](https://github.com/actions/checkout/blob/HEAD/CHANGELOG.md#v413)

[Compare Source](https://github.com/actions/checkout/compare/v4.1.2...v4.1.3)

- Check git version before attempting to disable `sparse-checkout` by [@&#8203;jww3](https://github.com/jww3) in [#&#8203;1656](https://github.com/actions/checkout/pull/1656)
- Add SSH user parameter by [@&#8203;cory-miller](https://github.com/cory-miller) in [#&#8203;1685](https://github.com/actions/checkout/pull/1685)
- Update `actions/checkout` version in `update-main-version.yml` by [@&#8203;jww3](https://github.com/jww3) in [#&#8203;1650](https://github.com/actions/checkout/pull/1650)

### [`v4.1.2`](https://github.com/actions/checkout/blob/HEAD/CHANGELOG.md#v412)

[Compare Source](https://github.com/actions/checkout/compare/v4.1.1...v4.1.2)

- Fix: Disable sparse checkout whenever `sparse-checkout` option is not present [@&#8203;dscho](https://github.com/dscho) in [#&#8203;1598](https://github.com/actions/checkout/pull/1598)

### [`v4.1.1`](https://github.com/actions/checkout/blob/HEAD/CHANGELOG.md#v411)

[Compare Source](https://github.com/actions/checkout/compare/v4.1.0...v4.1.1)

- Correct link to GitHub Docs by [@&#8203;peterbe](https://github.com/peterbe) in [#&#8203;1511](https://github.com/actions/checkout/pull/1511)
- Link to release page from what's new section by [@&#8203;cory-miller](https://github.com/cory-miller) in [#&#8203;1514](https://github.com/actions/checkout/pull/1514)

### [`v4.1.0`](https://github.com/actions/checkout/blob/HEAD/CHANGELOG.md#v410)

[Compare Source](https://github.com/actions/checkout/compare/v4.0.0...v4.1.0)

- [Add support for partial checkout filters](https://github.com/actions/checkout/pull/1396)

### [`v4.0.0`](https://github.com/actions/checkout/blob/HEAD/CHANGELOG.md#v400)

[Compare Source](https://github.com/actions/checkout/compare/v3.7.0...v4.0.0)

- [Support fetching without the --progress option](https://github.com/actions/checkout/pull/1067)
- [Update to node20](https://github.com/actions/checkout/pull/1436)

</details>

---

### Configuration

📅 **Schedule**: (in timezone America/Chicago)

- Branch creation
  - At any time (no schedule defined)
- Automerge
  - At any time (no schedule defined)

🚦 **Automerge**: Disabled by config. Please merge this manually once you are satisfied.

♻ **Rebasing**: Whenever PR becomes conflicted, or you tick the rebase/retry checkbox.

🔕 **Ignore**: Close this PR and you won't be reminded about this update again.

---

 - [ ] <!-- rebase-check -->If you want to rebase/retry this PR, check this box

---

This PR has been generated by [Mend Renovate CLI](https://github.com/renovatebot/renovate).
<!--renovate-debug:eyJjcmVhdGVkSW5WZXIiOiI0NC4xMTUuMTMiLCJ1cGRhdGVkSW5WZXIiOiI0NC4xMTUuMTMiLCJ0YXJnZXRCcmFuY2giOiJtYWluIiwibGFiZWxzIjpbIm1haW50ZW5hbmNlIl19-->
