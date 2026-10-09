package gitea

import (
	"context"
	"net/http"
	"strconv"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

func labelsToDomain(ls []label) []domain.Label {
	out := make([]domain.Label, len(ls))
	for i, l := range ls {
		out[i] = domain.Label{ID: l.ID, Name: l.Name, Color: l.Color}
	}
	return out
}

// ListLabels returns every page of labels available in the repository.
func (f *Forge) ListLabels(ctx context.Context, r domain.RepoRef) ([]domain.Label, error) {
	ls, err := list[label](ctx, f, repoPath(r)+"/labels", nil)
	return labelsToDomain(ls), err
}

func itemLabelsPath(item forge.ItemRef) string {
	// Forgejo shares issue label endpoints with pull requests.
	return repoPath(item.Repo) + "/issues/" + strconv.Itoa(item.Number) + "/labels"
}

// ItemLabels reads current labels, including labels inherited from an organization.
func (f *Forge) ItemLabels(ctx context.Context, item forge.ItemRef) ([]domain.Label, error) {
	var ls []label
	err := f.call(ctx, http.MethodGet, itemLabelsPath(item), nil, &ls)
	return labelsToDomain(ls), err
}

// SetLabels replaces the entire selection; an empty array removes every label.
func (f *Forge) SetLabels(ctx context.Context, item forge.ItemRef, ids []int64) ([]domain.Label, error) {
	if ids == nil {
		ids = []int64{}
	}
	var ls []label
	err := f.call(ctx, http.MethodPut, itemLabelsPath(item), struct {
		Labels []int64 `json:"labels"`
	}{ids}, &ls)
	return labelsToDomain(ls), err
}
