// Package forgetest holds the in-memory Fake forge and the contract suite adapters run; it is test support and demo mode.
package forgetest

import (
	"context"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

var (
	_ forge.Forge        = (*Fake)(nil)
	_ forge.Approver     = (*Fake)(nil)
	_ forge.RunLister    = (*Fake)(nil)
	_ forge.LogReader    = (*Fake)(nil)
	_ forge.AssetReader  = (*Fake)(nil)
	_ forge.ReadmeReader = (*Fake)(nil)
)

// Mutation records one state-changing call; Op is merge, approve, close, comment or edit-issue-body.
type Mutation struct {
	Op   string
	Item forge.ItemRef
	Body string
	Opts forge.MergeOpts
}

// Fake is an in-memory forge for tests; seed it, then use it as a forge.Forge.
type Fake struct {
	mu        sync.Mutex
	info      forge.HostInfo
	known     map[domain.RepoRef]bool
	labels    map[domain.RepoRef][]domain.Label
	repos     []domain.Repo
	crs       map[domain.RepoRef][]domain.ChangeRequest
	issues    map[domain.RepoRef][]domain.Issue
	releases  map[domain.RepoRef][]domain.Release
	readmes   map[domain.RepoRef]domain.Readme
	runs      map[domain.RepoRef][]domain.Run
	jobs      map[int64][]domain.Job
	logs      map[int64]string
	comments  map[forge.ItemRef][]domain.Comment
	assets    map[string][]byte
	gates     map[forge.Action]error
	failNext  error
	mutations []Mutation
	nextID    int64
}

// NewFake returns an empty Fake reporting info.
func NewFake(info forge.HostInfo) *Fake {
	return &Fake{
		info:     info,
		known:    map[domain.RepoRef]bool{},
		labels:   map[domain.RepoRef][]domain.Label{},
		crs:      map[domain.RepoRef][]domain.ChangeRequest{},
		issues:   map[domain.RepoRef][]domain.Issue{},
		releases: map[domain.RepoRef][]domain.Release{},
		readmes:  map[domain.RepoRef]domain.Readme{},
		runs:     map[domain.RepoRef][]domain.Run{},
		jobs:     map[int64][]domain.Job{},
		logs:     map[int64]string{},
		comments: map[forge.ItemRef][]domain.Comment{},
		assets:   map[string][]byte{},
		gates:    map[forge.Action]error{},
	}
}

// AddRepo seeds a repo.
func (f *Fake) AddRepo(r domain.Repo) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.known[r.RepoRef] = true
	f.repos = append(f.repos, r)
}

// AddChangeRequest seeds a change request; the repo becomes known even without AddRepo.
func (f *Fake) AddChangeRequest(r domain.RepoRef, cr domain.ChangeRequest) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.known[r] = true
	f.crs[r] = append(f.crs[r], cloneCR(cr))
}

// AddIssue seeds an issue; the repo becomes known even without AddRepo.
func (f *Fake) AddIssue(r domain.RepoRef, is domain.Issue) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.known[r] = true
	is.Labels = slices.Clone(is.Labels)
	is.LabelColors = maps.Clone(is.LabelColors)
	f.issues[r] = append(f.issues[r], is)
}

// AddRelease seeds a release; the repo becomes known even without AddRepo.
func (f *Fake) AddRelease(r domain.RepoRef, rel domain.Release) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.known[r] = true
	f.releases[r] = append(f.releases[r], rel)
}

// SetReadme seeds a repo's README and makes the repo known; a known repo without one answers ErrNotFound.
func (f *Fake) SetReadme(r domain.RepoRef, rd domain.Readme) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.known[r] = true
	f.readmes[r] = rd
}

// AddRun seeds a run with its jobs and per-job log text keyed by job ID.
func (f *Fake) AddRun(r domain.RepoRef, run domain.Run, jobs []domain.Job, logs map[int64]string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.known[r] = true
	f.runs[r] = append(f.runs[r], run)
	f.jobs[run.ID] = slices.Clone(jobs)
	for id, l := range logs {
		f.logs[id] = l
	}
}

// SetGate makes Gate(a) return err; nil clears it.
func (f *Fake) SetGate(a forge.Action, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gates[a] = err
}

// FailNext makes the next forge call return err, then clears itself.
func (f *Fake) FailNext(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failNext = err
}

// Mutations returns the recorded state-changing calls in order.
func (f *Fake) Mutations() []Mutation {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.mutations)
}

// Info implements forge.Forge.
func (f *Fake) Info() forge.HostInfo { return f.info }

// Gate implements forge.Forge.
func (f *Fake) Gate(a forge.Action) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.gates[a]
}

// begin locks the Fake and applies the cancellation and FailNext checks every call shares.
func (f *Fake) begin(ctx context.Context) error {
	f.mu.Lock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := f.failNext; err != nil {
		f.failNext = nil
		return err
	}
	return nil
}

// ListRepos implements forge.Forge.
func (f *Fake) ListRepos(ctx context.Context) ([]domain.Repo, error) {
	defer f.mu.Unlock()
	if err := f.begin(ctx); err != nil {
		return nil, err
	}
	out := slices.Clone(f.repos)
	slices.SortStableFunc(out, func(a, b domain.Repo) int { return b.LastActivity.Compare(a.LastActivity) })
	return out, nil
}

