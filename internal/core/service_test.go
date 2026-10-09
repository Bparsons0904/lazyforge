package core_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/core"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge/forgetest"
)

var (
	repoA = domain.RepoRef{Owner: "owner", Name: "a"}
	repoB = domain.RepoRef{Owner: "owner", Name: "b"}
	t0    = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
)

func seeded() *forgetest.Fake {
	f := forgetest.NewFake(forge.HostInfo{Kind: forge.KindForgejo, User: "bob", ChangeRequestTerm: "PR"})
	f.AddRepo(domain.Repo{RepoRef: repoA, LastActivity: t0})
	f.AddRepo(domain.Repo{RepoRef: repoB, LastActivity: t0.Add(-time.Hour)})
	f.AddChangeRequest(repoA, domain.ChangeRequest{Number: 1, Title: "open", State: domain.StateOpen})
	f.AddChangeRequest(repoA, domain.ChangeRequest{Number: 2, Title: "merged", State: domain.StateMerged})
	f.AddIssue(repoA, domain.Issue{Number: 10, Title: "open", State: domain.StateOpen})
	f.AddIssue(repoA, domain.Issue{Number: 11, Title: "closed", State: domain.StateClosed})
	f.AddRelease(repoA, domain.Release{Tag: "v1"})
	f.AddRun(repoA, domain.Run{ID: 100, Number: 1, Branch: "main"}, nil, nil)
	return f
}

// stepClock returns t0, t0+1m, t0+2m, ... on successive calls.
func stepClock() func() time.Time {
	var mu sync.Mutex
	n := 0
	return func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		n++
		return t0.Add(time.Duration(n-1) * time.Minute)
	}
}

// kindOps is one cached list, reduced to fetch/peek closures so one table covers every Kind.
type kindOps struct {
	name  string
	key   core.Key
	fetch func(s *core.Service) (n int, err error)
	peek  func(s *core.Service) (n int, fetched time.Time, ok bool)
}

func allKinds() []kindOps {
	ctx := context.Background()
	return []kindOps{
		{
			"repos",
			core.Key{Kind: core.KindRepos},
			func(s *core.Service) (int, error) { v, err := s.Repos(ctx); return len(v), err },
			func(s *core.Service) (int, time.Time, bool) { v, at, ok := s.PeekRepos(); return len(v), at, ok },
		},
		{
			"change requests",
			core.Key{Kind: core.KindChangeRequests, Repo: repoA},
			func(s *core.Service) (int, error) { v, err := s.ChangeRequests(ctx, repoA); return len(v), err },
			func(s *core.Service) (int, time.Time, bool) {
				v, at, ok := s.PeekChangeRequests(repoA)
				return len(v), at, ok
			},
		},
		{
			"issues",
			core.Key{Kind: core.KindIssues, Repo: repoA},
			func(s *core.Service) (int, error) { v, err := s.Issues(ctx, repoA); return len(v), err },
			func(s *core.Service) (int, time.Time, bool) { v, at, ok := s.PeekIssues(repoA); return len(v), at, ok },
		},
		{
			"releases",
			core.Key{Kind: core.KindReleases, Repo: repoA},
			func(s *core.Service) (int, error) { v, err := s.Releases(ctx, repoA); return len(v), err },
			func(s *core.Service) (int, time.Time, bool) {
				v, at, ok := s.PeekReleases(repoA)
				return len(v), at, ok
			},
		},
		{
			"runs",
			core.Key{Kind: core.KindRuns, Repo: repoA},
			func(s *core.Service) (int, error) { v, err := s.Runs(ctx, repoA); return len(v), err },
			func(s *core.Service) (int, time.Time, bool) { v, at, ok := s.PeekRuns(repoA); return len(v), at, ok },
		},
	}
}

func TestFetchThenPeek(t *testing.T) {
	// open-only filtering: repoA has 2 CRs and 2 issues but only 1 of each is open
	wantLen := map[string]int{"repos": 2, "change requests": 1, "issues": 1, "releases": 1, "runs": 1}
	for _, k := range allKinds() {
		t.Run(k.name, func(t *testing.T) {
			s := core.New(seeded(), core.Options{Now: stepClock()})

			if _, _, ok := k.peek(s); ok {
				t.Fatal("Peek before any fetch reported ok")
			}
			n, err := k.fetch(s)
			if err != nil {
				t.Fatal(err)
			}
			if n != wantLen[k.name] {
				t.Errorf("fetched %d items, want %d", n, wantLen[k.name])
			}
			pn, at, ok := k.peek(s)
			if !ok || pn != n {
				t.Fatalf("Peek = (%d, ok=%v), want (%d, true)", pn, ok, n)
			}
			if !at.Equal(t0) {
				t.Errorf("fetched time = %v, want injected Now %v", at, t0)
			}
		})
	}
}

