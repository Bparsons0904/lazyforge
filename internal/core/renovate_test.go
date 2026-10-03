package core_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/core"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge/forgetest"
)

// failRepo errors every change-request listing for one repo.
type failRepo struct {
	*forgetest.Fake
	bad domain.RepoRef
}

func (f failRepo) ListChangeRequests(ctx context.Context, r domain.RepoRef, flt forge.Filter) ([]domain.ChangeRequest, error) {
	if r == f.bad {
		return nil, errors.New("boom")
	}
	return f.Fake.ListChangeRequests(ctx, r, flt)
}

// cancelOnList cancels the caller's context as the first change-request listing starts.
type cancelOnList struct {
	*forgetest.Fake
	cancel context.CancelFunc
}

func (c cancelOnList) ListChangeRequests(ctx context.Context, r domain.RepoRef, flt forge.Filter) ([]domain.ChangeRequest, error) {
	c.cancel()
	return c.Fake.ListChangeRequests(ctx, r, flt)
}

func TestSuggestRenovateUser(t *testing.T) {
	renovateCR := domain.ChangeRequest{Number: 1, State: domain.StateOpen, SourceBranch: "renovate/foo", Author: "renovate-bot"}
	newFake := func() *forgetest.Fake {
		return forgetest.NewFake(forge.HostInfo{Kind: forge.KindForgejo, User: "bob", ChangeRequestTerm: "PR"})
	}

	t.Run("found", func(t *testing.T) {
		f := newFake()
		f.AddRepo(domain.Repo{RepoRef: repoA, LastActivity: t0})
		f.AddChangeRequest(repoA, renovateCR)
		got, err := core.New(f, core.Options{}).SuggestRenovateUser(context.Background())
		if err != nil || got != "renovate-bot" {
			t.Fatalf("got %q, %v", got, err)
		}
	})
	t.Run("skips a failing repo", func(t *testing.T) {
		f := newFake()
		f.AddRepo(domain.Repo{RepoRef: repoA, LastActivity: t0})
		f.AddRepo(domain.Repo{RepoRef: repoB, LastActivity: t0.Add(-time.Hour)})
		f.AddChangeRequest(repoB, renovateCR)
		got, err := core.New(failRepo{f, repoA}, core.Options{}).SuggestRenovateUser(context.Background())
		if err != nil || got != "renovate-bot" {
			t.Fatalf("got %q, %v", got, err)
		}
	})
	t.Run("none", func(t *testing.T) {
		f := newFake()
		f.AddRepo(domain.Repo{RepoRef: repoA, LastActivity: t0})
		f.AddChangeRequest(repoA, domain.ChangeRequest{Number: 1, State: domain.StateOpen, SourceBranch: "feat", Author: "bob"})
		got, err := core.New(f, core.Options{}).SuggestRenovateUser(context.Background())
		if err != nil || got != "" {
			t.Fatalf("got %q, %v", got, err)
		}
	})
	t.Run("cancelled", func(t *testing.T) {
		f := newFake()
		f.AddRepo(domain.Repo{RepoRef: repoA, LastActivity: t0})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := core.New(f, core.Options{}).SuggestRenovateUser(ctx); err == nil {
			t.Fatal("want error")
		}
	})
	t.Run("only the 10 most recent repos", func(t *testing.T) {
		f := newFake()
		for i := range 11 {
			f.AddRepo(domain.Repo{RepoRef: domain.RepoRef{Owner: "o", Name: fmt.Sprint("r", i)}, LastActivity: t0.Add(-time.Duration(i) * time.Hour)})
		}
		f.AddChangeRequest(domain.RepoRef{Owner: "o", Name: "r10"}, renovateCR)
		got, err := core.New(f, core.Options{}).SuggestRenovateUser(context.Background())
		if err != nil || got != "" {
			t.Fatalf("got %q, %v", got, err)
		}
	})
}

