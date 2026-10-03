package core

import (
	"context"
	"slices"
	"strings"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
)

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
