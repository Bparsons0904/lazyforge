package forgetest_test

import (
	"context"
	"errors"
	"io"
	"slices"
	"testing"
	"time"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge/forgetest"
)

var (
	_ forge.Forge     = (*forgetest.Fake)(nil)
	_ forge.Approver  = (*forgetest.Fake)(nil)
	_ forge.RunLister = (*forgetest.Fake)(nil)
	_ forge.LogReader = (*forgetest.Fake)(nil)
)

var (
	repoRef  = domain.RepoRef{Owner: "owner", Name: "name"}
	otherRef = domain.RepoRef{Owner: "owner", Name: "other"}
	t0       = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
)

const (
	openCR   = 1
	mergedCR = 2
	openIss  = 10
	closedIs = 11
	runID    = 100
	jobID    = 200
)

func seeded() *forgetest.Fake {
	f := forgetest.NewFake(forge.HostInfo{Kind: forge.KindForgejo, User: "bob", ChangeRequestTerm: "PR"})
	f.AddRepo(domain.Repo{RepoRef: repoRef, LastActivity: t0})
	f.AddRepo(domain.Repo{RepoRef: otherRef, LastActivity: t0.Add(time.Hour)})
	f.AddRepo(domain.Repo{RepoRef: domain.RepoRef{Owner: "owner", Name: "old"}, LastActivity: t0.Add(-time.Hour)})
	f.AddChangeRequest(repoRef, domain.ChangeRequest{Number: openCR, Title: "open", HeadSHA: "abc", State: domain.StateOpen, Labels: []string{"a"}})
	f.AddChangeRequest(repoRef, domain.ChangeRequest{Number: mergedCR, Title: "merged", HeadSHA: "def", State: domain.StateMerged})
	f.AddIssue(repoRef, domain.Issue{Number: openIss, Title: "issue", Body: "old body", State: domain.StateOpen, Labels: []string{"x"}})
	f.AddIssue(repoRef, domain.Issue{Number: closedIs, Title: "done", State: domain.StateClosed})
	f.AddRelease(repoRef, domain.Release{Tag: "v1"})
	f.AddRun(repoRef, domain.Run{ID: runID, Number: 1, Branch: "main"},
		[]domain.Job{{ID: jobID, RunID: runID, Name: "build"}}, map[int64]string{jobID: "log line\n"})
	return f
}

func TestContract(t *testing.T) {
	forgetest.RunContract(t, func(*testing.T) (forge.Forge, forgetest.Fixture) {
		return seeded(), forgetest.Fixture{
			Repo: repoRef, OpenCR: openCR, OpenCRHead: "abc", OpenIssue: openIss, Missing: 999,
		}
	})
}

func TestInfoAndGate(t *testing.T) {
	f := seeded()
	if got := f.Info(); got.Kind != forge.KindForgejo || got.User != "bob" || got.ChangeRequestTerm != "PR" {
		t.Errorf("Info() = %+v", got)
	}
	for _, a := range []forge.Action{forge.ActMerge, forge.ActLogs} {
		if err := f.Gate(a); err != nil {
			t.Errorf("default Gate(%d) = %v, want nil", a, err)
		}
	}
	want := errors.New("blocked")
	f.SetGate(forge.ActMerge, want)
	if err := f.Gate(forge.ActMerge); !errors.Is(err, want) {
		t.Errorf("Gate(merge) = %v, want %v", err, want)
	}
	if err := f.Gate(forge.ActClose); err != nil {
		t.Errorf("Gate(close) = %v, want nil", err)
	}
}

