# Forgejo Actions API samples

Recorded 2026-10-02 from https://git.bobparsons.dev (`/api/v1/version` = `16.0.5+gitea-1.22.0`),
unauthenticated, public repos `deadstyle/waugzee` and `deadstyle/lazyforge`. Paths are from
`/swagger.v1.json`. Trimmed: arrays cut to a few items, `event_payload` strings cut to 200 chars,
logs cut to head and tail.

| Operation | Endpoint | Sample |
|---|---|---|
| List runs | `GET /repos/{owner}/{repo}/actions/runs` (`ListActionRuns`; filters `event`, `status`, `run_number`, `head_sha`, `ref`, `workflow_id`) | `runs_list.json` |
| Get run | `GET /repos/{owner}/{repo}/actions/runs/{run_id}` (`ActionRun`) | `run_get.json` |
| List jobs for a run | `GET /repos/{owner}/{repo}/actions/runs/{run_id}/jobs` (`ListActionRunJobs`) | `run_jobs.json` |
| Job log | `GET /repos/{owner}/{repo}/actions/jobs/{job_id}/logs` (`repoGetActionJobLogs`, `text/plain`, `?attempt=`, `Range`) | `job_log.txt` |
| Run logs (all jobs) | `GET /repos/{owner}/{repo}/actions/runs/{run_id}/logs` (`repoGetActionRunLogs`, zip) | none, binary |
| List tasks | `GET /repos/{owner}/{repo}/actions/tasks` (`ListActionTasks`) | `tasks_list.json` |
| Cancel run | `POST /repos/{owner}/{repo}/actions/runs/{run_id}/cancel` (unauth: 401) | none |
| Re-run run | missing (no swagger path; `POST .../runs/{id}/rerun` unauth: 404 plain text) | none |
| Re-run job | missing (no swagger path; `POST .../jobs/{id}/rerun` unauth: 404 plain text) | none |
| Combined status for a SHA | `GET /repos/{owner}/{repo}/commits/{ref}/status` (`repoGetCombinedStatusByRef`) | `commit_status_combined_success.json`, `commit_status_combined_failure.json` |
| Statuses for a SHA | `GET /repos/{owner}/{repo}/commits/{ref}/statuses` | `commit_statuses.json` |
| Web job view (not API) | `POST /{owner}/{repo}/actions/runs/{index}/jobs/{job_index}` | `web_job_view.json` |
