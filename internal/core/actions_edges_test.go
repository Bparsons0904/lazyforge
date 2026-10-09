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
)

// S1-8: dedup keys on (repo, number), so the same number in another repo is a separate target.
func TestMergeDedupKeepsSameNumberInOtherRepo(t *testing.T) {
	f := &mergeHook{Fake: crFake(1, domain.CIPass)}
	f.AddChangeRequest(repoB, domain.ChangeRequest{Number: 1, State: domain.StateOpen, HeadSHA: "shaB", CI: domain.CIPass})
	s := core.New(f, core.Options{})
	a := core.Target{Repo: repoA, CR: domain.ChangeRequest{Number: 1, HeadSHA: "sha1"}}
	b := core.Target{Repo: repoB, CR: domain.ChangeRequest{Number: 1, HeadSHA: "shaB"}}
	res := s.Merge(context.Background(), []core.Target{a, b, a})
	if len(res) != 2 || res[0].Target.Repo != repoA || res[1].Target.Repo != repoB {
		t.Fatalf("results %+v, want repoA then repoB", res)
	}
	for _, r := range res {
		if r.Outcome != core.OutcomeMerged {
			t.Errorf("%v: %v %v, want merged", r.Target.Repo, r.Outcome, r.Err)
		}
	}
	if n := f.calls.Load(); n != 2 {
		t.Errorf("merge calls = %d, want 2", n)
	}
}

// S1-3: a real Fake refusal (the CR is no longer open) is a refusal that keeps the forge's reason.
func TestMergeRefusedByFakeKeepsReason(t *testing.T) {
	f := crFake(1, domain.CIPass)
	s := core.New(f, core.Options{})
	ts := targets(t, s)
	if err := f.Merge(context.Background(), repoA, 1, forge.MergeOpts{HeadSHA: "sha1"}); err != nil {
		t.Fatal(err)
	}
	res := s.Merge(context.Background(), ts)
	if res[0].Outcome != core.OutcomeRefused || !errors.Is(res[0].Err, forge.ErrRefused) {
		t.Fatalf("got %v %v, want refused", res[0].Outcome, res[0].Err)
	}
	if !strings.Contains(res[0].Err.Error(), "not open") {
		t.Errorf("reason %q lost the forge message", res[0].Err)
	}
}

// S1-4: the gate is per repo, so one repo's setting doesn't refuse another's failing-CI target.
func TestMergeCIGateIsPerRepo(t *testing.T) {
	f := crFake(1, domain.CIFail)
	f.AddChangeRequest(repoB, domain.ChangeRequest{Number: 1, State: domain.StateOpen, HeadSHA: "shaB", CI: domain.CIFail})
	s := core.New(f, core.Options{RequireGreenCI: func(r domain.RepoRef) bool { return r == repoA }})
	ts := []core.Target{
		{Repo: repoA, CR: domain.ChangeRequest{Number: 1, HeadSHA: "sha1", CI: domain.CIFail}},
		{Repo: repoB, CR: domain.ChangeRequest{Number: 1, HeadSHA: "shaB", CI: domain.CIFail}},
	}
	res := s.Merge(context.Background(), ts)
	if res[0].Outcome != core.OutcomeRefused || res[1].Outcome != core.OutcomeMerged {
		t.Errorf("outcomes %v, %v; want refused, merged", res[0].Outcome, res[1].Outcome)
	}
}

// S1-11: a fetch error lands in Checked.Err and leaves the cached CR untouched.
func TestRecheckErrorKeepsCache(t *testing.T) {
	f := crFake(2, domain.CIPass)
	s := core.New(f, core.Options{})
	ts := targets(t, s)
	ts[0].CR.Number = 404
	got := s.Recheck(context.Background(), ts[:1])
	if !errors.Is(got[0].Err, forge.ErrNotFound) {
		t.Fatalf("Err = %v, want ErrNotFound", got[0].Err)
	}
	if ns := cachedNumbers(s); fmt.Sprint(ns) != "[1 2]" {
		t.Errorf("cached %v, want [1 2]", ns)
	}
}

// S1-12: a failed mutation leaves the cache alone.
func TestFailedMutationsKeepCache(t *testing.T) {
	ctx := context.Background()
	boom := errors.New("500: boom")
	crRef := forge.ItemRef{Repo: repoA, Kind: forge.ItemChangeRequest, Number: 1}
	isRef := forge.ItemRef{Repo: repoA, Kind: forge.ItemIssue, Number: 10}
	calls := map[string]func(*core.Service) error{
		"close CR":      func(s *core.Service) error { return s.Close(ctx, crRef) },
		"close issue":   func(s *core.Service) error { return s.Close(ctx, isRef) },
		"comment issue": func(s *core.Service) error { return s.Comment(ctx, isRef, "x") },
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			f := crFake(1, domain.CIPass)
			s := core.New(f, core.Options{})
			if _, err := s.ChangeRequests(ctx, repoA); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Issues(ctx, repoA); err != nil {
				t.Fatal(err)
			}
			f.FailNext(boom)
			if err := call(s); !errors.Is(err, boom) {
				t.Fatalf("err = %v, want boom", err)
			}
			if ns := cachedNumbers(s); fmt.Sprint(ns) != "[1]" {
				t.Errorf("cached CRs %v, want [1]", ns)
			}
			if is, _, _ := s.PeekIssues(repoA); len(is) != 1 || is[0].Comments != 0 {
				t.Errorf("cached issues %+v, want one untouched", is)
			}
		})
	}
}