func TestListReposSorted(t *testing.T) {
	repos, err := seeded().ListRepos(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 3 {
		t.Fatalf("got %d repos, want 3", len(repos))
	}
	for i := 1; i < len(repos); i++ {
		if repos[i].LastActivity.After(repos[i-1].LastActivity) {
			t.Errorf("repos not sorted by LastActivity desc: %v", repos)
		}
	}
	if repos[0].RepoRef != otherRef {
		t.Errorf("first repo = %v, want %v", repos[0].RepoRef, otherRef)
	}
}

func TestListFiltering(t *testing.T) {
	f := seeded()
	ctx := context.Background()
	tests := []struct {
		state domain.State
		crs   []int
		iss   []int
	}{
		{domain.StateOpen, []int{openCR}, []int{openIss}},
		{domain.StateMerged, []int{mergedCR}, nil},
		{domain.StateClosed, nil, []int{closedIs}},
	}
	for _, tt := range tests {
		crs, err := f.ListChangeRequests(ctx, repoRef, forge.Filter{State: tt.state})
		if err != nil {
			t.Fatal(err)
		}
		if got := numbersCR(crs); !equalInts(got, tt.crs) {
			t.Errorf("CRs in state %d = %v, want %v", tt.state, got, tt.crs)
		}
		iss, err := f.ListIssues(ctx, repoRef, forge.Filter{State: tt.state})
		if err != nil {
			t.Fatal(err)
		}
		if got := numbersIssue(iss); !equalInts(got, tt.iss) {
			t.Errorf("issues in state %d = %v, want %v", tt.state, got, tt.iss)
		}
	}
}

func TestReleases(t *testing.T) {
	rels, err := seeded().ListReleases(context.Background(), repoRef)
	if err != nil {
		t.Fatal(err)
	}
	if len(rels) != 1 || rels[0].Tag != "v1" {
		t.Errorf("releases = %+v", rels)
	}
}

func TestNotFound(t *testing.T) {
	f := seeded()
	ctx := context.Background()
	missingCR := forge.ItemRef{Repo: repoRef, Kind: forge.ItemChangeRequest, Number: 999}
	missingIssue := forge.ItemRef{Repo: repoRef, Kind: forge.ItemIssue, Number: 999}
	calls := map[string]func() error{
		"GetChangeRequest": func() error { _, err := f.GetChangeRequest(ctx, repoRef, 999); return err },
		"Merge":            func() error { return f.Merge(ctx, repoRef, 999, forge.MergeOpts{HeadSHA: "abc"}) },
		"Approve":          func() error { return f.Approve(ctx, repoRef, 999) },
		"EditIssueBody":    func() error { return f.EditIssueBody(ctx, repoRef, 999, "x") },
		"Close issue":      func() error { return f.Close(ctx, missingIssue) },
		"Close CR":         func() error { return f.Close(ctx, missingCR) },
		"Comment":          func() error { return f.Comment(ctx, missingIssue, "x") },
		"ListComments":     func() error { _, err := f.ListComments(ctx, missingCR); return err },
		"ListJobs":         func() error { _, err := f.ListJobs(ctx, repoRef, 999); return err },
		"JobLog":           func() error { _, err := f.JobLog(ctx, repoRef, 999); return err },
		"unknown repo CRs": func() error {
			_, err := f.ListChangeRequests(ctx, domain.RepoRef{Owner: "no", Name: "pe"}, forge.Filter{})
			return err
		},
	}
	for name, call := range calls {
		if err := call(); !errors.Is(err, forge.ErrNotFound) {
			t.Errorf("%s: err = %v, want ErrNotFound", name, err)
		}
	}
}

func TestMerge(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		f := seeded()
		if err := f.Merge(ctx, repoRef, openCR, forge.MergeOpts{HeadSHA: "abc", Method: "squash"}); err != nil {
			t.Fatal(err)
		}
		assertCRState(ctx, t, f, domain.StateMerged)
		m := f.Mutations()
		if len(m) != 1 || m[0].Op != "merge" || m[0].Opts.HeadSHA != "abc" || m[0].Opts.Method != "squash" ||
			m[0].Item.Repo != repoRef || m[0].Item.Number != openCR {
			t.Errorf("mutations = %+v", m)
		}
	})

	t.Run("stale head leaves state alone", func(t *testing.T) {
		f := seeded()
		err := f.Merge(ctx, repoRef, openCR, forge.MergeOpts{HeadSHA: "stale"})
		if !errors.Is(err, forge.ErrHeadChanged) {
			t.Fatalf("err = %v, want ErrHeadChanged", err)
		}
		assertCRState(ctx, t, f, domain.StateOpen)
	})

	t.Run("empty head is an error", func(t *testing.T) {
		f := seeded()
		if err := f.Merge(ctx, repoRef, openCR, forge.MergeOpts{}); err == nil {
			t.Fatal("want error for empty HeadSHA")
		}
		assertCRState(ctx, t, f, domain.StateOpen)
	})

	t.Run("non-open CR", func(t *testing.T) {
		f := seeded()
		if err := f.Merge(ctx, repoRef, mergedCR, forge.MergeOpts{HeadSHA: "def"}); err == nil {
			t.Fatal("want error merging a merged CR")
		}
	})

	t.Run("second merge fails", func(t *testing.T) {
		f := seeded()
		opts := forge.MergeOpts{HeadSHA: "abc"}
		if err := f.Merge(ctx, repoRef, openCR, opts); err != nil {
			t.Fatal(err)
		}
		if err := f.Merge(ctx, repoRef, openCR, opts); err == nil {
			t.Fatal("want error on second merge")
		}
	})
}

