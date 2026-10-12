package gitlab

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

type label struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// labelColor drops the '#' as the gitea adapter's colors have none; a CSS color name becomes "", the UI's default.
func labelColor(c string) string {
	if hexColor.MatchString(c) {
		return c[1:]
	}
	return ""
}

// ListLabels includes labels inherited from the project's groups.
func (f *Forge) ListLabels(ctx context.Context, r domain.RepoRef) ([]domain.Label, error) {
	ls, err := list[label](ctx, f, projectPath(r)+"/labels", nil)
	if err != nil {
		return nil, err
	}
	out := make([]domain.Label, len(ls))
	for i, l := range ls {
		out[i] = domain.Label{ID: l.ID, Name: l.Name, Color: labelColor(l.Color)}
	}
	return out, nil
}

// ItemLabels gives a label that is on the item but missing from the project's list the ID 0.
func (f *Forge) ItemLabels(ctx context.Context, item forge.ItemRef) ([]domain.Label, error) {
	var it itemLabels
	if err := f.call(ctx, http.MethodGet, itemPath(item), nil, &it); err != nil {
		return nil, err
	}
	return f.labelsByName(ctx, item.Repo, it.Labels)
}

type itemLabels struct {
	Labels []string `json:"labels"`
}

func (f *Forge) labelsByName(ctx context.Context, r domain.RepoRef, names []string) ([]domain.Label, error) {
	out := make([]domain.Label, len(names))
	if len(names) == 0 {
		return out, nil
	}
	all, err := f.ListLabels(ctx, r)
	if err != nil {
		return nil, err
	}
	byName := make(map[string]domain.Label, len(all))
	for _, l := range all {
		byName[l.Name] = l
	}
	for i, n := range names {
		out[i] = domain.Label{Name: n}
		if l, ok := byName[n]; ok {
			out[i] = l
		}
	}
	return out, nil
}

// SetLabels replaces the whole set and returns the item's labels from the PUT response; nil or empty ids clears
// it. GitLab takes names and creates any it doesn't know, so an ID missing from the project's labels is an error.
func (f *Forge) SetLabels(ctx context.Context, item forge.ItemRef, ids []int64) ([]domain.Label, error) {
	names := make([]string, 0, len(ids))
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
	var it itemLabels
	if err := f.call(ctx, http.MethodPut, itemPath(item), map[string]string{"labels": strings.Join(names, ",")}, &it); err != nil {
		return nil, err
	}
	return f.labelsByName(ctx, item.Repo, it.Labels)
}
