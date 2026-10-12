package gitlab

import (
	"context"
	"fmt"
	"net/http"
	"slices"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

var _ forge.BranchUpdater = (*Forge)(nil)

// UpdateStyles returns rebase only: GitLab's API has no call that merges the target into an MR.
func (f *Forge) UpdateStyles() []forge.UpdateStyle {
	return []forge.UpdateStyle{forge.UpdateRebase}
}

// UpdateBranch returns once GitLab queues the rebase (202), so the head may still be the old one until it finishes.
func (f *Forge) UpdateBranch(ctx context.Context, r domain.RepoRef, n int, style forge.UpdateStyle) error {
	if !slices.Contains(f.UpdateStyles(), style) {
		return fmt.Errorf("update style %q: %w", style, forge.ErrUnsupported)
	}
	return f.call(ctx, http.MethodPut, mrPath(r, n)+"/rebase", nil, nil)
}
