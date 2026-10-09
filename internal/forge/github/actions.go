package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

type run struct {
	ID           int64     `json:"id"`
	RunNumber    int       `json:"run_number"`
	Name         string    `json:"name"`
	DisplayTitle string    `json:"display_title"`
	HeadBranch   string    `json:"head_branch"`
	HeadSHA      string    `json:"head_sha"`
	Event        string    `json:"event"`
	Status       string    `json:"status"`
	Conclusion   string    `json:"conclusion"`
	RunStartedAt time.Time `json:"run_started_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	HTMLURL      string    `json:"html_url"`
}

type job struct {
	ID         int64  `json:"id"`
	RunID      int64  `json:"run_id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	RunAttempt int    `json:"run_attempt"`
}

// ListRuns returns only the newest 100 runs, newest first (ADR 0017). Duration is zero until a run
// completes, since GitHub sends no end time.
func (f *Forge) ListRuns(ctx context.Context, r domain.RepoRef, flt forge.RunFilter) ([]domain.Run, error) {
	q := url.Values{"per_page": {pageSize}}
	if flt.Branch != "" {
		q.Set("branch", flt.Branch)
	}
	if flt.HeadSHA != "" {
		q.Set("head_sha", flt.HeadSHA)
	}
	path := repoPath(r) + "/actions/runs"
	resp, err := f.send(ctx, http.MethodGet, f.url(path, q), nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	var page struct {
		WorkflowRuns []run `json:"workflow_runs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		return nil, fmt.Errorf("GET %s: decode: %w", path, err)
	}
	out := make([]domain.Run, len(page.WorkflowRuns))
	for i, rn := range page.WorkflowRuns {
		out[i] = runToDomain(rn)
	}
	return out, nil
}

func runToDomain(rn run) domain.Run {
	var d time.Duration
	if rn.Status == "completed" && !rn.RunStartedAt.IsZero() {
		d = rn.UpdatedAt.Sub(rn.RunStartedAt)
	}
	return domain.Run{
		ID:        rn.ID,
		Number:    rn.RunNumber,
		Workflow:  rn.Name,
		Title:     rn.DisplayTitle,
		Branch:    rn.HeadBranch,
		Commit:    rn.HeadSHA,
		Event:     rn.Event,
		Status:    checkRunState(checkRun{Status: rn.Status, Conclusion: rn.Conclusion}),
		StartedAt: rn.RunStartedAt,
		Duration:  d,
		WebURL:    rn.HTMLURL,
	}
}

// ListJobs takes the run's ID, not its run number, and returns the latest attempt's jobs.
func (f *Forge) ListJobs(ctx context.Context, r domain.RepoRef, runID int64) ([]domain.Job, error) {
	path := repoPath(r) + "/actions/runs/" + strconv.FormatInt(runID, 10) + "/jobs"
	js, err := walk(ctx, f, path, nil, func(rd io.Reader) ([]job, error) {
		var page struct {
			Jobs []job `json:"jobs"`
		}
		err := json.NewDecoder(rd).Decode(&page)
		return page.Jobs, err
	})
	if err != nil {
		return nil, err
	}
	out := make([]domain.Job, len(js))
	for i, j := range js {
		out[i] = domain.Job{
			ID:      j.ID,
			RunID:   j.RunID,
			Name:    j.Name,
			Status:  checkRunState(checkRun{Status: j.Status, Conclusion: j.Conclusion}),
			Attempt: j.RunAttempt,
		}
	}
	return out, nil
}

// JobLog returns the raw text log; the caller closes it. It skips the ETag cache because the log host
// sends an ETag, so caching would buffer the whole log.
func (f *Forge) JobLog(ctx context.Context, r domain.RepoRef, jobID int64) (io.ReadCloser, error) {
	return f.stream(ctx, f.url(repoPath(r)+"/actions/jobs/"+strconv.FormatInt(jobID, 10)+"/logs", nil))
}