// ListChangeRequests implements forge.Forge.
func (f *Fake) ListChangeRequests(ctx context.Context, r domain.RepoRef, flt forge.Filter) ([]domain.ChangeRequest, error) {
	defer f.mu.Unlock()
	if err := f.begin(ctx); err != nil {
		return nil, err
	}
	if err := f.repoErr(r); err != nil {
		return nil, err
	}
	var out []domain.ChangeRequest
	for _, cr := range f.crs[r] {
		if cr.State == flt.State {
			out = append(out, cloneCR(cr))
		}
	}
	return out, nil
}

// GetChangeRequest implements forge.Forge.
func (f *Fake) GetChangeRequest(ctx context.Context, r domain.RepoRef, n int) (domain.ChangeRequest, error) {
	defer f.mu.Unlock()
	if err := f.begin(ctx); err != nil {
		return domain.ChangeRequest{}, err
	}
	cr, err := f.cr(r, n)
	if err != nil {
		return domain.ChangeRequest{}, err
	}
	return cloneCR(*cr), nil
}

// Merge implements forge.Forge; an empty HeadSHA is a plain error, not ErrHeadChanged.
func (f *Fake) Merge(ctx context.Context, r domain.RepoRef, n int, opts forge.MergeOpts) error {
	defer f.mu.Unlock()
	if err := f.begin(ctx); err != nil {
		return err
	}
	cr, err := f.cr(r, n)
	if err != nil {
		return err
	}
	if opts.HeadSHA == "" {
		return fmt.Errorf("merge %s#%d: head sha is required", r, n)
	}
	if cr.State != domain.StateOpen {
		return fmt.Errorf("merge %s#%d: %w: change request is not open", r, n, forge.ErrRefused)
	}
	if opts.HeadSHA != cr.HeadSHA {
		return fmt.Errorf("merge %s#%d: %w", r, n, forge.ErrHeadChanged)
	}
	cr.State = domain.StateMerged
	f.record(Mutation{Op: "merge", Item: forge.ItemRef{Repo: r, Kind: forge.ItemChangeRequest, Number: n}, Opts: opts})
	return nil
}

// ListIssues implements forge.Forge.
func (f *Fake) ListIssues(ctx context.Context, r domain.RepoRef, flt forge.Filter) ([]domain.Issue, error) {
	defer f.mu.Unlock()
	if err := f.begin(ctx); err != nil {
		return nil, err
	}
	if err := f.repoErr(r); err != nil {
		return nil, err
	}
	var out []domain.Issue
	for _, is := range f.issues[r] {
		if is.State == flt.State {
			is.Labels = slices.Clone(is.Labels)
			is.LabelColors = maps.Clone(is.LabelColors)
			out = append(out, is)
		}
	}
	return out, nil
}

// EditIssueBody implements forge.Forge.
func (f *Fake) EditIssueBody(ctx context.Context, r domain.RepoRef, n int, body string) error {
	defer f.mu.Unlock()
	if err := f.begin(ctx); err != nil {
		return err
	}
	is, err := f.issue(r, n)
	if err != nil {
		return err
	}
	is.Body = body
	f.record(Mutation{Op: "edit-issue-body", Item: forge.ItemRef{Repo: r, Kind: forge.ItemIssue, Number: n}, Body: body})
	return nil
}

// ListComments implements forge.Forge.
func (f *Fake) ListComments(ctx context.Context, item forge.ItemRef) ([]domain.Comment, error) {
	defer f.mu.Unlock()
	if err := f.begin(ctx); err != nil {
		return nil, err
	}
	if err := f.itemErr(item); err != nil {
		return nil, err
	}
	return slices.Clone(f.comments[item]), nil
}

// Comment implements forge.Forge.
func (f *Fake) Comment(ctx context.Context, item forge.ItemRef, body string) error {
	defer f.mu.Unlock()
	if err := f.begin(ctx); err != nil {
		return err
	}
	if err := f.itemErr(item); err != nil {
		return err
	}
	f.nextID++
	f.comments[item] = append(f.comments[item], domain.Comment{
		ID: f.nextID, Author: f.info.User, Body: body, CreatedAt: time.Now(),
	})
	if item.Kind == forge.ItemIssue {
		is, _ := f.issue(item.Repo, item.Number)
		is.Comments++
	}
	f.record(Mutation{Op: "comment", Item: item, Body: body})
	return nil
}

// Close implements forge.Forge.
func (f *Fake) Close(ctx context.Context, item forge.ItemRef) error {
	defer f.mu.Unlock()
	if err := f.begin(ctx); err != nil {
		return err
	}
	if item.Kind == forge.ItemIssue {
		is, err := f.issue(item.Repo, item.Number)
		if err != nil {
			return err
		}
		is.State = domain.StateClosed
	} else {
		cr, err := f.cr(item.Repo, item.Number)
		if err != nil {
			return err
		}
		cr.State = domain.StateClosed
	}
	f.record(Mutation{Op: "close", Item: item})
	return nil
}

