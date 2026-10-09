package core_test

import (
	"errors"
	"testing"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/core"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
)

var repoC = domain.RepoRef{Owner: "owner", Name: "c"}

func TestCoverageStartsEmpty(t *testing.T) {
	c := core.NewCoverage([]domain.RepoRef{repoA, repoB, repoC})
	if c.Total() != 3 || c.Scanned() != 0 || c.Complete() || len(c.Failed()) != 0 {
		t.Errorf("fresh coverage: total=%d scanned=%d complete=%v failed=%v", c.Total(), c.Scanned(), c.Complete(), c.Failed())
	}
}

func TestCoverageCountsFailuresAsScanned(t *testing.T) {
	boom := errors.New("boom")
	c := core.NewCoverage([]domain.RepoRef{repoA, repoB, repoC})
	c.Done(repoA, nil)
	c.Done(repoB, boom)

	if c.Scanned() != 2 {
		t.Errorf("Scanned = %d, want 2 (failures count as scanned)", c.Scanned())
	}
	if failed := c.Failed(); len(failed) != 1 || !errors.Is(failed[repoB], boom) {
		t.Errorf("Failed = %v, want only %s", failed, repoB)
	}
	if c.Complete() {
		t.Error("Complete with an unscanned repo and a failed one")
	}

	c.Done(repoC, nil)
	if c.Scanned() != 3 || c.Complete() {
		t.Errorf("all scanned but one failed: scanned=%d complete=%v, want 3/false", c.Scanned(), c.Complete())
	}
}

func TestCoverageCompleteWhenAllOK(t *testing.T) {
	c := core.NewCoverage([]domain.RepoRef{repoA, repoB})
	c.Done(repoA, nil)
	c.Done(repoB, nil)
	if !c.Complete() || c.Scanned() != 2 || len(c.Failed()) != 0 {
		t.Errorf("complete=%v scanned=%d failed=%v", c.Complete(), c.Scanned(), c.Failed())
	}
}

func TestCoverageDoneOverwrites(t *testing.T) {
	boom := errors.New("boom")
	c := core.NewCoverage([]domain.RepoRef{repoA, repoB})

	c.Done(repoA, boom)
	c.Done(repoA, boom)
	if c.Scanned() != 1 {
		t.Errorf("repeated Done double-counted: Scanned = %d, want 1", c.Scanned())
	}

	c.Done(repoA, nil) // a retry that succeeds clears the failure
	c.Done(repoB, nil)
	if len(c.Failed()) != 0 || !c.Complete() || c.Scanned() != 2 {
		t.Errorf("after retry: failed=%v complete=%v scanned=%d", c.Failed(), c.Complete(), c.Scanned())
	}

	c.Done(repoB, boom) // and a later failure overwrites a success
	if _, ok := c.Failed()[repoB]; !ok || c.Complete() || c.Scanned() != 2 {
		t.Errorf("after regression: failed=%v complete=%v scanned=%d", c.Failed(), c.Complete(), c.Scanned())
	}
}

func TestCoverageFailedIsACopy(t *testing.T) {
	c := core.NewCoverage([]domain.RepoRef{repoA, repoB})
	c.Done(repoA, errors.New("boom"))

	got := c.Failed()
	delete(got, repoA)
	got[repoB] = errors.New("injected")

	if failed := c.Failed(); len(failed) != 1 || failed[repoA] == nil {
		t.Errorf("mutating the returned map changed coverage: %v", failed)
	}
}

func TestCoverageIgnoresReposOutsideTheSet(t *testing.T) {
	c := core.NewCoverage([]domain.RepoRef{repoA, repoB})
	c.Done(repoA, nil)
	c.Done(repoC, errors.New("boom"))

	if c.Scanned() != 1 || c.Complete() || len(c.Failed()) != 0 {
		t.Errorf("outside repo leaked: scanned=%d complete=%v failed=%v", c.Scanned(), c.Complete(), c.Failed())
	}

	c.Done(repoC, nil)
	if c.Scanned() != 1 || c.Complete() {
		t.Errorf("outside repo counted toward completion: scanned=%d complete=%v", c.Scanned(), c.Complete())
	}
}

func TestCoverageDedupesInput(t *testing.T) {
	c := core.NewCoverage([]domain.RepoRef{repoA, repoA, repoB})
	if c.Total() != 2 {
		t.Fatalf("Total = %d, want 2", c.Total())
	}
	c.Done(repoA, nil)
	c.Done(repoB, nil)
	if !c.Complete() {
		t.Error("not Complete after scanning every distinct repo")
	}
}
