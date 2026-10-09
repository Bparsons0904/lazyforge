# GitHub adapter fixtures

Recorded 2026-10-09 from github.com with `gh api` (REST API version `2022-11-28`), authenticated as
`Bparsons0904`, against public repos only. `gh api <path>` is relative to `https://api.github.com`.
The fake server serves them under `/api/v3`, the GHES base, because its URL is not github.com.

| File | Source | Hand edits |
|---|---|---|
| `user.json` | `gh api user` | trimmed to `login`, `id`, `type`, `html_url` (the rest is profile data the adapter never reads) |
| `meta.json` | `gh api meta` | trimmed to `verifiable_password_authentication` and `ssh_key_fingerprints`; the IP-range arrays made it 150 KB. github.com sends no `installed_version` |
| `meta_ghes.json` | **synthesized, not recorded** | `{verifiable_password_authentication, installed_version}`, per the GHES REST docs for `GET /meta` ("Get GitHub Enterprise Server meta information"), which add `installed_version`. No GHES instance was available |
| `user_repos.json` | `gh api 'user/repos?per_page=100&sort=pushed&affiliation=owner,collaborator,organization_member'` | filtered with jq to `Bparsons0904/lazyforge` and `Bparsons0904/rootcamp`; shows `permissions` with `maintain`/`triage` and null `allow_*` flags |
| `repo_cli.json` | `gh api repos/cli/cli` | `permissions.push` set to `true` (recorded as pull-only) so the contract can merge in it; appended to the repo list by the fake server. `allow_*` are null as recorded, because the token can't push there |
| `pulls_open.json` | `gh api 'repos/cli/cli/pulls?state=open&per_page=2'` | none; #14634 (head `424f38df651e0b178d752073859af7ee1d3d02c3`) and #14629 |
| `pulls_closed.json` | `gh api 'repos/cli/cli/pulls?state=closed&per_page=4'` | none; #14633, #14632, #14631 merged (`merged_at` set), #14630 closed unmerged |
| `issues_open.json` | `gh api 'repos/cli/cli/issues?state=open&per_page=4'` | none; #14634 and #14629 are PRs (`pull_request` key), #14627 and #14621 are issues |
| `issue_comments.json` | `gh api 'repos/cli/cli/issues/14627/comments?per_page=100'` | none |
| `check_runs.json` | `gh api 'repos/cli/cli/commits/424f38df651e0b178d752073859af7ee1d3d02c3/check-runs?per_page=100'` | `check_runs` cut from 30 to items 0, 3, 18, 19 (success, skipped, success, success) and `total_count` set to 4 |
| `status_none.json` | `gh api repos/cli/cli/commits/424f38df651e0b178d752073859af7ee1d3d02c3/status` | none; `state:"pending"` with `total_count:0` |
| `status_failure.json` | `gh api repos/kubernetes/kubernetes/commits/5174647a4d29db996faab7a457170b56b6ffe4d1/status` | `statuses` cut from 14 to EasyCLA (success), tide (pending), pull-kubernetes-verify (failure); `total_count` set to 3; `state:"failure"` as recorded. Served for cli/cli #14629's head |
| `releases.json` | `gh api 'repos/cli/cli/releases?per_page=2'` | `assets` emptied (the adapter doesn't read them; they were most of the size) |
| `error_404.json` | `gh api repos/cli/cli/pulls/99999999` (status 404) | none |
| `error_401.json` | `curl -s https://api.github.com/user` without a token (status 401) | none |
| `merge_409.json` | **synthesized, not recorded** | GitHub REST docs, "Merge a pull request": 409 "Conflict if sha was provided and pull request head did not match"; message text as GitHub returns it. Reproducing needs a mutation on a repo we own |
| `merge_405.json` | **synthesized, not recorded** | Same docs page: 405 "Method Not Allowed if merge cannot be performed", message `Pull Request is not mergeable` |
| `rate_limit_403.json` | **synthesized, not recorded** | GitHub REST docs, "Rate limits for the REST API": an exhausted primary limit is 403 or 429 with `x-ratelimit-remaining: 0`; message shape from those docs, request ID zeroed |
| `actions_runs.json` | `gh api 'repos/cli/cli/actions/runs?per_page=30'`, plus run 37915254425 from `gh api 'repos/cli/cli/actions/runs?status=failure&per_page=1'` | kept runs 37946858179 (in_progress), 37946858122 (success), 37941367784 (skipped), 37915254425 (failure) and 37941364158; `repository`, `head_repository`, `head_commit`, `actor`, `triggering_actor` and `referenced_workflows` deleted and `pull_requests` emptied (unread, and most of the size); `total_count` set to 5. **37941364158's `status` set to `queued` and `conclusion` to null** (recorded completed/success), because `?status=queued` returned none |
| `actions_jobs.json` | `gh api 'repos/cli/cli/actions/runs/37942729045/jobs?per_page=100'` | `jobs` cut from 6 to items 1, 2, 0, `steps` emptied, `total_count` set to 3 |
| `job_log.txt` | `gh api --allow-escape-sequences repos/cli/cli/actions/jobs/113875256467/logs` (a 302 to `*.blob.core.windows.net`, which answers 200 `text/plain` with an `ETag`) | cut from 262 lines to the first 15 and last 3; secrets in it were already masked `***` |
| `labels.json` | `gh api 'repos/cli/cli/labels?per_page=100'` | cut from 83 to the first 5 |
| `issue_labels.json` | `gh api repos/cli/cli/issues/14627/labels` | none; its color is upper case as recorded |
