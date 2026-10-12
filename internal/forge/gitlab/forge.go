package gitlab

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

type user struct {
	Username string `json:"username"`
}

type accessGrant struct {
	AccessLevel int `json:"access_level"`
}

type project struct {
	PathWithNamespace string    `json:"path_with_namespace"`
	Description       string    `json:"description"`
	WebURL            string    `json:"web_url"`
	DefaultBranch     string    `json:"default_branch"`
	LastActivityAt    time.Time `json:"last_activity_at"`
	Archived          bool      `json:"archived"`
	MergeMethod       string    `json:"merge_method"`
	SquashOption      string    `json:"squash_option"`
	Permissions       *struct {
		ProjectAccess *accessGrant `json:"project_access"`
		GroupAccess   *accessGrant `json:"group_access"`
	} `json:"permissions"`
}

type pipeline struct {
	ID     int64  `json:"id"`
	SHA    string `json:"sha"`
	Status string `json:"status"`
}

type mergeRequest struct {
	IID          int       `json:"iid"`
	Title        string    `json:"title"`
	Description  string    `json:"description"`
	Author       user      `json:"author"`
	State        string    `json:"state"`
	SourceBranch string    `json:"source_branch"`
	TargetBranch string    `json:"target_branch"`
	SHA          string    `json:"sha"`
	Labels       []string  `json:"labels"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	WebURL       string    `json:"web_url"`
	HeadPipeline *pipeline `json:"head_pipeline"` // only on the single-MR GET
}

type issue struct {
	IID            int       `json:"iid"`
	Title          string    `json:"title"`
	Description    string    `json:"description"`
	Author         user      `json:"author"`
	State          string    `json:"state"`
	Labels         []string  `json:"labels"`
	UserNotesCount int       `json:"user_notes_count"`
	UpdatedAt      time.Time `json:"updated_at"`
	WebURL         string    `json:"web_url"`
}

type note struct {
	ID        int64     `json:"id"`
	Author    user      `json:"author"`
	Body      string    `json:"body"`
	System    bool      `json:"system"`
	CreatedAt time.Time `json:"created_at"`
}

type release struct {
	TagName     string     `json:"tag_name"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	ReleasedAt  *time.Time `json:"released_at"`
	CreatedAt   time.Time  `json:"created_at"`
	Links       struct {
		Self string `json:"self"`
	} `json:"_links"`
}

// ListRepos skips archived projects. GitLab often reports null permissions for inherited membership,
// and then one extra paged list of the projects the user can write to settles them.
func (f *Forge) ListRepos(ctx context.Context) ([]domain.Repo, error) {
	q := url.Values{"membership": {"true"}, "archived": {"false"}, "order_by": {"last_activity_at"}, "sort": {"desc"}}
	ps, err := list[project](ctx, f, "/projects", q)
	if err != nil {
		return nil, err
	}
	var writable map[string]bool
	if slices.ContainsFunc(ps, func(p project) bool { return grantedLevel(p) == 0 }) {
		q := url.Values{"membership": {"true"}, "min_access_level": {"30"}, "simple": {"true"}}
		ws, err := list[project](ctx, f, "/projects", q)
		if err != nil {
			return nil, err
		}
		writable = make(map[string]bool, len(ws))
		for _, w := range ws {
			writable[w.PathWithNamespace] = true
		}
	}
	out := make([]domain.Repo, len(ps))
	for i, p := range ps {
		out[i] = domain.Repo{
			RepoRef:       repoRef(p.PathWithNamespace),
			Description:   p.Description,
			WebURL:        p.WebURL,
			LastActivity:  p.LastActivityAt,
			Access:        access(p, writable[p.PathWithNamespace]),
			MergeStyle:    mergeStyle(p),
			DefaultBranch: p.DefaultBranch,
		}
	}
	slices.SortStableFunc(out, func(a, b domain.Repo) int { return b.LastActivity.Compare(a.LastActivity) })
	return out, nil
}

func repoRef(fullPath string) domain.RepoRef {
	i := strings.LastIndex(fullPath, "/")
	return domain.RepoRef{Owner: fullPath[:max(i, 0)], Name: fullPath[i+1:]}
}

// grantedLevel is the higher of the project and group access levels, 0 when GitLab reports neither.
func grantedLevel(p project) int {
	if p.Permissions == nil {
		return 0
	}
	level := 0
	for _, g := range []*accessGrant{p.Permissions.ProjectAccess, p.Permissions.GroupAccess} {
		if g != nil {
			level = max(level, g.AccessLevel)
		}
	}
	return level
}

// access takes writable from the min_access_level=30 list, used only when the project reports no level.
func access(p project, writable bool) domain.Access {
	level := grantedLevel(p)
	if level == 0 && writable {
		level = 30
	}
	a := domain.AccessRead // listed with membership=true, so at least Guest
	switch {
	case level >= 40:
		a = domain.AccessAdmin
	case level >= 30:
		a = domain.AccessWrite
	}
	if p.Archived {
		a = min(a, domain.AccessRead)
	}
	return a
}

