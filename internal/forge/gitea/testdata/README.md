# Gitea/Forgejo adapter fixtures

Recorded 2026-10-02 from https://git.bobparsons.dev (`/api/v1/version` = `16.0.5+gitea-1.22.0`),
unauthenticated GETs against public repos. `B=https://git.bobparsons.dev/api/v1`; each recording is
piped through `python3 -m json.tool`. Content is a personal forge's public repo metadata, no health data.
Actions samples (runs, jobs, logs, combined statuses) live in `actions/` and are reused unchanged.

| File | Source | Hand edits |
|---|---|---|
| `version.json` | `GET $B/version` | none |
| `user.json` | `GET $B/users/deadstyle` | none; stands in for `GET /user`, which needs auth and has the same `User` shape |
| `user_repos.json` | `GET $B/repos/deadstyle/lazyforge` and `GET $B/repos/deadstyle/waugzee`, combined into one array | lazyforge `permissions` set to `{admin:true,push:true,pull:true}`; waugzee left as recorded (`pull` only). Stands in for `GET /user/repos`, which needs auth |
| `pulls.json` | `GET $B/repos/deadstyle/lazyforge/pulls?state=all&page=1&limit=50` | none; #18 open (head `5f5b7243858f450342de15babccedd52a632fe43`), the rest merged |
| `pulls_closed_unmerged.json` | `GET $B/repos/deadstyle/waugzee/pulls?state=all&page=1&limit=50` | none; #4 and #3 closed unmerged, #2 and #1 merged |
| `issues.json` | `GET $B/repos/deadstyle/lazyforge/issues?type=issues&state=all&page=1&limit=50` | none; 15 issues, #17 has 1 comment |
| `issue_comments.json` | `GET $B/repos/deadstyle/lazyforge/issues/17/comments` | none |
| `releases.json` | `GET $B/repos/deadstyle/headroom-kompresser/releases?page=1&limit=50` | none |
| `commit_status_none.json` | `GET $B/repos/deadstyle/lazyforge/commits/b1bd5cf/status` | none; `state:""`, `total_count:0` |
| `commit_status_pr18.json` | `GET $B/repos/deadstyle/lazyforge/commits/5f5b7243858f450342de15babccedd52a632fe43/status` | none; `success` |
| `error_404.json` | `GET $B/repos/deadstyle/lazyforge/pulls/9999` (status 404) | none |
| `error_401.json` | `GET $B/user` without a token (status 401) | none |
| `merge_head_out_of_date.json` | **synthesized, not recorded** | Built from Forgejo v16.0 `routers/api/v1/repo/pull.go:1038` (`ctx.Error(http.StatusConflict, "Merge", "head out of date")`) and `services/context/api.go:324` (`{message,url}`). Reproducing it live needs auth and a mutation |
