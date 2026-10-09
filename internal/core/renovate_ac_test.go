package core_test

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/core"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/core/renovate"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge/forgetest"
)

const (
	pgBody    = "This PR contains the following updates:\n\n| Package | Update | Change |\n|---|---|---|\n| [postgres](https://x) | major | `16` → `17` |\n"
	pgBodyNew = "This PR contains the following updates:\n\n| Package | Update | Change |\n|---|---|---|\n| [postgres](https://x) | major | `16` → `18` |\n"
	badBody   = "This PR contains the following updates:\n\n| Package | Update | Change |\n|---|---|---|\n| [postgres](https://x) | major | 16 to 17 |\n"
)

type failIssues struct{ *forgetest.Fake }

var errIssues = errors.New("issues down")

func (failIssues) ListIssues(context.Context, domain.RepoRef, forge.Filter) ([]domain.Issue, error) {
	return nil, errIssues
}

// bodySwap serves a different body for GetChangeRequest, as if the bot rewrote the PR after the list was cached.
type bodySwap struct {
	*forgetest.Fake
	body string
}

func (b bodySwap) GetChangeRequest(ctx context.Context, r domain.RepoRef, n int) (domain.ChangeRequest, error) {
	cr, err := b.Fake.GetChangeRequest(ctx, r, n)
	cr.Body = b.body
	return cr, err
}

func newFake(crs ...domain.ChangeRequest) *forgetest.Fake {
	f := forgetest.NewFake(forge.HostInfo{Kind: forge.KindForgejo, ChangeRequestTerm: "PR"})
	f.AddRepo(domain.Repo{RepoRef: repoA, LastActivity: t0})
	for _, cr := range crs {
		cr.State = domain.StateOpen
		f.AddChangeRequest(repoA, cr)
	}
	return f
}

func TestServiceRenovateUser(t *testing.T) {
	if got := core.New(newFake(), core.Options{RenovateUser: "bot"}).RenovateUser(); got != "bot" {
		t.Errorf("RenovateUser = %q, want bot", got)
	}
	if got := core.New(newFake(), core.Options{}).RenovateUser(); got != "" {
		t.Errorf("RenovateUser = %q, want empty", got)
	}
}

func TestChangeRequestsRenovateDetection(t *testing.T) {
	cases := []struct {
		name   string
		user   string
		author string
		branch string
		want   bool
	}{
		{"author matches, any branch", "bot", "bot", "feature/x", true},
		{"author match ignores case", "Bot", "bOT", "feature/x", true},
		{"renovate branch, other author", "bot", "human", "renovate/pg", true},
		{"empty user, renovate branch", "", "human", "renovate/pg", true},
		{"empty user, author named renovate, plain branch", "", "renovate", "main", false},
		{"other author, plain branch", "bot", "human", "main", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFake(domain.ChangeRequest{Number: 1, Author: c.author, SourceBranch: c.branch, Title: "postgres docker tag", Body: pgBody})
			svc := core.New(f, core.Options{RenovateUser: c.user})
			crs, err := svc.ChangeRequests(context.Background(), repoA)
			if err != nil {
				t.Fatal(err)
			}
			if got := crs[0].Renovate != nil; got != c.want {
				t.Errorf("Renovate filled = %v, want %v", got, c.want)
			}
			scan, err := svc.RenovateScan(context.Background(), domain.Repo{RepoRef: repoA})
			if err != nil {
				t.Fatal(err)
			}
			if got := len(scan.PRs) == 1; got != c.want {
				t.Errorf("in scan = %v, want %v", got, c.want)
			}
		})
	}
}

func TestChangeRequestsRejectedBodyIsNil(t *testing.T) {
	f := newFake(
		domain.ChangeRequest{Number: 1, Author: "bot", Body: badBody},
		domain.ChangeRequest{Number: 2, Author: "bot", Body: pgBody, Title: "postgres docker tag"},
	)
	crs, err := core.New(f, core.Options{RenovateUser: "bot"}).ChangeRequests(context.Background(), repoA)
	if err != nil {
		t.Fatal(err)
	}
	if crs[0].Renovate != nil {
		t.Errorf("rejected body filled: %+v", crs[0].Renovate)
	}
	if len(crs[1].Renovate) != 1 {
		t.Errorf("good body not filled: %+v", crs[1].Renovate)
	}
}

