package gitlab

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

var _ forge.BranchReader = (*Forge)(nil)

// commitItem is one commit of GET /projects/:id/repository/commits, and a branch's tip.
type commitItem struct {
	ID            string    `json:"id"`
	Title         string    `json:"title"`
	AuthorName    string    `json:"author_name"`
	CommittedDate time.Time `json:"committed_date"`
}

// branchItem is one item of GET /projects/:id/repository/branches.
type branchItem struct {
	Name    string     `json:"name"`
	Default bool       `json:"default"`
	WebURL  string     `json:"web_url"`
	Commit  commitItem `json:"commit"`
}

// ListBranches returns every branch; GitLab inlines each tip commit, so it costs no extra calls.
func (f *Forge) ListBranches(ctx context.Context, r domain.RepoRef) ([]domain.Branch, error) {
	items, err := list[branchItem](ctx, f, projectPath(r)+"/repository/branches", nil)
	if err != nil {
		return nil, fmt.Errorf("list branches of %s: %w", r, err)
	}
	out := make([]domain.Branch, len(items))
	for i, b := range items {
		out[i] = domain.Branch{Name: b.Name, Default: b.Default, Commit: b.Commit.toCommit(), WebURL: b.WebURL}
	}
	return out, nil
}

// ListCommits returns the first page of 30 only.
func (f *Forge) ListCommits(ctx context.Context, r domain.RepoRef, branch string) ([]domain.Commit, error) {
	var items []commitItem
	q := url.Values{"ref_name": {branch}, "per_page": {"30"}}
	if err := f.callQuery(ctx, http.MethodGet, projectPath(r)+"/repository/commits", q, nil, &items); err != nil {
		return nil, fmt.Errorf("commits of %s on %s: %w", r, branch, err)
	}
	out := make([]domain.Commit, len(items))
	for i, c := range items {
		out[i] = c.toCommit()
	}
	return out, nil
}

func (c commitItem) toCommit() domain.Commit {
	return domain.Commit{SHA: c.ID, Message: c.Title, Author: c.AuthorName, Date: c.CommittedDate}
}
