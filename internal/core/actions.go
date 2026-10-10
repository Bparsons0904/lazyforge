package core

import (
	"context"
	"errors"
	"fmt"
	"net"
	"slices"
	"sync"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

var errCIGate = errors.New("CI isn't green and require_green_ci is on for this repo")

// Target is one change request as the user confirmed it; CR.HeadSHA pins what may be merged.
type Target struct {
	Repo domain.RepoRef
	CR   domain.ChangeRequest
}

// Outcome classifies one merge attempt.
type Outcome int

// Outcome values; only OutcomeUnknown leaves it open whether the forge merged.
const (
	OutcomeMerged     Outcome = iota
	OutcomeRefused            // head changed, conflicts, branch protection, CI gate
	OutcomeFailed             // network or server error; nothing merged
	OutcomeUnknown            // timed out; the forge may have merged it
	OutcomeNotStarted         // cancelled before it started
)

// MergeResult is one target's outcome; Err is the reason shown for any outcome but merged.
type MergeResult struct {
	Target  Target
	Outcome Outcome
	Err     error
}

// Checked is a Recheck result; Target.CR is fresh only when Err is nil.
type Checked struct {
	Target Target
	Err    error
}

// RequiresGreenCI reports whether Merge refuses r's change requests unless CI is green.
func (s *Service) RequiresGreenCI(r domain.RepoRef) bool {
	return s.greenOnly != nil && s.greenOnly(r)
}

// Merge dedups ts by (Repo, CR.Number), keeping the first occurrence and input order, and runs them under the
// semaphore. Cancelling ctx stops new starts; started merges finish. Results come back in the deduped order.
func (s *Service) Merge(ctx context.Context, ts []Target) []MergeResult {
	type id struct {
		repo domain.RepoRef
		n    int
	}
	seen := map[id]bool{}
	var out []MergeResult
	for _, t := range ts {
		if k := (id{t.Repo, t.CR.Number}); !seen[k] {
			seen[k] = true
			out = append(out, MergeResult{Target: t, Outcome: OutcomeNotStarted})
		}
	}
	// Started merges run detached from ctx, so esc can't abandon a request the forge may already be applying.
	run := context.WithoutCancel(ctx)
	var wg sync.WaitGroup
	for i := range out {
		if err := s.acquire(ctx); err != nil {
			for j := i; j < len(out); j++ {
				out[j].Err = fmt.Errorf("not started: %w", err)
			}
			break
		}
		wg.Go(func() {
			defer func() { <-s.sem }()
			out[i].Outcome, out[i].Err = s.mergeOne(run, out[i].Target)
		})
	}
	wg.Wait()
	return out
}

func (s *Service) mergeOne(ctx context.Context, t Target) (Outcome, error) {
	if s.RequiresGreenCI(t.Repo) && !t.CR.CI.Green() {
		return OutcomeRefused, errCIGate
	}
	err := s.f.Merge(ctx, t.Repo, t.CR.Number, forge.MergeOpts{HeadSHA: t.CR.HeadSHA})
	var ne net.Error
	switch {
	case err == nil:
		s.dropCR(t.Repo, t.CR.Number)
		return OutcomeMerged, nil
	case errors.Is(err, forge.ErrHeadChanged) || errors.Is(err, forge.ErrRefused):
		return OutcomeRefused, err
	case errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()):
		return OutcomeUnknown, err
	}
	return OutcomeFailed, err
}

// Recheck re-fetches each target concurrently under the semaphore and updates the cached CR list.
func (s *Service) Recheck(ctx context.Context, ts []Target) []Checked {
	out := make([]Checked, len(ts))
	var wg sync.WaitGroup
	for i, t := range ts {
		wg.Go(func() {
			out[i].Target = t
			cr, err := s.refresh(ctx, fmt.Sprintf("recheck %s#%d", t.Repo, t.CR.Number), t.Repo, t.CR.Number)
			if err != nil {
				out[i].Err = err
				return
			}
			out[i].Target.CR = cr
		})
	}
	wg.Wait()
	return out
}

// refresh fetches change request n under the semaphore, then drops it from the cached open list or replaces its entry.
// what labels the fetch in the error, so callers name the step that failed.
func (s *Service) refresh(ctx context.Context, what string, r domain.RepoRef, n int) (domain.ChangeRequest, error) {
	var cr domain.ChangeRequest
	err := s.do(ctx, what, func(ctx context.Context) (err error) {
		cr, err = s.f.GetChangeRequest(ctx, r, n)
		return err
	})
	if err != nil {
		return domain.ChangeRequest{}, err
	}
	cr = s.fillRenovate(cr)
	if cr.State != domain.StateOpen {
		s.dropCR(r, cr.Number)
		return cr, nil
	}
	update(s, Key{Kind: KindChangeRequests, Repo: r}, func(crs []domain.ChangeRequest) []domain.ChangeRequest {
		if j := slices.IndexFunc(crs, func(c domain.ChangeRequest) bool { return c.Number == cr.Number }); j >= 0 {
			crs[j] = cr
		}
		return crs
	})
	return cr, nil
}

