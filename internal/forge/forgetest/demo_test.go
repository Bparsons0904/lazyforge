package forgetest_test

import (
	"context"
	"io"
	"reflect"
	"testing"
	"time"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge/forgetest"
)

func TestNewDemo(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	f := forgetest.NewDemo(now)

	repos, err := f.ListRepos(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, r := range repos {
		names = append(names, r.Name)
	}
	if want := []string{"homelab", "infra", "lazyforge", "dotfiles"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("repos = %v, want %v", names, want)
	}

	want := map[string]struct{ crs, issues, runs int }{
		"homelab": {3, 2, 2}, "infra": {2, 1, 2}, "lazyforge": {2, 1, 1}, "dotfiles": {0, 0, 0},
	}
	for _, r := range repos {
		crs, _ := f.ListChangeRequests(ctx, r.RepoRef, forge.Filter{State: domain.StateOpen})
		issues, _ := f.ListIssues(ctx, r.RepoRef, forge.Filter{State: domain.StateOpen})
		runs, _ := f.ListRuns(ctx, r.RepoRef, forge.RunFilter{})
		if got := (struct{ crs, issues, runs int }{len(crs), len(issues), len(runs)}); got != want[r.Name] {
			t.Errorf("%s counts = %+v, want %+v", r.Name, got, want[r.Name])
		}
		for _, run := range runs {
			jobs, err := f.ListJobs(ctx, r.RepoRef, run.ID)
			if err != nil || len(jobs) == 0 {
				t.Fatalf("%s run %d jobs = %v, %v", r.Name, run.ID, jobs, err)
			}
			for _, j := range jobs {
				rc, err := f.JobLog(ctx, r.RepoRef, j.ID)
				if err != nil {
					t.Fatalf("job %d log: %v", j.ID, err)
				}
				if b, _ := io.ReadAll(rc); len(b) == 0 {
					t.Errorf("job %d has an empty log", j.ID)
				}
			}
		}
	}

	home := domain.RepoRef{Owner: "home", Name: "homelab"}
	crs, _ := f.ListChangeRequests(ctx, home, forge.Filter{State: domain.StateOpen})
	if crs[0].Number != 42 || crs[0].CI != domain.CIPass || crs[0].Title != "chore(deps): update postgres to v17.0" {
		t.Errorf("homelab #42 = %+v", crs[0])
	}
	if crs[1].Number != 41 || crs[1].CI != domain.CIFail {
		t.Errorf("homelab #41 = %+v", crs[1])
	}

	again, _ := forgetest.NewDemo(now).ListChangeRequests(ctx, home, forge.Filter{State: domain.StateOpen})
	if !reflect.DeepEqual(crs, again) {
		t.Error("NewDemo is not deterministic")
	}
}
