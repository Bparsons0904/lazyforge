package gitlab

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

type pipelineRun struct {
	ID        int64      `json:"id"`
	IID       int        `json:"iid"`
	SHA       string     `json:"sha"`
	Ref       string     `json:"ref"`
	Status    string     `json:"status"`
	Source    string     `json:"source"`
	Name      string     `json:"name"`
	CreatedAt time.Time  `json:"created_at"`
	StartedAt *time.Time `json:"started_at"`
	Duration  *float64   `json:"duration"` // list rows carry it from GitLab 19.4; older servers omit it
	WebURL    string     `json:"web_url"`
}

type pipelineJob struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Stage  string `json:"stage"`
	Status string `json:"status"`
}

// ListRuns returns only the newest 100 pipelines, newest first (ADR 0017's runs exception). Title stays empty,
// and Duration is zero on servers before GitLab 19.4.
func (f *Forge) ListRuns(ctx context.Context, r domain.RepoRef, flt forge.RunFilter) ([]domain.Run, error) {
	q := url.Values{"per_page": {pageSize}, "order_by": {"id"}, "sort": {"desc"}}
	if flt.Branch != "" {
		q.Set("ref", flt.Branch)
	}
	if flt.HeadSHA != "" {
		q.Set("sha", flt.HeadSHA)
	}
	var ps []pipelineRun
	if err := f.callQuery(ctx, http.MethodGet, projectPath(r)+"/pipelines", q, nil, &ps); err != nil {
		return nil, err
	}
	out := make([]domain.Run, len(ps))
	for i, p := range ps {
		out[i] = runToDomain(p)
	}
	return out, nil
}

func runToDomain(p pipelineRun) domain.Run {
	workflow := p.Name
	if workflow == "" {
		workflow = p.Source
	}
	started := p.CreatedAt
	if p.StartedAt != nil {
		started = *p.StartedAt
	}
	var d time.Duration
	if p.Duration != nil {
		d = time.Duration(*p.Duration * float64(time.Second))
	}
	return domain.Run{
		ID:        p.ID,
		Number:    p.IID,
		Workflow:  workflow,
		Branch:    p.Ref,
		Commit:    p.SHA,
		Event:     p.Source,
		Status:    pipelineState(p.Status),
		StartedAt: started,
		Duration:  d,
		WebURL:    p.WebURL,
	}
}

// ListJobs leaves out retried jobs and trigger (bridge) jobs.
func (f *Forge) ListJobs(ctx context.Context, r domain.RepoRef, runID int64) ([]domain.Job, error) {
	js, err := list[pipelineJob](ctx, f, projectPath(r)+"/pipelines/"+strconv.FormatInt(runID, 10)+"/jobs", nil)
	if err != nil {
		return nil, err
	}
	out := make([]domain.Job, len(js))
	for i, j := range js {
		out[i] = domain.Job{ID: j.ID, RunID: runID, Name: j.Name, Stage: j.Stage, Status: pipelineState(j.Status), Attempt: 1}
	}
	return out, nil
}

// JobLog returns the raw trace, ANSI and section markers included; the caller closes it.
func (f *Forge) JobLog(ctx context.Context, r domain.RepoRef, jobID int64) (io.ReadCloser, error) {
	resp, err := f.sendAccept(ctx, http.MethodGet, f.url(projectPath(r)+"/jobs/"+strconv.FormatInt(jobID, 10)+"/trace", nil), "text/plain", nil)
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}