// UpdateStyles returns the styles UpdateBranch accepts, merge first; nil when the forge can't update branches.
func (s *Service) UpdateStyles() []forge.UpdateStyle {
	b, ok := s.f.(forge.BranchUpdater)
	if !ok {
		return nil
	}
	return b.UpdateStyles()
}

// UpdateBranch brings change request n up to date with its target branch, then refreshes it in the cached list.
// It wraps forge.ErrUnsupported when the forge can't update branches, and makes no forge call in that case.
func (s *Service) UpdateBranch(ctx context.Context, r domain.RepoRef, n int, style forge.UpdateStyle) error {
	what := fmt.Sprintf("update %s#%d", r, n)
	b, ok := s.f.(forge.BranchUpdater)
	if !ok {
		return fmt.Errorf("%s: %w", what, forge.ErrUnsupported)
	}
	if err := s.do(ctx, what, func(ctx context.Context) error { return b.UpdateBranch(ctx, r, n, style) }); err != nil {
		return err
	}
	// The update already happened, so a failed refresh is reported as its own error, not as a failed update.
	_, err := s.refresh(ctx, fmt.Sprintf("refresh %s#%d after update", r, n), r, n)
	return err
}

// Approve wraps forge.ErrUnsupported when the forge can't approve.
func (s *Service) Approve(ctx context.Context, r domain.RepoRef, n int) error {
	what := fmt.Sprintf("approve %s#%d", r, n)
	a, ok := s.f.(forge.Approver)
	if !ok {
		return fmt.Errorf("%s: %w", what, forge.ErrUnsupported)
	}
	return s.do(ctx, what, func(ctx context.Context) error { return a.Approve(ctx, r, n) })
}

// Close drops the item from its cached open list on success.
func (s *Service) Close(ctx context.Context, item forge.ItemRef) error {
	err := s.do(ctx, fmt.Sprintf("close %s#%d", item.Repo, item.Number), func(ctx context.Context) error {
		return s.f.Close(ctx, item)
	})
	if err != nil {
		return err
	}
	if item.Kind == forge.ItemChangeRequest {
		s.dropCR(item.Repo, item.Number)
		return nil
	}
	update(s, Key{Kind: KindIssues, Repo: item.Repo}, func(is []domain.Issue) []domain.Issue {
		return slices.DeleteFunc(is, func(i domain.Issue) bool { return i.Number == item.Number })
	})
	return nil
}

// Comment bumps a cached issue's Comments count on success.
func (s *Service) Comment(ctx context.Context, item forge.ItemRef, body string) error {
	err := s.do(ctx, fmt.Sprintf("comment on %s#%d", item.Repo, item.Number), func(ctx context.Context) error {
		return s.f.Comment(ctx, item, body)
	})
	if err != nil || item.Kind != forge.ItemIssue {
		return err
	}
	update(s, Key{Kind: KindIssues, Repo: item.Repo}, func(is []domain.Issue) []domain.Issue {
		if j := slices.IndexFunc(is, func(i domain.Issue) bool { return i.Number == item.Number }); j >= 0 {
			is[j].Comments++
		}
		return is
	})
	return nil
}

// do runs call holding a semaphore slot, so mutations share the bound with reads.
func (s *Service) do(ctx context.Context, what string, call func(context.Context) error) error {
	if err := s.acquire(ctx); err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	defer func() { <-s.sem }()
	if err := call(ctx); err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	return nil
}

func (s *Service) dropCR(r domain.RepoRef, n int) {
	update(s, Key{Kind: KindChangeRequests, Repo: r}, func(crs []domain.ChangeRequest) []domain.ChangeRequest {
		return slices.DeleteFunc(crs, func(c domain.ChangeRequest) bool { return c.Number == n })
	})
}

// update rewrites a cached list and keeps its fetch time; an uncached key stays uncached.
func update[T any](s *Service, k Key, edit func([]T) []T) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.cache[k]
	if !ok {
		return
	}
	cur, ok := e.val.([]T)
	if !ok {
		return
	}
	e.val = edit(slices.Clone(cur))
	s.cache[k] = e
}