func TestCloseCommentEdit(t *testing.T) {
	f := seeded()
	ctx := context.Background()
	issue := forge.ItemRef{Repo: repoRef, Kind: forge.ItemIssue, Number: openIss}
	cr := forge.ItemRef{Repo: repoRef, Kind: forge.ItemChangeRequest, Number: openCR}

	if err := f.Comment(ctx, issue, "hello"); err != nil {
		t.Fatal(err)
	}
	if err := f.Comment(ctx, cr, "on the cr"); err != nil {
		t.Fatal(err)
	}
	cs, err := f.ListComments(ctx, issue)
	if err != nil || len(cs) != 1 || cs[0].Body != "hello" || cs[0].Author != "bob" {
		t.Errorf("issue comments = %+v, err %v", cs, err)
	}
	cs, err = f.ListComments(ctx, cr)
	if err != nil || len(cs) != 1 || cs[0].Body != "on the cr" {
		t.Errorf("cr comments = %+v, err %v", cs, err)
	}

	if err := f.EditIssueBody(ctx, repoRef, openIss, "new body"); err != nil {
		t.Fatal(err)
	}
	iss, _ := f.ListIssues(ctx, repoRef, forge.Filter{State: domain.StateOpen})
	if len(iss) != 1 || iss[0].Body != "new body" {
		t.Errorf("issues after edit = %+v", iss)
	}

	if err := f.Close(ctx, issue); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(ctx, cr); err != nil {
		t.Fatal(err)
	}
	assertCRState(ctx, t, f, domain.StateClosed)
	closed, _ := f.ListIssues(ctx, repoRef, forge.Filter{State: domain.StateClosed})
	if !equalInts(numbersIssue(closed), []int{closedIs, openIss}) && !equalInts(numbersIssue(closed), []int{openIss, closedIs}) {
		t.Errorf("closed issues = %v", numbersIssue(closed))
	}

	if err := f.Approve(ctx, repoRef, openCR); err != nil {
		t.Fatal(err)
	}

	m := f.Mutations()
	wantOps := []string{"comment", "comment", "edit-issue-body", "close", "close", "approve"}
	if len(m) != len(wantOps) {
		t.Fatalf("mutations = %+v, want ops %v", m, wantOps)
	}
	for i, op := range wantOps {
		if m[i].Op != op {
			t.Errorf("mutation %d op = %q, want %q", i, m[i].Op, op)
		}
	}
	if m[0].Body != "hello" || m[0].Item != issue {
		t.Errorf("comment mutation = %+v", m[0])
	}
	if m[2].Body != "new body" || m[2].Item.Repo != repoRef || m[2].Item.Number != openIss {
		t.Errorf("edit mutation = %+v", m[2])
	}
	if m[4].Item != cr {
		t.Errorf("close CR mutation item = %+v, want %+v", m[4].Item, cr)
	}
	if m[5].Item.Repo != repoRef || m[5].Item.Number != openCR {
		t.Errorf("approve mutation item = %+v", m[5].Item)
	}
}

