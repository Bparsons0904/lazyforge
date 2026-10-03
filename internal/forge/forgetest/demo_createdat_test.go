package forgetest_test

import (
	"context"
	"testing"
	"time"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge/forgetest"
)

func TestNewDemoEveryChangeRequestHasCreatedAt(t *testing.T) {
	ctx := context.Background()
	f := forgetest.NewDemo(demoNow)
	repos, err := f.ListRepos(ctx)
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	ages := map[string]bool{}
	for _, r := range repos {
		for _, st := range []domain.State{domain.StateOpen, domain.StateMerged, domain.StateClosed} {
			crs, err := f.ListChangeRequests(ctx, r.RepoRef, forge.Filter{State: st})
			if err != nil {
				t.Fatal(err)
			}
			for _, cr := range crs {
				seen++
				if cr.CreatedAt.IsZero() {
					t.Errorf("%s #%d CreatedAt is zero", r.RepoRef, cr.Number)
				}
				if cr.CreatedAt.After(cr.UpdatedAt) {
					t.Errorf("%s #%d CreatedAt %v is after UpdatedAt %v", r.RepoRef, cr.Number, cr.CreatedAt, cr.UpdatedAt)
				}
				if st == domain.StateOpen && cr.Author == "renovate" {
					ages[demoNow.Sub(cr.CreatedAt).String()] = true
				}
			}
		}
	}
	if seen == 0 {
		t.Fatal("no change requests seen")
	}
	if len(ages) < 3 {
		t.Errorf("open Renovate PR ages = %v, want varied ages so impact scores differ", ages)
	}
}

func TestNewDemoCreatedAtTracksNow(t *testing.T) {
	ctx := context.Background()
	open := forge.Filter{State: domain.StateOpen}
	a, _ := forgetest.NewDemo(demoNow).ListChangeRequests(ctx, demoRef("homelab"), open)
	b, _ := forgetest.NewDemo(demoNow.Add(24*time.Hour)).ListChangeRequests(ctx, demoRef("homelab"), open)
	for i := range a {
		if got := b[i].CreatedAt.Sub(a[i].CreatedAt).Hours(); got != 24 {
			t.Errorf("#%d CreatedAt shifted %vh, want 24h", a[i].Number, got)
		}
	}
}