func TestChangeRequestsLeavesNonRenovateUntouched(t *testing.T) {
	seed := domain.ChangeRequest{Number: 3, Author: "human", SourceBranch: "feature/x", Body: pgBody, Title: "mine"}
	f := newFake(seed)
	crs, err := core.New(f, core.Options{RenovateUser: "bot"}).ChangeRequests(context.Background(), repoA)
	if err != nil {
		t.Fatal(err)
	}
	if crs[0].Renovate != nil || crs[0].Body != pgBody || crs[0].Title != "mine" {
		t.Errorf("non-Renovate PR modified: %+v", crs[0])
	}
}

func TestRecheckReparsesAtFreshHead(t *testing.T) {
	f := newFake(domain.ChangeRequest{Number: 1, Author: "bot", Title: "postgres docker tag", Body: pgBody})
	svc := core.New(bodySwap{Fake: f, body: pgBodyNew}, core.Options{RenovateUser: "bot"})
	if _, err := svc.ChangeRequests(context.Background(), repoA); err != nil {
		t.Fatal(err)
	}
	got := svc.Recheck(context.Background(), []core.Target{{Repo: repoA, CR: domain.ChangeRequest{Number: 1}}})
	if got[0].Err != nil {
		t.Fatal(got[0].Err)
	}
	if u := got[0].Target.CR.Renovate; len(u) != 1 || u[0].To != "18" {
		t.Errorf("recheck Renovate = %+v, want postgres → 18", u)
	}
	cached, _, _ := svc.PeekChangeRequests(repoA)
	if u := cached[0].Renovate; len(u) != 1 || u[0].To != "18" {
		t.Errorf("cache Renovate = %+v, want the re-parsed value", u)
	}
}

func TestRecheckRejectedBodyBecomesNil(t *testing.T) {
	f := newFake(domain.ChangeRequest{Number: 1, Author: "bot", Title: "postgres docker tag", Body: pgBody})
	svc := core.New(bodySwap{Fake: f, body: badBody}, core.Options{RenovateUser: "bot"})
	if _, err := svc.ChangeRequests(context.Background(), repoA); err != nil {
		t.Fatal(err)
	}
	got := svc.Recheck(context.Background(), []core.Target{{Repo: repoA, CR: domain.ChangeRequest{Number: 1}}})
	if got[0].Target.CR.Renovate != nil {
		t.Errorf("Renovate = %+v, want nil after the body became unreadable", got[0].Target.CR.Renovate)
	}
}

func TestRecheckLeavesNonRenovateNil(t *testing.T) {
	f := newFake(domain.ChangeRequest{Number: 1, Author: "human", Body: pgBody})
	got := core.New(f, core.Options{RenovateUser: "bot"}).Recheck(context.Background(), []core.Target{{Repo: repoA, CR: domain.ChangeRequest{Number: 1}}})
	if got[0].Target.CR.Renovate != nil {
		t.Errorf("non-Renovate PR got Renovate: %+v", got[0].Target.CR.Renovate)
	}
}

func TestRenovateScanWrapsIssueFetchError(t *testing.T) {
	svc := core.New(failIssues{newFake()}, core.Options{})
	_, err := svc.RenovateScan(context.Background(), domain.Repo{RepoRef: repoA})
	if !errors.Is(err, errIssues) {
		t.Fatalf("err = %v, want it to wrap the issue error", err)
	}
	if !strings.Contains(err.Error(), repoA.String()) {
		t.Errorf("err = %q, want the repo named", err)
	}
}