func TestReturnedSlicesAreCopies(t *testing.T) {
	f := seeded()
	ctx := context.Background()
	open := forge.Filter{State: domain.StateOpen}

	repos, _ := f.ListRepos(ctx)
	repos[0].Description = "mutated"
	repos[0] = domain.Repo{}
	if again, _ := f.ListRepos(ctx); again[0].RepoRef != otherRef || again[0].Description != "" {
		t.Error("ListRepos result aliases fake state")
	}

	crs, _ := f.ListChangeRequests(ctx, repoRef, open)
	crs[0].Title = "mutated"
	crs[0].Labels[0] = "mutated"
	if again, _ := f.ListChangeRequests(ctx, repoRef, open); again[0].Title != "open" || again[0].Labels[0] != "a" {
		t.Error("ListChangeRequests result aliases fake state")
	}

	iss, _ := f.ListIssues(ctx, repoRef, open)
	iss[0].Title = "mutated"
	iss[0].Labels[0] = "mutated"
	if again, _ := f.ListIssues(ctx, repoRef, open); again[0].Title != "issue" || again[0].Labels[0] != "x" {
		t.Error("ListIssues result aliases fake state")
	}

	issue := forge.ItemRef{Repo: repoRef, Kind: forge.ItemIssue, Number: openIss}
	_ = f.Comment(ctx, issue, "keep")
	cs, _ := f.ListComments(ctx, issue)
	cs[0].Body = "mutated"
	if again, _ := f.ListComments(ctx, issue); again[0].Body != "keep" {
		t.Error("ListComments result aliases fake state")
	}

	cr, _ := f.GetChangeRequest(ctx, repoRef, openCR)
	cr.Title = "mutated"
	cr.Labels[0] = "mutated"
	if again, _ := f.GetChangeRequest(ctx, repoRef, openCR); again.Title != "open" || again.Labels[0] != "a" {
		t.Error("GetChangeRequest result aliases fake state")
	}

	rels, _ := f.ListReleases(ctx, repoRef)
	rels[0].Tag = "mutated"
	if again, _ := f.ListReleases(ctx, repoRef); again[0].Tag != "v1" {
		t.Error("ListReleases result aliases fake state")
	}

	jobs, _ := f.ListJobs(ctx, repoRef, runID)
	jobs[0].Name = "mutated"
	if again, _ := f.ListJobs(ctx, repoRef, runID); again[0].Name != "build" {
		t.Error("ListJobs result aliases fake state")
	}

	runs, _ := f.ListRuns(ctx, repoRef, forge.RunFilter{})
	runs[0].Branch = "mutated"
	if again, _ := f.ListRuns(ctx, repoRef, forge.RunFilter{}); again[0].Branch != "main" {
		t.Error("ListRuns result aliases fake state")
	}
}

func TestContextCancelled(t *testing.T) {
	f := seeded()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	issue := forge.ItemRef{Repo: repoRef, Kind: forge.ItemIssue, Number: openIss}
	calls := map[string]func() error{
		"ListRepos":          func() error { _, err := f.ListRepos(ctx); return err },
		"ListChangeRequests": func() error { _, err := f.ListChangeRequests(ctx, repoRef, forge.Filter{}); return err },
		"GetChangeRequest":   func() error { _, err := f.GetChangeRequest(ctx, repoRef, openCR); return err },
		"Merge":              func() error { return f.Merge(ctx, repoRef, openCR, forge.MergeOpts{HeadSHA: "abc"}) },
		"ListIssues":         func() error { _, err := f.ListIssues(ctx, repoRef, forge.Filter{}); return err },
		"EditIssueBody":      func() error { return f.EditIssueBody(ctx, repoRef, openIss, "x") },
		"ListComments":       func() error { _, err := f.ListComments(ctx, issue); return err },
		"Comment":            func() error { return f.Comment(ctx, issue, "x") },
		"Close":              func() error { return f.Close(ctx, issue) },
		"ListReleases":       func() error { _, err := f.ListReleases(ctx, repoRef); return err },
		"Approve":            func() error { return f.Approve(ctx, repoRef, openCR) },
		"ListRuns":           func() error { _, err := f.ListRuns(ctx, repoRef, forge.RunFilter{}); return err },
		"ListJobs":           func() error { _, err := f.ListJobs(ctx, repoRef, runID); return err },
		"JobLog":             func() error { _, err := f.JobLog(ctx, repoRef, jobID); return err },
	}
	for name, call := range calls {
		if err := call(); !errors.Is(err, context.Canceled) {
			t.Errorf("%s: err = %v, want context.Canceled", name, err)
		}
	}
	if len(f.Mutations()) != 0 {
		t.Errorf("cancelled calls recorded mutations: %+v", f.Mutations())
	}
}

