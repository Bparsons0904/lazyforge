This PR contains the following updates:

| Package | Type | Update | Change |
|---|---|---|---|
| [actions/setup-node](https://github.com/actions/setup-node) | action | major | `v3` → `v7` |

---

### Release Notes

<details>
<summary>actions/setup-node (actions/setup-node)</summary>

### [`v7.0.0`](https://github.com/actions/setup-node/releases/tag/v7.0.0)

[Compare Source](https://github.com/actions/setup-node/compare/v6.5.0...v7.0.0)

#### What's Changed

##### Enhancements:

- Add cache-primary-key and cache-matched-key as outputs by [@&#8203;gowridurgad](https://github.com/gowridurgad) in [#&#8203;1577](https://github.com/actions/setup-node/pull/1577)
- Migrate to ESM and upgrade dependencies by [@&#8203;gowridurgad](https://github.com/gowridurgad) in [#&#8203;1574](https://github.com/actions/setup-node/pull/1574)

##### Bug fixes:

- Remove dummy NODE\_AUTH\_TOKEN export by [@&#8203;gowridurgad](https://github.com/gowridurgad) in [#&#8203;1558](https://github.com/actions/setup-node/pull/1558)
- Only use `mirrorToken` in `getManifest` if it's provided by [@&#8203;deiga](https://github.com/deiga) in [#&#8203;1548](https://github.com/actions/setup-node/pull/1548)

##### Documentation updates:

- Add documentation for publishing to npm with Trusted Publisher (OIDC) by [@&#8203;chiranjib-swain](https://github.com/chiranjib-swain) in [#&#8203;1536](https://github.com/actions/setup-node/pull/1536)
- docs: Update restore-only cache documentation by [@&#8203;priya-kinthali](https://github.com/priya-kinthali) in [#&#8203;1550](https://github.com/actions/setup-node/pull/1550)
- docs: Update caching recommendations to mitigate cache poisoning risks by [@&#8203;chiranjib-swain](https://github.com/chiranjib-swain) in [#&#8203;1567](https://github.com/actions/setup-node/pull/1567)

##### Dependency update:

- Upgrade [@&#8203;actions/cache](https://github.com/actions/cache) to 5.1.0, log cache write denied by [@&#8203;jasongin](https://github.com/jasongin) in [#&#8203;1569](https://github.com/actions/setup-node/pull/1569)

#### New Contributors

- [@&#8203;chiranjib-swain](https://github.com/chiranjib-swain) made their first contribution in [#&#8203;1536](https://github.com/actions/setup-node/pull/1536)
- [@&#8203;deiga](https://github.com/deiga) made their first contribution in [#&#8203;1548](https://github.com/actions/setup-node/pull/1548)
- [@&#8203;jasongin](https://github.com/jasongin) made their first contribution in [#&#8203;1569](https://github.com/actions/setup-node/pull/1569)

**Full Changelog**: <https://github.com/actions/setup-node/compare/v6...v7.0.0>

### [`v6.5.0`](https://github.com/actions/setup-node/releases/tag/v6.5.0)

[Compare Source](https://github.com/actions/setup-node/compare/v6.4.0...v6.5.0)

#### What's Changed

- Update [@&#8203;actions/cache](https://github.com/actions/cache) to 5.1.0 and add security overrides for undici and fast-xml-parser by [@&#8203;HarithaVattikuti](https://github.com/HarithaVattikuti) in [#&#8203;1579](https://github.com/actions/setup-node/pull/1579)

**Full Changelog**: <https://github.com/actions/setup-node/compare/v6.4.0...v6.5.0>

### [`v6.4.0`](https://github.com/actions/setup-node/releases/tag/v6.4.0)

[Compare Source](https://github.com/actions/setup-node/compare/v6.3.0...v6.4.0)

#### What's Changed

##### Dependency updates:

- Upgrade [@&#8203;actions](https://github.com/actions) dependencies by [@&#8203;Copilot](https://github.com/Copilot) in [#&#8203;1525](https://github.com/actions/setup-node/pull/1525)
- Update Node.js versions in versions.yml and bump package to v6.4.0  by [@&#8203;priya-kinthali](https://github.com/priya-kinthali) in [#&#8203;1533](https://github.com/actions/setup-node/pull/1533)

#### New Contributors

- [@&#8203;Copilot](https://github.com/Copilot) made their first contribution in [#&#8203;1525](https://github.com/actions/setup-node/pull/1525)

**Full Changelog**: <https://github.com/actions/setup-node/compare/v6...v6.4.0>

### [`v6.3.0`](https://github.com/actions/setup-node/releases/tag/v6.3.0)

[Compare Source](https://github.com/actions/setup-node/compare/v6.2.0...v6.3.0)

#### What's Changed

##### Enhancements:

- Support parsing `devEngines` field by [@&#8203;susnux](https://github.com/susnux) in [#&#8203;1283](https://github.com/actions/setup-node/pull/1283)

> When using node-version-file: package.json, setup-node now prefers devEngines.runtime over engines.node.

##### Dependency updates:

- Fix npm audit issues by [@&#8203;gowridurgad](https://github.com/gowridurgad) in [#&#8203;1491](https://github.com/actions/setup-node/pull/1491)
- Replace uuid with crypto.randomUUID() by [@&#8203;trivikr](https://github.com/trivikr) in [#&#8203;1378](https://github.com/actions/setup-node/pull/1378)
- Upgrade minimatch from 3.1.2 to 3.1.5 by [@&#8203;dependabot](https://github.com/dependabot) in [#&#8203;1498](https://github.com/actions/setup-node/pull/1498)

##### Bug fixes:

- Remove hardcoded bearer for mirror-url [@&#8203;marco-ippolito](https://github.com/marco-ippolito) in [#&#8203;1467](https://github.com/actions/setup-node/pull/1467)
- Scope test lockfiles by package manager and update cache tests by [@&#8203;gowridurgad](https://github.com/gowridurgad) in [#&#8203;1495](https://github.com/actions/setup-node/pull/1495)

#### New Contributors

- [@&#8203;susnux](https://github.com/susnux) made their first contribution in [#&#8203;1283](https://github.com/actions/setup-node/pull/1283)

**Full Changelog**: <https://github.com/actions/setup-node/compare/v6...v6.3.0>

### [`v6.2.0`](https://github.com/actions/setup-node/releases/tag/v6.2.0)

[Compare Source](https://github.com/actions/setup-node/compare/v6.1.0...v6.2.0)

#### What's Changed

##### Documentation

- Documentation update related to absence of Lockfile by [@&#8203;mahabaleshwars](https://github.com/mahabaleshwars) in [#&#8203;1454](https://github.com/actions/setup-node/pull/1454)
- Correct mirror option typos by [@&#8203;MikeMcC399](https://github.com/MikeMcC399) in [#&#8203;1442](https://github.com/actions/setup-node/pull/1442)
- Readme update on checkout version v6 by [@&#8203;deining](https://github.com/deining) in [#&#8203;1446](https://github.com/actions/setup-node/pull/1446)
- Readme typo fixes [@&#8203;munyari](https://github.com/munyari) in [#&#8203;1226](https://github.com/actions/setup-node/pull/1226)
- Advanced document update on checkout version v6 by [@&#8203;aparnajyothi-y](https://github.com/aparnajyothi-y)  in [#&#8203;1468](https://github.com/actions/setup-node/pull/1468)

##### Dependency updates:

- Upgrade [@&#8203;actions/cache](https://github.com/actions/cache) to v5.0.1 by [@&#8203;salmanmkc](https://github.com/salmanmkc) in [#&#8203;1449](https://github.com/actions/setup-node/pull/1449)

#### New Contributors

- [@&#8203;mahabaleshwars](https://github.com/mahabaleshwars) made their first contribution in [#&#8203;1454](https://github.com/actions/setup-node/pull/1454)
- [@&#8203;MikeMcC399](https://github.com/MikeMcC399) made their first contribution in [#&#8203;1442](https://github.com/actions/setup-node/pull/1442)
- [@&#8203;deining](https://github.com/deining) made their first contribution in [#&#8203;1446](https://github.com/actions/setup-node/pull/1446)
- [@&#8203;munyari](https://github.com/munyari) made their first contribution in [#&#8203;1226](https://github.com/actions/setup-node/pull/1226)

**Full Changelog**: <https://github.com/actions/setup-node/compare/v6...v6.2.0>

### [`v6.1.0`](https://github.com/actions/setup-node/releases/tag/v6.1.0)

[Compare Source](https://github.com/actions/setup-node/compare/v6.0.0...v6.1.0)

#### What's Changed

##### Enhancement:

- Remove always-auth configuration handling by [@&#8203;priyagupta108](https://github.com/priyagupta108) in [#&#8203;1436](https://github.com/actions/setup-node/pull/1436)

##### Dependency updates:

- Upgrade [@&#8203;actions/cache](https://github.com/actions/cache) from 4.0.3 to 4.1.0 by [@&#8203;dependabot](https://github.com/dependabot)\[bot] in [#&#8203;1384](https://github.com/actions/setup-node/pull/1384)
- Upgrade actions/checkout from 5 to 6 by [@&#8203;dependabot](https://github.com/dependabot)\[bot] in [#&#8203;1439](https://github.com/actions/setup-node/pull/1439)
- Upgrade js-yaml from 3.14.1 to 3.14.2 by [@&#8203;dependabot](https://github.com/dependabot)\[bot] in [#&#8203;1435](https://github.com/actions/setup-node/pull/1435)

##### Documentation update:

- Add example for restore-only cache in documentation by [@&#8203;aparnajyothi-y](https://github.com/aparnajyothi-y) in [#&#8203;1419](https://github.com/actions/setup-node/pull/1419)

**Full Changelog**: <https://github.com/actions/setup-node/compare/v6...v6.1.0>

### [`v6.0.0`](https://github.com/actions/setup-node/releases/tag/v6.0.0)

[Compare Source](https://github.com/actions/setup-node/compare/v5.0.0...v6.0.0)

#### What's Changed

**Breaking Changes**

- Limit automatic caching to npm, update workflows and documentation by [@&#8203;priyagupta108](https://github.com/priyagupta108) in [#&#8203;1374](https://github.com/actions/setup-node/pull/1374)

**Dependency Upgrades**

- Upgrade ts-jest from 29.1.2 to 29.4.1 and document breaking changes in v5 by [@&#8203;dependabot](https://github.com/dependabot)\[bot] in [#&#8203;1336](https://github.com/actions/setup-node/pull/1336)
- Upgrade prettier from 2.8.8 to 3.6.2 by [@&#8203;dependabot](https://github.com/dependabot)\[bot] in [#&#8203;1334](https://github.com/actions/setup-node/pull/1334)
- Upgrade actions/publish-action from 0.3.0 to 0.4.0 by [@&#8203;dependabot](https://github.com/dependabot)\[bot] in [#&#8203;1362](https://github.com/actions/setup-node/pull/1362)

**Full Changelog**: <https://github.com/actions/setup-node/compare/v5...v6.0.0>

### [`v5.0.0`](https://github.com/actions/setup-node/releases/tag/v5.0.0)

[Compare Source](https://github.com/actions/setup-node/compare/v4.4.0...v5.0.0)

#### What's Changed

##### Breaking Changes

- Enhance caching in setup-node with automatic package manager detection by [@&#8203;priya-kinthali](https://github.com/priya-kinthali) in [#&#8203;1348](https://github.com/actions/setup-node/pull/1348)

This update, introduces automatic caching when a valid `packageManager` field is present in your `package.json`. This aims to improve workflow performance and make dependency management more seamless.
To disable this automatic caching, set `package-manager-cache: false`

```yaml
steps:
- uses: actions/checkout@v5
- uses: actions/setup-node@v5
  with:
    package-manager-cache: false
```

- Upgrade action to use node24 by [@&#8203;salmanmkc](https://github.com/salmanmkc) in [#&#8203;1325](https://github.com/actions/setup-node/pull/1325)

Make sure your runner is on version v2.327.1 or later to ensure compatibility with this release. [See Release Notes](https://github.com/actions/runner/releases/tag/v2.327.1)

##### Dependency Upgrades

- Upgrade [@&#8203;octokit/request-error](https://github.com/octokit/request-error) and [@&#8203;actions/github](https://github.com/actions/github) by [@&#8203;dependabot](https://github.com/dependabot)\[bot] in [#&#8203;1227](https://github.com/actions/setup-node/pull/1227)
- Upgrade uuid from 9.0.1 to 11.1.0 by [@&#8203;dependabot](https://github.com/dependabot)\[bot] in [#&#8203;1273](https://github.com/actions/setup-node/pull/1273)
- Upgrade undici from 5.28.5 to 5.29.0 by [@&#8203;dependabot](https://github.com/dependabot)\[bot] in [#&#8203;1295](https://github.com/actions/setup-node/pull/1295)
- Upgrade form-data to bring in fix for critical vulnerability by [@&#8203;gowridurgad](https://github.com/gowridurgad) in [#&#8203;1332](https://github.com/actions/setup-node/pull/1332)
- Upgrade actions/checkout from 4 to 5 by [@&#8203;dependabot](https://github.com/dependabot)\[bot] in [#&#8203;1345](https://github.com/actions/setup-node/pull/1345)

#### New Contributors

- [@&#8203;priya-kinthali](https://github.com/priya-kinthali) made their first contribution in [#&#8203;1348](https://github.com/actions/setup-node/pull/1348)
- [@&#8203;salmanmkc](https://github.com/salmanmkc) made their first contribution in [#&#8203;1325](https://github.com/actions/setup-node/pull/1325)

**Full Changelog**: <https://github.com/actions/setup-node/compare/v4...v5.0.0>

### [`v4.4.0`](https://github.com/actions/setup-node/releases/tag/v4.4.0)

[Compare Source](https://github.com/actions/setup-node/compare/v4.3.0...v4.4.0)

#### What's Changed

##### Bug fixes:

- Make eslint-compact matcher compatible with Stylelint by [@&#8203;FloEdelmann](https://github.com/FloEdelmann) in [#&#8203;98](https://github.com/actions/setup-node/pull/98)
- Add support for indented eslint output by [@&#8203;fregante](https://github.com/fregante) in [#&#8203;1245](https://github.com/actions/setup-node/pull/1245)

##### Enhancement:

- Support private mirrors by [@&#8203;marco-ippolito](https://github.com/marco-ippolito) in [#&#8203;1240](https://github.com/actions/setup-node/pull/1240)

##### Dependency update:

- Upgrade [@&#8203;action/cache](https://github.com/action/cache) from 4.0.2 to 4.0.3 by [@&#8203;aparnajyothi-y](https://github.com/aparnajyothi-y) in [#&#8203;1262](https://github.com/actions/setup-node/pull/1262)

#### New Contributors

- [@&#8203;FloEdelmann](https://github.com/FloEdelmann) made their first contribution in [#&#8203;98](https://github.com/actions/setup-node/pull/98)
- [@&#8203;fregante](https://github.com/fregante) made their first contribution in [#&#8203;1245](https://github.com/actions/setup-node/pull/1245)
- [@&#8203;marco-ippolito](https://github.com/marco-ippolito) made their first contribution in [#&#8203;1240](https://github.com/actions/setup-node/pull/1240)

**Full Changelog**: <https://github.com/actions/setup-node/compare/v4...v4.4.0>

### [`v4.3.0`](https://github.com/actions/setup-node/releases/tag/v4.3.0)

[Compare Source](https://github.com/actions/setup-node/compare/v4.2.0...v4.3.0)

#### What's Changed

##### Dependency updates

- Upgrade [@&#8203;actions/glob](https://github.com/actions/glob) from 0.4.0 to 0.5.0 by [@&#8203;dependabot](https://github.com/dependabot) in [#&#8203;1200](https://github.com/actions/setup-node/pull/1200)
- Upgrade [@&#8203;action/cache](https://github.com/action/cache) from 4.0.0 to 4.0.2 by [@&#8203;gowridurgad](https://github.com/gowridurgad) in [#&#8203;1251](https://github.com/actions/setup-node/pull/1251)
- Upgrade [@&#8203;vercel/ncc](https://github.com/vercel/ncc) from 0.38.1 to 0.38.3 by [@&#8203;dependabot](https://github.com/dependabot) in [#&#8203;1203](https://github.com/actions/setup-node/pull/1203)
- Upgrade [@&#8203;actions/tool-cache](https://github.com/actions/tool-cache) from 2.0.1 to 2.0.2 by [@&#8203;dependabot](https://github.com/dependabot) in [#&#8203;1220](https://github.com/actions/setup-node/pull/1220)

#### New Contributors

- [@&#8203;gowridurgad](https://github.com/gowridurgad) made their first contribution in [#&#8203;1251](https://github.com/actions/setup-node/pull/1251)

**Full Changelog**: <https://github.com/actions/setup-node/compare/v4...v4.3.0>

### [`v4.2.0`](https://github.com/actions/setup-node/releases/tag/v4.2.0)

[Compare Source](https://github.com/actions/setup-node/compare/v4.1.0...v4.2.0)

#### What's Changed

- Enhance workflows and upgrade publish-actions from 0.2.2 to 0.3.0 by [@&#8203;aparnajyothi-y](https://github.com/aparnajyothi-y) in [#&#8203;1174](https://github.com/actions/setup-node/pull/1174)
- Add recommended permissions section to readme by [@&#8203;benwells](https://github.com/benwells) in [#&#8203;1193](https://github.com/actions/setup-node/pull/1193)
- Configure Dependabot settings by [@&#8203;HarithaVattikuti](https://github.com/HarithaVattikuti) in [#&#8203;1192](https://github.com/actions/setup-node/pull/1192)
- Upgrade `@actions/cache` to `^4.0.0` by [@&#8203;priyagupta108](https://github.com/priyagupta108) in [#&#8203;1191](https://github.com/actions/setup-node/pull/1191)
- Upgrade pnpm/action-setup from 2 to 4 by [@&#8203;dependabot](https://github.com/dependabot) in [#&#8203;1194](https://github.com/actions/setup-node/pull/1194)
- Upgrade actions/publish-immutable-action from 0.0.3 to 0.0.4 by [@&#8203;dependabot](https://github.com/dependabot) in [#&#8203;1195](https://github.com/actions/setup-node/pull/1195)
- Upgrade semver from 7.6.0 to 7.6.3 by [@&#8203;dependabot](https://github.com/dependabot) in [#&#8203;1196](https://github.com/actions/setup-node/pull/1196)
- Upgrade [@&#8203;types/jest](https://github.com/types/jest) from 29.5.12 to 29.5.14 by [@&#8203;dependabot](https://github.com/dependabot) in [#&#8203;1201](https://github.com/actions/setup-node/pull/1201)
- Upgrade undici from 5.28.4 to 5.28.5 by [@&#8203;dependabot](https://github.com/dependabot) in [#&#8203;1205](https://github.com/actions/setup-node/pull/1205)

#### New Contributors

- [@&#8203;benwells](https://github.com/benwells) made their first contribution in [#&#8203;1193](https://github.com/actions/setup-node/pull/1193)

**Full Changelog**: <https://github.com/actions/setup-node/compare/v4...v4.2.0>

### [`v4.1.0`](https://github.com/actions/setup-node/releases/tag/v4.1.0)

[Compare Source](https://github.com/actions/setup-node/compare/v4.0.4...v4.1.0)

#### What's Changed

- Resolve High Security Alerts by upgrading Dependencies by [@&#8203;aparnajyothi-y](https://github.com/aparnajyothi-y) in [#&#8203;1132](https://github.com/actions/setup-node/pull/1132)
- Upgrade IA Publish by [@&#8203;Jcambass](https://github.com/Jcambass) in [#&#8203;1134](https://github.com/actions/setup-node/pull/1134)
- Revise `isGhes` logic by [@&#8203;jww3](https://github.com/jww3) in [#&#8203;1148](https://github.com/actions/setup-node/pull/1148)
- Add architecture to cache key by [@&#8203;pengx17](https://github.com/pengx17) in [#&#8203;843](https://github.com/actions/setup-node/pull/843)
  This addresses issues with caching by adding the architecture (arch) to the cache key, ensuring that cache keys are accurate to prevent conflicts.
  Note: This change may break previous cache keys as they will no longer be compatible with the new format.

#### New Contributors

- [@&#8203;jww3](https://github.com/jww3) made their first contribution in [#&#8203;1148](https://github.com/actions/setup-node/pull/1148)
- [@&#8203;pengx17](https://github.com/pengx17) made their first contribution in [#&#8203;843](https://github.com/actions/setup-node/pull/843)

**Full Changelog**: <https://github.com/actions/setup-node/compare/v4...v4.1.0>

### [`v4.0.4`](https://github.com/actions/setup-node/releases/tag/v4.0.4)

[Compare Source](https://github.com/actions/setup-node/compare/v4.0.3...v4.0.4)

#### What's Changed

- Add workflow file for publishing releases to immutable action package by [@&#8203;Jcambass](https://github.com/Jcambass) in [#&#8203;1125](https://github.com/actions/setup-node/pull/1125)
- Enhance Windows ARM64 Setup and Update micromatch Dependency by [@&#8203;priyagupta108](https://github.com/priyagupta108) in [#&#8203;1126](https://github.com/actions/setup-node/pull/1126)

##### Documentation changes:

- Documentation update in the README file by [@&#8203;suyashgaonkar](https://github.com/suyashgaonkar) in [#&#8203;1106](https://github.com/actions/setup-node/pull/1106)
- Correct invalid 'lts' version string reference by [@&#8203;fulldecent](https://github.com/fulldecent) in [#&#8203;1124](https://github.com/actions/setup-node/pull/1124)

#### New Contributors

- [@&#8203;suyashgaonkar](https://github.com/suyashgaonkar) made their first contribution in [#&#8203;1106](https://github.com/actions/setup-node/pull/1106)
- [@&#8203;priyagupta108](https://github.com/priyagupta108) made their first contribution in [#&#8203;1126](https://github.com/actions/setup-node/pull/1126)
- [@&#8203;Jcambass](https://github.com/Jcambass) made their first contribution in [#&#8203;1125](https://github.com/actions/setup-node/pull/1125)
- [@&#8203;fulldecent](https://github.com/fulldecent) made their first contribution in [#&#8203;1124](https://github.com/actions/setup-node/pull/1124)

**Full Changelog**: <https://github.com/actions/setup-node/compare/v4...v4.0.4>

### [`v4.0.3`](https://github.com/actions/setup-node/releases/tag/v4.0.3)

[Compare Source](https://github.com/actions/setup-node/compare/v4.0.2...v4.0.3)

#### What's Changed

##### Bug fixes:

- Fix macos latest check failures by [@&#8203;HarithaVattikuti](https://github.com/HarithaVattikuti) in [#&#8203;1041](https://github.com/actions/setup-node/pull/1041)

##### Documentation changes:

- Documentation update to update default Node version to 20 by [@&#8203;bengreeley](https://github.com/bengreeley) in [#&#8203;949](https://github.com/actions/setup-node/pull/949)

##### Dependency  updates:

- Bump undici from 5.26.5 to 5.28.3 by [@&#8203;dependabot](https://github.com/dependabot) in [#&#8203;965](https://github.com/actions/setup-node/pull/965)
- Bump braces from 3.0.2 to 3.0.3 and other dependency updates by [@&#8203;dependabot](https://github.com/dependabot) in [#&#8203;1087](https://github.com/actions/setup-node/pull/1087)

#### New Contributors

- [@&#8203;bengreeley](https://github.com/bengreeley) made their first contribution in [#&#8203;949](https://github.com/actions/setup-node/pull/949)
- [@&#8203;HarithaVattikuti](https://github.com/HarithaVattikuti) made their first contribution in [#&#8203;1041](https://github.com/actions/setup-node/pull/1041)

**Full Changelog**: <https://github.com/actions/setup-node/compare/v4...v4.0.3>

### [`v4.0.2`](https://github.com/actions/setup-node/releases/tag/v4.0.2)

[Compare Source](https://github.com/actions/setup-node/compare/v4.0.1...v4.0.2)

#### What's Changed

- Add support for `volta.extends` by [@&#8203;ThisIsManta](https://github.com/ThisIsManta) in [#&#8203;921](https://github.com/actions/setup-node/pull/921)
- Add support for arm64 Windows by [@&#8203;dmitry-shibanov](https://github.com/dmitry-shibanov) in [#&#8203;927](https://github.com/actions/setup-node/pull/927)

#### New Contributors

- [@&#8203;ThisIsManta](https://github.com/ThisIsManta) made their first contribution in [#&#8203;921](https://github.com/actions/setup-node/pull/921)

**Full Changelog**: <https://github.com/actions/setup-node/compare/v4.0.1...v4.0.2>

### [`v4.0.1`](https://github.com/actions/setup-node/releases/tag/v4.0.1)

[Compare Source](https://github.com/actions/setup-node/compare/v4.0.0...v4.0.1)

#### What's Changed

- Ignore engines in Yarn 1 e2e-cache tests by [@&#8203;trivikr](https://github.com/trivikr) in [#&#8203;882](https://github.com/actions/setup-node/pull/882)
- Update setup-node references in the README.md file to setup-node\@&#8203;v4 by [@&#8203;jwetzell](https://github.com/jwetzell) in [#&#8203;884](https://github.com/actions/setup-node/pull/884)
- Update reusable workflows to use Node.js v20 by [@&#8203;MaksimZhukov](https://github.com/MaksimZhukov) in [#&#8203;889](https://github.com/actions/setup-node/pull/889)
- Add fix for cache to resolve slow post action step by [@&#8203;aparnajyothi-y](https://github.com/aparnajyothi-y) in [#&#8203;917](https://github.com/actions/setup-node/pull/917)
- Fix README.md by [@&#8203;takayamaki](https://github.com/takayamaki) in [#&#8203;898](https://github.com/actions/setup-node/pull/898)
- Add `package.json` to `node-version-file` list of examples. by [@&#8203;TWiStErRob](https://github.com/TWiStErRob) in [#&#8203;879](https://github.com/actions/setup-node/pull/879)
- Fix node-version-file interprets entire package.json as a version by [@&#8203;NullVoxPopuli](https://github.com/NullVoxPopuli) in [#&#8203;865](https://github.com/actions/setup-node/pull/865)

#### New Contributors

- [@&#8203;trivikr](https://github.com/trivikr) made their first contribution in [#&#8203;882](https://github.com/actions/setup-node/pull/882)
- [@&#8203;jwetzell](https://github.com/jwetzell) made their first contribution in [#&#8203;884](https://github.com/actions/setup-node/pull/884)
- [@&#8203;aparnajyothi-y](https://github.com/aparnajyothi-y) made their first contribution in [#&#8203;917](https://github.com/actions/setup-node/pull/917)
- [@&#8203;takayamaki](https://github.com/takayamaki) made their first contribution in [#&#8203;898](https://github.com/actions/setup-node/pull/898)
- [@&#8203;TWiStErRob](https://github.com/TWiStErRob) made their first contribution in [#&#8203;879](https://github.com/actions/setup-node/pull/879)
- [@&#8203;NullVoxPopuli](https://github.com/NullVoxPopuli) made their first contribution in [#&#8203;865](https://github.com/actions/setup-node/pull/865)

**Full Changelog**: <https://github.com/actions/setup-node/compare/v4...v4.0.1>

### [`v4.0.0`](https://github.com/actions/setup-node/releases/tag/v4.0.0)

[Compare Source](https://github.com/actions/setup-node/compare/v3.9.1...v4.0.0)

#### What's Changed

In scope of this release we changed version of node runtime for action from node16 to node20 and updated dependencies in [#&#8203;866](https://github.com/actions/setup-node/pull/866)

Besides, release contains such changes as:

- Upgrade actions/checkout to v4 by [@&#8203;gmembre-zenika](https://github.com/gmembre-zenika) in [#&#8203;868](https://github.com/actions/setup-node/pull/868)
- Update actions/checkout for documentation and yaml by [@&#8203;dmitry-shibanov](https://github.com/dmitry-shibanov) in [#&#8203;876](https://github.com/actions/setup-node/pull/876)

#### New Contributors

- [@&#8203;gmembre-zenika](https://github.com/gmembre-zenika) made their first contribution in [#&#8203;868](https://github.com/actions/setup-node/pull/868)

**Full Changelog**: <https://github.com/actions/setup-node/compare/v3...v4.0.0>

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
