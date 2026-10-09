package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
)

const (
	maxBranches       = 100
	branchDetailLimit = 8
)

// branchRef is one item of GET /repos/{o}/{r}/branches; the list carries only each tip's SHA.
type branchRef struct {
	Name   string `json:"name"`
	Commit struct {
		SHA string `json:"sha"`
	} `json:"commit"`
}

// branchDetail is GET /repos/{o}/{r}/branches/{branch}.
type branchDetail struct {
	Name   string     `json:"name"`
	Commit commitBody `json:"commit"`
}

// commitBody is the commit part of GET /repos/{o}/{r}/commits/{sha} and of a branch's tip.
type commitBody struct {
	SHA    string `json:"sha"`
	Commit struct {
		Message string `json:"message"`
		Author  struct {
			Name string `json:"name"`
		} `json:"author"`
		Committer struct {
			Date time.Time `json:"date"`
		} `json:"committer"`
	} `json:"commit"`
}

// ListBranches implements forge.BranchReader; it returns at most 100 branches, the default always among them.
func (f *Forge) ListBranches(ctx context.Context, r domain.RepoRef) ([]domain.Branch, error) {
	var repo struct {
		DefaultBranch string `json:"default_branch"`
	}
	if err := f.call(ctx, http.MethodGet, repoPath(r), nil, &repo); err != nil {
		return nil, fmt.Errorf("read %s: %w", r, err)
	}
	var page []branchRef
	if err := f.call(ctx, http.MethodGet, repoPath(r)+"/branches?per_page="+pageSize, nil, &page); err != nil {
		return nil, fmt.Errorf("list branches of %s: %w", r, err)
	}
	if len(page) == 0 {
		return nil, nil
	}
	// The default is fetched on its own, so it survives even when the first page leaves it out.
	others := make([]branchRef, 0, maxBranches-1)
	for _, b := range page {
		if b.Name != repo.DefaultBranch && len(others) < maxBranches-1 {
			others = append(others, b)
		}
	}
	var def branchDetail
	if err := f.call(ctx, http.MethodGet, repoPath(r)+"/branches/"+escapeBranch(repo.DefaultBranch), nil, &def); err != nil {
		return nil, fmt.Errorf("read default branch of %s: %w", r, err)
	}
	details := make([]domain.Branch, len(others))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(branchDetailLimit)
	for i, b := range others {
		g.Go(func() error {
			var c commitBody
			if err := f.call(gctx, http.MethodGet, repoPath(r)+"/commits/"+url.PathEscape(b.Commit.SHA), nil, &c); err != nil {
				return fmt.Errorf("read commit of %s in %s: %w", b.Name, r, err)
			}
			details[i] = f.branch(r, b.Name, false, c)
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, fmt.Errorf("list branches of %s: %w", r, err)
	}
	return append([]domain.Branch{f.branch(r, def.Name, true, def.Commit)}, details...), nil
}

// ListCommits implements forge.BranchReader.
func (f *Forge) ListCommits(ctx context.Context, r domain.RepoRef, branch string) ([]domain.Commit, error) {
	q := url.Values{"sha": {branch}, "per_page": {"30"}}
	resp, err := f.send(ctx, http.MethodGet, f.url(repoPath(r)+"/commits", q), nil)
	if err != nil {
		return nil, fmt.Errorf("commits of %s on %s: %w", r, branch, err)
	}
	defer func() { _ = resp.Body.Close() }()
	var items []commitBody
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return nil, fmt.Errorf("commits of %s on %s: decode: %w", r, branch, err)
	}
	out := make([]domain.Commit, len(items))
	for i, c := range items {
		out[i] = c.toCommit()
	}
	return out, nil
}

func (f *Forge) branch(r domain.RepoRef, name string, isDefault bool, tip commitBody) domain.Branch {
	return domain.Branch{
		Name: name, Default: isDefault, Commit: tip.toCommit(),
		WebURL: f.web + "/" + r.String() + "/tree/" + escapeBranch(name),
	}
}

func (c commitBody) toCommit() domain.Commit {
	return domain.Commit{
		SHA: c.SHA, Message: subject(c.Commit.Message), Author: c.Commit.Author.Name, Date: c.Commit.Committer.Date,
	}
}

// subject is the first line of a commit message, trimmed.
func subject(msg string) string {
	line, _, _ := strings.Cut(msg, "\n")
	return strings.TrimSpace(line)
}

// escapeBranch escapes each segment of a branch name so its slashes stay path separators.
func escapeBranch(name string) string {
	segs := strings.Split(name, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return strings.Join(segs, "/")
}
