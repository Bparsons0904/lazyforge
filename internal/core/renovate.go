package core

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/core/renovate"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
)

// RenovateUser returns the configured renovate_user, "" when Renovate PRs are detected by branch only.
func (s *Service) RenovateUser() string { return s.renovUser }

// ponytail: sequential scan of 10 repos; raise or parallelise if bots live in older repos.
const suggestRepoCap = 10

// SuggestRenovateUser returns the author of the first open renovate/ change request found, or "" when none is.
func (s *Service) SuggestRenovateUser(ctx context.Context) (string, error) {
	repos, err := s.Repos(ctx)
	if err != nil {
		return "", err
	}
	repos = slices.SortedStableFunc(slices.Values(repos), func(a, b domain.Repo) int {
		return b.LastActivity.Compare(a.LastActivity)
	})
	for _, r := range repos[:min(len(repos), suggestRepoCap)] {
		crs, err := s.ChangeRequests(ctx, r.RepoRef)
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if err != nil {
			continue
		}
		for _, cr := range crs {
			if strings.HasPrefix(cr.SourceBranch, "renovate/") {
				return cr.Author, nil
			}
		}
	}
	return "", nil
}

// fillRenovate sets cr.Renovate from the PR body, or nil when the body is rejected; other PRs are left as the forge gave them.
func (s *Service) fillRenovate(cr domain.ChangeRequest) domain.ChangeRequest {
	if renovate.IsRenovate(cr, s.renovUser) {
		cr.Renovate, _ = renovate.Parse(cr.Title, cr.Body)
	}
	return cr
}

// RenovateScan fetches r's open change requests and issues and keeps the Renovate PRs and parsed dashboards.
func (s *Service) RenovateScan(ctx context.Context, r domain.Repo) (renovate.RepoScan, error) {
	crs, err := s.ChangeRequests(ctx, r.RepoRef)
	if err != nil {
		return renovate.RepoScan{}, fmt.Errorf("renovate scan %s: %w", r.RepoRef, err)
	}
	issues, err := s.Issues(ctx, r.RepoRef)
	if err != nil {
		return renovate.RepoScan{}, fmt.Errorf("renovate scan %s: %w", r.RepoRef, err)
	}
	return s.buildScan(r, crs, issues), nil
}

// PeekRenovateScan builds the scan from cache; ok is false unless both lists are cached.
func (s *Service) PeekRenovateScan(r domain.Repo) (renovate.RepoScan, bool) {
	crs, _, ok := s.PeekChangeRequests(r.RepoRef)
	if !ok {
		return renovate.RepoScan{}, false
	}
	issues, _, ok := s.PeekIssues(r.RepoRef)
	if !ok {
		return renovate.RepoScan{}, false
	}
	return s.buildScan(r, crs, issues), true
}

func (s *Service) buildScan(r domain.Repo, crs []domain.ChangeRequest, issues []domain.Issue) renovate.RepoScan {
	scan := renovate.RepoScan{Repo: r}
	for _, cr := range crs {
		if renovate.IsRenovate(cr, s.renovUser) {
			scan.PRs = append(scan.PRs, cr)
		}
	}
	for _, is := range issues {
		if renovate.IsDashboard(is, s.renovUser) {
			scan.Dashboards = append(scan.Dashboards, renovate.Dashboard{Repo: r.RepoRef, Issue: is, Entries: renovate.ParseDashboard(is.Body)})
		}
	}
	return scan
}