func TestFailNext(t *testing.T) {
	f := seeded()
	ctx := context.Background()
	boom := errors.New("boom")

	f.FailNext(boom)
	if _, err := f.ListRepos(ctx); !errors.Is(err, boom) {
		t.Fatalf("first call err = %v, want boom", err)
	}
	if _, err := f.ListRepos(ctx); err != nil {
		t.Fatalf("second call err = %v, want nil (FailNext clears)", err)
	}

	f.FailNext(boom)
	if err := f.Merge(ctx, repoRef, openCR, forge.MergeOpts{HeadSHA: "abc"}); !errors.Is(err, boom) {
		t.Fatalf("merge err = %v, want boom", err)
	}
	assertCRState(ctx, t, f, domain.StateOpen)
	if len(f.Mutations()) != 0 {
		t.Errorf("failed merge recorded mutations: %+v", f.Mutations())
	}
}

func TestRunsJobsLogs(t *testing.T) {
	f := seeded()
	ctx := context.Background()

	runs, err := f.ListRuns(ctx, repoRef, forge.RunFilter{})
	if err != nil || len(runs) != 1 || runs[0].ID != runID {
		t.Fatalf("runs = %+v, err %v", runs, err)
	}
	jobs, err := f.ListJobs(ctx, repoRef, runID)
	if err != nil || len(jobs) != 1 || jobs[0].ID != jobID {
		t.Fatalf("jobs = %+v, err %v", jobs, err)
	}
	rc, err := f.JobLog(ctx, repoRef, jobID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rc.Close() }()
	b, err := io.ReadAll(rc)
	if err != nil || string(b) != "log line\n" {
		t.Errorf("log = %q, err %v", b, err)
	}
}

func TestListRunsFilter(t *testing.T) {
	f := seeded()
	f.AddRun(repoRef, domain.Run{ID: 101, Number: 2, Branch: "dev", Commit: "sha2"}, nil, nil)
	ctx := context.Background()
	tests := []struct {
		name string
		flt  forge.RunFilter
		want []int64
	}{
		{"none", forge.RunFilter{}, []int64{runID, 101}},
		{"branch", forge.RunFilter{Branch: "dev"}, []int64{101}},
		{"head sha", forge.RunFilter{HeadSHA: "sha2"}, []int64{101}},
		{"branch and sha disagree", forge.RunFilter{Branch: "main", HeadSHA: "sha2"}, nil},
	}
	for _, tt := range tests {
		runs, err := f.ListRuns(ctx, repoRef, tt.flt)
		if err != nil {
			t.Fatal(err)
		}
		var got []int64
		for _, r := range runs {
			got = append(got, r.ID)
		}
		if !slices.Equal(got, tt.want) {
			t.Errorf("%s: runs = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func assertCRState(ctx context.Context, t *testing.T, f *forgetest.Fake, want domain.State) {
	t.Helper()
	cr, err := f.GetChangeRequest(ctx, repoRef, openCR)
	if err != nil {
		t.Fatal(err)
	}
	if cr.State != want {
		t.Errorf("CR #%d state = %d, want %d", openCR, cr.State, want)
	}
}

func numbersCR(crs []domain.ChangeRequest) []int {
	var out []int
	for _, c := range crs {
		out = append(out, c.Number)
	}
	return out
}

func numbersIssue(iss []domain.Issue) []int {
	var out []int
	for _, i := range iss {
		out = append(out, i.Number)
	}
	return out
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