func TestRenovateScanWrapsChangeRequestErrorKeepsCause(t *testing.T) {
	f := newFake()
	svc := core.New(failRepo{Fake: f, bad: repoA}, core.Options{})
	_, err := svc.RenovateScan(context.Background(), domain.Repo{RepoRef: repoA})
	if err == nil || !strings.Contains(err.Error(), repoA.String()) || !strings.Contains(err.Error(), "boom") {
		t.Errorf("err = %v, want repo and cause", err)
	}
}

func TestRenovateScanCarriesRepoAndIssuesRespectOpenOnly(t *testing.T) {
	f := newFake()
	f.AddIssue(repoA, domain.Issue{Number: 1, State: domain.StateOpen, Title: "dependency dashboard", Body: "## S\n- [ ] a <!-- x-branch=renovate/a -->\n"})
	f.AddIssue(repoA, domain.Issue{Number: 2, State: domain.StateClosed, Title: "Dependency Dashboard"})
	repo := domain.Repo{RepoRef: repoA, WebURL: "https://x/a"}
	scan, err := core.New(f, core.Options{}).RenovateScan(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if scan.Repo != repo {
		t.Errorf("Repo = %+v, want the full repo passed in", scan.Repo)
	}
	if len(scan.Dashboards) != 1 || scan.Dashboards[0].Issue.Number != 1 || scan.Dashboards[0].Repo != repoA {
		t.Errorf("dashboards = %+v, want only open #1", scan.Dashboards)
	}
	if e := scan.Dashboards[0].Entries; len(e) != 1 || e[0].Branch != "renovate/a" {
		t.Errorf("entries = %+v", e)
	}
}

func TestPeekRenovateScanNeedsBothLists(t *testing.T) {
	f := newFake(domain.ChangeRequest{Number: 1, Author: "bot"})
	svc := core.New(f, core.Options{RenovateUser: "bot"})
	repo := domain.Repo{RepoRef: repoA}
	ctx := context.Background()
	if _, ok := svc.PeekRenovateScan(repo); ok {
		t.Fatal("ok with nothing cached")
	}
	if _, err := svc.ChangeRequests(ctx, repoA); err != nil {
		t.Fatal(err)
	}
	if _, ok := svc.PeekRenovateScan(repo); ok {
		t.Fatal("ok with only change requests cached")
	}
	other := core.New(f, core.Options{})
	if _, err := other.Issues(ctx, repoA); err != nil {
		t.Fatal(err)
	}
	if _, ok := other.PeekRenovateScan(repo); ok {
		t.Fatal("ok with only issues cached")
	}
	if _, err := svc.Issues(ctx, repoA); err != nil {
		t.Fatal(err)
	}
	scan, ok := svc.PeekRenovateScan(repo)
	if !ok || len(scan.PRs) != 1 {
		t.Errorf("Peek = %+v ok=%v, want the cached scan", scan, ok)
	}
}

func TestDemoPostgresPRsGroupTogether(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	svc := core.New(forgetest.NewDemo(now), core.Options{})
	ctx := context.Background()
	repos, err := svc.Repos(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var scans []renovate.RepoScan
	for _, r := range repos {
		s, err := svc.RenovateScan(ctx, r)
		if err != nil {
			t.Fatal(err)
		}
		scans = append(scans, s)
	}
	view := renovate.Build(scans, now)
	var pg *renovate.Group
	for i, g := range view.Groups {
		if g.Key.Package == "postgres" {
			if pg != nil {
				t.Fatal("postgres appears in more than one group")
			}
			pg = &view.Groups[i]
		}
	}
	if pg == nil || pg.Key.To != "17.0" || len(pg.Members) != 2 {
		t.Fatalf("postgres group = %+v", pg)
	}
	var got []string
	for _, m := range pg.Members {
		got = append(got, m.Repo.String()+"#"+strconv.Itoa(m.CR.Number))
	}
	slices.Sort(got)
	if want := []string{"home/homelab#42", "home/infra#17"}; !slices.Equal(got, want) {
		t.Errorf("members = %v, want %v", got, want)
	}
}
