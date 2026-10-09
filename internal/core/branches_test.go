package core_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/core"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

// noBranches hides the wrapped forge's BranchReader: embedding the interface promotes only forge.Forge.
type noBranches struct{ forge.Forge }

func branchNames(bs []domain.Branch) []string {
	out := make([]string, 0, len(bs))
	for _, b := range bs {
		out = append(out, b.Name)
	}
	return out
}

func TestBranchesSortDefaultFirstThenNewestThenName(t *testing.T) {
	f := seeded()
	day := func(d int) time.Time { return t0.AddDate(0, 0, d) }
	f.AddBranch(repoA, domain.Branch{Name: "zeta", Commit: domain.Commit{SHA: "z", Date: day(4)}})
	f.AddBranch(repoA, domain.Branch{Name: "main", Default: true, Commit: domain.Commit{SHA: "m", Date: day(-30)}})
	f.AddBranch(repoA, domain.Branch{Name: "beta", Commit: domain.Commit{SHA: "b", Date: day(2)}})
	f.AddBranch(repoA, domain.Branch{Name: "alpha", Commit: domain.Commit{SHA: "a", Date: day(2)}})
	f.AddBranch(repoA, domain.Branch{Name: "gamma", Commit: domain.Commit{SHA: "g", Date: day(9)}})
	s := core.New(f, core.Options{Now: stepClock()})

	got, err := s.Branches(context.Background(), repoA)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"main", "gamma", "zeta", "alpha", "beta"}
	if !slices.Equal(branchNames(got), want) {
		t.Errorf("order = %v, want %v", branchNames(got), want)
	}
	if !got[0].Default {
		t.Errorf("first branch lost its Default flag: %+v", got[0])
	}
}

func TestBranchesCachedPerRepo(t *testing.T) {
	ctx := context.Background()
	f := seeded()
	f.AddBranch(repoA, domain.Branch{Name: "main", Default: true})
	s := core.New(f, core.Options{Now: stepClock()})
	if _, _, ok := s.PeekBranches(repoA); ok {
		t.Fatal("PeekBranches before any fetch reported ok")
	}

	got, err := s.Branches(ctx, repoA)
	if err != nil || len(got) != 1 {
		t.Fatalf("Branches() = %+v, %v; want one branch and nil", got, err)
	}
	peek, at, ok := s.PeekBranches(repoA)
	if !ok || len(peek) != 1 || !at.Equal(t0) {
		t.Errorf("PeekBranches() = %+v at %v, ok=%v; want one branch at %v, true", peek, at, ok, t0)
	}
	if _, _, ok := s.PeekBranches(repoB); ok {
		t.Error("PeekBranches for repoB reported branches it never fetched")
	}

	f.FailNext(forge.ErrRateLimited)
	// Fetches always reach the forge; the cache is what Peek serves, and a failed refetch must not evict it.
	if _, err := s.Branches(ctx, repoA); !errors.Is(err, forge.ErrRateLimited) {
		t.Errorf("second Branches() err = %v, want the forge to be asked again and fail with ErrRateLimited", err)
	}
	if peek, _, ok := s.PeekBranches(repoA); !ok || len(peek) != 1 {
		t.Errorf("PeekBranches() after a failed refetch = %+v, ok=%v; want the cached branch kept", peek, ok)
	}
}

func TestBranchesUnsupportedWithoutBranchReader(t *testing.T) {
	ctx := context.Background()
	s := core.New(noBranches{seeded()}, core.Options{})
	if _, err := s.Branches(ctx, repoA); !errors.Is(err, forge.ErrUnsupported) {
		t.Errorf("Branches() err = %v, want ErrUnsupported", err)
	}
	if _, err := s.Commits(ctx, repoA, "main"); !errors.Is(err, forge.ErrUnsupported) {
		t.Errorf("Commits() err = %v, want ErrUnsupported", err)
	}
	if _, _, ok := s.PeekBranches(repoA); ok {
		t.Error("unsupported Branches left a cache entry")
	}
	if _, _, ok := s.PeekCommits(repoA, "main"); ok {
		t.Error("unsupported Commits left a cache entry")
	}
}