func TestSuggestRenovateUserEdges(t *testing.T) {
	newFake := func() *forgetest.Fake {
		return forgetest.NewFake(forge.HostInfo{Kind: forge.KindForgejo, User: "bob", ChangeRequestTerm: "PR"})
	}
	cr := func(n int, branch, author string, st domain.State) domain.ChangeRequest {
		return domain.ChangeRequest{Number: n, State: st, SourceBranch: branch, Author: author}
	}
	suggest := func(t *testing.T, f forge.Forge) string {
		t.Helper()
		got, err := core.New(f, core.Options{}).SuggestRenovateUser(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		return got
	}

	t.Run("no repos", func(t *testing.T) {
		if got := suggest(t, newFake()); got != "" {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("closed and merged renovate CRs are ignored", func(t *testing.T) {
		f := newFake()
		f.AddRepo(domain.Repo{RepoRef: repoA, LastActivity: t0})
		f.AddChangeRequest(repoA, cr(1, "renovate/foo", "old-bot", domain.StateMerged))
		f.AddChangeRequest(repoA, cr(2, "renovate/bar", "old-bot", domain.StateClosed))
		if got := suggest(t, f); got != "" {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("branch must start with renovate/", func(t *testing.T) {
		f := newFake()
		f.AddRepo(domain.Repo{RepoRef: repoA, LastActivity: t0})
		f.AddChangeRequest(repoA, cr(1, "feature/renovate/foo", "human", domain.StateOpen))
		f.AddChangeRequest(repoA, cr(2, "renovate-foo", "human2", domain.StateOpen))
		f.AddChangeRequest(repoA, cr(3, "renovate", "human3", domain.StateOpen))
		if got := suggest(t, f); got != "" {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("skips non-renovate CR before a renovate one in the same repo", func(t *testing.T) {
		f := newFake()
		f.AddRepo(domain.Repo{RepoRef: repoA, LastActivity: t0})
		f.AddChangeRequest(repoA, cr(1, "feat/x", "human", domain.StateOpen))
		f.AddChangeRequest(repoA, cr(2, "renovate/y", "mend-bot", domain.StateOpen))
		if got := suggest(t, f); got != "mend-bot" {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("most recently active repo wins", func(t *testing.T) {
		f := newFake()
		f.AddRepo(domain.Repo{RepoRef: repoB, LastActivity: t0.Add(-time.Hour)})
		f.AddRepo(domain.Repo{RepoRef: repoA, LastActivity: t0})
		f.AddChangeRequest(repoA, cr(1, "renovate/a", "newer-bot", domain.StateOpen))
		f.AddChangeRequest(repoB, cr(1, "renovate/b", "older-bot", domain.StateOpen))
		if got := suggest(t, f); got != "newer-bot" {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("10th repo is scanned", func(t *testing.T) {
		f := newFake()
		for i := range 11 {
			f.AddRepo(domain.Repo{RepoRef: domain.RepoRef{Owner: "o", Name: fmt.Sprint("r", i)}, LastActivity: t0.Add(-time.Duration(i) * time.Hour)})
		}
		f.AddChangeRequest(domain.RepoRef{Owner: "o", Name: "r9"}, cr(1, "renovate/z", "edge-bot", domain.StateOpen))
		f.AddChangeRequest(domain.RepoRef{Owner: "o", Name: "r10"}, cr(1, "renovate/z", "too-old-bot", domain.StateOpen))
		if got := suggest(t, f); got != "edge-bot" {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("all repos failing yields empty, no error", func(t *testing.T) {
		f := newFake()
		f.AddRepo(domain.Repo{RepoRef: repoA, LastActivity: t0})
		f.AddChangeRequest(repoA, cr(1, "renovate/a", "bot", domain.StateOpen))
		got, err := core.New(failRepo{f, repoA}, core.Options{}).SuggestRenovateUser(context.Background())
		if err != nil || got != "" {
			t.Fatalf("got %q, %v", got, err)
		}
	})
	t.Run("repo listing failure is returned", func(t *testing.T) {
		f := newFake()
		f.AddRepo(domain.Repo{RepoRef: repoA, LastActivity: t0})
		f.FailNext(forge.ErrUnauthorized)
		_, err := core.New(f, core.Options{}).SuggestRenovateUser(context.Background())
		if !errors.Is(err, forge.ErrUnauthorized) {
			t.Fatalf("got %v", err)
		}
	})
}

func TestSuggestRenovateUserCancelledMidScan(t *testing.T) {
	f := forgetest.NewFake(forge.HostInfo{Kind: forge.KindForgejo, User: "bob", ChangeRequestTerm: "PR"})
	f.AddRepo(domain.Repo{RepoRef: repoA, LastActivity: t0})
	f.AddRepo(domain.Repo{RepoRef: repoB, LastActivity: t0.Add(-time.Hour)})
	f.AddChangeRequest(repoB, domain.ChangeRequest{Number: 1, State: domain.StateOpen, SourceBranch: "renovate/x", Author: "bot"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	got, err := core.New(cancelOnList{f, cancel}, core.Options{}).SuggestRenovateUser(ctx)
	if !errors.Is(err, context.Canceled) || got != "" {
		t.Fatalf("got %q, %v", got, err)
	}
}
