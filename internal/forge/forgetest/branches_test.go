package forgetest_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge/forgetest"
)

var _ forge.BranchReader = (*forgetest.Fake)(nil)

func TestListBranchesReturnsSeededInInsertionOrder(t *testing.T) {
	f := seeded()
	f.AddBranch(repoRef, domain.Branch{Name: "main", Default: true})
	f.AddBranch(repoRef, domain.Branch{Name: "feature/x"})

	got, err := f.ListBranches(context.Background(), repoRef)
	if err != nil {
		t.Fatalf("ListBranches() error = %v", err)
	}
	names := []string{got[0].Name, got[1].Name}
	if !slices.Equal(names, []string{"main", "feature/x"}) || len(got) != 2 {
		t.Errorf("ListBranches() names = %v, want [main feature/x]", names)
	}
	if !got[0].Default || got[1].Default {
		t.Errorf("Default flags = %v, %v; want true, false", got[0].Default, got[1].Default)
	}
}

func TestListBranchesIsIsolatedFromCallers(t *testing.T) {
	f := seeded()
	f.AddBranch(repoRef, domain.Branch{Name: "main", Default: true})

	got, _ := f.ListBranches(context.Background(), repoRef)
	got[0].Name = "mutated"

	again, _ := f.ListBranches(context.Background(), repoRef)
	if again[0].Name != "main" {
		t.Errorf("caller mutation leaked into the fake: %q", again[0].Name)
	}
}

func TestListBranchesOtherRepoIsEmpty(t *testing.T) {
	f := seeded()
	f.AddBranch(repoRef, domain.Branch{Name: "main", Default: true})

	got, err := f.ListBranches(context.Background(), otherRef)
	if err != nil || len(got) != 0 {
		t.Errorf("ListBranches(other) = %+v, %v; want empty and nil", got, err)
	}
}

func TestListCommitsReturnsSeededAndCopies(t *testing.T) {
	ctx := context.Background()
	f := seeded()
	f.SetCommits(repoRef, "main", []domain.Commit{{SHA: "a1", Message: "one"}, {SHA: "a2", Message: "two"}})

	got, err := f.ListCommits(ctx, repoRef, "main")
	if err != nil || len(got) != 2 || got[0].SHA != "a1" || got[1].SHA != "a2" {
		t.Fatalf("ListCommits() = %+v, %v; want a1 then a2, nil", got, err)
	}
	got[0].SHA = "mutated"

	again, _ := f.ListCommits(ctx, repoRef, "main")
	if again[0].SHA != "a1" {
		t.Errorf("caller mutation leaked into the fake: %q", again[0].SHA)
	}
}

func TestListCommitsUnseededBranchIsNotFound(t *testing.T) {
	f := seeded()
	f.SetCommits(repoRef, "main", []domain.Commit{{SHA: "a1"}})

	_, err := f.ListCommits(context.Background(), repoRef, "missing")
	if !errors.Is(err, forge.ErrNotFound) {
		t.Errorf("ListCommits(missing) error = %v, want ErrNotFound", err)
	}
}

func TestListBranchesAndCommitsHonorCancel(t *testing.T) {
	f := seeded()
	f.AddBranch(repoRef, domain.Branch{Name: "main", Default: true})
	f.SetCommits(repoRef, "main", []domain.Commit{{SHA: "a1"}})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := f.ListBranches(ctx, repoRef); !errors.Is(err, context.Canceled) {
		t.Errorf("ListBranches() on cancelled ctx error = %v, want context.Canceled", err)
	}
	if _, err := f.ListCommits(ctx, repoRef, "main"); !errors.Is(err, context.Canceled) {
		t.Errorf("ListCommits() on cancelled ctx error = %v, want context.Canceled", err)
	}
}

func TestListBranchesFailNext(t *testing.T) {
	f := seeded()
	f.AddBranch(repoRef, domain.Branch{Name: "main", Default: true})
	f.FailNext(forge.ErrRateLimited)

	if _, err := f.ListBranches(context.Background(), repoRef); !errors.Is(err, forge.ErrRateLimited) {
		t.Errorf("ListBranches() error = %v, want ErrRateLimited", err)
	}
}

func TestDemoBranches(t *testing.T) {
	ctx := context.Background()
	f := forgetest.NewDemo(time.Now())
	repos, err := f.ListRepos(ctx)
	if err != nil || len(repos) == 0 {
		t.Fatalf("ListRepos() = %d repos, %v; want some and nil", len(repos), err)
	}

	for _, r := range repos {
		branches, err := f.ListBranches(ctx, r.RepoRef)
		if err != nil {
			t.Fatalf("ListBranches(%v) error = %v", r.RepoRef, err)
		}
		if len(branches) != 4 {
			t.Errorf("%v has %d branches, want 4 (default plus three others)", r.RepoRef, len(branches))
			continue
		}
		defaults := 0
		for _, b := range branches {
			if b.Default {
				defaults++
				if b.Name != "main" {
					t.Errorf("%v default branch = %q, want main", r.RepoRef, b.Name)
				}
			}
		}
		if defaults != 1 {
			t.Errorf("%v has %d default branches, want exactly 1", r.RepoRef, defaults)
		}
	}
}

func TestDemoBranchMatchesOpenChangeRequest(t *testing.T) {
	ctx := context.Background()
	f := forgetest.NewDemo(time.Now())
	repos, _ := f.ListRepos(ctx)

	matched := 0
	for _, r := range repos {
		crs, err := f.ListChangeRequests(ctx, r.RepoRef, forge.Filter{State: domain.StateOpen})
		if err != nil {
			t.Fatalf("ListChangeRequests(%v) error = %v", r.RepoRef, err)
		}
		branches, _ := f.ListBranches(ctx, r.RepoRef)
		for _, cr := range crs {
			for _, b := range branches {
				if b.Name == cr.SourceBranch && !b.Default {
					matched++
				}
			}
		}
	}
	if matched == 0 {
		t.Error("no demo branch equals an open change request's source branch; the Branches tab's #N marker has nothing to show")
	}
}
