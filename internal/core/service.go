// Package core holds forge-agnostic logic: the cache, refresh plumbing, mutations, coverage tracking and the image cache; it imports only forge, domain and core/renovate.
package core

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

const defaultMaxConcurrent = 4

// Kind is the kind of data a cache entry holds.
type Kind int

// Cache entry kinds.
const (
	KindRepos Kind = iota
	KindChangeRequests
	KindIssues
	KindReleases
	KindRuns
	KindReadme
	KindBranches
	KindCommits
)

// Key identifies a cache entry; Repo is zero for KindRepos, Number is for per-item kinds and Ref for branch kinds.
type Key struct {
	Kind   Kind
	Repo   domain.RepoRef
	Number int
	Ref    string
}

// Options configures a Service.
type Options struct {
	MaxConcurrent  int                       // 0 means 4
	Now            func() time.Time          // nil means time.Now
	RequireGreenCI func(domain.RepoRef) bool // nil means off
	RenovateUser   string                    // "" detects Renovate PRs by branch only
	HideRenovate   bool                      // the repo list omits the ★ Renovate row
}

type entry struct {
	val any
	at  time.Time
}

// Service caches reads from one forge and bounds its concurrent calls; it is safe for concurrent use.
type Service struct {
	f         forge.Forge
	now       func() time.Time
	sem       chan struct{}
	greenOnly func(domain.RepoRef) bool
	renovUser string
	hideStar  bool

	mu     sync.Mutex
	cache  map[Key]entry
	images *imageCache
}

// New returns a Service over f.
func New(f forge.Forge, opts Options) *Service {
	if opts.MaxConcurrent <= 0 {
		opts.MaxConcurrent = defaultMaxConcurrent
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Service{
		f:         f,
		now:       opts.Now,
		sem:       make(chan struct{}, opts.MaxConcurrent),
		greenOnly: opts.RequireGreenCI,
		renovUser: opts.RenovateUser,
		hideStar:  opts.HideRenovate,
		cache:     map[Key]entry{},
		images:    newImageCache(),
	}
}

// Info returns the host description.
func (s *Service) Info() forge.HostInfo { return s.f.Info() }

// Can reports whether action a is available on r.
func (s *Service) Can(a forge.Action, r domain.Repo) forge.Availability {
	return forge.Can(s.f, a, r)
}

// Invalidate makes the next Peek for k report ok=false.
func (s *Service) Invalidate(k Key) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.cache, k)
}

// Repos fetches the repo list in the adapter's order.
func (s *Service) Repos(ctx context.Context) ([]domain.Repo, error) {
	return fetch(ctx, s, Key{Kind: KindRepos}, "list repos", s.f.ListRepos)
}

// PeekRepos returns the cached repo list without I/O.
func (s *Service) PeekRepos() ([]domain.Repo, time.Time, bool) {
	return peek[domain.Repo](s, Key{Kind: KindRepos})
}

// ChangeRequests fetches open change requests for r, with Renovate filled on Renovate PRs.
func (s *Service) ChangeRequests(ctx context.Context, r domain.RepoRef) ([]domain.ChangeRequest, error) {
	return fetch(ctx, s, Key{Kind: KindChangeRequests, Repo: r}, "list change requests for "+r.String(),
		func(ctx context.Context) ([]domain.ChangeRequest, error) {
			crs, err := s.f.ListChangeRequests(ctx, r, forge.Filter{State: domain.StateOpen})
			for i := range crs {
				crs[i] = s.fillRenovate(crs[i])
			}
			return crs, err
		})
}

// PeekChangeRequests returns the cached open change requests for r without I/O.
func (s *Service) PeekChangeRequests(r domain.RepoRef) ([]domain.ChangeRequest, time.Time, bool) {
	return peek[domain.ChangeRequest](s, Key{Kind: KindChangeRequests, Repo: r})
}

// Issues fetches open issues for r.
func (s *Service) Issues(ctx context.Context, r domain.RepoRef) ([]domain.Issue, error) {
	return fetch(ctx, s, Key{Kind: KindIssues, Repo: r}, "list issues for "+r.String(),
		func(ctx context.Context) ([]domain.Issue, error) {
			return s.f.ListIssues(ctx, r, forge.Filter{State: domain.StateOpen})
		})
}

// PeekIssues returns the cached open issues for r without I/O.
func (s *Service) PeekIssues(r domain.RepoRef) ([]domain.Issue, time.Time, bool) {
	return peek[domain.Issue](s, Key{Kind: KindIssues, Repo: r})
}

// Releases fetches releases for r.
func (s *Service) Releases(ctx context.Context, r domain.RepoRef) ([]domain.Release, error) {
	return fetch(ctx, s, Key{Kind: KindReleases, Repo: r}, "list releases for "+r.String(),
		func(ctx context.Context) ([]domain.Release, error) {
			return s.f.ListReleases(ctx, r)
		})
}

// PeekReleases returns the cached releases for r without I/O.
func (s *Service) PeekReleases(r domain.RepoRef) ([]domain.Release, time.Time, bool) {
	return peek[domain.Release](s, Key{Kind: KindReleases, Repo: r})
}

