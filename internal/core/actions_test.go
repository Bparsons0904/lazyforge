package core_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/core"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge/forgetest"
)

// mergeHook wraps the Fake so a test can block, fail or count merges; hook runs before the Fake's Merge.
type mergeHook struct {
	*forgetest.Fake
	hook  func(n int) error // non-nil error is returned instead of merging
	after error             // returned after the Fake merged, to simulate apply-then-time-out
	calls atomic.Int32

	mu        sync.Mutex
	cur, peak int
}

func (m *mergeHook) Merge(ctx context.Context, r domain.RepoRef, n int, opts forge.MergeOpts) error {
	m.calls.Add(1)
	m.mu.Lock()
	m.cur++
	m.peak = max(m.peak, m.cur)
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		m.cur--
		m.mu.Unlock()
	}()
	if m.hook != nil {
		if err := m.hook(n); err != nil {
			return err
		}
	}
	if err := m.Fake.Merge(ctx, r, n, opts); err != nil {
		return err
	}
	return m.after
}

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

func crFake(n int, ci domain.CIState) *forgetest.Fake {
	f := forgetest.NewFake(forge.HostInfo{Kind: forge.KindForgejo, User: "bob"})
	for i := 1; i <= n; i++ {
		f.AddChangeRequest(repoA, domain.ChangeRequest{Number: i, State: domain.StateOpen, HeadSHA: fmt.Sprint("sha", i), CI: ci})
	}
	f.AddIssue(repoA, domain.Issue{Number: 10, State: domain.StateOpen})
	return f
}

func targets(t *testing.T, s *core.Service) []core.Target {
	t.Helper()
	crs, err := s.ChangeRequests(context.Background(), repoA)
	if err != nil {
		t.Fatal(err)
	}
	var ts []core.Target
	for _, cr := range crs {
		ts = append(ts, core.Target{Repo: repoA, CR: cr})
	}
	return ts
}

func cachedNumbers(s *core.Service) []int {
	crs, _, _ := s.PeekChangeRequests(repoA)
	var ns []int
	for _, cr := range crs {
		ns = append(ns, cr.Number)
	}
	return ns
}

func TestMergeClassification(t *testing.T) {
	tests := []struct {
		name    string
		hook    error
		after   error
		stale   bool
		want    core.Outcome
		wantMsg string // substring Err must keep
		wantIs  error
		cached  bool
		applied bool
	}{
		{name: "merged", want: core.OutcomeMerged},
		{name: "head changed", stale: true, want: core.OutcomeRefused, wantIs: forge.ErrHeadChanged, cached: true},
		{name: "forge refused", hook: fmt.Errorf("POST x: %w: merge conflict", forge.ErrRefused), want: core.OutcomeRefused, wantIs: forge.ErrRefused, wantMsg: "merge conflict", cached: true},
		{name: "server error", hook: errors.New("POST x: 500: boom"), want: core.OutcomeFailed, cached: true},
		{name: "deadline", hook: context.DeadlineExceeded, want: core.OutcomeUnknown, cached: true},
		{name: "net timeout after apply", after: fmt.Errorf("POST x: %w", timeoutErr{}), want: core.OutcomeUnknown, cached: true, applied: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &mergeHook{Fake: crFake(1, domain.CIPass), hook: func(int) error { return tt.hook }, after: tt.after}
			s := core.New(f, core.Options{})
			ts := targets(t, s)
			if tt.stale {
				ts[0].CR.HeadSHA = "old"
			}
			res := s.Merge(context.Background(), ts)
			if len(res) != 1 || res[0].Outcome != tt.want {
				t.Fatalf("results %+v, want one %v", res, tt.want)
			}
			if (res[0].Err == nil) != (tt.want == core.OutcomeMerged) {
				t.Errorf("Err = %v", res[0].Err)
			}
			if tt.wantIs != nil && !errors.Is(res[0].Err, tt.wantIs) {
				t.Errorf("Err = %v, want %v", res[0].Err, tt.wantIs)
			}
			if tt.wantMsg != "" && !strings.Contains(res[0].Err.Error(), tt.wantMsg) {
				t.Errorf("reason %q lost the forge message", res[0].Err)
			}
			if got := len(cachedNumbers(s)) == 1; got != tt.cached {
				t.Errorf("cached = %v, want %v", got, tt.cached)
			}
			muts := f.Mutations()
			if wantMut := tt.want == core.OutcomeMerged || tt.applied; (len(muts) == 1) != wantMut {
				t.Fatalf("mutations %+v", muts)
			}
			if len(muts) == 1 && (muts[0].Opts.HeadSHA != "sha1" || muts[0].Opts.Method != "") {
				t.Errorf("opts %+v", muts[0].Opts)
			}
			if tt.applied {
				got := s.Recheck(context.Background(), ts)
				if got[0].Err != nil || got[0].Target.CR.State != domain.StateMerged {
					t.Errorf("recheck %+v, want merged", got[0])
				}
			}
		})
	}
}

