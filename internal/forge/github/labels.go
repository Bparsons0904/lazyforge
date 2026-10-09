package github

import (
	"context"
	"fmt"
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

// ListLabels returns repo labels only; GitHub has no organization labels.
func (f *Forge) ListLabels(ctx context.Context, r domain.RepoRef) ([]domain.Label, error) {
	ls, err := list[label](ctx, f, repoPath(r)+"/labels", nil)
	if err != nil {
		return nil, err
	}
	return labelsToDomain(ls), nil
}

func itemLabelsPath(item forge.ItemRef) string {
	return repoPath(item.Repo) + "/issues/" + strconv.Itoa(item.Number) + "/labels"
}

// ItemLabels reads a PR's labels through the issues endpoint, since GitHub keeps one label set for both.
func (f *Forge) ItemLabels(ctx context.Context, item forge.ItemRef) ([]domain.Label, error) {
	ls, err := list[label](ctx, f, itemLabelsPath(item), nil)
	if err != nil {
		return nil, err
	}
	return labelsToDomain(ls), nil
}

// SetLabels replaces the whole set; nil or empty ids clears it. GitHub takes names, so it reads the
// repo's labels first and returns an error for an ID that isn't among them.
func (f *Forge) SetLabels(ctx context.Context, item forge.ItemRef, ids []int64) ([]domain.Label, error) {
	names := []string{}
	if len(ids) > 0 {
		all, err := f.ListLabels(ctx, item.Repo)
		if err != nil {
			return nil, err
		}
		byID := make(map[int64]string, len(all))
		for _, l := range all {
			byID[l.ID] = l.Name
		}
		for _, id := range ids {
			name, ok := byID[id]
			if !ok {
				return nil, fmt.Errorf("set labels: label %d: %w", id, forge.ErrNotFound)
			}
			names = append(names, name)
		}
	}
	var ls []label
	err := f.call(ctx, http.MethodPut, itemLabelsPath(item), struct {
		Labels []string `json:"labels"`
	}{names}, &ls)
	if err != nil {
		return nil, err
	}
	return labelsToDomain(ls), nil
}
