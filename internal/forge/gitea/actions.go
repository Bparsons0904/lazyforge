package gitea

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

type run struct {
	ID          int64     `json:"id"`
	IndexInRepo int       `json:"index_in_repo"`
	WorkflowID  string    `json:"workflow_id"`
	Title       string    `json:"title"`
	PrettyRef   string    `json:"prettyref"`
	CommitSHA   string    `json:"commit_sha"`
	Event       string    `json:"event"`
	Status      string    `json:"status"`
	Started     time.Time `json:"started"`
	Duration    int64     `json:"duration"` // nanoseconds
	HTMLURL     string    `json:"html_url"`
}

type job struct {
	ID      int64  `json:"id"`
	RunID   int64  `json:"run_id"`
	Name    string `json:"name"`
	Status  string `json:"status"`
	Attempt int    `json:"attempt"`
}

// ListRuns pages on the body's total_count, which Forgejo sends instead of X-Total-Count here.
func (f *Forge) ListRuns(ctx context.Context, r domain.RepoRef, flt forge.RunFilter) ([]domain.Run, error) {
	q := url.Values{}
	if flt.Branch != "" {
		q.Set("ref", "refs/heads/"+flt.Branch) // a bare branch name matches nothing
	}
	if flt.HeadSHA != "" {
		q.Set("head_sha", flt.HeadSHA)
	}
	runs, err := walk(ctx, f, repoPath(r)+"/actions/runs", q, func(rd io.Reader) ([]run, int, error) {
		var page struct {
			WorkflowRuns []run `json:"workflow_runs"`
			TotalCount   int   `json:"total_count"`
		}
		err := json.NewDecoder(rd).Decode(&page)
		return page.WorkflowRuns, page.TotalCount, err
	})
	if err != nil {
		return nil, err
	}
	out := make([]domain.Run, len(runs))
	for i, rn := range runs {
		out[i] = domain.Run{
			ID:        rn.ID,
			Number:    rn.IndexInRepo,
			Workflow:  rn.WorkflowID,
			Title:     rn.Title,
			Branch:    rn.PrettyRef,
			Commit:    rn.CommitSHA,
			Event:     rn.Event,
			Status:    runCI(rn.Status),
			StartedAt: rn.Started,
			Duration:  time.Duration(rn.Duration),
			WebURL:    rn.HTMLURL,
		}
	}
	return out, nil
}

// ListJobs takes the run's ID, not its per-repo number; the endpoint is unpaginated.
func (f *Forge) ListJobs(ctx context.Context, r domain.RepoRef, runID int64) ([]domain.Job, error) {
	var js []job
	path := repoPath(r) + "/actions/runs/" + strconv.FormatInt(runID, 10) + "/jobs"
	if err := f.call(ctx, http.MethodGet, path, nil, &js); err != nil {
		return nil, err
	}
	out := make([]domain.Job, len(js))
	for i, j := range js {
		out[i] = domain.Job{ID: j.ID, RunID: j.RunID, Name: j.Name, Status: runCI(j.Status), Attempt: j.Attempt}
	}
	return out, nil
}

// JobLog returns the raw text/plain log; the caller closes it.
func (f *Forge) JobLog(ctx context.Context, r domain.RepoRef, jobID int64) (io.ReadCloser, error) {
	resp, err := f.send(ctx, http.MethodGet, repoPath(r)+"/actions/jobs/"+strconv.FormatInt(jobID, 10)+"/logs", nil, nil)
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

func runCI(s string) domain.CIState {
	switch s {
	case "success":
		return domain.CIPass
	case "failure":
		return domain.CIFail
	case "cancelled":
		return domain.CICancelled
	case "skipped":
		return domain.CISkipped
	case "running":
		return domain.CIRunning
	case "waiting", "blocked":
		return domain.CIPending
	}
	return domain.CINone
}