func TestMergeCIGate(t *testing.T) {
	on := func(domain.RepoRef) bool { return true }
	off := func(domain.RepoRef) bool { return false }
	tests := []struct {
		ci   domain.CIState
		gate func(domain.RepoRef) bool
		want core.Outcome
	}{
		{domain.CIFail, on, core.OutcomeRefused},
		{domain.CIPending, on, core.OutcomeRefused},
		{domain.CIRunning, on, core.OutcomeRefused},
		{domain.CIPass, on, core.OutcomeMerged},
		{domain.CINone, on, core.OutcomeMerged},
		{domain.CISkipped, on, core.OutcomeMerged},
		{domain.CIFail, nil, core.OutcomeMerged},
		{domain.CIFail, off, core.OutcomeMerged},
	}
	for _, tt := range tests {
		f := crFake(1, tt.ci)
		s := core.New(f, core.Options{RequireGreenCI: tt.gate})
		res := s.Merge(context.Background(), targets(t, s))
		if res[0].Outcome != tt.want {
			t.Errorf("ci %v gate %v: got %v, want %v", tt.ci, tt.gate != nil, res[0].Outcome, tt.want)
		}
		if tt.want == core.OutcomeRefused {
			if !strings.Contains(res[0].Err.Error(), "require_green_ci") {
				t.Errorf("reason %q doesn't name the setting", res[0].Err)
			}
			if len(f.Mutations()) != 0 {
				t.Error("gate let the merge reach the forge")
			}
		}
	}
}

func TestMergeCarriesOnAndDedups(t *testing.T) {
	f := &mergeHook{Fake: crFake(5, domain.CIPass), hook: func(n int) error {
		if n == 2 || n == 4 {
			return errors.New("boom")
		}
		return nil
	}}
	s := core.New(f, core.Options{})
	ts := targets(t, s)
	ts = append(ts, ts[0], ts[2])
	res := s.Merge(context.Background(), ts)
	if len(res) != 5 {
		t.Fatalf("got %d results, want 5", len(res))
	}
	for i, r := range res {
		want := core.OutcomeMerged
		if i == 1 || i == 3 {
			want = core.OutcomeFailed
		}
		if r.Target.CR.Number != i+1 || r.Outcome != want {
			t.Errorf("res[%d] = #%d %v, want #%d %v", i, r.Target.CR.Number, r.Outcome, i+1, want)
		}
	}
	if n := f.calls.Load(); n != 5 {
		t.Errorf("merge calls = %d, want 5", n)
	}
}

func TestMergeCancel(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{})
	var once sync.Once
	f := &mergeHook{Fake: crFake(3, domain.CIPass), hook: func(int) error {
		once.Do(func() { close(started) })
		<-release
		return nil
	}}
	s := core.New(f, core.Options{MaxConcurrent: 1})
	ts := targets(t, s)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan []core.MergeResult)
	go func() { done <- s.Merge(ctx, ts) }()
	<-started
	cancel()
	close(release)
	res := <-done
	want := []core.Outcome{core.OutcomeMerged, core.OutcomeNotStarted, core.OutcomeNotStarted}
	for i, r := range res {
		if r.Outcome != want[i] {
			t.Errorf("res[%d] = %v, want %v", i, r.Outcome, want[i])
		}
	}
	if n := f.calls.Load(); n != 1 {
		t.Errorf("merge calls = %d, want 1", n)
	}

	f2 := crFake(2, domain.CIPass)
	s2 := core.New(f2, core.Options{})
	ctx2, cancel2 := context.WithCancel(context.Background())
	cancel2()
	for _, r := range s2.Merge(ctx2, targets(t, s2)) {
		if r.Outcome != core.OutcomeNotStarted || r.Err == nil {
			t.Errorf("got %v %v, want not started", r.Outcome, r.Err)
		}
	}
	if len(f2.Mutations()) != 0 {
		t.Error("cancelled merge reached the forge")
	}
}

