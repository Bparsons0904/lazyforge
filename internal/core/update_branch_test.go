package core_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/core"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge/forgetest"
)

// plainForge hides the Fake's optional capabilities, so the Service sees a forge that can't update branches.
type plainForge struct{ forge.Forge }

// refetchFails updates through the Fake but fails the re-fetch that follows.
type refetchFails struct {
	*forgetest.Fake
	err error
}

func (f refetchFails) GetChangeRequest(context.Context, domain.RepoRef, int) (domain.ChangeRequest, error) {
	return domain.ChangeRequest{}, f.err
}

// mergedOnRefetch reports the change request as merged once the update has run.
type mergedOnRefetch struct{ *forgetest.Fake }

func (f mergedOnRefetch) GetChangeRequest(ctx context.Context, r domain.RepoRef, n int) (domain.ChangeRequest, error) {
	cr, err := f.Fake.GetChangeRequest(ctx, r, n)
	cr.State = domain.StateMerged
	return cr, err
}

func TestUpdateStyles(t *testing.T) {
	f := crFake(1, domain.CIPass)
	if got := core.New(f, core.Options{}).UpdateStyles(); len(got) != 2 || got[0] != forge.UpdateMerge || got[1] != forge.UpdateRebase {
		t.Errorf("UpdateStyles() = %v, want [merge rebase]", got)
	}
	f.SetUpdateStyles(forge.UpdateMerge)
	if got := core.New(f, core.Options{}).UpdateStyles(); len(got) != 1 || got[0] != forge.UpdateMerge {
		t.Errorf("UpdateStyles() after SetUpdateStyles(merge) = %v, want [merge]", got)
	}
	if got := core.New(plainForge{f}, core.Options{}).UpdateStyles(); got != nil {
		t.Errorf("UpdateStyles() on a forge without BranchUpdater = %v, want nil", got)
	}
}

func TestUpdateBranchWithoutCapabilityMakesNoCall(t *testing.T) {
	f := crFake(1, domain.CIPass)
	err := core.New(plainForge{f}, core.Options{}).UpdateBranch(context.Background(), repoA, 1, forge.UpdateRebase)
	if !errors.Is(err, forge.ErrUnsupported) {
		t.Fatalf("UpdateBranch error = %v, want ErrUnsupported", err)
	}
	if n := len(f.Mutations()); n != 0 {
		t.Errorf("%d mutations recorded, want none", n)
	}
}

func TestUpdateBranchRebasesAndRefreshesCache(t *testing.T) {
	ctx := context.Background()
	f := crFake(1, domain.CIPass)
	s := core.New(f, core.Options{})
	if _, err := s.ChangeRequests(ctx, repoA); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateBranch(ctx, repoA, 1, forge.UpdateRebase); err != nil {
		t.Fatalf("UpdateBranch: %v", err)
	}
	ms := f.Mutations()
	if len(ms) != 1 || ms[0].Op != "update-branch" || ms[0].Style != forge.UpdateRebase {
		t.Fatalf("mutations = %+v, want one update-branch with rebase", ms)
	}
	want, err := f.GetChangeRequest(ctx, repoA, 1)
	if err != nil {
		t.Fatal(err)
	}
	crs, _, ok := s.PeekChangeRequests(repoA)
	if !ok || len(crs) != 1 {
		t.Fatalf("cached list = %+v, ok %v; want one change request", crs, ok)
	}
	if crs[0].HeadSHA != want.HeadSHA || crs[0].CI != domain.CIPending {
		t.Errorf("cached head %q CI %v; want the forge's head %q with CI pending", crs[0].HeadSHA, crs[0].CI, want.HeadSHA)
	}
}

func TestUpdateBranchForgeFailureKeepsCache(t *testing.T) {
	ctx := context.Background()
	f := crFake(1, domain.CIPass)
	s := core.New(f, core.Options{})
	if _, err := s.ChangeRequests(ctx, repoA); err != nil {
		t.Fatal(err)
	}
	f.FailNext(fmt.Errorf("%w: merge failed because of conflict", forge.ErrRefused))
	err := s.UpdateBranch(ctx, repoA, 1, forge.UpdateMerge)
	if !errors.Is(err, forge.ErrRefused) {
		t.Fatalf("UpdateBranch error = %v, want ErrRefused", err)
	}
	for _, want := range []string{fmt.Sprintf("update %s#1", repoA), "merge failed because of conflict"} {
		if msg := err.Error(); !strings.Contains(msg, want) {
			t.Errorf("error %q lacks %q", msg, want)
		}
	}
	crs, _, _ := s.PeekChangeRequests(repoA)
	if len(crs) != 1 || crs[0].HeadSHA != "sha1" {
		t.Errorf("cached list changed on failure: %+v", crs)
	}
}

