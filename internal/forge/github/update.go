package github

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strconv"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

// UpdateStyles returns merge only: GitHub's update-branch endpoint has no rebase option.
func (f *Forge) UpdateStyles() []forge.UpdateStyle {
	return []forge.UpdateStyle{forge.UpdateMerge}
}

// UpdateBranch returns once GitHub queues the update (202), so the head may still be the old one until the job finishes.
func (f *Forge) UpdateBranch(ctx context.Context, r domain.RepoRef, n int, style forge.UpdateStyle) error {
	if !slices.Contains(f.UpdateStyles(), style) {
		return fmt.Errorf("update style %q: %w", style, forge.ErrUnsupported)
	}
	resp, err := f.send(ctx, http.MethodPut, f.url(repoPath(r)+"/pulls/"+strconv.Itoa(n)+"/update-branch", nil), nil)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}
