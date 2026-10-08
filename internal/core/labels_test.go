package core_test

import (
	"errors"
	"slices"
	"testing"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/core"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

func TestLabelSaveUpdatesCacheFromResponse(t *testing.T) {
	f := seeded()
	f.AddLabels(repoA, domain.Label{ID: 1, Name: "bug", Color: "ff0000"})
	s := core.New(f, core.Options{})
	ctx := t.Context()
	if _, err := s.ChangeRequests(ctx, repoA); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Issues(ctx, repoA); err != nil {
		t.Fatal(err)
	}
	for _, item := range []forge.ItemRef{
		{Repo: repoA, Kind: forge.ItemChangeRequest, Number: 1},
		{Repo: repoA, Kind: forge.ItemIssue, Number: 10},
	} {
		choices, selected, err := s.Labels(ctx, item)
		if err != nil || len(choices) != 1 || len(selected) != 0 {
			t.Fatalf("load=%v %v %v", choices, selected, err)
		}
		if err := s.SetLabels(ctx, item, []int64{1}); err != nil {
			t.Fatal(err)
		}
		peek := func() ([]string, map[string]string) {
			if item.Kind == forge.ItemChangeRequest {
				items, _, _ := s.PeekChangeRequests(repoA)
				return items[0].Labels, items[0].LabelColors
			}
			items, _, _ := s.PeekIssues(repoA)
			return items[0].Labels, items[0].LabelColors
		}
		names, colors := peek()
		if !slices.Equal(names, []string{"bug"}) || colors["bug"] != "ff0000" {
			t.Fatalf("cache=%v %v", names, colors)
		}
		f.FailNext(forge.ErrUnauthorized)
		if err := s.SetLabels(ctx, item, nil); !errors.Is(err, forge.ErrUnauthorized) {
			t.Fatal(err)
		}
		names, _ = peek()
		if len(names) != 1 {
			t.Fatal("failed write changed cache")
		}
		if err := s.SetLabels(ctx, item, nil); err != nil {
			t.Fatal(err)
		}
		names, _ = peek()
		if len(names) != 0 {
			t.Fatal("clear-all did not update cache")
		}
	}
}