// mergeStyle uses the strings the gitea adapter hands the merge dialog; "" for a method GitLab adds later.
func mergeStyle(p project) string {
	if squashes(p.SquashOption) {
		return "squash"
	}
	switch p.MergeMethod {
	case "merge":
		return "merge"
	case "rebase_merge":
		return "rebase-merge"
	case "ff":
		return "fast-forward-only"
	}
	return ""
}

func squashes(option string) bool { return option == "always" || option == "default_on" }

// ListChangeRequests takes CI from one page of the project's newest pipelines, matched on the MR's head SHA.
// An open MR with no match costs one GET for its head_pipeline (a merged-results pipeline has another SHA); a merged
// or closed one gets CINone. A pipelines listing that is refused or missing, as with CI/CD disabled, counts as empty.
func (f *Forge) ListChangeRequests(ctx context.Context, r domain.RepoRef, flt forge.Filter) ([]domain.ChangeRequest, error) {
	state := map[domain.State]string{domain.StateOpen: "opened", domain.StateMerged: "merged", domain.StateClosed: "closed"}[flt.State]
	q := url.Values{"state": {state}, "order_by": {"updated_at"}, "sort": {"desc"}}
	mrs, err := list[mergeRequest](ctx, f, projectPath(r)+"/merge_requests", q)
	if err != nil || len(mrs) == 0 {
		return nil, err
	}
	var pipes []pipeline
	pq := url.Values{"per_page": {pageSize}, "order_by": {"id"}, "sort": {"desc"}}
	if err := f.callQuery(ctx, http.MethodGet, projectPath(r)+"/pipelines", pq, nil, &pipes); err != nil && !errors.Is(err, forge.ErrUnauthorized) && !errors.Is(err, forge.ErrNotFound) {
		return nil, err
	}
	out := make([]domain.ChangeRequest, len(mrs))
	for i, mr := range mrs {
		p := pipelineFor(pipes, mr.SHA)
		if p == nil && mr.State == "opened" {
			full, err := f.getMR(ctx, r, mr.IID)
			if err != nil {
				return nil, err
			}
			p = full.HeadPipeline
		}
		out[i] = changeRequest(mr, p)
	}
	return out, nil
}

// pipelineFor returns the newest pipeline for sha from a list sorted newest first, or nil.
func pipelineFor(pipes []pipeline, sha string) *pipeline {
	if sha == "" {
		return nil
	}
	i := slices.IndexFunc(pipes, func(p pipeline) bool { return p.SHA == sha })
	if i < 0 {
		return nil
	}
	return &pipes[i]
}

// GetChangeRequest returns ErrNotFound for a number that isn't an MR.
func (f *Forge) GetChangeRequest(ctx context.Context, r domain.RepoRef, n int) (domain.ChangeRequest, error) {
	mr, err := f.getMR(ctx, r, n)
	if err != nil {
		return domain.ChangeRequest{}, err
	}
	return changeRequest(mr, mr.HeadPipeline), nil
}

func (f *Forge) getMR(ctx context.Context, r domain.RepoRef, n int) (mergeRequest, error) {
	var mr mergeRequest
	err := f.call(ctx, http.MethodGet, mrPath(r, n), nil, &mr)
	return mr, err
}

func mrPath(r domain.RepoRef, n int) string {
	return projectPath(r) + "/merge_requests/" + strconv.Itoa(n)
}

func changeRequest(mr mergeRequest, p *pipeline) domain.ChangeRequest {
	ci := domain.CINone
	if p != nil {
		ci = pipelineState(p.Status)
	}
	state := domain.StateOpen
	switch mr.State {
	case "merged":
		state = domain.StateMerged
	case "closed":
		state = domain.StateClosed
	}
	return domain.ChangeRequest{
		Number:       mr.IID,
		Title:        mr.Title,
		Body:         mr.Description,
		Author:       mr.Author.Username,
		State:        state,
		SourceBranch: mr.SourceBranch,
		TargetBranch: mr.TargetBranch,
		HeadSHA:      mr.SHA,
		CI:           ci,
		Labels:       mr.Labels,
		UpdatedAt:    mr.UpdatedAt,
		CreatedAt:    mr.CreatedAt,
		WebURL:       mr.WebURL,
	}
}

// pipelineState maps an unknown status to pending, so a value GitLab adds later never reads as green.
func pipelineState(status string) domain.CIState {
	switch status {
	case "failed":
		return domain.CIFail
	case "canceled", "canceling":
		return domain.CICancelled
	case "running":
		return domain.CIRunning
	case "success":
		return domain.CIPass
	case "skipped":
		return domain.CISkipped
	}
	return domain.CIPending
}