func TestBranchesPassesAdapterErrorsThrough(t *testing.T) {
	ctx := context.Background()
	f := seeded()
	f.AddBranch(repoA, domain.Branch{Name: "main", Default: true})
	s := core.New(f, core.Options{Now: stepClock()})

	f.FailNext(forge.ErrRateLimited)
	_, err := s.Branches(ctx, repoA)
	if !errors.Is(err, forge.ErrRateLimited) {
		t.Fatalf("err = %v, want it to match ErrRateLimited", err)
	}
	if err.Error() == forge.ErrRateLimited.Error() {
		t.Errorf("err %q carries no context", err)
	}
	if _, _, ok := s.PeekBranches(repoA); ok {
		t.Error("failed fetch was cached")
	}

	got, err := s.Branches(ctx, repoA)
	if err != nil || len(got) != 1 {
		t.Errorf("retry Branches() = %+v, %v; want the branch and nil", got, err)
	}
}

func TestCommitsCachedPerBranchAndRepo(t *testing.T) {
	ctx := context.Background()
	f := seeded()
	f.SetCommits(repoA, "main", []domain.Commit{{SHA: "a1", Message: "one"}})
	f.SetCommits(repoA, "feature", []domain.Commit{{SHA: "b1", Message: "two"}, {SHA: "b2", Message: "three"}})
	s := core.New(f, core.Options{Now: stepClock()})

	mainCommits, err := s.Commits(ctx, repoA, "main")
	if err != nil || len(mainCommits) != 1 || mainCommits[0].SHA != "a1" {
		t.Errorf("Commits(main) = %+v, %v; want the one main commit and nil", mainCommits, err)
	}
	featureCommits, err := s.Commits(ctx, repoA, "feature")
	if err != nil || len(featureCommits) != 2 || featureCommits[0].SHA != "b1" {
		t.Errorf("Commits(feature) = %+v, %v; want two feature commits, newest first, and nil", featureCommits, err)
	}
	if peek, _, ok := s.PeekCommits(repoA, "feature"); !ok || len(peek) != 2 {
		t.Errorf("PeekCommits(feature) = %+v, ok=%v; want the two cached commits", peek, ok)
	}
	if _, _, ok := s.PeekCommits(repoB, "main"); ok {
		t.Error("PeekCommits for repoB reported commits it never fetched")
	}
	if _, _, ok := s.PeekCommits(repoA, "other"); ok {
		t.Error("PeekCommits for an unfetched branch reported ok")
	}
}

func TestCommitsNotFoundIsEmptyAndCached(t *testing.T) {
	ctx := context.Background()
	f := seeded()
	s := core.New(f, core.Options{Now: stepClock()})

	// A branch with no seeded commits is what the adapter reports as not found, for a deleted or empty branch.
	got, err := s.Commits(ctx, repoA, "gone")
	if err != nil || len(got) != 0 {
		t.Fatalf("Commits() for a missing branch = %+v, %v; want empty and nil", got, err)
	}
	peek, _, ok := s.PeekCommits(repoA, "gone")
	if !ok || len(peek) != 0 {
		t.Errorf("PeekCommits() after not-found = %+v, ok=%v; want the empty list cached", peek, ok)
	}

	f.FailNext(forge.ErrRateLimited)
	// Fetches always reach the forge; a failed refetch must not evict the cached empty list.
	if _, err := s.Commits(ctx, repoA, "gone"); !errors.Is(err, forge.ErrRateLimited) {
		t.Errorf("second Commits() err = %v, want the forge to be asked again and fail with ErrRateLimited", err)
	}
	if peek, _, ok := s.PeekCommits(repoA, "gone"); !ok || len(peek) != 0 {
		t.Errorf("PeekCommits() after a failed refetch = %+v, ok=%v; want the empty list kept", peek, ok)
	}
}

func TestCommitsPassesAdapterErrorsThroughAndIsNotCached(t *testing.T) {
	ctx := context.Background()
	f := seeded()
	f.SetCommits(repoA, "main", []domain.Commit{{SHA: "a1"}})
	s := core.New(f, core.Options{Now: stepClock()})

	f.FailNext(forge.ErrRateLimited)
	_, err := s.Commits(ctx, repoA, "main")
	if !errors.Is(err, forge.ErrRateLimited) {
		t.Fatalf("err = %v, want it to match ErrRateLimited", err)
	}
	if _, _, ok := s.PeekCommits(repoA, "main"); ok {
		t.Error("failed fetch was cached")
	}

	got, err := s.Commits(ctx, repoA, "main")
	if err != nil || len(got) != 1 {
		t.Errorf("retry Commits() = %+v, %v; want the commit and nil", got, err)
	}
}