// ListReleases implements forge.Forge.
func (f *Fake) ListReleases(ctx context.Context, r domain.RepoRef) ([]domain.Release, error) {
	defer f.mu.Unlock()
	if err := f.begin(ctx); err != nil {
		return nil, err
	}
	if err := f.repoErr(r); err != nil {
		return nil, err
	}
	return slices.Clone(f.releases[r]), nil
}

// GetReadme implements forge.ReadmeReader.
func (f *Fake) GetReadme(ctx context.Context, r domain.RepoRef) (domain.Readme, error) {
	defer f.mu.Unlock()
	if err := f.begin(ctx); err != nil {
		return domain.Readme{}, err
	}
	if err := f.repoErr(r); err != nil {
		return domain.Readme{}, err
	}
	rd, ok := f.readmes[r]
	if !ok {
		return domain.Readme{}, fmt.Errorf("README of %s: %w", r, forge.ErrNotFound)
	}
	return rd, nil
}

// Approve implements forge.Approver.
func (f *Fake) Approve(ctx context.Context, r domain.RepoRef, n int) error {
	defer f.mu.Unlock()
	if err := f.begin(ctx); err != nil {
		return err
	}
	if _, err := f.cr(r, n); err != nil {
		return err
	}
	f.record(Mutation{Op: "approve", Item: forge.ItemRef{Repo: r, Kind: forge.ItemChangeRequest, Number: n}})
	return nil
}

// ListRuns implements forge.RunLister; RunFilter.HeadSHA matches Run.Commit.
func (f *Fake) ListRuns(ctx context.Context, r domain.RepoRef, flt forge.RunFilter) ([]domain.Run, error) {
	defer f.mu.Unlock()
	if err := f.begin(ctx); err != nil {
		return nil, err
	}
	if err := f.repoErr(r); err != nil {
		return nil, err
	}
	var out []domain.Run
	for _, run := range f.runs[r] {
		if (flt.Branch == "" || run.Branch == flt.Branch) && (flt.HeadSHA == "" || run.Commit == flt.HeadSHA) {
			out = append(out, run)
		}
	}
	return out, nil
}

// ListJobs implements forge.RunLister.
func (f *Fake) ListJobs(ctx context.Context, r domain.RepoRef, runID int64) ([]domain.Job, error) {
	defer f.mu.Unlock()
	if err := f.begin(ctx); err != nil {
		return nil, err
	}
	if err := f.repoErr(r); err != nil {
		return nil, err
	}
	if !slices.ContainsFunc(f.runs[r], func(run domain.Run) bool { return run.ID == runID }) {
		return nil, fmt.Errorf("run %d in %s: %w", runID, r, forge.ErrNotFound)
	}
	return slices.Clone(f.jobs[runID]), nil
}

// JobLog implements forge.LogReader.
func (f *Fake) JobLog(ctx context.Context, r domain.RepoRef, jobID int64) (io.ReadCloser, error) {
	defer f.mu.Unlock()
	if err := f.begin(ctx); err != nil {
		return nil, err
	}
	if err := f.repoErr(r); err != nil {
		return nil, err
	}
	for _, run := range f.runs[r] {
		if slices.ContainsFunc(f.jobs[run.ID], func(j domain.Job) bool { return j.ID == jobID }) {
			return io.NopCloser(strings.NewReader(f.logs[jobID])), nil
		}
	}
	return nil, fmt.Errorf("job %d in %s: %w", jobID, r, forge.ErrNotFound)
}

func (f *Fake) record(m Mutation) { f.mutations = append(f.mutations, m) }

func (f *Fake) repoErr(r domain.RepoRef) error {
	if !f.known[r] {
		return fmt.Errorf("repo %s: %w", r, forge.ErrNotFound)
	}
	return nil
}

func (f *Fake) cr(r domain.RepoRef, n int) (*domain.ChangeRequest, error) {
	if err := f.repoErr(r); err != nil {
		return nil, err
	}
	for i := range f.crs[r] {
		if f.crs[r][i].Number == n {
			return &f.crs[r][i], nil
		}
	}
	return nil, fmt.Errorf("change request %s#%d: %w", r, n, forge.ErrNotFound)
}

func (f *Fake) issue(r domain.RepoRef, n int) (*domain.Issue, error) {
	if err := f.repoErr(r); err != nil {
		return nil, err
	}
	for i := range f.issues[r] {
		if f.issues[r][i].Number == n {
			return &f.issues[r][i], nil
		}
	}
	return nil, fmt.Errorf("issue %s#%d: %w", r, n, forge.ErrNotFound)
}

func (f *Fake) itemErr(item forge.ItemRef) error {
	var err error
	if item.Kind == forge.ItemIssue {
		_, err = f.issue(item.Repo, item.Number)
	} else {
		_, err = f.cr(item.Repo, item.Number)
	}
	return err
}

func cloneCR(cr domain.ChangeRequest) domain.ChangeRequest {
	cr.Labels = slices.Clone(cr.Labels)
	cr.LabelColors = maps.Clone(cr.LabelColors)
	cr.Renovate = slices.Clone(cr.Renovate)
	return cr
}