func TestMergeBound(t *testing.T) {
	const bound = 2
	entered, release := make(chan int, 8), make(chan struct{})
	f := &mergeHook{Fake: crFake(8, domain.CIPass), hook: func(n int) error {
		entered <- n
		<-release
		return nil
	}}
	s := core.New(f, core.Options{MaxConcurrent: bound})
	ts := targets(t, s)
	done := make(chan []core.MergeResult, 1)
	var once sync.Once
	releaseAll := func() { once.Do(func() { close(release) }) }
	t.Cleanup(releaseAll)
	go func() { done <- s.Merge(context.Background(), ts) }()
	// Merges stay blocked until released, so reaching the bound is observed, not raced.
	for range bound {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("bound never reached")
		}
	}
	f.mu.Lock()
	cur := f.cur
	f.mu.Unlock()
	if cur != bound {
		t.Errorf("in flight = %d, want %d", cur, bound)
	}
	releaseAll()
	<-done
	if f.peak != bound {
		t.Errorf("peak in flight = %d, want %d", f.peak, bound)
	}
}

func TestRecheck(t *testing.T) {
	f := crFake(3, domain.CIPending)
	s := core.New(f, core.Options{})
	ts := targets(t, s)
	if err := f.Merge(context.Background(), repoA, 2, forge.MergeOpts{HeadSHA: "sha2"}); err != nil {
		t.Fatal(err)
	}
	ts = append(ts, core.Target{Repo: repoA, CR: domain.ChangeRequest{Number: 404}})
	got := s.Recheck(context.Background(), ts)
	if got[0].Err != nil || got[0].Target.CR.HeadSHA != "sha1" || got[0].Target.CR.CI != domain.CIPending {
		t.Errorf("got[0] = %+v", got[0])
	}
	if got[1].Target.CR.State != domain.StateMerged {
		t.Errorf("got[1] state %v, want merged", got[1].Target.CR.State)
	}
	if !errors.Is(got[3].Err, forge.ErrNotFound) {
		t.Errorf("got[3].Err = %v, want ErrNotFound", got[3].Err)
	}
	if ns := cachedNumbers(s); fmt.Sprint(ns) != "[1 3]" {
		t.Errorf("cached %v, want [1 3]", ns)
	}
}

func TestCloseCommentApprove(t *testing.T) {
	ctx := context.Background()
	f := crFake(2, domain.CIPass)
	s := core.New(f, core.Options{})
	targets(t, s)
	if _, err := s.Issues(ctx, repoA); err != nil {
		t.Fatal(err)
	}
	crRef := forge.ItemRef{Repo: repoA, Kind: forge.ItemChangeRequest, Number: 1}
	isRef := forge.ItemRef{Repo: repoA, Kind: forge.ItemIssue, Number: 10}
	if err := s.Comment(ctx, isRef, "hi"); err != nil {
		t.Fatal(err)
	}
	if is, _, _ := s.PeekIssues(repoA); is[0].Comments != 1 {
		t.Errorf("cached comments = %d, want 1", is[0].Comments)
	}
	if err := s.Close(ctx, crRef); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(ctx, isRef); err != nil {
		t.Fatal(err)
	}
	if ns := cachedNumbers(s); fmt.Sprint(ns) != "[2]" {
		t.Errorf("cached CRs %v, want [2]", ns)
	}
	if is, _, _ := s.PeekIssues(repoA); len(is) != 0 {
		t.Errorf("cached issues %v, want none", is)
	}
	if err := s.Approve(ctx, repoA, 2); err != nil {
		t.Fatal(err)
	}
	var ops []string
	for _, m := range f.Mutations() {
		ops = append(ops, fmt.Sprintf("%s/%v", m.Op, m.Item.Kind))
	}
	want := []string{
		fmt.Sprintf("comment/%v", forge.ItemIssue), fmt.Sprintf("close/%v", forge.ItemChangeRequest),
		fmt.Sprintf("close/%v", forge.ItemIssue), fmt.Sprintf("approve/%v", forge.ItemChangeRequest),
	}
	if !slices.Equal(ops, want) {
		t.Errorf("mutations %v, want %v", ops, want)
	}

	noApprove := struct{ forge.Forge }{f}
	if err := core.New(noApprove, core.Options{}).Approve(ctx, repoA, 2); !errors.Is(err, forge.ErrUnsupported) {
		t.Errorf("Approve err = %v, want ErrUnsupported", err)
	}
}

func TestRequiresGreenCI(t *testing.T) {
	s := core.New(crFake(0, 0), core.Options{RequireGreenCI: func(r domain.RepoRef) bool { return r == repoA }})
	if !s.RequiresGreenCI(repoA) || s.RequiresGreenCI(repoB) {
		t.Error("RequiresGreenCI doesn't follow the option")
	}
	if core.New(crFake(0, 0), core.Options{}).RequiresGreenCI(repoA) {
		t.Error("nil option should mean off")
	}
}
