package gitea

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

// branchItem is one item of GET /repos/{o}/{r}/branches; its commit is the tip.
type branchItem struct {
	Name   string `json:"name"`
	Commit struct {
		ID        string    `json:"id"`
		Message   string    `json:"message"`
		Timestamp time.Time `json:"timestamp"`
		Author    struct {
			Name string `json:"name"`
		} `json:"author"`
	} `json:"commit"`
}

// commitItem is one item of GET /repos/{o}/{r}/commits.
type commitItem struct {
	SHA    string `json:"sha"`
	Commit struct {
		Message string `json:"message"`
		Author  struct {
			Name string    `json:"name"`
			Date time.Time `json:"date"`
		} `json:"author"`
	} `json:"commit"`
}

// ListBranches implements forge.BranchReader; the default branch needs one extra repo read.
func (f *Forge) ListBranches(ctx context.Context, r domain.RepoRef) ([]domain.Branch, error) {
	var repo struct {
		DefaultBranch string `json:"default_branch"`
	}
	if err := f.call(ctx, http.MethodGet, repoPath(r), nil, &repo); err != nil {
		return nil, fmt.Errorf("read %s: %w", r, err)
	}
	items, err := list[branchItem](ctx, f, repoPath(r)+"/branches", nil)
	if err != nil {
		return nil, fmt.Errorf("list branches of %s: %w", r, err)
	}
	out := make([]domain.Branch, len(items))
	for i, b := range items {
		out[i] = domain.Branch{
			Name:    b.Name,
			Default: b.Name == repo.DefaultBranch,
			Commit: domain.Commit{
				SHA: b.Commit.ID, Message: subject(b.Commit.Message), Author: b.Commit.Author.Name, Date: b.Commit.Timestamp,
			},
			WebURL: f.base + "/" + r.String() + "/src/branch/" + escapePath(b.Name),
		}
	}
	return out, nil
}

// ListCommits implements forge.BranchReader.
func (f *Forge) ListCommits(ctx context.Context, r domain.RepoRef, branch string) ([]domain.Commit, error) {
	q := url.Values{"sha": {branch}, "limit": {"30"}, "stat": {"false"}, "verification": {"false"}, "files": {"false"}}
	resp, err := f.send(ctx, http.MethodGet, repoPath(r)+"/commits", q, nil)
	if errors.Is(err, forge.ErrRefused) {
		// Gitea answers 409 for a repo with no commits yet, which reads the same as a missing branch.
		return nil, fmt.Errorf("commits of %s on %s: %w", r, branch, forge.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("commits of %s on %s: %w", r, branch, err)
	}
	defer func() { _ = resp.Body.Close() }()
	var items []commitItem
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return nil, fmt.Errorf("commits of %s on %s: decode: %w", r, branch, err)
	}
	out := make([]domain.Commit, len(items))
	for i, c := range items {
		out[i] = domain.Commit{
			SHA: c.SHA, Message: subject(c.Commit.Message), Author: c.Commit.Author.Name, Date: c.Commit.Author.Date,
		}
	}
	return out, nil
}

// subject is the first line of a commit message, trimmed.
func subject(msg string) string {
	line, _, _ := strings.Cut(msg, "\n")
	return strings.TrimSpace(line)
}

// escapePath escapes each segment of a repo path so its slashes stay path separators.
func escapePath(name string) string {
	segs := strings.Split(name, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return strings.Join(segs, "/")
}
