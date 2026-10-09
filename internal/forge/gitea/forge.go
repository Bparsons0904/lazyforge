package gitea

import (
	"context"
	"errors"
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
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

type repo struct {
	Owner             user      `json:"owner"`
	Name              string    `json:"name"`
	Description       string    `json:"description"`
	HTMLURL           string    `json:"html_url"`
	UpdatedAt         time.Time `json:"updated_at"`
	DefaultMergeStyle string    `json:"default_merge_style"`
	Permissions       struct {
		Admin bool `json:"admin"`
		Push  bool `json:"push"`
		Pull  bool `json:"pull"`
	} `json:"permissions"`
}

type asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

func attachments(as []asset) []domain.Attachment {
	var out []domain.Attachment
	for _, a := range as {
		out = append(out, domain.Attachment{Name: a.Name, URL: a.URL})
	}
	return out
}

type pull struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	User   user   `json:"user"`
	State  string `json:"state"`
	Merged bool   `json:"merged"`
	Head   struct {
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
	Assets    []asset   `json:"assets"`
}

type issue struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	User      user      `json:"user"`
	State     string    `json:"state"`
	Labels    []label   `json:"labels"`
	Comments  int       `json:"comments"`
	UpdatedAt time.Time `json:"updated_at"`
	HTMLURL   string    `json:"html_url"`
	Assets    []asset   `json:"assets"`
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

// ListRepos sorts by updated_at, which moves on push and settings changes but not on PR or issue activity.
// ponytail: derive activity from the latest PR or issue if the ★ ordering feels wrong.
func (f *Forge) ListRepos(ctx context.Context) ([]domain.Repo, error) {
	// /user/repos covers owned, collaborator and team-reached org repos; /repos/search without uid lists the whole instance.
	rs, err := list[repo](ctx, f, "/user/repos", nil)
	if err != nil {
		return nil, err
	}
	out := make([]domain.Repo, len(rs))
	for i, r := range rs {
		access := domain.AccessNone
		switch {
		case r.Permissions.Admin:
			access = domain.AccessAdmin
		case r.Permissions.Push:
			access = domain.AccessWrite
		case r.Permissions.Pull:
			access = domain.AccessRead
		}
		out[i] = domain.Repo{
			RepoRef:      domain.RepoRef{Owner: r.Owner.Login, Name: r.Name},
			Description:  r.Description,
			WebURL:       r.HTMLURL,
			LastActivity: r.UpdatedAt,
			Access:       access,
			MergeStyle:   r.DefaultMergeStyle,
		}
	}
	slices.SortStableFunc(out, func(a, b domain.Repo) int { return b.LastActivity.Compare(a.LastActivity) })
	return out, nil
}

// ListChangeRequests costs one extra request per PR for its combined CI status.
// ponytail: sequential N+1; fan out with a bounded pool if it measurably stalls.
func (f *Forge) ListChangeRequests(ctx context.Context, r domain.RepoRef, flt forge.Filter) ([]domain.ChangeRequest, error) {
	state := "closed"
	if flt.State == domain.StateOpen {
		state = "open"
	}
	q := url.Values{"state": {state}, "sort": {"recentupdate"}}
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
	var st struct {
		State string `json:"state"`
	}
	if err := f.call(ctx, http.MethodGet, repoPath(r)+"/commits/"+url.PathEscape(p.Head.SHA)+"/status", nil, &st); err != nil {
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
		CI:           combinedCI(st.State),
		Labels:       labelNames(p.Labels),
		LabelColors:  labelColors(p.Labels),
		UpdatedAt:    p.UpdatedAt,
		CreatedAt:    p.CreatedAt,
		WebURL:       p.HTMLURL,
		Attachments:  attachments(p.Assets),
	}, nil
}

func prState(p pull) domain.State {
	switch {
	case p.Merged:
		return domain.StateMerged
	case p.State == "closed":
		return domain.StateClosed
	}
	return domain.StateOpen
}

func combinedCI(s string) domain.CIState {
	switch s {
	// Forgejo reports running Actions jobs as pending, so running can't be told apart here.
	case "pending":
		return domain.CIPending
	case "success":
		return domain.CIPass
	// warning counts as a failure so bulk merge never treats it as green.
	case "failure", "error", "warning":
		return domain.CIFail
	case "skipped":
		return domain.CISkipped
	}
	return domain.CINone
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

// Merge with an empty Method uses the repo's default_merge_style, because Forgejo itself falls back to "merge".
func (f *Forge) Merge(ctx context.Context, r domain.RepoRef, n int, opts forge.MergeOpts) error {
	if opts.HeadSHA == "" {
		return errors.New("merge: head SHA is required")
	}
	do := opts.Method
	if do == "" {
		var rp repo
		if err := f.call(ctx, http.MethodGet, repoPath(r), nil, &rp); err != nil {
			return err
		}
		do = rp.DefaultMergeStyle
		if do == "" {
			do = "merge"
		}
	}
	body := map[string]string{"Do": do, "head_commit_id": opts.HeadSHA}
	return f.call(ctx, http.MethodPost, repoPath(r)+"/pulls/"+strconv.Itoa(n)+"/merge", body, nil)
}

// Approve submits an APPROVED review with no body.
func (f *Forge) Approve(ctx context.Context, r domain.RepoRef, n int) error {
	body := map[string]string{"event": "APPROVED"}
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
	raw, err := list[issue](ctx, f, repoPath(r)+"/issues", url.Values{"type": {"issues"}, "state": {state}})
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
			Attachments: attachments(is.Assets),
		}
	}
	return out, nil
}

// EditIssueBody replaces the whole body.
func (f *Forge) EditIssueBody(ctx context.Context, r domain.RepoRef, n int, body string) error {
	return f.call(ctx, http.MethodPatch, repoPath(r)+"/issues/"+strconv.Itoa(n), map[string]string{"body": body}, nil)
}

// ListComments serves issues and PRs alike; the endpoint is unpaginated and returns every comment.
func (f *Forge) ListComments(ctx context.Context, item forge.ItemRef) ([]domain.Comment, error) {
	var cs []comment
	if err := f.call(ctx, http.MethodGet, commentsPath(item), nil, &cs); err != nil {
		return nil, err
	}
	out := make([]domain.Comment, len(cs))
	for i, c := range cs {
		out[i] = domain.Comment{ID: c.ID, Author: c.User.Login, Body: c.Body, CreatedAt: c.CreatedAt}
	}
	return out, nil
}

// Comment posts to the issue comments endpoint for both issues and PRs.
func (f *Forge) Comment(ctx context.Context, item forge.ItemRef, body string) error {
	return f.call(ctx, http.MethodPost, commentsPath(item), map[string]string{"body": body}, nil)
}

func commentsPath(item forge.ItemRef) string {
	return repoPath(item.Repo) + "/issues/" + strconv.Itoa(item.Number) + "/comments"
}

// Close patches pulls/{n} for a change request and issues/{n} otherwise.
func (f *Forge) Close(ctx context.Context, item forge.ItemRef) error {
	kind := "/issues/"
	if item.Kind == forge.ItemChangeRequest {
		kind = "/pulls/"
	}
	path := repoPath(item.Repo) + kind + strconv.Itoa(item.Number)
	return f.call(ctx, http.MethodPatch, path, map[string]string{"state": "closed"}, nil)
}

// ListReleases includes drafts and prereleases.
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
