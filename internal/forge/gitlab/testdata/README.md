# GitLab adapter fixtures

**Synthesised, not recorded.** No gitlab.com token was available and the employer GitLab must not be used, so nothing here
was captured from a live server. Every file is written by hand from the response examples in the GitLab REST docs, read
2026-10-12, with example values (`gitlab.example.com`, users `alice`, `bob`, `renovate-bot`) in place of real ones.
The fake server serves them under `/api/v4`. All sources are pages under https://docs.gitlab.com/api/.

| File | Source (docs.gitlab.com/api/) | Hand edits |
|---|---|---|
| `user.json` | `users/`, "For normal users" | cut to `id`, `username`, `name`, `state`, `web_url` |
| `metadata.json` | `metadata/`, "Retrieve metadata information" | none beyond example values |
| `projects.json` | `projects/`, "List all projects" (`membership=true`) | four projects written to cover the access cases: a subgroup project with `project_access` 40; a project with both `permissions` fields null, `merge_method` `rebase_merge` and `squash_option` `always`; a null-permissions project with `ff` and `never`; an archived project with `project_access` 50. Ordered by `last_activity_at` descending |
| `projects_min_access.json` | same, `min_access_level=30&simple=true` | the `simple` shape (`id`, `name`, `path_with_namespace`, `web_url`); lists the two projects the user holds Developer or better on |
| `merge_requests.json` | `merge_requests/`, "List project merge requests" | four MRs (opened, opened draft, merged, closed); no `head_pipeline`, as the list never carries it |
| `head_pipelines.json` | `merge_requests/`, "Get single MR" (`head_pipeline`) | keyed by MR `iid`; MR 7's pipeline matches the pipeline list by `sha`, MR 8's has a different `sha` (a merged-results pipeline) |
| `pipelines.json` | `pipelines/`, "List project pipelines" | four pipelines, newest first; two share MR 7's `sha`, the newer is `running` |
| `issues.json` | `issues/`, "List project issues" | adds `issue_type` `incident`, `test_case` and `task` entries, which GitLab returns unless `issue_type=issue` is sent |
| `notes_issue_3.json` | `notes/`, "List project issue notes" | four notes, newest first as the default `sort=desc` returns them; one has `system: true` |
| `notes_mr_7.json` | `notes/`, "List all merge request notes" | two notes; one has `system: true` |
| `releases.json` | `releases/`, "List releases" | the second release has no `released_at`, to exercise the `created_at` fallback |
| `error_401.json` | `rest/authentication/` | none; the body GitLab sends for a missing or bad token |
| `error_404.json` | `rest/troubleshooting/` | none |
| `merge_405.json` | `merge_requests/`, "Merge a merge request" (405 not mergeable) | message as the docs word it |
| `merge_409.json` | same (409 `SHA does not match HEAD of source branch`) | none |
| `merge_422.json` | same (422 `Branch cannot be merged`) | none |
| `error_object.json` | `rest/troubleshooting/`, validation errors | `message` as a `{field: [errors]}` object, which GitLab sends for validation failures |
| `repo/file_readme.json` | `repository_files/`, "Get file from repository" | `content` is the base64 of a three-line README; the rest is the docs example trimmed |
| `repo/branches.json` | `branches/`, "List repository branches" | two branches, one the default; the second's `committed_date` carries a +01:00 offset to exercise the time parse |
| `repo/commits.json` | `commits/`, "List repository commits" | two commits, newest first; `message` runs past the `title` so the subject line is told apart from the body |
| `repo/tree_src.json` | `repositories/`, "List repository tree" | five entries for one directory: a `tree`, two `blob`s, a `commit` and a `blob` with mode `160000` (both submodules) |