func TestInvalidate(t *testing.T) {
	for _, k := range allKinds() {
		t.Run(k.name, func(t *testing.T) {
			s := core.New(seeded(), core.Options{})
			if _, err := k.fetch(s); err != nil {
				t.Fatal(err)
			}
			s.Invalidate(k.key)
			if _, _, ok := k.peek(s); ok {
				t.Error("Peek after Invalidate reported ok")
			}
			if _, err := k.fetch(s); err != nil {
				t.Fatal(err)
			}
			if _, _, ok := k.peek(s); !ok {
				t.Error("Peek after refetch reported not ok")
			}
		})
	}
}

func TestInvalidateIsScopedToItsKey(t *testing.T) {
	ctx := context.Background()
	f := seeded()
	f.AddIssue(repoB, domain.Issue{Number: 1, State: domain.StateOpen})
	s := core.New(f, core.Options{})
	for _, r := range []domain.RepoRef{repoA, repoB} {
		if _, err := s.Issues(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.ChangeRequests(ctx, repoA); err != nil {
		t.Fatal(err)
	}

	s.Invalidate(core.Key{Kind: core.KindIssues, Repo: repoA})

	if _, _, ok := s.PeekIssues(repoA); ok {
		t.Error("invalidated issues still peekable")
	}
	if _, _, ok := s.PeekIssues(repoB); !ok {
		t.Error("invalidating repo A issues dropped repo B issues")
	}
	if _, _, ok := s.PeekChangeRequests(repoA); !ok {
		t.Error("invalidating issues dropped change requests")
	}
}

func TestDefaultNowIsWallClock(t *testing.T) {
	s := core.New(seeded(), core.Options{})
	before := time.Now()
	if _, err := s.Repos(context.Background()); err != nil {
		t.Fatal(err)
	}
	after := time.Now()
	_, at, _ := s.PeekRepos()
	if at.Before(before) || at.After(after) {
		t.Errorf("fetched time %v outside [%v, %v]", at, before, after)
	}
}

func TestFailedFetchWrapsErrorAndKeepsEntry(t *testing.T) {
	for _, k := range allKinds() {
		t.Run(k.name, func(t *testing.T) {
			f := seeded()
			s := core.New(f, core.Options{Now: stepClock()})
			n, err := k.fetch(s)
			if err != nil {
				t.Fatal(err)
			}

			f.FailNext(forge.ErrRateLimited)
			_, err = k.fetch(s)
			if !errors.Is(err, forge.ErrRateLimited) {
				t.Fatalf("err = %v, want it to match ErrRateLimited", err)
			}
			if err.Error() == forge.ErrRateLimited.Error() {
				t.Errorf("err %q carries no context", err)
			}
			pn, at, ok := k.peek(s)
			if !ok || pn != n || !at.Equal(t0) {
				t.Errorf("Peek after failed fetch = (%d, %v, ok=%v), want old (%d, %v, true)", pn, at, ok, n, t0)
			}
		})
	}
}

func TestFailedFirstFetchStaysEmpty(t *testing.T) {
	f := seeded()
	s := core.New(f, core.Options{})
	f.FailNext(forge.ErrUnauthorized)
	if _, err := s.Repos(context.Background()); !errors.Is(err, forge.ErrUnauthorized) {
		t.Fatalf("err = %v, want ErrUnauthorized", err)
	}
	if _, _, ok := s.PeekRepos(); ok {
		t.Error("failed first fetch left a cache entry")
	}
}

// listing returns fixed backing slices, so the test can mutate what the forge handed over.
type listing struct {
	forge.Forge
	repos  []domain.Repo
	issues []domain.Issue
}

func (l *listing) ListRepos(context.Context) ([]domain.Repo, error) { return l.repos, nil }
func (l *listing) ListIssues(context.Context, domain.RepoRef, forge.Filter) ([]domain.Issue, error) {
	return l.issues, nil
}

func TestCachedSlicesAreCopies(t *testing.T) {
	ctx := context.Background()
	l := &listing{
		Forge:  seeded(),
		repos:  []domain.Repo{{RepoRef: repoA}, {RepoRef: repoB}},
		issues: []domain.Issue{{Number: 1, Title: "one"}, {Number: 2, Title: "two"}},
	}
	s := core.New(l, core.Options{})

	returned, err := s.Issues(ctx, repoA)
	if err != nil {
		t.Fatal(err)
	}
	l.issues[0].Title = "forge mutated after store"
	returned[1].Title = "caller mutated fetch result"

	peeked, _, _ := s.PeekIssues(repoA)
	if peeked[0].Title != "one" || peeked[1].Title != "two" {
		t.Errorf("cache shares memory with the forge or the fetch result: %+v", peeked)
	}
	peeked[0].Title = "caller mutated peek result"
	again, _, _ := s.PeekIssues(repoA)
	if again[0].Title != "one" {
		t.Errorf("cache shares memory with a Peek result: %+v", again)
	}

	if _, err := s.Repos(ctx); err != nil {
		t.Fatal(err)
	}
	l.repos[0].RepoRef = domain.RepoRef{Owner: "x", Name: "y"}
	repos, _, _ := s.PeekRepos()
	if repos[0].RepoRef != repoA {
		t.Errorf("repo cache shares memory with the forge: %+v", repos)
	}
}

func TestReposKeepAdapterOrder(t *testing.T) {
	want := []domain.RepoRef{{Owner: "o", Name: "z"}, {Owner: "o", Name: "a"}, {Owner: "o", Name: "m"}}
	l := &listing{Forge: seeded()}
	for _, r := range want {
		l.repos = append(l.repos, domain.Repo{RepoRef: r})
	}
	s := core.New(l, core.Options{})

	got, err := s.Repos(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	refs := func(rs []domain.Repo) []domain.RepoRef {
		out := make([]domain.RepoRef, len(rs))
		for i, r := range rs {
			out[i] = r.RepoRef
		}
		return out
	}
	if !slices.Equal(refs(got), want) {
		t.Errorf("Repos order = %v, want %v", refs(got), want)
	}
	peeked, _, _ := s.PeekRepos()
	if !slices.Equal(refs(peeked), want) {
		t.Errorf("PeekRepos order = %v, want %v", refs(peeked), want)
	}
}

func TestOpenOnlyFilters(t *testing.T) {
	ctx := context.Background()
	s := core.New(seeded(), core.Options{})
	crs, err := s.ChangeRequests(ctx, repoA)
	if err != nil {
		t.Fatal(err)
	}
	if len(crs) != 1 || crs[0].State != domain.StateOpen {
		t.Errorf("ChangeRequests = %+v, want only the open one", crs)
	}
	iss, err := s.Issues(ctx, repoA)
	if err != nil {
		t.Fatal(err)
	}
	if len(iss) != 1 || iss[0].State != domain.StateOpen {
		t.Errorf("Issues = %+v, want only the open one", iss)
	}
}

// noRuns hides the wrapped forge's RunLister: embedding the interface promotes only forge.Forge.
type noRuns struct{ forge.Forge }

func TestRunsUnsupportedWithoutRunLister(t *testing.T) {
	s := core.New(noRuns{seeded()}, core.Options{})
	_, err := s.Runs(context.Background(), repoA)
	if !errors.Is(err, forge.ErrUnsupported) {
		t.Fatalf("err = %v, want ErrUnsupported", err)
	}
	if _, _, ok := s.PeekRuns(repoA); ok {
		t.Error("unsupported Runs left a cache entry")
	}
}

func TestInfoAndCanDelegate(t *testing.T) {
	f := seeded()
	f.SetGate(forge.ActMerge, errors.New("merging is off"))
	s := core.New(f, core.Options{})

	if got, want := s.Info(), f.Info(); got != want {
		t.Errorf("Info() = %+v, want %+v", got, want)
	}
	repo := domain.Repo{RepoRef: repoA, Access: domain.AccessWrite}
	for _, a := range []forge.Action{forge.ActMerge, forge.ActClose, forge.ActRuns} {
		if got, want := s.Can(a, repo), forge.Can(f, a, repo); got != want {
			t.Errorf("Can(%d) = %+v, want %+v", a, got, want)
		}
	}
	if s.Can(forge.ActMerge, repo).OK {
		t.Error("gated action reported OK")
	}
}

// slowIssues counts concurrent ListIssues calls and holds each one until released.
type slowIssues struct {
	forge.Forge
	mu       sync.Mutex
	inflight int
	peak     int
	calls    int
	entered  chan struct{}
	release  chan struct{}
}

func newSlowIssues() *slowIssues {
	return &slowIssues{Forge: seeded(), entered: make(chan struct{}, 64), release: make(chan struct{})}
}

func (f *slowIssues) ListIssues(ctx context.Context, _ domain.RepoRef, _ forge.Filter) ([]domain.Issue, error) {
	f.mu.Lock()
	f.calls++
	f.inflight++
	f.peak = max(f.peak, f.inflight)
	f.mu.Unlock()
	f.entered <- struct{}{}
	defer func() {
		f.mu.Lock()
		f.inflight--
		f.mu.Unlock()
	}()
	select {
	case <-f.release:
		return nil, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (f *slowIssues) stats() (inflight, peak, calls int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.inflight, f.peak, f.calls
}

func TestConcurrencyIsBounded(t *testing.T) {
	const callers = 12
	tests := []struct {
		name string
		opts core.Options
		max  int
	}{
		{"default is 4", core.Options{}, 4},
		{"explicit limit", core.Options{MaxConcurrent: 2}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newSlowIssues()
			s := core.New(f, tt.opts)
			var wg sync.WaitGroup
			for i := range callers {
				wg.Add(1)
				go func() {
					defer wg.Done()
					r := domain.RepoRef{Owner: "o", Name: string(rune('a' + i))}
					if _, err := s.Issues(context.Background(), r); err != nil {
						t.Errorf("Issues: %v", err)
					}
				}()
			}
			for range tt.max {
				<-f.entered
			}
			// A leaked slot would let a further caller in; give it time to show up.
			select {
			case <-f.entered:
				t.Fatalf("more than %d forge calls in flight", tt.max)
			case <-time.After(100 * time.Millisecond):
			}
			if inflight, _, _ := f.stats(); inflight != tt.max {
				t.Errorf("in flight = %d, want exactly %d", inflight, tt.max)
			}

			close(f.release)
			wg.Wait()
			if _, peak, calls := f.stats(); peak > tt.max || calls != callers {
				t.Errorf("peak = %d (max %d), calls = %d (want %d)", peak, tt.max, calls, callers)
			}
		})
	}
}

// doneSpy closes waiting the first time Done is called, which a call blocked on a slot does.
type doneSpy struct {
	context.Context
	once    sync.Once
	waiting chan struct{}
}

func (c *doneSpy) Done() <-chan struct{} {
	c.once.Do(func() { close(c.waiting) })
	return c.Context.Done()
}

func TestQueuedCallHonorsCancelledContext(t *testing.T) {
	f := newSlowIssues()
	s := core.New(f, core.Options{MaxConcurrent: 1})

	holder := make(chan error, 1)
	go func() {
		_, err := s.Issues(context.Background(), repoA)
		holder <- err
	}()
	<-f.entered // the only slot is now held

	base, cancel := context.WithCancel(context.Background())
	ctx := &doneSpy{Context: base, waiting: make(chan struct{})}
	queued := make(chan error, 1)
	go func() {
		_, err := s.Issues(ctx, repoB)
		queued <- err
	}()
	<-ctx.waiting // the call is blocked on the slot, not short-circuited by an Err check
	cancel()

	if err := <-queued; !errors.Is(err, context.Canceled) {
		t.Errorf("queued err = %v, want context.Canceled", err)
	}
	if _, _, calls := f.stats(); calls != 1 {
		t.Errorf("forge called %d times, want 1 (the queued call must never reach it)", calls)
	}

	close(f.release)
	if err := <-holder; err != nil {
		t.Errorf("holder err = %v", err)
	}
	if _, _, ok := s.PeekIssues(repoB); ok {
		t.Error("cancelled call left a cache entry")
	}
}

func TestSlotIsReleasedAfterFetch(t *testing.T) {
	f := seeded()
	s := core.New(f, core.Options{MaxConcurrent: 1})
	ctx := context.Background()
	for range 3 {
		f.FailNext(forge.ErrNotFound)
		if _, err := s.Repos(ctx); err == nil {
			t.Fatal("want failure")
		}
	}
	done := make(chan error, 1)
	go func() { _, err := s.Repos(ctx); done <- err }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("slot leaked: fetch after failed fetches never completed")
	}
}

func TestConcurrentUseIsRaceFree(_ *testing.T) {
	s := core.New(seeded(), core.Options{})
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx := context.Background()
			for range 50 {
				_, _ = s.Repos(ctx)
				_, _ = s.Issues(ctx, repoA)
				_, _ = s.Runs(ctx, repoA)
				s.PeekRepos()
				s.PeekIssues(repoA)
				s.Invalidate(core.Key{Kind: core.KindIssues, Repo: repoA})
			}
		}()
	}
	wg.Wait()
}