// Runs fetches CI runs for r; the error matches forge.ErrUnsupported when the forge can't list runs.
func (s *Service) Runs(ctx context.Context, r domain.RepoRef) ([]domain.Run, error) {
	rl, ok := s.f.(forge.RunLister)
	if !ok {
		return nil, fmt.Errorf("list runs for %s: %w", r, forge.ErrUnsupported)
	}
	return fetch(ctx, s, Key{Kind: KindRuns, Repo: r}, "list runs for "+r.String(),
		func(ctx context.Context) ([]domain.Run, error) {
			return rl.ListRuns(ctx, r, forge.RunFilter{})
		})
}

// PeekRuns returns the cached runs for r without I/O.
func (s *Service) PeekRuns(r domain.RepoRef) ([]domain.Run, time.Time, bool) {
	return peek[domain.Run](s, Key{Kind: KindRuns, Repo: r})
}

// Readme fetches r's README; a repo without one yields the zero Readme and a nil error, cached like any answer.
func (s *Service) Readme(ctx context.Context, r domain.RepoRef) (domain.Readme, error) {
	rr, ok := s.f.(forge.ReadmeReader)
	if !ok {
		return domain.Readme{}, fmt.Errorf("README of %s: %w", r, forge.ErrUnsupported)
	}
	vals, err := fetch(ctx, s, Key{Kind: KindReadme, Repo: r}, "README of "+r.String(),
		func(ctx context.Context) ([]domain.Readme, error) {
			rd, err := rr.GetReadme(ctx, r)
			if errors.Is(err, forge.ErrNotFound) {
				return []domain.Readme{{}}, nil
			}
			return []domain.Readme{rd}, err
		})
	if err != nil {
		return domain.Readme{}, err
	}
	return vals[0], nil
}

// PeekReadme returns the cached README for r without I/O.
func (s *Service) PeekReadme(r domain.RepoRef) (domain.Readme, time.Time, bool) {
	vals, at, ok := peek[domain.Readme](s, Key{Kind: KindReadme, Repo: r})
	if !ok {
		return domain.Readme{}, time.Time{}, false
	}
	return vals[0], at, true
}

// Branches fetches r's branches: the default first, then newest tip commit first, ties by name.
func (s *Service) Branches(ctx context.Context, r domain.RepoRef) ([]domain.Branch, error) {
	br, ok := s.f.(forge.BranchReader)
	if !ok {
		return nil, fmt.Errorf("branches of %s: %w", r, forge.ErrUnsupported)
	}
	return fetch(ctx, s, Key{Kind: KindBranches, Repo: r}, "list branches for "+r.String(),
		func(ctx context.Context) ([]domain.Branch, error) {
			bs, err := br.ListBranches(ctx, r)
			slices.SortFunc(bs, compareBranches)
			return bs, err
		})
}

// PeekBranches returns the cached branches for r without I/O.
func (s *Service) PeekBranches(r domain.RepoRef) ([]domain.Branch, time.Time, bool) {
	return peek[domain.Branch](s, Key{Kind: KindBranches, Repo: r})
}

func compareBranches(a, b domain.Branch) int {
	if a.Default != b.Default {
		if a.Default {
			return -1
		}
		return 1
	}
	if c := b.Commit.Date.Compare(a.Commit.Date); c != 0 {
		return c
	}
	return strings.Compare(a.Name, b.Name)
}

// Commits fetches the newest-first commits of branch; a branch the forge reports as gone or empty yields none, not an error.
func (s *Service) Commits(ctx context.Context, r domain.RepoRef, branch string) ([]domain.Commit, error) {
	br, ok := s.f.(forge.BranchReader)
	if !ok {
		return nil, fmt.Errorf("commits of %s on %s: %w", r, branch, forge.ErrUnsupported)
	}
	return fetch(ctx, s, Key{Kind: KindCommits, Repo: r, Ref: branch}, "commits of "+branch+" in "+r.String(),
		func(ctx context.Context) ([]domain.Commit, error) {
			cs, err := br.ListCommits(ctx, r, branch)
			if errors.Is(err, forge.ErrNotFound) {
				return nil, nil
			}
			return cs, err
		})
}

// PeekCommits returns the cached commits of branch in r without I/O.
func (s *Service) PeekCommits(r domain.RepoRef, branch string) ([]domain.Commit, time.Time, bool) {
	return peek[domain.Commit](s, Key{Kind: KindCommits, Repo: r, Ref: branch})
}

// acquire takes a semaphore slot, giving up if ctx ends first.
func (s *Service) acquire(ctx context.Context) error {
	// Check ctx first: select picks randomly when both cases are ready.
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case s.sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// fetch is a function because methods can't take type parameters.
func fetch[T any](ctx context.Context, s *Service, k Key, what string, call func(context.Context) ([]T, error)) ([]T, error) {
	if err := s.acquire(ctx); err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	vals, err := func() ([]T, error) {
		defer func() { <-s.sem }()
		return call(ctx)
	}()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	s.mu.Lock()
	s.cache[k] = entry{val: slices.Clone(vals), at: s.now()}
	s.mu.Unlock()
	return vals, nil
}

func peek[T any](s *Service, k Key) ([]T, time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.cache[k]
	if !ok {
		return nil, time.Time{}, false
	}
	return slices.Clone(e.val.([]T)), e.at, true
}
