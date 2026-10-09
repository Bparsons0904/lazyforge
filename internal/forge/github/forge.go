package github

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"time"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

type user struct {
	Login string `json:"login"`
}

type label struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

type repo struct {
	Owner            user      `json:"owner"`
	Name             string    `json:"name"`
	Description      string    `json:"description"`
	HTMLURL          string    `json:"html_url"`
	PushedAt         time.Time `json:"pushed_at"`
	Archived         bool      `json:"archived"`
	AllowMergeCommit bool      `json:"allow_merge_commit"` // null unless the token can push, and always null in /user/repos
	AllowSquashMerge bool      `json:"allow_squash_merge"`
	AllowRebaseMerge bool      `json:"allow_rebase_merge"`
	Permissions      struct {
		Admin    bool `json:"admin"`
		Maintain bool `json:"maintain"`
		Push     bool `json:"push"`
		Triage   bool `json:"triage"`
		Pull     bool `json:"pull"`
	} `json:"permissions"`
}

type pull struct {
	Number   int        `json:"number"`
	Title    string     `json:"title"`
	Body     string     `json:"body"`
	User     user       `json:"user"`
	State    string     `json:"state"`
	MergedAt *time.Time `json:"merged_at"`
	Head     struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
	Labels    []label   `json:"labels"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	HTMLURL   string    `json:"html_url"`
}

type issue struct {
	Number      int       `json:"number"`
	Title       string    `json:"title"`
	Body        string    `json:"body"`
	User        user      `json:"user"`
	State       string    `json:"state"`
	Labels      []label   `json:"labels"`
	Comments    int       `json:"comments"`
	UpdatedAt   time.Time `json:"updated_at"`
	HTMLURL     string    `json:"html_url"`
	PullRequest *struct{} `json:"pull_request"` // non-nil marks a PR, which the issues endpoint also returns
}

type comment struct {
	ID        int64     `json:"id"`
	User      user      `json:"user"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

type release struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	Body        string    `json:"body"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
	PublishedAt time.Time `json:"published_at"`
	HTMLURL     string    `json:"html_url"`
}

type checkRun struct {
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
}

type combinedStatus struct {
	State      string `json:"state"`
	TotalCount int    `json:"total_count"`
}

// ListRepos leaves MergeStyle empty: /user/repos omits the allow_* flags, and Merge reads them when it needs them.
func (f *Forge) ListRepos(ctx context.Context) ([]domain.Repo, error) {
	q := url.Values{"affiliation": {"owner,collaborator,organization_member"}, "sort": {"pushed"}}
	rs, err := list[repo](ctx, f, "/user/repos", q)
	if err != nil {
		return nil, err
	}
	out := make([]domain.Repo, len(rs))
	for i, r := range rs {
		out[i] = domain.Repo{
			RepoRef:      domain.RepoRef{Owner: r.Owner.Login, Name: r.Name},
			Description:  r.Description,
			WebURL:       r.HTMLURL,
			LastActivity: r.PushedAt,
			Access:       access(r),
		}
	}
	slices.SortStableFunc(out, func(a, b domain.Repo) int { return b.LastActivity.Compare(a.LastActivity) })
	return out, nil
}

func access(r repo) domain.Access {
	p := r.Permissions
	a := domain.AccessNone
	switch {
	case p.Admin:
		a = domain.AccessAdmin
	case p.Push || p.Maintain:
		a = domain.AccessWrite
	case p.Pull || p.Triage:
		a = domain.AccessRead
	}
	// An archived repo is read-only for everyone, admins included.
	if r.Archived && a > domain.AccessRead {
		a = domain.AccessRead
	}
	return a
}

// ListChangeRequests costs two extra requests per PR for its CI (check runs and combined status); ETags make repeats free.
// ponytail: sequential N+1; fan out with a bounded pool, or move to GraphQL, if a large account's first load stalls.
func (f *Forge) ListChangeRequests(ctx context.Context, r domain.RepoRef, flt forge.Filter) ([]domain.ChangeRequest, error) {
	state := "closed"
	if flt.State == domain.StateOpen {
		state = "open"
	}
	q := url.Values{"state": {state}, "sort": {"updated"}, "direction": {"desc"}}
	ps, err := list[pull](ctx, f, repoPath(r)+"/pulls", q)
	if err != nil {
		return nil, err
	}
	var out []domain.ChangeRequest
	for _, p := range ps {
		if prState(p) != flt.State {
			continue
		}
		cr, err := f.changeRequest(ctx, r, p)
		if err != nil {
			return nil, err
		}
		out = append(out, cr)
	}
	return out, nil
}

// GetChangeRequest returns ErrNotFound for a number that isn't a PR.
func (f *Forge) GetChangeRequest(ctx context.Context, r domain.RepoRef, n int) (domain.ChangeRequest, error) {
	var p pull
	if err := f.call(ctx, http.MethodGet, repoPath(r)+"/pulls/"+strconv.Itoa(n), nil, &p); err != nil {
		return domain.ChangeRequest{}, err
	}
	return f.changeRequest(ctx, r, p)
}

func (f *Forge) changeRequest(ctx context.Context, r domain.RepoRef, p pull) (domain.ChangeRequest, error) {
	ci, err := f.ci(ctx, r, p.Head.SHA)
	if err != nil {
		return domain.ChangeRequest{}, err
	}
	return domain.ChangeRequest{
		Number:       p.Number,
		Title:        p.Title,
		Body:         p.Body,
		Author:       p.User.Login,
		State:        prState(p),
		SourceBranch: p.Head.Ref,
		TargetBranch: p.Base.Ref,
		HeadSHA:      p.Head.SHA,
		CI:           ci,
		Labels:       labelNames(p.Labels),
		LabelColors:  labelColors(p.Labels),
		UpdatedAt:    p.UpdatedAt,
		CreatedAt:    p.CreatedAt,
		WebURL:       p.HTMLURL,
	}, nil
}

func (f *Forge) ci(ctx context.Context, r domain.RepoRef, sha string) (domain.CIState, error) {
	base := repoPath(r) + "/commits/" + url.PathEscape(sha)
	runs, err := walk(ctx, f, base+"/check-runs", nil, func(rd io.Reader) ([]checkRun, error) {
		var page struct {
			CheckRuns []checkRun `json:"check_runs"`
		}
		err := json.NewDecoder(rd).Decode(&page)
		return page.CheckRuns, err
	})
	if err := ciSourceErr(err); err != nil {
		return domain.CINone, err
	}
	var st combinedStatus
	if err := ciSourceErr(f.call(ctx, http.MethodGet, base+"/status", nil, &st)); err != nil {
		return domain.CINone, err
	}
	return foldCI(runs, st), nil
}

// ciSourceErr folds a 404 or 422 to no CI: GitHub answers so for a head SHA it no longer has,
// and one such PR must not fail the whole list.
func ciSourceErr(err error) error {
	if errors.Is(err, forge.ErrNotFound) || errors.Is(err, forge.ErrRefused) {
		return nil
	}
	return err
}

var ciRank = map[domain.CIState]int{
	domain.CINone:      0,
	domain.CISkipped:   1,
	domain.CIPass:      2,
	domain.CIPending:   3,
	domain.CIRunning:   4,
	domain.CICancelled: 5,
	domain.CIFail:      6,
}

func foldCI(runs []checkRun, st combinedStatus) domain.CIState {
	out := domain.CINone
	fold := func(s domain.CIState) {
		if ciRank[s] > ciRank[out] {
			out = s
		}
	}
	for _, r := range runs {
		fold(checkRunState(r))
	}
	// A commit with no statuses reports "pending" with total_count 0; that means no status CI, not waiting.
	if st.TotalCount > 0 {
		fold(statusState(st.State))
	}
	return out
}

// checkRunState maps an unknown status or conclusion to pending, so a value GitHub adds later never reads as green.
func checkRunState(r checkRun) domain.CIState {
	if r.Status != "completed" {
		if r.Status == "in_progress" {
			return domain.CIRunning
		}
		return domain.CIPending
	}
	switch r.Conclusion {
	case "failure", "timed_out", "action_required", "startup_failure":
		return domain.CIFail
	case "cancelled":
		return domain.CICancelled
	case "success", "neutral":
		return domain.CIPass
	case "skipped", "stale":
		return domain.CISkipped
	}
	return domain.CIPending
}

func statusState(s string) domain.CIState {
	switch s {
	case "success":
		return domain.CIPass
	case "failure", "error":
		return domain.CIFail
	}
	return domain.CIPending
}

func prState(p pull) domain.State {
	switch {
	case p.MergedAt != nil:
		return domain.StateMerged
	case p.State == "closed":
		return domain.StateClosed
	}
	return domain.StateOpen
}

func labelColors(ls []label) map[string]string {
	var out map[string]string
	for _, l := range ls {
		if l.Color != "" {
			if out == nil {
				out = make(map[string]string)
			}
			out[l.Name] = l.Color
		}
	}
	return out
}

func labelNames(ls []label) []string {
	var out []string
	for _, l := range ls {
		out = append(out, l.Name)
	}
	return out
}

// Merge with an empty Method reads the repo and takes the first allowed of merge, squash, rebase,
// the order GitHub's merge button offers; with none readable it sends no method and GitHub uses merge.
func (f *Forge) Merge(ctx context.Context, r domain.RepoRef, n int, opts forge.MergeOpts) error {
	if opts.HeadSHA == "" {
		return errors.New("merge: head SHA is required")
	}
	body := map[string]string{"sha": opts.HeadSHA}
	method := opts.Method
	if method == "" {
		var rp repo
		if err := f.call(ctx, http.MethodGet, repoPath(r), nil, &rp); err != nil {
			return err
		}
		method = defaultMergeMethod(rp)
	}
	if method != "" {
		body["merge_method"] = method
	}
	return f.call(ctx, http.MethodPut, repoPath(r)+"/pulls/"+strconv.Itoa(n)+"/merge", body, nil)
}

func defaultMergeMethod(r repo) string {
	switch {
	case r.AllowMergeCommit:
		return "merge"
	case r.AllowSquashMerge:
		return "squash"
	case r.AllowRebaseMerge:
		return "rebase"
	}
	return ""
}

// Approve returns ErrRefused for the user's own PR, which GitHub won't let its author approve.
func (f *Forge) Approve(ctx context.Context, r domain.RepoRef, n int) error {
	body := map[string]string{"event": "APPROVE"}
	return f.call(ctx, http.MethodPost, repoPath(r)+"/pulls/"+strconv.Itoa(n)+"/reviews", body, nil)
}

// ListIssues returns nil for StateMerged without a request; PRs are never included.
func (f *Forge) ListIssues(ctx context.Context, r domain.RepoRef, flt forge.Filter) ([]domain.Issue, error) {
	if flt.State == domain.StateMerged {
		return nil, nil
	}
	state := "open"
	if flt.State == domain.StateClosed {
		state = "closed"
	}
	q := url.Values{"state": {state}, "sort": {"updated"}, "direction": {"desc"}}
	raw, err := list[issue](ctx, f, repoPath(r)+"/issues", q)
	if err != nil {
		return nil, err
	}
	var out []domain.Issue
	for _, is := range raw {
		if is.PullRequest != nil {
			continue
		}
		st := domain.StateOpen
		if is.State == "closed" {
			st = domain.StateClosed
		}
		out = append(out, domain.Issue{
			Number:      is.Number,
			Title:       is.Title,
			Body:        is.Body,
			Author:      is.User.Login,
			State:       st,
			Labels:      labelNames(is.Labels),
			LabelColors: labelColors(is.Labels),
			Comments:    is.Comments,
			UpdatedAt:   is.UpdatedAt,
			WebURL:      is.HTMLURL,
		})
	}
	return out, nil
}

// EditIssueBody replaces the whole body.
func (f *Forge) EditIssueBody(ctx context.Context, r domain.RepoRef, n int, body string) error {
	return f.call(ctx, http.MethodPatch, repoPath(r)+"/issues/"+strconv.Itoa(n), map[string]string{"body": body}, nil)
}

// ListComments serves issues and PRs alike, oldest first; PR review comments are not included.
func (f *Forge) ListComments(ctx context.Context, item forge.ItemRef) ([]domain.Comment, error) {
	cs, err := list[comment](ctx, f, commentsPath(item), nil)
	if err != nil {
		return nil, err
	}
	out := make([]domain.Comment, len(cs))
	for i, c := range cs {
		out[i] = domain.Comment{ID: c.ID, Author: c.User.Login, Body: c.Body, CreatedAt: c.CreatedAt}
	}
	return out, nil
}

// Comment works for issues and PRs alike.
func (f *Forge) Comment(ctx context.Context, item forge.ItemRef, body string) error {
	return f.call(ctx, http.MethodPost, commentsPath(item), map[string]string{"body": body}, nil)
}

func commentsPath(item forge.ItemRef) string {
	return repoPath(item.Repo) + "/issues/" + strconv.Itoa(item.Number) + "/comments"
}

// Close works on issues and PRs; closing a PR doesn't delete its branch.
func (f *Forge) Close(ctx context.Context, item forge.ItemRef) error {
	kind := "/issues/"
	if item.Kind == forge.ItemChangeRequest {
		kind = "/pulls/"
	}
	path := repoPath(item.Repo) + kind + strconv.Itoa(item.Number)
	return f.call(ctx, http.MethodPatch, path, map[string]string{"state": "closed"}, nil)
}

// ListReleases includes drafts (when the token can push) and prereleases.
func (f *Forge) ListReleases(ctx context.Context, r domain.RepoRef) ([]domain.Release, error) {
	rs, err := list[release](ctx, f, repoPath(r)+"/releases", nil)
	if err != nil {
		return nil, err
	}
	out := make([]domain.Release, len(rs))
	for i, rl := range rs {
		out[i] = domain.Release{
			Tag:         rl.TagName,
			Name:        rl.Name,
			Notes:       rl.Body,
			Draft:       rl.Draft,
			Prerelease:  rl.Prerelease,
			PublishedAt: rl.PublishedAt,
			WebURL:      rl.HTMLURL,
		}
	}
	return out, nil
}
