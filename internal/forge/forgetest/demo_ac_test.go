package forgetest_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge/forgetest"
)

var demoNow = time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)

func demoRef(name string) domain.RepoRef { return domain.RepoRef{Owner: "home", Name: name} }

func TestNewDemoHost(t *testing.T) {
	want := forge.HostInfo{Kind: forge.KindForgejo, URL: "https://forge.home.arpa", Version: "16.0.5+gitea-1.22.0", User: "you", ChangeRequestTerm: "PR"}
	if got := forgetest.NewDemo(demoNow).Info(); got != want {
		t.Errorf("Info = %+v, want %+v", got, want)
	}
}

func TestNewDemoRepoActivityTracksNow(t *testing.T) {
	repos, err := forgetest.NewDemo(demoNow).ListRepos(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ages := map[string]time.Duration{"homelab": 4 * time.Minute, "infra": time.Hour, "lazyforge": 3 * time.Hour, "dotfiles": 48 * time.Hour}
	var names []string
	for _, r := range repos {
		names = append(names, r.Name)
		if r.Owner != "home" || r.Access != domain.AccessWrite || r.WebURL != "https://forge.home.arpa/home/"+r.Name {
			t.Errorf("%s = %+v", r.Name, r)
		}
		if want := demoNow.Add(-ages[r.Name]); !r.LastActivity.Equal(want) {
			t.Errorf("%s LastActivity = %v, want %v", r.Name, r.LastActivity, want)
		}
	}
	if want := []string{"homelab", "infra", "lazyforge", "dotfiles"}; !reflect.DeepEqual(names, want) {
		t.Errorf("repo order = %v, want %v", names, want)
	}
}

func TestNewDemoItemNumbersAndStatuses(t *testing.T) {
	ctx := context.Background()
	f := forgetest.NewDemo(demoNow)
	open := forge.Filter{State: domain.StateOpen}

	crs := map[string][]int{"homelab": {42, 41, 39}, "infra": {18, 17}, "lazyforge": {2, 3}, "dotfiles": nil}
	issues := map[string][]int{"homelab": {12, 30}, "infra": {3}, "lazyforge": {1}, "dotfiles": nil}
	runs := map[string][]int64{"homelab": {318, 317}, "infra": {91, 90}, "lazyforge": {12}, "dotfiles": nil}
	for name := range crs {
		var gotCR, gotIssue []int
		var gotRun []int64
		list, err := f.ListChangeRequests(ctx, demoRef(name), open)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range list {
			gotCR = append(gotCR, c.Number)
		}
		is, err := f.ListIssues(ctx, demoRef(name), open)
		if err != nil {
			t.Fatal(err)
		}
		for _, i := range is {
			gotIssue = append(gotIssue, i.Number)
		}
		rs, err := f.ListRuns(ctx, demoRef(name), forge.RunFilter{})
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range rs {
			gotRun = append(gotRun, r.ID)
		}
		if !reflect.DeepEqual(gotCR, crs[name]) || !reflect.DeepEqual(gotIssue, issues[name]) || !reflect.DeepEqual(gotRun, runs[name]) {
			t.Errorf("%s: CRs %v issues %v runs %v", name, gotCR, gotIssue, gotRun)
		}
	}

	cr, err := f.ListChangeRequests(ctx, demoRef("homelab"), open)
	if err != nil {
		t.Fatal(err)
	}
	if cr[0].CI != domain.CIPass || cr[0].Title != "chore(deps): update postgres docker tag to v17.0" || cr[1].CI != domain.CIFail {
		t.Errorf("homelab #42 = %v %q, #41 = %v", cr[0].CI, cr[0].Title, cr[1].CI)
	}

	status := map[string]map[int64]domain.CIState{
		"homelab":   {318: domain.CIPass, 317: domain.CIFail},
		"infra":     {91: domain.CIRunning, 90: domain.CIPass},
		"lazyforge": {12: domain.CIPass},
	}
	for name, want := range status {
		rs, err := f.ListRuns(ctx, demoRef(name), forge.RunFilter{})
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range rs {
			if got, ok := want[r.ID]; !ok || r.Status != got {
				t.Errorf("%s run %d status = %v, want %v (listed: %v)", name, r.ID, r.Status, got, ok)
			}
		}
	}
}

func TestNewDemoTimestampsRelativeToNow(t *testing.T) {
	ctx := context.Background()
	later := demoNow.Add(24 * time.Hour)
	a, err := forgetest.NewDemo(demoNow).ListChangeRequests(ctx, demoRef("homelab"), forge.Filter{State: domain.StateOpen})
	if err != nil {
		t.Fatal(err)
	}
	b, err := forgetest.NewDemo(later).ListChangeRequests(ctx, demoRef("homelab"), forge.Filter{State: domain.StateOpen})
	if err != nil {
		t.Fatal(err)
	}
	for i := range a {
		if got := b[i].UpdatedAt.Sub(a[i].UpdatedAt); got != 24*time.Hour {
			t.Errorf("#%d UpdatedAt shifted by %v, want 24h", a[i].Number, got)
		}
	}
}
