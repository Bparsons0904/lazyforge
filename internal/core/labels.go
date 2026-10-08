package core

import (
	"context"
	"fmt"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

// Labels loads the repository choices and the item's current selection together.
func (s *Service) Labels(ctx context.Context, item forge.ItemRef) (available, selected []domain.Label, err error) {
	f, ok := s.f.(forge.Labeler)
	if !ok {
		return nil, nil, forge.ErrUnsupported
	}
	err = s.do(ctx, fmt.Sprintf("load labels for %s#%d", item.Repo, item.Number), func(ctx context.Context) error {
		var err error
		available, err = f.ListLabels(ctx, item.Repo)
		if err != nil {
			return err
		}
		selected, err = f.ItemLabels(ctx, item)
		return err
	})
	return
}

// SetLabels saves a selection and updates the cached item from the server response.
func (s *Service) SetLabels(ctx context.Context, item forge.ItemRef, ids []int64) error {
	f, ok := s.f.(forge.Labeler)
	if !ok {
		return forge.ErrUnsupported
	}
	var labels []domain.Label
	err := s.do(ctx, fmt.Sprintf("save labels for %s#%d", item.Repo, item.Number), func(ctx context.Context) (err error) {
		labels, err = f.SetLabels(ctx, item, ids)
		return
	})
	if err != nil {
		return err
	}
	names := make([]string, len(labels))
	colors := make(map[string]string, len(labels))
	for i, l := range labels {
		names[i] = l.Name
		colors[l.Name] = l.Color
	}
	if item.Kind == forge.ItemChangeRequest {
		update(s, Key{Kind: KindChangeRequests, Repo: item.Repo}, func(items []domain.ChangeRequest) []domain.ChangeRequest {
			for i := range items {
				if items[i].Number == item.Number {
					items[i].Labels, items[i].LabelColors = names, colors
				}
			}
			return items
		})
	} else {
		update(s, Key{Kind: KindIssues, Repo: item.Repo}, func(items []domain.Issue) []domain.Issue {
			for i := range items {
				if items[i].Number == item.Number {
					items[i].Labels, items[i].LabelColors = names, colors
				}
			}
			return items
		})
	}
	return nil
}