func TestUpdateBranchRefreshFailureIsReported(t *testing.T) {
	ctx := context.Background()
	fetchErr := errors.New("fetch failed")
	f := crFake(1, domain.CIPass)
	s := core.New(refetchFails{Fake: f, err: fetchErr}, core.Options{})
	if _, err := s.ChangeRequests(ctx, repoA); err != nil {
		t.Fatal(err)
	}
	err := s.UpdateBranch(ctx, repoA, 1, forge.UpdateRebase)
	if !errors.Is(err, fetchErr) {
		t.Fatalf("UpdateBranch error = %v, want the fetch error wrapped", err)
	}
	if prefix := fmt.Sprintf("refresh %s#1 after update:", repoA); !strings.HasPrefix(err.Error(), prefix) {
		t.Errorf("error %q does not start with %q", err, prefix)
	}
	if n := len(f.Mutations()); n != 1 {
		t.Errorf("%d mutations, want the update to have happened", n)
	}
	crs, _, _ := s.PeekChangeRequests(repoA)
	if len(crs) != 1 || crs[0].HeadSHA != "sha1" {
		t.Errorf("cached list changed when the refresh failed: %+v", crs)
	}
}

func TestUpdateBranchDropsRefetchedClosedChangeRequest(t *testing.T) {
	ctx := context.Background()
	f := crFake(2, domain.CIPass)
	s := core.New(mergedOnRefetch{f}, core.Options{})
	if _, err := s.ChangeRequests(ctx, repoA); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateBranch(ctx, repoA, 1, forge.UpdateRebase); err != nil {
		t.Fatalf("UpdateBranch: %v", err)
	}
	crs, _, _ := s.PeekChangeRequests(repoA)
	for _, c := range crs {
		if c.Number == 1 {
			t.Errorf("change request 1 still cached after the refetch reported it merged: %+v", crs)
		}
	}
	if len(crs) != 1 {
		t.Errorf("cached list = %+v, want only change request 2", crs)
	}
}

func TestUpdateStylesEmptyWhenForgeHasNone(t *testing.T) {
	f := crFake(1, domain.CIPass)
	f.SetUpdateStyles()
	if got := core.New(f, core.Options{}).UpdateStyles(); len(got) != 0 {
		t.Errorf("UpdateStyles() after SetUpdateStyles() = %v, want none", got)
	}
}

func TestUpdateBranchSendsChosenStyle(t *testing.T) {
	for _, style := range []forge.UpdateStyle{forge.UpdateMerge, forge.UpdateRebase} {
		t.Run(string(style), func(t *testing.T) {
			ctx := context.Background()
			f := crFake(1, domain.CIPass)
			s := core.New(f, core.Options{})
			if _, err := s.ChangeRequests(ctx, repoA); err != nil {
				t.Fatal(err)
			}
			if err := s.UpdateBranch(ctx, repoA, 1, style); err != nil {
				t.Fatalf("UpdateBranch: %v", err)
			}
			ms := f.Mutations()
			if len(ms) != 1 || ms[0].Op != "update-branch" || ms[0].Style != style {
				t.Errorf("mutations = %+v, want one update-branch with %s", ms, style)
			}
		})
	}
}

func TestUpdateBranchLeavesOtherChangeRequestsCached(t *testing.T) {
	ctx := context.Background()
	f := crFake(2, domain.CIPass)
	s := core.New(f, core.Options{})
	if _, err := s.ChangeRequests(ctx, repoA); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateBranch(ctx, repoA, 1, forge.UpdateRebase); err != nil {
		t.Fatalf("UpdateBranch: %v", err)
	}
	crs, _, ok := s.PeekChangeRequests(repoA)
	if !ok || len(crs) != 2 {
		t.Fatalf("cached list = %+v, ok %v; want both change requests", crs, ok)
	}
	byNumber := map[int]domain.ChangeRequest{}
	for _, c := range crs {
		byNumber[c.Number] = c
	}
	if got := byNumber[2]; got.HeadSHA != "sha2" || got.CI != domain.CIPass {
		t.Errorf("change request 2 cached as head %q CI %v; want its untouched head sha2 with CI pass", got.HeadSHA, got.CI)
	}
	if got := byNumber[1]; got.HeadSHA == "sha1" {
		t.Errorf("change request 1 still cached with its old head %q", got.HeadSHA)
	}
}