// Merge reads the project for its squash option; GitLab has no per-call method, so a Method other than
// the project's own is ErrUnsupported.
func (f *Forge) Merge(ctx context.Context, r domain.RepoRef, n int, opts forge.MergeOpts) error {
	if opts.HeadSHA == "" {
		return errors.New("merge: head SHA is required")
	}
	var p project
	if err := f.call(ctx, http.MethodGet, projectPath(r), nil, &p); err != nil {
		return err
	}
	if opts.Method != "" && opts.Method != mergeStyle(p) {
		return fmt.Errorf("merge with %q: the project merges with %q: %w", opts.Method, mergeStyle(p), forge.ErrUnsupported)
	}
	body := map[string]any{"sha": opts.HeadSHA, "squash": squashes(p.SquashOption)}
	return f.call(ctx, http.MethodPut, mrPath(r, n)+"/merge", body, nil)
}

// Approve returns ErrRefused when GitLab won't take the approval, such as one already given.
func (f *Forge) Approve(ctx context.Context, r domain.RepoRef, n int) error {
	return f.call(ctx, http.MethodPost, mrPath(r, n)+"/approve", nil, nil)
}

// ListIssues returns nil for StateMerged without a request; incidents, test cases and tasks are never included.
func (f *Forge) ListIssues(ctx context.Context, r domain.RepoRef, flt forge.Filter) ([]domain.Issue, error) {
	if flt.State == domain.StateMerged {
		return nil, nil
	}
	state := "opened"
	if flt.State == domain.StateClosed {
		state = "closed"
	}
	q := url.Values{"state": {state}, "issue_type": {"issue"}, "order_by": {"updated_at"}, "sort": {"desc"}}
	raw, err := list[issue](ctx, f, projectPath(r)+"/issues", q)
	if err != nil {
		return nil, err
	}
	out := make([]domain.Issue, len(raw))
	for i, is := range raw {
		st := domain.StateOpen
		if is.State == "closed" {
			st = domain.StateClosed
		}
		out[i] = domain.Issue{
			Number:    is.IID,
			Title:     is.Title,
			Body:      is.Description,
			Author:    is.Author.Username,
			State:     st,
			Labels:    is.Labels,
			Comments:  is.UserNotesCount,
			UpdatedAt: is.UpdatedAt,
			WebURL:    is.WebURL,
		}
	}
	return out, nil
}

// EditIssueBody replaces the whole description.
func (f *Forge) EditIssueBody(ctx context.Context, r domain.RepoRef, n int, body string) error {
	path := projectPath(r) + "/issues/" + strconv.Itoa(n)
	return f.call(ctx, http.MethodPut, path, map[string]string{"description": body}, nil)
}

// ListComments returns user notes oldest first; GitLab's system notes ("changed the description") are dropped.
func (f *Forge) ListComments(ctx context.Context, item forge.ItemRef) ([]domain.Comment, error) {
	q := url.Values{"sort": {"asc"}, "order_by": {"created_at"}}
	ns, err := list[note](ctx, f, itemPath(item)+"/notes", q)
	if err != nil {
		return nil, err
	}
	var out []domain.Comment
	for _, n := range ns {
		if n.System {
			continue
		}
		out = append(out, domain.Comment{ID: n.ID, Author: n.Author.Username, Body: n.Body, CreatedAt: n.CreatedAt})
	}
	return out, nil
}

// Comment works for issues and MRs alike.
func (f *Forge) Comment(ctx context.Context, item forge.ItemRef, body string) error {
	return f.call(ctx, http.MethodPost, itemPath(item)+"/notes", map[string]string{"body": body}, nil)
}

func itemPath(item forge.ItemRef) string {
	kind := "/issues/"
	if item.Kind == forge.ItemChangeRequest {
		kind = "/merge_requests/"
	}
	return projectPath(item.Repo) + kind + strconv.Itoa(item.Number)
}

// Close works on issues and MRs; closing an MR doesn't delete its branch.
func (f *Forge) Close(ctx context.Context, item forge.ItemRef) error {
	return f.call(ctx, http.MethodPut, itemPath(item), map[string]string{"state_event": "close"}, nil)
}

// ListReleases never reports a draft or prerelease, since GitLab releases have neither.
func (f *Forge) ListReleases(ctx context.Context, r domain.RepoRef) ([]domain.Release, error) {
	rs, err := list[release](ctx, f, projectPath(r)+"/releases", nil)
	if err != nil {
		return nil, err
	}
	out := make([]domain.Release, len(rs))
	for i, rl := range rs {
		published := rl.CreatedAt
		if rl.ReleasedAt != nil {
			published = *rl.ReleasedAt
		}
		out[i] = domain.Release{Tag: rl.TagName, Name: rl.Name, Notes: rl.Description, PublishedAt: published, WebURL: rl.Links.Self}
	}
	return out, nil
}
