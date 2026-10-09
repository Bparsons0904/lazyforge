package core

import (
	"maps"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
)

// Coverage tracks a host-wide scan so the UI can show progress and bulk actions can see gaps.
// It is owned by one goroutine and not safe for concurrent use.
type Coverage struct {
	expect map[domain.RepoRef]struct{}
	done   map[domain.RepoRef]struct{}
	failed map[domain.RepoRef]error
}

// NewCoverage returns a Coverage expecting a scan of repos; duplicates count once.
func NewCoverage(repos []domain.RepoRef) *Coverage {
	expect := make(map[domain.RepoRef]struct{}, len(repos))
	for _, r := range repos {
		expect[r] = struct{}{}
	}
	return &Coverage{expect: expect, done: map[domain.RepoRef]struct{}{}, failed: map[domain.RepoRef]error{}}
}

// Done records the outcome for r; a repeat call overwrites the earlier outcome.
// A repo outside the expected set is ignored so Scanned never exceeds Total.
func (c *Coverage) Done(r domain.RepoRef, err error) {
	if _, ok := c.expect[r]; !ok {
		return
	}
	c.done[r] = struct{}{}
	if err != nil {
		c.failed[r] = err
		return
	}
	delete(c.failed, r)
}

// Total returns the number of repos expected.
func (c *Coverage) Total() int { return len(c.expect) }

// Scanned returns the number of repos with an outcome, failed or not.
func (c *Coverage) Scanned() int { return len(c.done) }

// Failed returns a copy of the failures by repo.
func (c *Coverage) Failed() map[domain.RepoRef]error { return maps.Clone(c.failed) }

// Complete reports whether every repo was scanned without error.
func (c *Coverage) Complete() bool { return len(c.done) == len(c.expect) && len(c.failed) == 0 }
