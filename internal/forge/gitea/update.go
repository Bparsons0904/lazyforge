package gitea

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

// UpdateStyles returns merge and rebase, the two values Forgejo's style query accepts.
func (f *Forge) UpdateStyles() []forge.UpdateStyle {
	return []forge.UpdateStyle{forge.UpdateMerge, forge.UpdateRebase}
}

// UpdateBranch checks the style before sending, so an unsupported one never reaches the server.
func (f *Forge) UpdateBranch(ctx context.Context, r domain.RepoRef, n int, style forge.UpdateStyle) error {
	if !slices.Contains(f.UpdateStyles(), style) {
		return fmt.Errorf("update style %q: %w", style, forge.ErrUnsupported)
	}
	q := url.Values{"style": {string(style)}}
	resp, err := f.send(ctx, http.MethodPost, repoPath(r)+"/pulls/"+strconv.Itoa(n)+"/update", q, nil)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}
