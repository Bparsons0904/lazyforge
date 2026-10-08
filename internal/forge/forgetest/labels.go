package forgetest

import (
	"context"
	"slices"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

// AddLabels seeds the available repository labels.
func (f *Fake) AddLabels(repo domain.RepoRef, labels ...domain.Label) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.labels[repo] = append(f.labels[repo], labels...)
}

// ListLabels returns the seeded repository labels.
func (f *Fake) ListLabels(ctx context.Context, repo domain.RepoRef) ([]domain.Label, error) {
	defer f.mu.Unlock()
	if err := f.begin(ctx); err != nil {
		return nil, err
	}
	if err := f.repoErr(repo); err != nil {
		return nil, err
	}
	return slices.Clone(f.labels[repo]), nil
}

func (f *Fake) labelNamesFor(item forge.ItemRef) ([]string, error) {
	if item.Kind == forge.ItemChangeRequest {
		cr, err := f.cr(item.Repo, item.Number)
		if err != nil {
			return nil, err
		}
		return cr.Labels, nil
	}
	for _, is := range f.issues[item.Repo] {
		if is.Number == item.Number {
			return is.Labels, nil
		}
	}
	return nil, forge.ErrNotFound
}

// ItemLabels returns current labels using the seeded catalog IDs.
func (f *Fake) ItemLabels(ctx context.Context, item forge.ItemRef) ([]domain.Label, error) {
	defer f.mu.Unlock()
	if err := f.begin(ctx); err != nil {
		return nil, err
	}
	names, err := f.labelNamesFor(item)
	if err != nil {
		return nil, err
	}
	var out []domain.Label
	for _, l := range f.labels[item.Repo] {
		if slices.Contains(names, l.Name) {
			out = append(out, l)
		}
	}
	return out, nil
}

// SetLabels replaces an item's selection and records the mutation.
func (f *Fake) SetLabels(ctx context.Context, item forge.ItemRef, ids []int64) ([]domain.Label, error) {
	defer f.mu.Unlock()
	if err := f.begin(ctx); err != nil {
		return nil, err
	}
	if _, err := f.labelNamesFor(item); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(ids))
	colors := map[string]string{}
	out := make([]domain.Label, 0, len(ids))
	for _, id := range ids {
		idx := slices.IndexFunc(f.labels[item.Repo], func(l domain.Label) bool { return l.ID == id })
		if idx < 0 {
			return nil, forge.ErrNotFound
		}
		l := f.labels[item.Repo][idx]
		names = append(names, l.Name)
		colors[l.Name] = l.Color
		out = append(out, l)
	}
	if item.Kind == forge.ItemChangeRequest {
		for i := range f.crs[item.Repo] {
			if f.crs[item.Repo][i].Number == item.Number {
				f.crs[item.Repo][i].Labels = names
				f.crs[item.Repo][i].LabelColors = colors
			}
		}
	} else {
		for i := range f.issues[item.Repo] {
			if f.issues[item.Repo][i].Number == item.Number {
				f.issues[item.Repo][i].Labels = names
				f.issues[item.Repo][i].LabelColors = colors
			}
		}
	}
	f.mutations = append(f.mutations, Mutation{Op: "set-labels", Item: item})
	return out, nil
}
